// Package enroll owns node identity: minting one-time enrollment tokens,
// exchanging them for per-node credentials, binding hardware fingerprints, and
// raising re-attestation when a bound fingerprint changes.
//
// Everything here is storage-agnostic. The service talks to a Repo, and the
// only thing that knows about Postgres is internal/store. That is not
// architecture for its own sake: the interesting decisions in this package are
// about what to do when a fingerprint does not match, and those need to be
// testable without a database.
package enroll

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	proto "github.com/meterhome/meternode-proto"

	"github.com/meterhome/meternode-console/internal/model"
)

// Errors returned by the service. Handlers map these onto HTTP status codes
// and stable API error codes; nothing else should string-match on them.
var (
	ErrTokenInvalid        = errors.New("enroll: token not recognised")
	ErrTokenExpired        = errors.New("enroll: token has expired")
	ErrTokenConsumed       = errors.New("enroll: token has already been used")
	ErrTokenRevoked        = errors.New("enroll: token has been revoked")
	ErrNodeRevoked         = errors.New("enroll: node credential has been revoked")
	ErrNodeQuarantined     = errors.New("enroll: node is quarantined")
	ErrFingerprintConflict = errors.New("enroll: fingerprint is already bound to a different node")
	ErrBadRequest          = errors.New("enroll: malformed request")
)

// ReattestationError is returned when an already-enrolled node presents a
// fingerprint that does not match the one bound to its identity. It carries
// enough for the handler to render a 409 the field tech can act on.
type ReattestationError struct {
	NodeID          uuid.UUID
	ReattestationID uuid.UUID
	BoundHash       string
	PresentedHash   string
	ChangedFields   []string
}

func (e *ReattestationError) Error() string {
	return fmt.Sprintf("enroll: node %s presented changed hardware (%s); re-attestation %s is pending operator review",
		e.NodeID, strings.Join(e.ChangedFields, ", "), e.ReattestationID)
}

// Tx is the transactional surface enrollment needs. Every method runs inside
// one database transaction, because enrollment is not a sequence of
// independently-valid steps: consuming a token, creating a node, binding
// hardware, and issuing a credential either all happen or none do. A partial
// enrollment leaves a machine in a homeowner's house with a token it has
// already spent and no identity, which needs a truck roll to fix.
type Tx interface {
	TokenByHash(ctx context.Context, hash []byte) (*model.Token, error)
	MarkTokenConsumed(ctx context.Context, tokenID, nodeID uuid.UUID, at time.Time) error

	NodeByFingerprint(ctx context.Context, fingerprintHash string) (*model.Node, error)
	// NodeByPartialHardware finds a node that shares a motherboard UUID, NIC
	// MAC, or GPU UUID with the presented fingerprint without matching it
	// exactly. This is what catches a swapped component on a node that has
	// lost its credential: without it, a re-imaged machine with a new GPU is
	// indistinguishable from a box that arrived from the warehouse.
	NodeByPartialHardware(ctx context.Context, fp proto.HardwareFingerprint) (*model.Node, error)
	NodeByID(ctx context.Context, id uuid.UUID) (*model.Node, error)
	InsertNode(ctx context.Context, n *model.Node) error
	MarkNodeEnrolled(ctx context.Context, id uuid.UUID, agentVersion string, at time.Time) error
	QuarantineNode(ctx context.Context, id uuid.UUID, reason string, at time.Time) error

	HardwareByNode(ctx context.Context, nodeID uuid.UUID) (*model.Hardware, error)
	UpsertHardware(ctx context.Context, h *model.Hardware) error

	ActiveCredential(ctx context.Context, nodeID uuid.UUID) (*model.Credential, error)
	RevokeCredentials(ctx context.Context, nodeID uuid.UUID, by, reason string, at time.Time) error
	InsertCredential(ctx context.Context, c *model.Credential) error

	PendingReattestation(ctx context.Context, nodeID uuid.UUID) (*model.Reattestation, error)
	InsertReattestation(ctx context.Context, r *model.Reattestation) error

	InsertEvent(ctx context.Context, e *model.Event) error
	InsertAudit(ctx context.Context, a *model.AuditEntry) error
}

