package enroll

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"

	"github.com/MeterHome/Meter-Node/console/internal/keys"
	"github.com/MeterHome/Meter-Node/console/internal/model"
)

type fixture struct {
	svc     *Service
	repo    *fakeRepo
	signer  *keys.Signer
	now     time.Time
	nodePub ed25519.PublicKey
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := newFakeRepo()
	signer, err := keys.Generate("ck-test")
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{repo: repo, signer: signer, now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)}
	f.svc = New(repo, signer, Options{
		TokenTTL: 72 * time.Hour,
		Clock:    func() time.Time { return f.now },
	})
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.nodePub = pub
	return f
}

func (f *fixture) mint(t *testing.T) string {
	t.Helper()
	res, err := f.svc.MintToken(context.Background(), MintParams{
		Label: "Ridgefield install", Actor: "tech@example.com", ActorRole: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.Secret
}

func fingerprint() proto.HardwareFingerprint {
	return proto.HardwareFingerprint{
		MotherboardUUID: "4C4C4544-0037-5A10-8054-B4C04F503733",
		GPUUUIDs:        []string{"GPU-aaaa1111-2222-3333-4444-555566667777"},
		PrimaryNICMAC:   "a4:bb:6d:11:22:33",
	}
}

func (f *fixture) request(token string, fp proto.HardwareFingerprint) *proto.EnrollRequest {
	return &proto.EnrollRequest{
		Token:        token,
		NodePub:      f.nodePub,
		Fingerprint:  fp,
		AgentVersion: "1.0.0",
		Hardware: proto.HardwareInventory{
			Hostname: "mn-ridgefield-01", CPUModel: "AMD Ryzen 9 7950X", CPUCores: 16,
			MemTotalMB: 65536, Kernel: "6.8.0-45-generic", OS: "Ubuntu 24.04.1 LTS",
			GPUs: []proto.GPUInventory{{Index: 0, UUID: fp.GPUUUIDs[0], Name: "NVIDIA RTX PRO 6000", MemTotalMB: 98304}},
		},
		SentAtMS: f.now.UnixMilli(),
	}
}

func TestEnrollHappyPath(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	secret := f.mint(t)

	res, err := f.svc.Enroll(ctx, f.request(secret, fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if res.ReEnrolled {
		t.Error("a first enrollment is not a re-enrollment")
	}

	// The credential must verify against the controller's published key — this
	// is exactly what the agent will do.
	cred, sig, err := proto.ParseCredential(res.Credential)
	if err != nil {
		t.Fatalf("parse credential: %v", err)
	}
	pub, ok := f.signer.PublicKey(cred.KeyID)
	if !ok {
		t.Fatalf("credential names an unpublished key %q", cred.KeyID)
	}
	if err := cred.Verify(pub, sig, f.now); err != nil {
		t.Fatalf("issued credential must verify: %v", err)
	}
	if cred.NodeID != res.NodeID.String() {
		t.Errorf("credential node id %q != %q", cred.NodeID, res.NodeID)
	}
	if string(cred.NodePub) != string(f.nodePub) {
		t.Error("credential must bind the key the agent generated")
	}
	if cred.FingerprintHash != fingerprint().Hash() {
		t.Error("credential must bind the presented fingerprint")
	}

	node := f.repo.nodes[res.NodeID]
	if node.Lifecycle != model.LifecycleEnrolled {
		t.Errorf("node lifecycle = %q, want enrolled", node.Lifecycle)
	}
	if node.Name != "mn-ridgefield-01" {
		t.Errorf("node name = %q, want the reported hostname", node.Name)
	}
	if hw := f.repo.hardware[res.NodeID]; hw == nil || !hw.FingerprintComplete {
		t.Error("a complete fingerprint should be recorded as complete")
	}
	if got := len(f.repo.eventsWithCode(proto.CodeEnrollCompleted)); got != 1 {
		t.Errorf("expected 1 enroll.completed event, got %d", got)
	}
	if got := len(f.repo.auditsWithAction(model.ActionNodeEnroll)); got != 1 {
		t.Errorf("expected 1 node.enroll audit row, got %d", got)
	}
}

// TestTokenIsSingleUse is the property the whole enrollment story rests on: a
// token that leaks after use must be worth nothing.
func TestTokenIsSingleUse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	secret := f.mint(t)

	if _, err := f.svc.Enroll(ctx, f.request(secret, fingerprint()), "203.0.113.9"); err != nil {
		t.Fatal(err)
	}

	// Replay with different hardware — the case that matters, because it is a
	// second machine trying to join on a spent token.
	other := fingerprint()
	other.MotherboardUUID = "11111111-2222-3333-4444-555555555555"
	other.GPUUUIDs = []string{"GPU-bbbb1111-2222-3333-4444-555566667777"}
	other.PrimaryNICMAC = "a4:bb:6d:99:88:77"

	_, err := f.svc.Enroll(ctx, f.request(secret, other), "198.51.100.4")
	if !errors.Is(err, ErrTokenConsumed) {
		t.Fatalf("replayed token must be refused, got %v", err)
	}
	if len(f.repo.nodes) != 1 {
		t.Fatalf("a replayed token must not create a second node; have %d", len(f.repo.nodes))
	}
}

func TestTokenExpiryAndRevocation(t *testing.T) {
	ctx := context.Background()

	t.Run("expired", func(t *testing.T) {
		f := newFixture(t)
		secret := f.mint(t)
		f.now = f.now.Add(73 * time.Hour)
		if _, err := f.svc.Enroll(ctx, f.request(secret, fingerprint()), ""); !errors.Is(err, ErrTokenExpired) {
			t.Fatalf("got %v, want ErrTokenExpired", err)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		f := newFixture(t)
		res, err := f.svc.MintToken(ctx, MintParams{Actor: "tech@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.RevokeToken(ctx, res.Token.ID, "admin@example.com", "admin", "install sheet lost in the van", "", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Enroll(ctx, f.request(res.Secret, fingerprint()), ""); !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("got %v, want ErrTokenRevoked", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.svc.Enroll(ctx, f.request("NOTAREALTOKEN0000000000000000000", fingerprint()), ""); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("got %v, want ErrTokenInvalid", err)
		}
	})
}

// TestReEnrollSameHardware is the recovery path: a re-imaged node, or one whose
// credential file was lost. It must succeed, keep the same node identity, and
// leave exactly one live credential.
func TestReEnrollSameHardware(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	f.now = f.now.Add(24 * time.Hour)
	second, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatalf("re-enrollment of known hardware must succeed: %v", err)
	}

	if second.NodeID != first.NodeID {
		t.Fatalf("re-enrollment must keep the node identity: %s != %s", second.NodeID, first.NodeID)
	}
	if !second.ReEnrolled {
		t.Error("result should be flagged as a re-enrollment")
	}
	if second.Credential == first.Credential {
		t.Error("re-enrollment must issue a fresh credential")
	}
	// The old credential must be dead. Two live credentials would mean a
	// revoked machine could keep reporting under its previous identity.
	if got := f.repo.activeCredCount(first.NodeID); got != 1 {
		t.Fatalf("expected exactly 1 live credential after re-enrollment, got %d", got)
	}
	if got := len(f.repo.auditsWithAction(model.ActionNodeReEnroll)); got != 1 {
		t.Errorf("expected a node.re_enroll audit row, got %d", got)
	}
}

// TestFingerprintChangeTriggersReattestation is the security-critical case.
// A GPU swap and a cloned provisioning image look identical from here, so the
// only safe move is to stop and ask a human.
func TestFingerprintChangeTriggersReattestation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	// Same motherboard and NIC, different GPU. The fingerprint hash changed, so
	// an exact lookup misses — the node is recognised by the credential it
	// still holds.
	swapped := fingerprint()
	swapped.GPUUUIDs = []string{"GPU-cccc1111-2222-3333-4444-555566667777"}

	req := f.request(f.mint(t), swapped)
	req.PriorCredential = first.Credential
	_, err = f.svc.Enroll(ctx, req, "203.0.113.9")

	var re *ReattestationError
	if !errors.As(err, &re) {
		t.Fatalf("a changed fingerprint must raise re-attestation, got %v", err)
	}
	if re.NodeID != first.NodeID {
		t.Errorf("re-attestation names node %s, want %s", re.NodeID, first.NodeID)
	}
	if len(re.ChangedFields) == 0 || re.ChangedFields[0] != "gpu_uuids" {
		t.Errorf("changed fields = %v, want gpu_uuids", re.ChangedFields)
	}

	// The node is quarantined, not merely refused: until a human decides, we
	// do not know that the machine reporting under this identity is ours.
	if got := f.repo.nodes[first.NodeID].Lifecycle; got != model.LifecycleQuarantined {
		t.Errorf("node lifecycle = %q, want quarantined", got)
	}
	if got := len(f.repo.eventsWithCode(proto.CodeFingerprintChanged)); got != 1 {
		t.Errorf("expected 1 identity.fingerprint_changed event, got %d", got)
	}

	// A node that cannot enroll retries forever with backoff. Those retries
	// must not stack one re-attestation row per attempt and bury the queue.
	for i := 0; i < 5; i++ {
		retry := f.request(f.mint(t), swapped)
		retry.PriorCredential = first.Credential
		if _, err := f.svc.Enroll(ctx, retry, "203.0.113.9"); !errors.As(err, &re) {
			t.Fatalf("retry %d: got %v", i, err)
		}
	}
	if got := len(f.repo.reattests[first.NodeID]); got != 1 {
		t.Fatalf("retries must reuse the pending re-attestation, got %d rows", got)
	}
}

// TestQuarantinedNodeCannotEnroll: a held node stays held even with a fresh,
// valid token.
func TestQuarantinedNodeCannotEnroll(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	res, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.QuarantineNode(ctx, res.NodeID, "suspected clone", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), ""); !errors.Is(err, ErrNodeQuarantined) {
		t.Fatalf("got %v, want ErrNodeQuarantined", err)
	}
}

func TestRetiredNodeCannotEnroll(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	res, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "")
	if err != nil {
		t.Fatal(err)
	}
	f.repo.nodes[res.NodeID].Lifecycle = model.LifecycleRetired
	if _, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), ""); !errors.Is(err, ErrNodeRevoked) {
		t.Fatalf("got %v, want ErrNodeRevoked", err)
	}
}

