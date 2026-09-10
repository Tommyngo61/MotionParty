package enroll

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	proto "github.com/meterhome/meternode-proto"

	"github.com/meterhome/meternode-console/internal/model"
)

// fakeRepo is an in-memory Repo/Tx.
//
// It exists so the decisions this package makes — what happens to a node whose
// GPU was swapped, what happens when a token is replayed — are testable without
// a database. It is deliberately NOT transactional: InTx runs the function
// against live maps and rolls nothing back on error. Every test that cares
// about atomicity asserts on the real Postgres implementation instead
// (internal/store, build tag `integration`), because a fake that pretended to
// roll back would be testing the fake.
type fakeRepo struct {
	mu sync.Mutex

	tokens    map[uuid.UUID]*model.Token
	nodes     map[uuid.UUID]*model.Node
	hardware  map[uuid.UUID]*model.Hardware
	creds     map[uuid.UUID][]*model.Credential
	reattests map[uuid.UUID][]*model.Reattestation

	events []model.Event
	audits []model.AuditEntry

	// failOn makes a specific method return an error, to exercise the paths
	// where storage fails partway through enrollment.
	failOn map[string]error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		tokens:    map[uuid.UUID]*model.Token{},
		nodes:     map[uuid.UUID]*model.Node{},
		hardware:  map[uuid.UUID]*model.Hardware{},
		creds:     map[uuid.UUID][]*model.Credential{},
		reattests: map[uuid.UUID][]*model.Reattestation{},
		failOn:    map[string]error{},
	}
}

func (f *fakeRepo) fail(name string) error { return f.failOn[name] }

func (f *fakeRepo) InTx(ctx context.Context, fn func(Tx) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn(f)
}