// Repo is the storage the service needs outside a transaction.
type Repo interface {
	InTx(ctx context.Context, fn func(Tx) error) error

	InsertToken(ctx context.Context, t *model.Token) error
	ListTokens(ctx context.Context, f TokenFilter) ([]model.Token, error)
	RevokeToken(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) error
	InsertAudit(ctx context.Context, a *model.AuditEntry) error
}

// Signer produces the controller's signature over an issued credential.
// Implemented by internal/keys; an interface here so the service can be tested
// with a throwaway key and so a KMS-backed signer can be dropped in later
// without touching this package.
type Signer interface {
	KeyID() string
	Sign(message []byte) []byte
	PublicKeys() map[string][]byte
	// PublicKey resolves one key id, so a credential signed by a key that has
	// since been rotated out still verifies.
	PublicKey(keyID string) (ed25519.PublicKey, bool)
}

// Clock is injected so tests can drive expiry deterministically.
type Clock func() time.Time

// Options configure a Service.
type Options struct {
	TokenTTL      time.Duration
	CredentialTTL time.Duration // 0 = no expiry
	Hints         proto.AgentConfigHints
	Clock         Clock
}

// Service implements enrollment.
type Service struct {
	repo   Repo
	signer Signer
	opt    Options
}

// New builds a Service, filling in sane defaults for anything unset.
func New(repo Repo, signer Signer, opt Options) *Service {
	if opt.Clock == nil {
		opt.Clock = time.Now
	}
	if opt.TokenTTL <= 0 {
		opt.TokenTTL = 72 * time.Hour
	}
	if opt.Hints.SampleIntervalS == 0 {
		d := proto.DefaultConfigHints()
		opt.Hints.SampleIntervalS = d.SampleIntervalS
		opt.Hints.FlushIntervalS = d.FlushIntervalS
		opt.Hints.HeartbeatIntervalS = d.HeartbeatIntervalS
		opt.Hints.MonthlyBudgetMB = d.MonthlyBudgetMB
	}
	return &Service{repo: repo, signer: signer, opt: opt}
}

// --- Token minting -------------------------------------------------------

// tokenAlphabet is Crockford-ish base32: no padding, no I/L/O/U, so a field
// tech reading a token off a printed sheet in a dim utility room cannot
// confuse 0 with O or 1 with I. That is not a cosmetic concern — a mistyped
// token is a failed install and a second visit.
var tokenAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// TokenBytes is the entropy in a minted token. 20 bytes is 160 bits, which
// renders as 32 characters — long enough that guessing is not a threat model,
// short enough to type.
const TokenBytes = 20

// MintParams describe a token to mint.
type MintParams struct {
	Label        string
	SiteID       *uuid.UUID
	NodeNameHint string
	TTL          time.Duration // 0 = service default
	Actor        string
	ActorRole    string
	RequestID    string
	RemoteAddr   string
}

// MintResult carries the one and only copy of the raw token.
type MintResult struct {
	Token  model.Token
	Secret string // shown once, never stored, unrecoverable afterwards
}

