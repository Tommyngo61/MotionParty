package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	proto "github.com/meterhome/meternode-proto"

	"github.com/meterhome/meternode-console/internal/config"
	"github.com/meterhome/meternode-console/internal/enroll"
	"github.com/meterhome/meternode-console/internal/keys"
	"github.com/meterhome/meternode-console/internal/model"
)

// --- test doubles ---------------------------------------------------------

type stubPrincipals map[string]Principal // sha256 hex -> principal

func (s stubPrincipals) PrincipalByTokenHash(ctx context.Context, hash []byte) (*Principal, error) {
	if p, ok := s[string(hash)]; ok {
		return &p, nil
	}
	return nil, nil
}

type stubHealth struct{ err error }

func (s stubHealth) Ping(context.Context) error { return s.err }

// memRepo is the enroll.Repo the handler tests run against. It is the same
// shape as the one in internal/enroll's tests; duplicated rather than exported
// so the fake never becomes something production code can reach for.
type memRepo struct {
	tokens    map[uuid.UUID]*model.Token
	nodes     map[uuid.UUID]*model.Node
	hardware  map[uuid.UUID]*model.Hardware
	creds     map[uuid.UUID][]*model.Credential
	reattests map[uuid.UUID][]*model.Reattestation
}

func newMemRepo() *memRepo {
	return &memRepo{
		tokens: map[uuid.UUID]*model.Token{}, nodes: map[uuid.UUID]*model.Node{},
		hardware: map[uuid.UUID]*model.Hardware{}, creds: map[uuid.UUID][]*model.Credential{},
		reattests: map[uuid.UUID][]*model.Reattestation{},
	}
}