func TestEnrollRejectsMalformedRequests(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	secret := f.mint(t)

	cases := []struct {
		name string
		mut  func(*proto.EnrollRequest)
	}{
		{"no token", func(r *proto.EnrollRequest) { r.Token = "" }},
		{"no key", func(r *proto.EnrollRequest) { r.NodePub = nil }},
		{"short key", func(r *proto.EnrollRequest) { r.NodePub = []byte{1, 2, 3} }},
		{"empty fingerprint", func(r *proto.EnrollRequest) { r.Fingerprint = proto.HardwareFingerprint{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := f.request(secret, fingerprint())
			c.mut(req)
			if _, err := f.svc.Enroll(ctx, req, ""); !errors.Is(err, ErrBadRequest) {
				t.Fatalf("got %v, want ErrBadRequest", err)
			}
		})
	}
	if _, err := f.svc.Enroll(ctx, nil, ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("nil request: got %v, want ErrBadRequest", err)
	}
}

// TestIncompleteFingerprintStillEnrolls: consumer boards that report a vendor
// placeholder UUID must still be able to join, but the weaker identity binding
// has to be visible rather than assumed.
func TestIncompleteFingerprintStillEnrolls(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	fp := fingerprint()
	fp.MotherboardUUID = "03000200-0400-0500-0006-000700080009" // a shipped vendor default
	res, err := f.svc.Enroll(ctx, f.request(f.mint(t), fp), "")
	if err != nil {
		t.Fatalf("a placeholder board UUID must not block enrollment: %v", err)
	}
	hw := f.repo.hardware[res.NodeID]
	if hw.FingerprintComplete {
		t.Error("a placeholder board UUID must be recorded as an incomplete fingerprint")
	}
	ev := f.repo.eventsWithCode(proto.CodeEnrollCompleted)
	if len(ev) != 1 || ev[0].Detail["fingerprint_complete"] != "false" {
		t.Errorf("the enrollment event should carry fingerprint_complete=false, got %v", ev)
	}
}

// TestMintedSecretIsNeverPersisted: only the hash goes to storage. A database
// dump must not be convertible into a fleet enrollment.
func TestMintedSecretIsNeverPersisted(t *testing.T) {
	f := newFixture(t)
	res, err := f.svc.MintToken(context.Background(), MintParams{Actor: "tech@example.com", Label: "x"})
	if err != nil {
		t.Fatal(err)
	}
	stored := f.repo.tokens[res.Token.ID]
	if strings.Contains(string(stored.TokenHash), res.Secret) {
		t.Fatal("the raw token must not be stored")
	}
	if stored.TokenPrefix != res.Secret[:8] {
		t.Errorf("prefix should be the first 8 chars for identification, got %q", stored.TokenPrefix)
	}
	for _, a := range f.repo.audits {
		for k, v := range a.Detail {
			if v == res.Secret {
				t.Fatalf("audit detail %q leaked the raw token", k)
			}
		}
	}
	// The alphabet must not contain the characters a field tech confuses when
	// reading a token off a printed sheet.
	if strings.ContainsAny(res.Secret, "ILOU") {
		t.Errorf("token %q contains an ambiguous character", res.Secret)
	}
}

func TestMintRequiresAnActor(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.MintToken(context.Background(), MintParams{}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("got %v, want ErrBadRequest — every audit row must name a person", err)
	}
}

// TestReImagedNodeWithSwappedPartIsCaught covers the case that has no
// credential to lean on: a node was re-imaged (so it lost its credential file)
// AND had a component swapped (so its fingerprint no longer matches). Without
// the partial-hardware lookup this enrols as a SECOND identity for one physical
// machine, and every metric, alert, and command for that box is then split
// across two node records.
func TestReImagedNodeWithSwappedPartIsCaught(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	swapped := fingerprint()
	swapped.GPUUUIDs = []string{"GPU-dddd1111-2222-3333-4444-555566667777"}

	// No PriorCredential: the re-image wiped /var/lib/meternode.
	_, err = f.svc.Enroll(ctx, f.request(f.mint(t), swapped), "203.0.113.9")

	var re *ReattestationError
	if !errors.As(err, &re) {
		t.Fatalf("a shared motherboard must be recognised as the same machine, got %v", err)
	}
	if re.NodeID != first.NodeID {
		t.Errorf("re-attestation names node %s, want %s", re.NodeID, first.NodeID)
	}
	if len(f.repo.nodes) != 1 {
		t.Fatalf("one physical machine must not become two node records; have %d", len(f.repo.nodes))
	}
}

// TestGenuinelyNewHardwareEnrolsCleanly is the control for the test above: a
// machine that shares nothing with the fleet must not be dragged into a
// re-attestation by an over-eager partial match.
func TestGenuinelyNewHardwareEnrolsCleanly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "")
	if err != nil {
		t.Fatal(err)
	}

	fresh := proto.HardwareFingerprint{
		MotherboardUUID: "99999999-8888-7777-6666-555555555555",
		GPUUUIDs:        []string{"GPU-eeee1111-2222-3333-4444-555566667777"},
		PrimaryNICMAC:   "b8:ca:3a:44:55:66",
	}
	second, err := f.svc.Enroll(ctx, f.request(f.mint(t), fresh), "")
	if err != nil {
		t.Fatalf("unrelated hardware must enrol cleanly: %v", err)
	}
	if second.NodeID == first.NodeID {
		t.Fatal("two different machines must not share a node identity")
	}
	if second.ReEnrolled {
		t.Error("a new machine is not a re-enrollment")
	}
	if len(f.repo.nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(f.repo.nodes))
	}
}