// MintToken creates a single-use enrollment token.
//
// The raw secret is returned here and nowhere else. Only its SHA-256 is
// persisted, so a database dump, a support bundle, or a replicated backup
// cannot be turned into a fleet enrollment. Losing the printout costs one
// re-mint, which is a much cheaper failure than the alternative.
func (s *Service) MintToken(ctx context.Context, p MintParams) (*MintResult, error) {
	if p.Actor == "" {
		return nil, fmt.Errorf("%w: mint requires an actor", ErrBadRequest)
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = s.opt.TokenTTL
	}

	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("enroll: generate token: %w", err)
	}
	secret := tokenAlphabet.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))

	now := s.opt.Clock()
	tok := model.Token{
		ID:           uuid.New(),
		TokenHash:    sum[:],
		TokenPrefix:  secret[:8],
		Label:        p.Label,
		SiteID:       p.SiteID,
		NodeNameHint: p.NodeNameHint,
		CreatedBy:    p.Actor,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
	}
	if err := s.repo.InsertToken(ctx, &tok); err != nil {
		return nil, fmt.Errorf("enroll: persist token: %w", err)
	}

	// The audit row records the prefix, never the secret.
	_ = s.repo.InsertAudit(ctx, &model.AuditEntry{
		At: now, Actor: p.Actor, ActorRole: p.ActorRole,
		Action: model.ActionTokenMint, SiteID: p.SiteID, Target: tok.ID.String(),
		Result: "ok", RequestID: p.RequestID, RemoteAddr: p.RemoteAddr,
		Detail: map[string]string{
			"token_prefix": tok.TokenPrefix,
			"label":        p.Label,
			"expires_at":   tok.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})

	return &MintResult{Token: tok, Secret: secret}, nil
}

// TokenFilter narrows a token listing.
type TokenFilter struct {
	SiteID   *uuid.UUID
	OnlyOpen bool // unconsumed, unrevoked, unexpired
	Limit    int
}

// ListTokens returns token metadata. It can never return a secret, because one
// was never stored.
func (s *Service) ListTokens(ctx context.Context, f TokenFilter) ([]model.Token, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	return s.repo.ListTokens(ctx, f)
}

// RevokeToken cancels an unconsumed token — the "the install sheet went
// missing" path.
func (s *Service) RevokeToken(ctx context.Context, id uuid.UUID, actor, actorRole, reason, requestID, remoteAddr string) error {
	now := s.opt.Clock()
	if err := s.repo.RevokeToken(ctx, id, actor, reason, now); err != nil {
		return err
	}
	_ = s.repo.InsertAudit(ctx, &model.AuditEntry{
		At: now, Actor: actor, ActorRole: actorRole, Action: model.ActionTokenRevoke,
		Target: id.String(), Result: "ok", RequestID: requestID, RemoteAddr: remoteAddr,
		Detail: map[string]string{"reason": reason},
	})
	return nil
}

// --- Enrollment ----------------------------------------------------------

// Result is a successful enrollment.
type Result struct {
	NodeID     uuid.UUID
	SiteID     *uuid.UUID
	Credential string
	ReEnrolled bool
}