func (m *memRepo) InTx(ctx context.Context, fn func(enroll.Tx) error) error { return fn(m) }
func (m *memRepo) InsertToken(ctx context.Context, t *model.Token) error {
	cp := *t
	m.tokens[t.ID] = &cp
	return nil
}
func (m *memRepo) ListTokens(ctx context.Context, f enroll.TokenFilter) ([]model.Token, error) {
	var out []model.Token
	for _, t := range m.tokens {
		if f.OnlyOpen && !t.Usable(time.Now()) {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}
func (m *memRepo) RevokeToken(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) error {
	t, ok := m.tokens[id]
	if !ok {
		return enroll.ErrTokenInvalid
	}
	t.RevokedAt, t.RevokedBy, t.RevokeReason = &at, by, reason
	return nil
}
func (m *memRepo) InsertAudit(ctx context.Context, a *model.AuditEntry) error { return nil }
func (m *memRepo) TokenByHash(ctx context.Context, hash []byte) (*model.Token, error) {
	for _, t := range m.tokens {
		if string(t.TokenHash) == string(hash) {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) MarkTokenConsumed(ctx context.Context, tokenID, nodeID uuid.UUID, at time.Time) error {
	t := m.tokens[tokenID]
	t.ConsumedAt, t.ConsumedByNode = &at, &nodeID
	return nil
}
func (m *memRepo) NodeByFingerprint(ctx context.Context, fp string) (*model.Node, error) {
	for id, hw := range m.hardware {
		if hw.FingerprintHash == fp {
			cp := *m.nodes[id]
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) NodeByPartialHardware(ctx context.Context, fp proto.HardwareFingerprint) (*model.Node, error) {
	for id, hw := range m.hardware {
		if hw.MotherboardUUID != "" && hw.MotherboardUUID == fp.MotherboardUUID {
			cp := *m.nodes[id]
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) NodeByID(ctx context.Context, id uuid.UUID) (*model.Node, error) {
	n, ok := m.nodes[id]
	if !ok {
		return nil, nil
	}
	cp := *n
	return &cp, nil
}
func (m *memRepo) InsertNode(ctx context.Context, n *model.Node) error {
	cp := *n
	m.nodes[n.ID] = &cp
	return nil
}
func (m *memRepo) MarkNodeEnrolled(ctx context.Context, id uuid.UUID, v string, at time.Time) error {
	n := m.nodes[id]
	n.Lifecycle, n.AgentVersion, n.EnrolledAt = model.LifecycleEnrolled, v, &at
	return nil
}
func (m *memRepo) QuarantineNode(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	n := m.nodes[id]
	n.Lifecycle, n.QuarantineReason = model.LifecycleQuarantined, reason
	return nil
}
func (m *memRepo) HardwareByNode(ctx context.Context, id uuid.UUID) (*model.Hardware, error) {
	hw, ok := m.hardware[id]
	if !ok {
		return nil, nil
	}
	cp := *hw
	return &cp, nil
}
func (m *memRepo) UpsertHardware(ctx context.Context, h *model.Hardware) error {
	cp := *h
	m.hardware[h.NodeID] = &cp
	return nil
}
func (m *memRepo) ActiveCredential(ctx context.Context, id uuid.UUID) (*model.Credential, error) {
	return nil, nil
}
func (m *memRepo) RevokeCredentials(ctx context.Context, id uuid.UUID, by, reason string, at time.Time) error {
	for _, c := range m.creds[id] {
		if c.RevokedAt == nil {
			c.RevokedAt = &at
		}
	}
	return nil
}
func (m *memRepo) InsertCredential(ctx context.Context, c *model.Credential) error {
	cp := *c
	m.creds[c.NodeID] = append(m.creds[c.NodeID], &cp)
	return nil
}
func (m *memRepo) PendingReattestation(ctx context.Context, id uuid.UUID) (*model.Reattestation, error) {
	for _, r := range m.reattests[id] {
		if r.State == model.ReattestPending {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) InsertReattestation(ctx context.Context, r *model.Reattestation) error {
	cp := *r
	m.reattests[r.NodeID] = append(m.reattests[r.NodeID], &cp)
	return nil
}
func (m *memRepo) InsertEvent(ctx context.Context, e *model.Event) error { return nil }

// --- harness --------------------------------------------------------------

type harness struct {
	t      *testing.T
	srv    http.Handler
	repo   *memRepo
	signer *keys.Signer
	tokens stubPrincipals
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	signer, err := keys.Generate("ck-test")
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemRepo()
	cfg := &config.Config{
		Env: "dev", PublicURL: "http://localhost:8080",
		DatabaseURL: "x", DBMaxConns: 1, EnrollTokenTTL: 72 * time.Hour,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	tokens := stubPrincipals{}
	add := func(secret string, p Principal) {
		sum := sha256.Sum256([]byte(secret))
		tokens[string(sum[:])] = p
	}
	add("admin-token", Principal{Name: "admin@example.com", Role: model.RoleAdmin})
	add("operator-token", Principal{Name: "op@example.com", Role: model.RoleOperator})
	add("viewer-token", Principal{Name: "viewer@example.com", Role: model.RoleViewer})

	s := New(Options{
		Config:     cfg,
		Logger:     slog.New(slog.NewTextHandler(discard{}, nil)),
		Enroll:     enroll.New(repo, signer, enroll.Options{TokenTTL: 72 * time.Hour}),
		Principals: tokens,
		Health:     stubHealth{},
		Version:    "test",
	})
	return &harness{t: t, srv: s.Routes(), repo: repo, signer: signer, tokens: tokens}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func (h *harness) do(method, path, auth string, body any) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	return rec
}

func (h *harness) mintToken(auth string) mintTokenResponse {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens", auth, mintTokenRequest{Label: "test"})
	if rec.Code != http.StatusCreated {
		h.t.Fatalf("mint returned %d: %s", rec.Code, rec.Body.String())
	}
	var out mintTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func enrollBody(token string, pub ed25519.PublicKey) proto.EnrollRequest {
	return proto.EnrollRequest{
		Token: token, NodePub: pub,
		Fingerprint: proto.HardwareFingerprint{
			MotherboardUUID: "4C4C4544-0037-5A10-8054-B4C04F503733",
			GPUUUIDs:        []string{"GPU-aaaa1111-2222-3333-4444-555566667777"},
			PrimaryNICMAC:   "a4:bb:6d:11:22:33",
		},
		AgentVersion: "1.0.0",
		Hardware:     proto.HardwareInventory{Hostname: "mn-test-01"},
		SentAtMS:     time.Now().UnixMilli(),
	}
}

// --- tests ----------------------------------------------------------------

// TestLivenessSurvivesDatabaseOutage is the reason /healthz and /readyz are
// separate. If liveness went red on a database blip, the orchestrator would
// restart every replica, dropping every agent socket at once — and the whole
// fleet would reconnect together, which is precisely the storm the transport
// design exists to avoid.
func TestLivenessSurvivesDatabaseOutage(t *testing.T) {
	signer, _ := keys.Generate("ck-test")
	cfg := &config.Config{Env: "dev", PublicURL: "http://x", DatabaseURL: "x", DBMaxConns: 1, EnrollTokenTTL: time.Hour}
	s := New(Options{
		Config: cfg, Logger: slog.New(slog.NewTextHandler(discard{}, nil)),
		Enroll:     enroll.New(newMemRepo(), signer, enroll.Options{}),
		Principals: stubPrincipals{}, Health: stubHealth{err: errDBDown}, Version: "test",
	})
	srv := s.Routes()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("liveness must stay green through a database outage, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readiness must go red on a database outage, got %d", rec.Code)
	}
}

var errDBDown = &stubErr{"connection refused"}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }

func TestHealthAndSchemaNeedNoCredential(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/healthz", "/readyz", "/v1/schema"} {
		if rec := h.do(http.MethodGet, path, "", nil); rec.Code != http.StatusOK {
			t.Errorf("%s returned %d without a credential, want 200", path, rec.Code)
		}
	}
}

func TestSchemaEndpointReportsTheCompatibilityWindow(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/v1/schema", "", nil)
	var out schemaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.EnvelopeVersion != proto.SchemaVersion || out.MinSupported != proto.MinSupportedVersion {
		t.Errorf("schema endpoint must report the real window, got %+v", out)
	}
	if out.SampleIntervalS != 10 || out.HeartbeatInterval != 15 || out.MonthlyBudgetMB != 150 {
		t.Errorf("schema endpoint must report the contract's budget, got %+v", out)
	}
}

func TestOperatorRoutesRequireAuth(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(http.MethodGet, "/v1/admin/enrollment-tokens", "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated list returned %d, want 401", rec.Code)
	}
	if rec := h.do(http.MethodGet, "/v1/admin/enrollment-tokens", "not-a-real-token", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token returned %d, want 401", rec.Code)
	}
}

// TestRolesAreEnforced: minting a token creates the one credential that exists
// before a node has an identity, so a viewer must not be able to do it.
func TestRolesAreEnforced(t *testing.T) {
	h := newHarness(t)

	if rec := h.do(http.MethodGet, "/v1/admin/enrollment-tokens", "viewer-token", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer should be able to list tokens, got %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens", "viewer-token", mintTokenRequest{}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer must not mint tokens, got %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens", "operator-token", mintTokenRequest{}); rec.Code != http.StatusCreated {
		t.Errorf("operator should mint tokens, got %d", rec.Code)
	}
}

// TestMintedTokenIsShownOnceAndNeverListed pins the property that makes a
// database dump useless for joining the fleet.
func TestMintedTokenIsShownOnceAndNeverListed(t *testing.T) {
	h := newHarness(t)
	minted := h.mintToken("operator-token")
	if minted.Token == "" {
		t.Fatal("mint must return the raw token once")
	}

	rec := h.do(http.MethodGet, "/v1/admin/enrollment-tokens", "admin-token", nil)
	if bytes.Contains(rec.Body.Bytes(), []byte(minted.Token)) {
		t.Fatal("the token listing must never contain a raw token")
	}
	var listing struct {
		Tokens []tokenView `json:"tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Tokens) != 1 || listing.Tokens[0].State != "open" {
		t.Fatalf("expected one open token, got %+v", listing.Tokens)
	}
	if listing.Tokens[0].Prefix != minted.Token[:8] {
		t.Error("the listing should carry the prefix so a human can identify the token")
	}
}

func TestEnrollEndToEnd(t *testing.T) {
	h := newHarness(t)
	minted := h.mintToken("operator-token")
	pub, _, _ := ed25519.GenerateKey(nil)

	rec := h.do(http.MethodPost, proto.EnrollPath, "", enrollBody(minted.Token, pub))
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll returned %d: %s", rec.Code, rec.Body.String())
	}
	var resp proto.EnrollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Config.StreamURL != "ws://localhost:8080/v1/stream" {
		t.Errorf("stream url = %q", resp.Config.StreamURL)
	}
	if resp.Config.TelemetryURL != "http://localhost:8080/v1/telemetry" {
		t.Errorf("telemetry url = %q", resp.Config.TelemetryURL)
	}
	if resp.Config.SampleIntervalS != 10 || resp.Config.HeartbeatIntervalS != 15 {
		t.Errorf("config hints must carry the contract's budget: %+v", resp.Config)
	}
	if len(resp.ControllerKeys) == 0 {
		t.Fatal("the agent must be handed the keys it will pin for command verification")
	}

	// The credential must verify with a key the response published — exactly
	// what the agent does before it stores anything.
	cred, sig, err := proto.ParseCredential(resp.Credential)
	if err != nil {
		t.Fatal(err)
	}
	pinned, ok := resp.ControllerKeys[cred.KeyID]
	if !ok {
		t.Fatalf("credential signed by %q, which was not published", cred.KeyID)
	}
	if err := cred.Verify(ed25519.PublicKey(pinned), sig, time.Now()); err != nil {
		t.Fatalf("credential must verify against a published key: %v", err)
	}
	if resp.ServerTimeMS == 0 {
		t.Error("the response must carry server time so the agent can seed its clock skew")
	}
}

// TestEnrollErrorsUseTheRightStatusCodes matters operationally: the agent
// retries a 5xx with backoff and gives up on a 4xx. Classifying a spent token
// as a server error would leave a node hammering the controller forever.
func TestEnrollErrorsUseTheRightStatusCodes(t *testing.T) {
	h := newHarness(t)
	pub, _, _ := ed25519.GenerateKey(nil)
	minted := h.mintToken("operator-token")

	if rec := h.do(http.MethodPost, proto.EnrollPath, "", enrollBody(minted.Token, pub)); rec.Code != http.StatusOK {
		t.Fatalf("first enroll: %d %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name   string
		body   proto.EnrollRequest
		status int
		code   string
	}{
		{"replayed token", enrollBody(minted.Token, pub), http.StatusConflict, proto.ErrCodeTokenConsumed},
		{"unknown token", enrollBody("NOTAREALTOKEN00000000000000000000", pub), http.StatusUnauthorized, proto.ErrCodeTokenInvalid},
		{"no token", func() proto.EnrollRequest {
			b := enrollBody("", pub)
			return b
		}(), http.StatusBadRequest, proto.ErrCodeBadRequest},
		{"bad key", func() proto.EnrollRequest {
			b := enrollBody(h.mintToken("operator-token").Token, pub)
			b.NodePub = []byte{1, 2, 3}
			return b
		}(), http.StatusBadRequest, proto.ErrCodeBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := h.do(http.MethodPost, proto.EnrollPath, "", c.body)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.status, rec.Body.String())
			}
			var e proto.APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if e.Code != c.code {
				t.Errorf("code = %q, want %q", e.Code, c.code)
			}
			if e.Message == "" {
				t.Error("every error must carry a message a field tech can act on")
			}
		})
	}
}

func TestEnrollRejectsOversizedBody(t *testing.T) {
	h := newHarness(t)
	pub, _, _ := ed25519.GenerateKey(nil)
	body := enrollBody(h.mintToken("operator-token").Token, pub)
	// A hostile inventory: the endpoint is unauthenticated, so this is the one
	// place an unauthenticated caller can make the controller allocate.
	body.Hardware.Hostname = string(bytes.Repeat([]byte("a"), maxEnrollBody+1))

	rec := h.do(http.MethodPost, proto.EnrollPath, "", body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestReattestationReturns409WithDetail(t *testing.T) {
	h := newHarness(t)
	pub, _, _ := ed25519.GenerateKey(nil)

	first := h.do(http.MethodPost, proto.EnrollPath, "", enrollBody(h.mintToken("operator-token").Token, pub))
	if first.Code != http.StatusOK {
		t.Fatal(first.Body.String())
	}

	// Same motherboard, different GPU: the machine is recognisable, the
	// fingerprint is not.
	body := enrollBody(h.mintToken("operator-token").Token, pub)
	body.Fingerprint.GPUUUIDs = []string{"GPU-cccc1111-2222-3333-4444-555566667777"}

	rec := h.do(http.MethodPost, proto.EnrollPath, "", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var out proto.ReattestationRequired
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.ChangedFields) == 0 || out.ChangedFields[0] != "gpu_uuids" {
		t.Errorf("changed fields = %v, want gpu_uuids", out.ChangedFields)
	}
	if out.ReattestationID == "" || out.NodeID == "" || out.Message == "" {
		t.Errorf("the 409 must be actionable: %+v", out)
	}
}

func TestUnknownFieldsAreNamedClearly(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodPost, proto.EnrollPath,
		bytes.NewBufferString(`{"token":"x","some_future_field":1}`))
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var e proto.APIError
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if !bytes.Contains([]byte(e.Message), []byte("upgrade the controller")) {
		t.Errorf("a newer agent should be told what is actually wrong, got %q", e.Message)
	}
}

func TestRevokeToken(t *testing.T) {
	h := newHarness(t)
	minted := h.mintToken("operator-token")

	rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens/"+minted.ID+"/revoke", "operator-token",
		revokeTokenRequest{Reason: "install sheet lost in the van"})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke returned %d: %s", rec.Code, rec.Body.String())
	}

	pub, _, _ := ed25519.GenerateKey(nil)
	if rec := h.do(http.MethodPost, proto.EnrollPath, "", enrollBody(minted.Token, pub)); rec.Code != http.StatusUnauthorized {
		t.Errorf("a revoked token must not enrol, got %d", rec.Code)
	}

	if rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens/"+uuid.NewString()+"/revoke", "operator-token", nil); rec.Code != http.StatusNotFound {
		t.Errorf("revoking an unknown token should 404, got %d", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/v1/admin/enrollment-tokens/not-a-uuid/revoke", "operator-token", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed id should 400, got %d", rec.Code)
	}
}

func TestResponsesAreNotCacheable(t *testing.T) {
	// Enrollment responses contain credentials; the fleet view is live data.
	// Neither may sit in an intermediary cache.
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/v1/admin/enrollment-tokens", "viewer-token", nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestRequestIDIsEchoed(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Errorf("X-Request-ID = %q, want the caller's value echoed back", got)
	}
}