// TestForgedPriorCredentialIsIgnored: the prior credential is a claim of
// identity to be verified, never authorisation. One signed by a key we do not
// know must not let a machine assume another node's identity.
func TestForgedPriorCredentialIsIgnored(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	victim, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "")
	if err != nil {
		t.Fatal(err)
	}

	attackerSigner, err := keys.Generate("ck-test") // same key id, different key
	if err != nil {
		t.Fatal(err)
	}
	forged := &proto.Credential{
		V: 1, NodeID: victim.NodeID.String(), NodePub: f.nodePub,
		FingerprintHash: "whatever", IssuedAt: f.now.UnixMilli(), KeyID: "ck-test",
	}
	encoded, err := forged.EncodeWith(attackerSigner.Sign)
	if err != nil {
		t.Fatal(err)
	}

	attackerFP := proto.HardwareFingerprint{
		MotherboardUUID: "77777777-6666-5555-4444-333333333333",
		GPUUUIDs:        []string{"GPU-ffff1111-2222-3333-4444-555566667777"},
		PrimaryNICMAC:   "de:ad:be:ef:00:01",
	}
	req := f.request(f.mint(t), attackerFP)
	req.PriorCredential = encoded

	res, err := f.svc.Enroll(ctx, req, "198.51.100.66")
	if err != nil {
		t.Fatalf("the enrollment itself is legitimate (valid token, new hardware): %v", err)
	}
	if res.NodeID == victim.NodeID {
		t.Fatal("a credential signed by an unknown key must not confer another node's identity")
	}
}

