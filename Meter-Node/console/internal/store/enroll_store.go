package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MeterHome/Meter-Node/console/internal/enroll"
	"github.com/MeterHome/Meter-Node/console/internal/model"
)

// EnrollRepo implements enroll.Repo against PostgreSQL.
type EnrollRepo struct{ s *Store }

// NewEnrollRepo builds the repository.
func NewEnrollRepo(s *Store) *EnrollRepo { return &EnrollRepo{s: s} }

var _ enroll.Repo = (*EnrollRepo)(nil)

// InTx runs an enrollment in one transaction.
//
// Enrollment is not a sequence of independently-valid steps: consuming a token,
// creating a node, binding hardware, and issuing a credential either all happen
// or none do. A partial enrollment strands a machine in a homeowner's house
// with a spent token and no identity, and fixing that needs a truck roll.
func (r *EnrollRepo) InTx(ctx context.Context, fn func(enroll.Tx) error) error {
	return r.s.inTx(ctx, func(tx pgx.Tx) error { return fn(&enrollTx{tx: tx}) })
}

func (r *EnrollRepo) InsertToken(ctx context.Context, t *model.Token) error {
	_, err := r.s.pool.Exec(ctx, `
		INSERT INTO enrollment_tokens
			(id, token_hash, token_prefix, label, site_id, node_name_hint, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.TokenHash, t.TokenPrefix, nullStr(t.Label), t.SiteID,
		nullStr(t.NodeNameHint), t.CreatedBy, t.CreatedAt, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store: insert enrollment token: %w", err)
	}
	return nil
}

func (r *EnrollRepo) ListTokens(ctx context.Context, f enroll.TokenFilter) ([]model.Token, error) {
	// The filter is applied in SQL rather than in Go so a fleet with thousands
	// of historical tokens does not stream all of them into the controller to
	// throw most away.
	q := `
		SELECT id, token_hash, token_prefix, coalesce(label, ''), site_id,
		       coalesce(node_name_hint, ''), created_by, created_at, expires_at,
		       consumed_at, consumed_by_node, revoked_at, coalesce(revoked_by, ''),
		       coalesce(revoke_reason, '')
		FROM enrollment_tokens
		WHERE ($1::uuid IS NULL OR site_id = $1)
		  AND (NOT $2::boolean OR (consumed_at IS NULL AND revoked_at IS NULL AND expires_at > now()))
		ORDER BY created_at DESC
		LIMIT $3`

	rows, err := r.s.pool.Query(ctx, q, f.SiteID, f.OnlyOpen, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("store: list enrollment tokens: %w", err)
	}
	defer rows.Close()

	var out []model.Token
	for rows.Next() {
		var t model.Token
		if err := rows.Scan(&t.ID, &t.TokenHash, &t.TokenPrefix, &t.Label, &t.SiteID,
			&t.NodeNameHint, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt,
			&t.ConsumedAt, &t.ConsumedByNode, &t.RevokedAt, &t.RevokedBy, &t.RevokeReason); err != nil {
			return nil, fmt.Errorf("store: scan enrollment token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *EnrollRepo) RevokeToken(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) error {
	// Revoking an already-consumed token is a no-op rather than an error: it
	// changes nothing, and an operator tidying up a list should not have to
	// care which of the tokens in it were already used.
	tag, err := r.s.pool.Exec(ctx, `
		UPDATE enrollment_tokens
		SET revoked_at = $2, revoked_by = $3, revoke_reason = $4
		WHERE id = $1 AND revoked_at IS NULL`,
		id, at, by, nullStr(reason))
	if err != nil {
		return fmt.Errorf("store: revoke enrollment token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either it does not exist or it was already revoked. Distinguish, so
		// the API can 404 correctly.
		var exists bool
		if err := r.s.pool.QueryRow(ctx, `SELECT true FROM enrollment_tokens WHERE id = $1`, id).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return enroll.ErrTokenInvalid
			}
			return fmt.Errorf("store: check enrollment token: %w", err)
		}
	}
	return nil
}

func (r *EnrollRepo) InsertAudit(ctx context.Context, a *model.AuditEntry) error {
	return insertAudit(ctx, r.s.pool, a)
}

// --- transactional surface -------------------------------------------------

type enrollTx struct{ tx pgx.Tx }

var _ enroll.Tx = (*enrollTx)(nil)

func (t *enrollTx) TokenByHash(ctx context.Context, hash []byte) (*model.Token, error) {
	// FOR UPDATE is what makes a token genuinely single-use. Two agents
	// enrolling with the same token at the same instant — which happens when a
	// field tech retries an install that looked like it timed out — would
	// otherwise both read it as unconsumed and both get a credential.
	var tok model.Token
	err := t.tx.QueryRow(ctx, `
		SELECT id, token_hash, token_prefix, coalesce(label, ''), site_id,
		       coalesce(node_name_hint, ''), created_by, created_at, expires_at,
		       consumed_at, consumed_by_node, revoked_at, coalesce(revoked_by, ''),
		       coalesce(revoke_reason, '')
		FROM enrollment_tokens WHERE token_hash = $1 FOR UPDATE`, hash).
		Scan(&tok.ID, &tok.TokenHash, &tok.TokenPrefix, &tok.Label, &tok.SiteID,
			&tok.NodeNameHint, &tok.CreatedBy, &tok.CreatedAt, &tok.ExpiresAt,
			&tok.ConsumedAt, &tok.ConsumedByNode, &tok.RevokedAt, &tok.RevokedBy, &tok.RevokeReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load enrollment token: %w", err)
	}
	return &tok, nil
}

func (t *enrollTx) MarkTokenConsumed(ctx context.Context, tokenID, nodeID uuid.UUID, at time.Time) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE enrollment_tokens
		SET consumed_at = $2, consumed_by_node = $3
		WHERE id = $1 AND consumed_at IS NULL`, tokenID, at, nodeID)
	if err != nil {
		return fmt.Errorf("store: consume enrollment token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Lost the race despite FOR UPDATE (a different transaction isolation,
		// a future refactor). Fail the enrollment rather than issue a second
		// credential on one token.
		return enroll.ErrTokenConsumed
	}
	return nil
}

const nodeColumns = `id, site_id, name, lifecycle, coalesce(agent_version, ''),
	enrolled_at, last_seen_at, last_seq, coalesce(quarantine_reason, ''), created_at`

func scanNode(row pgx.Row) (*model.Node, error) {
	var n model.Node
	err := row.Scan(&n.ID, &n.SiteID, &n.Name, &n.Lifecycle, &n.AgentVersion,
		&n.EnrolledAt, &n.LastSeenAt, &n.LastSeq, &n.QuarantineReason, &n.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan node: %w", err)
	}
	return &n, nil
}

func (t *enrollTx) NodeByFingerprint(ctx context.Context, fp string) (*model.Node, error) {
	return scanNode(t.tx.QueryRow(ctx, `
		SELECT `+nodeColumns+` FROM nodes n
		WHERE EXISTS (SELECT 1 FROM node_hardware h WHERE h.node_id = n.id AND h.fingerprint_hash = $1)
		FOR UPDATE`, fp))
}

// NodeByPartialHardware finds a node that shares a component with the presented
// fingerprint without matching it exactly.
//
// This is what catches a re-imaged node that also had a part swapped: it has no
// credential to identify itself with and its fingerprint no longer matches, so
// without this it would enrol as a SECOND identity for one physical machine —
// splitting that box's metrics, alerts, and commands across two node records.
//
// Motherboard and NIC are matched first because they are the strongest signals.
// A shared GPU UUID is weaker (a card really can be moved between machines),
// but that case is exactly the one that deserves a human look, so matching on
// it and raising re-attestation is the right outcome either way.
func (t *enrollTx) NodeByPartialHardware(ctx context.Context, fp proto.HardwareFingerprint) (*model.Node, error) {
	gpus := fp.GPUUUIDs
	if gpus == nil {
		gpus = []string{}
	}
	return scanNode(t.tx.QueryRow(ctx, `
		SELECT `+nodeColumns+` FROM nodes n
		JOIN node_hardware h ON h.node_id = n.id
		WHERE (nullif($1, '') IS NOT NULL AND lower(h.motherboard_uuid) = lower($1))
		   OR (nullif($2, '') IS NOT NULL AND lower(h.primary_nic_mac)  = lower($2))
		   OR (cardinality($3::text[]) > 0 AND h.gpu_uuids && $3::text[])
		ORDER BY
			CASE WHEN lower(h.motherboard_uuid) = lower($1) THEN 0
			     WHEN lower(h.primary_nic_mac)  = lower($2) THEN 1
			     ELSE 2 END
		LIMIT 1
		FOR UPDATE OF n`,
		fp.MotherboardUUID, fp.PrimaryNICMAC, gpus))
}

func (t *enrollTx) NodeByID(ctx context.Context, id uuid.UUID) (*model.Node, error) {
	return scanNode(t.tx.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1 FOR UPDATE`, id))
}

func (t *enrollTx) InsertNode(ctx context.Context, n *model.Node) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO nodes (id, site_id, name, lifecycle, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)`,
		n.ID, n.SiteID, n.Name, string(n.Lifecycle), n.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: insert node: %w", err)
	}
	return nil
}

func (t *enrollTx) MarkNodeEnrolled(ctx context.Context, id uuid.UUID, agentVersion string, at time.Time) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE nodes
		SET lifecycle = 'enrolled', agent_version = $2, enrolled_at = coalesce(enrolled_at, $3),
		    quarantine_reason = NULL, updated_at = $3
		WHERE id = $1`, id, nullStr(agentVersion), at)
	if err != nil {
		return fmt.Errorf("store: mark node enrolled: %w", err)
	}
	return nil
}

func (t *enrollTx) QuarantineNode(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE nodes SET lifecycle = 'quarantined', quarantine_reason = $2, updated_at = $3
		WHERE id = $1`, id, reason, at)
	if err != nil {
		return fmt.Errorf("store: quarantine node: %w", err)
	}
	return nil
}