// Enroll exchanges a one-time token for a node credential.
//
// The whole operation runs in one transaction. The interesting part is not the
// happy path but the four ways a node can arrive here:
//
//  1. New hardware, valid token → create the node, bind the fingerprint, issue.
//  2. Known hardware, valid token, fingerprint matches → re-issue. This is the
//     normal recovery path: a re-imaged node, or one whose credential file was
//     lost. It revokes the old credential in the same transaction.
//  3. Known node, fingerprint CHANGED → refuse, and open a re-attestation for a
//     human. Never re-bind silently: the benign reading is a swapped GPU, and
//     the other reading is a provisioning image cloned onto a second machine.
//     Auto-binding turns the second case into two machines sharing one
//     identity, which corrupts every metric, alert, and command that identity
//     touches.
//  4. Known hardware bound to a DIFFERENT node → refuse outright. This is the
//     clone case seen from the other side.
func (s *Service) Enroll(ctx context.Context, req *proto.EnrollRequest, remoteAddr string) (*Result, error) {
	if req == nil || req.Token == "" {
		return nil, fmt.Errorf("%w: token is required", ErrBadRequest)
	}
	if len(req.NodePub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: node_pub must be a %d-byte ed25519 public key", ErrBadRequest, ed25519.PublicKeySize)
	}
	fp := req.Fingerprint
	if fp.Hash() == "" || (len(fp.GPUUUIDs) == 0 && fp.MotherboardUUID == "" && fp.PrimaryNICMAC == "") {
		return nil, fmt.Errorf("%w: fingerprint is empty", ErrBadRequest)
	}

	now := s.opt.Clock()
	presented := fp.Hash()
	sum := sha256.Sum256([]byte(req.Token))
	tokenHash := sum[:]

	var out Result
	err := s.repo.InTx(ctx, func(tx Tx) error {
		tok, err := tx.TokenByHash(ctx, tokenHash)
		if err != nil {
			return err
		}
		if tok == nil {
			return ErrTokenInvalid
		}
		switch {
		case tok.RevokedAt != nil:
			return ErrTokenRevoked
		case tok.ConsumedAt != nil:
			return ErrTokenConsumed
		case !now.Before(tok.ExpiresAt):
			return ErrTokenExpired
		}

		existing, err := s.identify(ctx, tx, req, presented)
		if err != nil {
			return err
		}

		var node *model.Node
		reEnrolled := false

		if existing != nil {
			// A node already waiting on a human gets the specific, actionable
			// answer on every retry rather than the generic quarantine error.
			// It will retry with backoff forever, and "node is quarantined"
			// tells a field tech standing in a utility room nothing about what
			// to do next.
			if pending, err := tx.PendingReattestation(ctx, existing.ID); err != nil {
				return err
			} else if pending != nil {
				return &ReattestationError{
					NodeID: existing.ID, ReattestationID: pending.ID,
					BoundHash: pending.BoundHash, PresentedHash: pending.PresentedHash,
					ChangedFields: pending.ChangedFields,
				}
			}

			// Cases 2, 3, and 4: this machine is already known to us.
			switch existing.Lifecycle {
			case model.LifecycleRetired:
				return fmt.Errorf("%w: node %s is retired", ErrNodeRevoked, existing.ID)
			case model.LifecycleQuarantined:
				return fmt.Errorf("%w: %s", ErrNodeQuarantined, existing.QuarantineReason)
			}
			node = existing
			reEnrolled = true
		} else {
			node = &model.Node{
				ID:        uuid.New(),
				SiteID:    tok.SiteID,
				Name:      nodeName(tok, req),
				Lifecycle: model.LifecycleUnenrolled,
				CreatedAt: now,
			}
			if err := tx.InsertNode(ctx, node); err != nil {
				return err
			}
		}

		// Case 3: the node exists and its bound fingerprint disagrees with
		// what was just presented.
		bound, err := tx.HardwareByNode(ctx, node.ID)
		if err != nil {
			return err
		}
		if bound != nil && bound.FingerprintHash != presented {
			return s.openReattestation(ctx, tx, node, bound, fp, presented, remoteAddr, now)
		}

		// Bind (or re-affirm) the hardware.
		hw := &model.Hardware{
			NodeID:              node.ID,
			FingerprintHash:     presented,
			MotherboardUUID:     fp.MotherboardUUID,
			GPUUUIDs:            fp.GPUUUIDs,
			PrimaryNICMAC:       fp.PrimaryNICMAC,
			FingerprintComplete: fp.Complete(),
			Inventory:           req.Hardware,
			BoundAt:             now,
		}
		if err := tx.UpsertHardware(ctx, hw); err != nil {
			return err
		}

		// Re-enrollment revokes whatever credential the node held. Two live
		// credentials for one node would mean a revoked machine could keep
		// reporting under its old identity.
		if reEnrolled {
			if err := tx.RevokeCredentials(ctx, node.ID, "system", "superseded by re-enrollment", now); err != nil {
				return err
			}
		}

		cred := &proto.Credential{
			V:               1,
			NodeID:          node.ID.String(),
			NodePub:         req.NodePub,
			FingerprintHash: presented,
			IssuedAt:        now.UnixMilli(),
			KeyID:           s.signer.KeyID(),
		}
		if node.SiteID != nil {
			cred.SiteID = node.SiteID.String()
		}
		if s.opt.CredentialTTL > 0 {
			cred.ExpiresAt = now.Add(s.opt.CredentialTTL).UnixMilli()
		}
		encoded, err := encodeCredential(cred, s.signer)
		if err != nil {
			return err
		}

		credSum := sha256.Sum256([]byte(encoded))
		rec := &model.Credential{
			ID: uuid.New(), NodeID: node.ID, KeyID: s.signer.KeyID(),
			NodePub: req.NodePub, FingerprintHash: presented,
			CredentialSHA: credSum[:], IssuedAt: now,
		}
		if s.opt.CredentialTTL > 0 {
			exp := now.Add(s.opt.CredentialTTL)
			rec.ExpiresAt = &exp
		}
		if err := tx.InsertCredential(ctx, rec); err != nil {
			return err
		}

		if err := tx.MarkNodeEnrolled(ctx, node.ID, req.AgentVersion, now); err != nil {
			return err
		}
		if err := tx.MarkTokenConsumed(ctx, tok.ID, node.ID, now); err != nil {
			return err
		}

		action := model.ActionNodeEnroll
		if reEnrolled {
			action = model.ActionNodeReEnroll
		}
		if err := tx.InsertEvent(ctx, &model.Event{
			NodeID: node.ID, T: now, Severity: proto.SeverityInfo.String(),
			Code: proto.CodeEnrollCompleted, Source: "controller",
			Message: "node enrolled",
			Detail: map[string]string{
				"agent_version":        req.AgentVersion,
				"fingerprint_complete": fmt.Sprint(fp.Complete()),
				"re_enrolled":          fmt.Sprint(reEnrolled),
			},
		}); err != nil {
			return err
		}
		nodeID := node.ID
		if err := tx.InsertAudit(ctx, &model.AuditEntry{
			At: now, Actor: "node:" + node.ID.String(), ActorRole: "node",
			Action: action, NodeID: &nodeID, SiteID: node.SiteID,
			Target: tok.ID.String(), Result: "ok", RemoteAddr: remoteAddr,
			Detail: map[string]string{
				"token_prefix":  tok.TokenPrefix,
				"agent_version": req.AgentVersion,
				"fingerprint":   presented,
			},
		}); err != nil {
			return err
		}

		out = Result{NodeID: node.ID, SiteID: node.SiteID, Credential: encoded, ReEnrolled: reEnrolled}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// identify works out which known node, if any, is presenting itself.
//
// The order is by descending confidence, and it matters:
//
//  1. A prior credential the controller itself signed. This is the strongest
//     claim available and the only one that survives a hardware change, which
//     is precisely the case we need to catch.
//  2. An exact fingerprint match. Nothing changed; this is a plain re-enroll.
//  3. A partial hardware match — same motherboard, same NIC, or a shared GPU.
//     This catches a re-imaged node that lost its credential AND had a
//     component swapped, which would otherwise enrol as a second identity for
//     one physical machine.
//
// A nil node with a nil error means "genuinely new hardware".
func (s *Service) identify(ctx context.Context, tx Tx, req *proto.EnrollRequest, presented string) (*model.Node, error) {
	if req.PriorCredential != "" {
		cred, sig, err := proto.ParseCredential(req.PriorCredential)
		// A malformed or unverifiable prior credential is NOT an error. An
		// agent may present a credential signed by a controller key we have
		// since rotated out and forgotten, and refusing the enrollment would
		// strand a machine in someone's house. Fall through to the weaker
		// identifiers instead.
		if err == nil {
			if pub, ok := s.signer.PublicKey(cred.KeyID); ok && cred.Verify(pub, sig, s.opt.Clock()) == nil {
				if id, err := uuid.Parse(cred.NodeID); err == nil {
					if n, err := tx.NodeByID(ctx, id); err != nil {
						return nil, err
					} else if n != nil {
						return n, nil
					}
				}
			}
		}
	}

	if n, err := tx.NodeByFingerprint(ctx, presented); err != nil {
		return nil, err
	} else if n != nil {
		return n, nil
	}

	return tx.NodeByPartialHardware(ctx, req.Fingerprint)
}

// openReattestation records the mismatch, quarantines the node, and returns a
// ReattestationError. The node is quarantined rather than merely refused
// because until a human decides, we do not know whether the machine still
// reporting under this identity is the one we think it is — and a quarantined
// node must not start workloads.
func (s *Service) openReattestation(ctx context.Context, tx Tx, node *model.Node, bound *model.Hardware,
	fp proto.HardwareFingerprint, presented, remoteAddr string, now time.Time) error {

	if pending, err := tx.PendingReattestation(ctx, node.ID); err != nil {
		return err
	} else if pending != nil {
		// Do not stack a new record per retry. A node that cannot enroll will
		// retry with backoff forever, and one row per attempt would bury the
		// operator queue.
		return &ReattestationError{
			NodeID: node.ID, ReattestationID: pending.ID,
			BoundHash: pending.BoundHash, PresentedHash: pending.PresentedHash,
			ChangedFields: pending.ChangedFields,
		}
	}

	boundFP := proto.HardwareFingerprint{
		MotherboardUUID: bound.MotherboardUUID,
		GPUUUIDs:        bound.GPUUUIDs,
		PrimaryNICMAC:   bound.PrimaryNICMAC,
	}
	changed := boundFP.Diff(fp)

	ra := &model.Reattestation{
		ID: uuid.New(), NodeID: node.ID, State: model.ReattestPending,
		BoundHash: bound.FingerprintHash, PresentedHash: presented,
		ChangedFields: changed, PresentedFromIP: remoteAddr, CreatedAt: now,
	}
	if err := tx.InsertReattestation(ctx, ra); err != nil {
		return err
	}
	reason := "hardware fingerprint changed: " + strings.Join(changed, ", ")
	if err := tx.QuarantineNode(ctx, node.ID, reason, now); err != nil {
		return err
	}
	if err := tx.InsertEvent(ctx, &model.Event{
		NodeID: node.ID, T: now, Severity: proto.SeverityCritical.String(),
		Code: proto.CodeFingerprintChanged, Source: "controller", Message: reason,
		Detail: map[string]string{
			"bound_hash":     bound.FingerprintHash,
			"presented_hash": presented,
			"changed_fields": strings.Join(changed, ","),
			"remote_addr":    remoteAddr,
		},
	}); err != nil {
		return err
	}
	nodeID := node.ID
	if err := tx.InsertAudit(ctx, &model.AuditEntry{
		At: now, Actor: "system", ActorRole: "system", Action: model.ActionReattestOpen,
		NodeID: &nodeID, Target: ra.ID.String(), Result: "denied", RemoteAddr: remoteAddr,
		Detail: map[string]string{"changed_fields": strings.Join(changed, ",")},
	}); err != nil {
		return err
	}

	return &ReattestationError{
		NodeID: node.ID, ReattestationID: ra.ID,
		BoundHash: bound.FingerprintHash, PresentedHash: presented, ChangedFields: changed,
	}
}

// ConfigHints returns the operating envelope handed to a node at enrollment.
func (s *Service) ConfigHints() proto.AgentConfigHints { return s.opt.Hints }

// ControllerKeys returns the command-signing public keys an agent pins.
func (s *Service) ControllerKeys() map[string][]byte { return s.signer.PublicKeys() }

func nodeName(tok *model.Token, req *proto.EnrollRequest) string {
	if tok.NodeNameHint != "" {
		return tok.NodeNameHint
	}
	if req.Hardware.Hostname != "" {
		return req.Hardware.Hostname
	}
	return "node-" + tok.TokenPrefix
}

// encodeCredential keeps the Signer interface narrow: a KMS-backed signer will
// sign a message but will never hand over an ed25519.PrivateKey.
func encodeCredential(c *proto.Credential, s Signer) (string, error) {
	encoded, err := c.EncodeWith(s.Sign)
	if err != nil {
		return "", fmt.Errorf("enroll: %w", err)
	}
	return encoded, nil
}