// TestRefusedEnrollmentStillRecordsTheReattestation pins the thing that has to
// happen in TWO transactions rather than one.
//
// Refusing the enrollment must roll back — the token has to stay unspent so a
// field tech can retry once an operator releases the node. But the evidence the
// operator needs to make that decision (the re-attestation row, the quarantine,
// the CRITICAL event) must survive that rollback. An earlier version wrote all
// of it inside the enrollment transaction, so the entire review flow was
// silently discarded and the operator queue stayed empty.
func TestRefusedEnrollmentStillRecordsTheReattestation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	swapped := fingerprint()
	swapped.GPUUUIDs = []string{"GPU-cccc1111-2222-3333-4444-555566667777"}

	res, err := f.svc.MintToken(ctx, MintParams{Actor: "tech@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	req := f.request(res.Secret, swapped)
	req.PriorCredential = first.Credential

	var re *ReattestationError
	if _, err := f.svc.Enroll(ctx, req, "203.0.113.9"); !errors.As(err, &re) {
		t.Fatalf("expected a re-attestation refusal, got %v", err)
	}

	// The evidence survived the rollback.
	pending := f.repo.reattests[first.NodeID]
	if len(pending) != 1 {
		t.Fatalf("the re-attestation must be recorded despite the refusal, got %d rows", len(pending))
	}
	if pending[0].State != model.ReattestPending {
		t.Errorf("state = %q, want pending", pending[0].State)
	}
	if re.ReattestationID != pending[0].ID {
		t.Errorf("the error names %s but the record is %s", re.ReattestationID, pending[0].ID)
	}
	if got := f.repo.nodes[first.NodeID].Lifecycle; got != model.LifecycleQuarantined {
		t.Errorf("node lifecycle = %q, want quarantined", got)
	}
	if got := len(f.repo.eventsWithCode(proto.CodeFingerprintChanged)); got != 1 {
		t.Errorf("expected 1 identity.fingerprint_changed event, got %d", got)
	}
	if got := len(f.repo.auditsWithAction(model.ActionReattestOpen)); got != 1 {
		t.Errorf("expected 1 reattestation.open audit row, got %d", got)
	}

	// And the token was NOT spent — the refusal rolled back, so a field tech
	// can retry with it once an operator releases the node. A consumed token
	// here is a second site visit.
	if tok := f.repo.tokens[res.Token.ID]; tok.ConsumedAt != nil {
		t.Error("a refused enrollment must not consume the token")
	}
	// Nor was a credential issued.
	if got := f.repo.activeCredCount(first.NodeID); got != 1 {
		t.Errorf("the refused enrollment must not issue a credential; live count = %d", got)
	}
}

// TestConcurrentDetectionRecordsOneRow: two agents (or one retrying fast) can
// both detect the same mismatch before either records it. The operator queue
// gets one row, not two.
func TestConcurrentDetectionRecordsOneRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first, err := f.svc.Enroll(ctx, f.request(f.mint(t), fingerprint()), "")
	if err != nil {
		t.Fatal(err)
	}

	swapped := fingerprint()
	swapped.GPUUUIDs = []string{"GPU-dddd1111-2222-3333-4444-555566667777"}

	for i := 0; i < 4; i++ {
		req := f.request(f.mint(t), swapped)
		req.PriorCredential = first.Credential
		var re *ReattestationError
		if _, err := f.svc.Enroll(ctx, req, ""); !errors.As(err, &re) {
			t.Fatalf("attempt %d: got %v", i, err)
		}
	}
	if got := len(f.repo.reattests[first.NodeID]); got != 1 {
		t.Fatalf("repeated detection must reuse one record, got %d rows", got)
	}
}