func (t *enrollTx) HardwareByNode(ctx context.Context, nodeID uuid.UUID) (*model.Hardware, error) {
	var h model.Hardware
	var inventory []byte
	err := t.tx.QueryRow(ctx, `
		SELECT node_id, fingerprint_hash, coalesce(motherboard_uuid, ''), gpu_uuids,
		       coalesce(primary_nic_mac, ''), fingerprint_complete, inventory, bound_at
		FROM node_hardware WHERE node_id = $1`, nodeID).
		Scan(&h.NodeID, &h.FingerprintHash, &h.MotherboardUUID, &h.GPUUUIDs,
			&h.PrimaryNICMAC, &h.FingerprintComplete, &inventory, &h.BoundAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load node hardware: %w", err)
	}
	if len(inventory) > 0 {
		// A malformed inventory blob must not fail an enrollment. It is
		// descriptive data for the UI, not something identity depends on.
		_ = json.Unmarshal(inventory, &h.Inventory)
	}
	return &h, nil
}

func (t *enrollTx) UpsertHardware(ctx context.Context, h *model.Hardware) error {
	inventory, err := json.Marshal(h.Inventory)
	if err != nil {
		return fmt.Errorf("store: encode hardware inventory: %w", err)
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO node_hardware
			(node_id, fingerprint_hash, motherboard_uuid, gpu_uuids, primary_nic_mac,
			 fingerprint_complete, inventory, bound_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		ON CONFLICT (node_id) DO UPDATE SET
			fingerprint_hash = EXCLUDED.fingerprint_hash,
			motherboard_uuid = EXCLUDED.motherboard_uuid,
			gpu_uuids = EXCLUDED.gpu_uuids,
			primary_nic_mac = EXCLUDED.primary_nic_mac,
			fingerprint_complete = EXCLUDED.fingerprint_complete,
			inventory = EXCLUDED.inventory,
			updated_at = EXCLUDED.updated_at`,
		h.NodeID, h.FingerprintHash, nullStr(h.MotherboardUUID), h.GPUUUIDs,
		nullStr(h.PrimaryNICMAC), h.FingerprintComplete, inventory, h.BoundAt)
	if err != nil {
		return fmt.Errorf("store: upsert node hardware: %w", err)
	}
	return nil
}

func (t *enrollTx) ActiveCredential(ctx context.Context, nodeID uuid.UUID) (*model.Credential, error) {
	var c model.Credential
	err := t.tx.QueryRow(ctx, `
		SELECT id, node_id, key_id, node_pub, fingerprint_hash, credential_sha256,
		       issued_at, expires_at, revoked_at, coalesce(revoked_by, ''), coalesce(revoke_reason, '')
		FROM node_credentials WHERE node_id = $1 AND revoked_at IS NULL`, nodeID).
		Scan(&c.ID, &c.NodeID, &c.KeyID, &c.NodePub, &c.FingerprintHash, &c.CredentialSHA,
			&c.IssuedAt, &c.ExpiresAt, &c.RevokedAt, &c.RevokedBy, &c.RevokeReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load node credential: %w", err)
	}
	return &c, nil
}

func (t *enrollTx) RevokeCredentials(ctx context.Context, nodeID uuid.UUID, by, reason string, at time.Time) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE node_credentials
		SET revoked_at = $2, revoked_by = $3, revoke_reason = $4
		WHERE node_id = $1 AND revoked_at IS NULL`, nodeID, at, by, nullStr(reason))
	if err != nil {
		return fmt.Errorf("store: revoke node credentials: %w", err)
	}
	return nil
}

func (t *enrollTx) InsertCredential(ctx context.Context, c *model.Credential) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO node_credentials
			(id, node_id, key_id, node_pub, fingerprint_hash, credential_sha256, issued_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		c.ID, c.NodeID, c.KeyID, c.NodePub, c.FingerprintHash, c.CredentialSHA, c.IssuedAt, c.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store: insert node credential: %w", err)
	}
	return nil
}