func (f *fakeRepo) InsertToken(ctx context.Context, t *model.Token) error {
	if err := f.fail("InsertToken"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.tokens[t.ID] = &cp
	return nil
}

func (f *fakeRepo) ListTokens(ctx context.Context, filter TokenFilter) ([]model.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.Token
	for _, t := range f.tokens {
		if filter.SiteID != nil && (t.SiteID == nil || *t.SiteID != *filter.SiteID) {
			continue
		}
		if filter.OnlyOpen && !t.Usable(time.Now()) {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}

func (f *fakeRepo) RevokeToken(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[id]
	if !ok {
		return ErrTokenInvalid
	}
	t.RevokedAt, t.RevokedBy, t.RevokeReason = &at, by, reason
	return nil
}

// --- Tx --------------------------------------------------------------------

func (f *fakeRepo) TokenByHash(ctx context.Context, hash []byte) (*model.Token, error) {
	if err := f.fail("TokenByHash"); err != nil {
		return nil, err
	}
	for _, t := range f.tokens {
		if string(t.TokenHash) == string(hash) {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) MarkTokenConsumed(ctx context.Context, tokenID, nodeID uuid.UUID, at time.Time) error {
	if err := f.fail("MarkTokenConsumed"); err != nil {
		return err
	}
	t, ok := f.tokens[tokenID]
	if !ok {
		return ErrTokenInvalid
	}
	t.ConsumedAt, t.ConsumedByNode = &at, &nodeID
	return nil
}

func (f *fakeRepo) NodeByFingerprint(ctx context.Context, fp string) (*model.Node, error) {
	for nodeID, hw := range f.hardware {
		if hw.FingerprintHash == fp {
			n := f.nodes[nodeID]
			cp := *n
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) NodeByPartialHardware(ctx context.Context, fp proto.HardwareFingerprint) (*model.Node, error) {
	norm := func(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
	gpus := map[string]bool{}
	for _, g := range fp.GPUUUIDs {
		if g = norm(g); g != "" {
			gpus[g] = true
		}
	}
	for nodeID, hw := range f.hardware {
		if norm(hw.MotherboardUUID) != "" && norm(hw.MotherboardUUID) == norm(fp.MotherboardUUID) {
			return f.copyNode(nodeID), nil
		}
		if norm(hw.PrimaryNICMAC) != "" && norm(hw.PrimaryNICMAC) == norm(fp.PrimaryNICMAC) {
			return f.copyNode(nodeID), nil
		}
		for _, g := range hw.GPUUUIDs {
			if gpus[norm(g)] {
				return f.copyNode(nodeID), nil
			}
		}
	}
	return nil, nil
}

func (f *fakeRepo) copyNode(id uuid.UUID) *model.Node {
	n, ok := f.nodes[id]
	if !ok {
		return nil
	}
	cp := *n
	return &cp
}

func (f *fakeRepo) NodeByID(ctx context.Context, id uuid.UUID) (*model.Node, error) {
	n, ok := f.nodes[id]
	if !ok {
		return nil, nil
	}
	cp := *n
	return &cp, nil
}

func (f *fakeRepo) InsertNode(ctx context.Context, n *model.Node) error {
	if err := f.fail("InsertNode"); err != nil {
		return err
	}
	cp := *n
	f.nodes[n.ID] = &cp
	return nil
}

func (f *fakeRepo) MarkNodeEnrolled(ctx context.Context, id uuid.UUID, agentVersion string, at time.Time) error {
	n, ok := f.nodes[id]
	if !ok {
		return ErrBadRequest
	}
	n.Lifecycle, n.AgentVersion, n.EnrolledAt = model.LifecycleEnrolled, agentVersion, &at
	return nil
}

func (f *fakeRepo) QuarantineNode(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	n, ok := f.nodes[id]
	if !ok {
		return ErrBadRequest
	}
	n.Lifecycle, n.QuarantineReason = model.LifecycleQuarantined, reason
	return nil
}

func (f *fakeRepo) HardwareByNode(ctx context.Context, nodeID uuid.UUID) (*model.Hardware, error) {
	hw, ok := f.hardware[nodeID]
	if !ok {
		return nil, nil
	}
	cp := *hw
	return &cp, nil
}

func (f *fakeRepo) UpsertHardware(ctx context.Context, h *model.Hardware) error {
	if err := f.fail("UpsertHardware"); err != nil {
		return err
	}
	cp := *h
	f.hardware[h.NodeID] = &cp
	return nil
}

func (f *fakeRepo) ActiveCredential(ctx context.Context, nodeID uuid.UUID) (*model.Credential, error) {
	for _, c := range f.creds[nodeID] {
		if c.RevokedAt == nil {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) RevokeCredentials(ctx context.Context, nodeID uuid.UUID, by, reason string, at time.Time) error {
	for _, c := range f.creds[nodeID] {
		if c.RevokedAt == nil {
			c.RevokedAt, c.RevokedBy, c.RevokeReason = &at, by, reason
		}
	}
	return nil
}

func (f *fakeRepo) InsertCredential(ctx context.Context, c *model.Credential) error {
	if err := f.fail("InsertCredential"); err != nil {
		return err
	}
	cp := *c
	f.creds[c.NodeID] = append(f.creds[c.NodeID], &cp)
	return nil
}

func (f *fakeRepo) PendingReattestation(ctx context.Context, nodeID uuid.UUID) (*model.Reattestation, error) {
	for _, r := range f.reattests[nodeID] {
		if r.State == model.ReattestPending {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) InsertReattestation(ctx context.Context, r *model.Reattestation) error {
	cp := *r
	f.reattests[r.NodeID] = append(f.reattests[r.NodeID], &cp)
	return nil
}

func (f *fakeRepo) InsertEvent(ctx context.Context, e *model.Event) error {
	f.events = append(f.events, *e)
	return nil
}

func (f *fakeRepo) InsertAudit(ctx context.Context, a *model.AuditEntry) error {
	f.audits = append(f.audits, *a)
	return nil
}

// helpers used by the tests

func (f *fakeRepo) eventsWithCode(code string) []model.Event {
	var out []model.Event
	for _, e := range f.events {
		if e.Code == code {
			out = append(out, e)
		}
	}
	return out
}

func (f *fakeRepo) auditsWithAction(action string) []model.AuditEntry {
	var out []model.AuditEntry
	for _, a := range f.audits {
		if a.Action == action {
			out = append(out, a)
		}
	}
	return out
}

func (f *fakeRepo) activeCredCount(nodeID uuid.UUID) int {
	n := 0
	for _, c := range f.creds[nodeID] {
		if c.RevokedAt == nil {
			n++
		}
	}
	return n
}