func (t *enrollTx) PendingReattestation(ctx context.Context, nodeID uuid.UUID) (*model.Reattestation, error) {
	var r model.Reattestation
	var inventory []byte
	var ip *netip.Addr
	err := t.tx.QueryRow(ctx, `
		SELECT id, node_id, state, bound_hash, presented_hash, changed_fields,
		       presented_inventory, presented_from_ip, created_at
		FROM node_reattestations
		WHERE node_id = $1 AND state = 'pending'
		ORDER BY created_at DESC LIMIT 1`, nodeID).
		Scan(&r.ID, &r.NodeID, &r.State, &r.BoundHash, &r.PresentedHash, &r.ChangedFields,
			&inventory, &ip, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: load pending re-attestation: %w", err)
	}
	if len(inventory) > 0 {
		_ = json.Unmarshal(inventory, &r.PresentedInventory)
	}
	if ip != nil {
		r.PresentedFromIP = ip.String()
	}
	return &r, nil
}

func (t *enrollTx) InsertReattestation(ctx context.Context, r *model.Reattestation) error {
	inventory, err := json.Marshal(r.PresentedInventory)
	if err != nil {
		return fmt.Errorf("store: encode presented inventory: %w", err)
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO node_reattestations
			(id, node_id, state, bound_hash, presented_hash, changed_fields,
			 presented_inventory, presented_from_ip, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		r.ID, r.NodeID, string(r.State), r.BoundHash, r.PresentedHash, r.ChangedFields,
		inventory, nullIP(r.PresentedFromIP), r.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: insert re-attestation: %w", err)
	}
	return nil
}

func (t *enrollTx) InsertEvent(ctx context.Context, e *model.Event) error {
	detail, err := json.Marshal(orEmptyMap(e.Detail))
	if err != nil {
		return fmt.Errorf("store: encode event detail: %w", err)
	}
	source := e.Source
	if source == "" {
		source = "agent"
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO events (node_id, t, severity, code, message, detail, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.NodeID, e.T, e.Severity, e.Code, e.Message, detail, source)
	if err != nil {
		return fmt.Errorf("store: insert event: %w", err)
	}
	return nil
}

func (t *enrollTx) InsertAudit(ctx context.Context, a *model.AuditEntry) error {
	return insertAudit(ctx, t.tx, a)
}

// --- shared helpers --------------------------------------------------------

// execer is satisfied by both a pgxpool.Pool and a pgx.Tx, so an audit row can
// be written from inside an enrollment transaction or on its own without two
// copies of the query.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func insertAudit(ctx context.Context, q execer, a *model.AuditEntry) error {
	detail, err := json.Marshal(orEmptyMap(a.Detail))
	if err != nil {
		return fmt.Errorf("store: encode audit detail: %w", err)
	}
	at := a.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err = q.Exec(ctx, `
		INSERT INTO audit_log
			(at, actor, actor_role, action, node_id, site_id, command_id, target,
			 result, detail, request_id, remote_addr)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		at, a.Actor, nullStr(a.ActorRole), a.Action, a.NodeID, a.SiteID, a.CommandID,
		nullStr(a.Target), a.Result, detail, nullStr(a.RequestID), nullIP(a.RemoteAddr))
	if err != nil {
		return fmt.Errorf("store: insert audit entry: %w", err)
	}
	return nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// nullIP parses a client address for the inet column. A value that will not
// parse (an empty RemoteAddr in a test, a proxy sending something odd) becomes
// NULL rather than failing the write: losing the IP on an audit row is a much
// smaller problem than losing the audit row.
func nullIP(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return &addr
}

func orEmptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
