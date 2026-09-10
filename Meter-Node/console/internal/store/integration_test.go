//go:build integration

// Integration tests for the PostgreSQL/TimescaleDB layer.
//
// These are separated behind a build tag because they need a real database with
// the TimescaleDB extension, which is what `make test-integration` and
// `deploy/docker-compose.yml` provide. They exist to cover exactly what the
// in-memory fakes in internal/enroll CANNOT: transaction atomicity, the unique
// constraints, and row locking under concurrency. A fake that pretended to roll
// back would only be testing the fake.
//
//	make test-integration
//	METERNODE_TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/store/
package store

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
	"github.com/google/uuid"

	"github.com/MeterHome/Meter-Node/console/internal/enroll"
	"github.com/MeterHome/Meter-Node/console/internal/keys"
	"github.com/MeterHome/Meter-Node/console/internal/model"
)

func testDB(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("METERNODE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set METERNODE_TEST_DATABASE_URL to run integration tests (see `make test-integration`)")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if err := Migrate(ctx, url, log); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s, err := Connect(ctx, Options{URL: url, MaxConns: 16, MinConns: 2})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	truncate(t, s)
	return s
}

// truncate resets the tables these tests touch. CASCADE follows the foreign
// keys so the order does not have to be maintained by hand as the schema grows.
func truncate(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `
		TRUNCATE nodes, sites, enrollment_tokens, node_credentials, node_hardware,
		         node_reattestations, audit_log, events, api_tokens, controller_keys CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func testService(t *testing.T, s *Store) (*enroll.Service, *keys.Signer) {
	t.Helper()
	signer, err := LoadOrCreateSigningKey(context.Background(), s, "ck-test", "", true,
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if err != nil {
		t.Fatal(err)
	}
	return enroll.New(NewEnrollRepo(s), signer, enroll.Options{TokenTTL: time.Hour}), signer
}

func mint(t *testing.T, svc *enroll.Service) string {
	t.Helper()
	res, err := svc.MintToken(context.Background(), enroll.MintParams{Actor: "test@example.com", Label: "integration"})
	if err != nil {
		t.Fatal(err)
	}
	return res.Secret
}

func enrollReq(t *testing.T, token string, index int) *proto.EnrollRequest {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &proto.EnrollRequest{
		Token: token, NodePub: pub,
		Fingerprint: proto.HardwareFingerprint{
			MotherboardUUID: fmt.Sprintf("4c4c4544-0037-5a10-8054-%012x", index),
			GPUUUIDs:        []string{fmt.Sprintf("GPU-%08x-1111-2222-3333-444455556666", index)},
			PrimaryNICMAC:   fmt.Sprintf("a4:bb:6d:00:00:%02x", index),
		},
		Hardware:     proto.HardwareInventory{Hostname: fmt.Sprintf("mn-int-%03d", index)},
		AgentVersion: "1.0.0", SentAtMS: time.Now().UnixMilli(),
	}
}

func TestMigrationsApplyCleanly(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()

	// The hypertables are the part most likely to break silently: a plain
	// table with the right columns looks fine until the fleet grid runs its
	// first time_bucket query over a month of data.
	for _, table := range []string{"metric_samples", "gpu_samples", "disk_samples", "heartbeats", "container_samples"} {
		var isHyper bool
		err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name = $1)`, table).Scan(&isHyper)
		if err != nil {
			t.Fatalf("check hypertable %s: %v", table, err)
		}
		if !isHyper {
			t.Errorf("%s is not a hypertable", table)
		}
	}

	for _, view := range []string{"metric_samples_1m", "metric_samples_5m", "metric_samples_1h", "gpu_samples_1m", "gpu_samples_1h"} {
		var exists bool
		err := s.pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM timescaledb_information.continuous_aggregates WHERE view_name = $1)`, view).Scan(&exists)
		if err != nil {
			t.Fatalf("check continuous aggregate %s: %v", view, err)
		}
		if !exists {
			t.Errorf("continuous aggregate %s is missing", view)
		}
	}
}

func TestEnrollmentIsAtomic(t *testing.T) {
	s := testDB(t)
	svc, _ := testService(t, s)
	ctx := context.Background()

	secret := mint(t, svc)
	res, err := svc.Enroll(ctx, enrollReq(t, secret, 1), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	// Every row the transaction was supposed to write must be there.
	assertCount(t, s, 1, `SELECT count(*) FROM nodes WHERE id = $1 AND lifecycle = 'enrolled'`, res.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM node_hardware WHERE node_id = $1`, res.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM node_credentials WHERE node_id = $1 AND revoked_at IS NULL`, res.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM enrollment_tokens WHERE consumed_by_node = $1`, res.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM events WHERE node_id = $1 AND code = $2`, res.NodeID, proto.CodeEnrollCompleted)
	assertCount(t, s, 1, `SELECT count(*) FROM audit_log WHERE node_id = $1 AND action = $2`, res.NodeID, model.ActionNodeEnroll)
}

// TestConcurrentEnrollmentWithOneToken is the race the FOR UPDATE in
// TokenByHash exists for. A field tech retrying an install that looked like it
// timed out really does produce two simultaneous enrollments on one token, and
// exactly one of them may succeed.
func TestConcurrentEnrollmentWithOneToken(t *testing.T) {
	s := testDB(t)
	svc, _ := testService(t, s)
	ctx := context.Background()

	secret := mint(t, svc)

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	nodeIDs := make([]uuid.UUID, racers)
	start := make(chan struct{})

	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			res, err := svc.Enroll(ctx, enrollReq(t, secret, 100+i), "203.0.113.9")
			results[i] = err
			if res != nil {
				nodeIDs[i] = res.NodeID
			}
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for i, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		if !errors.Is(err, enroll.ErrTokenConsumed) && !errors.Is(err, enroll.ErrTokenInvalid) {
			t.Errorf("racer %d failed for an unexpected reason: %v", i, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("exactly one racer may enrol on a single-use token, got %d", succeeded)
	}
	assertCount(t, s, 1, `SELECT count(*) FROM nodes`)
	assertCount(t, s, 1, `SELECT count(*) FROM node_credentials`)
}

// TestFailedEnrollmentRollsBack: if anything after the node insert fails, the
// transaction must leave nothing behind — no half-created node, and crucially
// no consumed token. A machine in a homeowner's house holding a spent token
// with no identity needs a truck roll to fix.
//
// The failure is forced through a real constraint rather than a fault-injection
// hook, so this tests the actual rollback path the production code takes.
func TestFailedEnrollmentRollsBack(t *testing.T) {
	s := testDB(t)
	svc, _ := testService(t, s)
	ctx := context.Background()

	var siteID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sites (short_name, name) VALUES ('ridgefield', 'Ridgefield') RETURNING id`).Scan(&siteID)
	if err != nil {
		t.Fatal(err)
	}
	// Occupy the (site_id, name) the enrollment is about to claim.
	_, err = s.pool.Exec(ctx, `INSERT INTO nodes (site_id, name) VALUES ($1, 'mn-int-009')`, siteID)
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.MintToken(ctx, enroll.MintParams{
		Actor: "test@example.com", SiteID: &siteID, NodeNameHint: "mn-int-009",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Enroll(ctx, enrollReq(t, res.Secret, 9), "203.0.113.9"); err == nil {
		t.Fatal("expected the duplicate node name to fail the enrollment")
	}

	// Exactly the one pre-existing node, and nothing the rolled-back
	// transaction wrote.
	assertCount(t, s, 1, `SELECT count(*) FROM nodes`)
	assertCount(t, s, 0, `SELECT count(*) FROM node_credentials`)
	assertCount(t, s, 0, `SELECT count(*) FROM node_hardware`)
	assertCount(t, s, 0, `SELECT count(*) FROM events`)
	// The token is still spendable. This is the row that matters most: a
	// consumed token here is a second site visit.
	assertCount(t, s, 1, `SELECT count(*) FROM enrollment_tokens WHERE id = $1 AND consumed_at IS NULL`, res.Token.ID)
}

// TestOneFingerprintOneNode pins the unique index that stops a cloned
// provisioning image from becoming two nodes sharing one machine's identity.
func TestOneFingerprintOneNode(t *testing.T) {
	s := testDB(t)
	svc, _ := testService(t, s)
	ctx := context.Background()

	first, err := svc.Enroll(ctx, enrollReq(t, mint(t, svc), 42), "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Enroll(ctx, enrollReq(t, mint(t, svc), 42), "")
	if err != nil {
		t.Fatalf("identical hardware should re-enrol, not fail: %v", err)
	}
	if first.NodeID != second.NodeID {
		t.Fatalf("identical hardware produced two node identities: %s and %s", first.NodeID, second.NodeID)
	}
	assertCount(t, s, 1, `SELECT count(*) FROM nodes`)
	// Exactly one live credential: the unique partial index enforces it even if
	// the service ever forgets to revoke.
	assertCount(t, s, 1, `SELECT count(*) FROM node_credentials WHERE node_id = $1 AND revoked_at IS NULL`, first.NodeID)
	assertCount(t, s, 2, `SELECT count(*) FROM node_credentials WHERE node_id = $1`, first.NodeID)
}

func TestFingerprintChangeQuarantinesInTheDatabase(t *testing.T) {
	s := testDB(t)
	svc, _ := testService(t, s)
	ctx := context.Background()

	first, err := svc.Enroll(ctx, enrollReq(t, mint(t, svc), 5), "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}

	// Same motherboard, different GPU: found by the partial-hardware lookup.
	req := enrollReq(t, mint(t, svc), 5)
	req.Fingerprint.GPUUUIDs = []string{"GPU-deadbeef-1111-2222-3333-444455556666"}

	var re *enroll.ReattestationError
	if _, err := svc.Enroll(ctx, req, "203.0.113.9"); !errors.As(err, &re) {
		t.Fatalf("expected re-attestation, got %v", err)
	}

	assertCount(t, s, 1, `SELECT count(*) FROM node_reattestations WHERE node_id = $1 AND state = 'pending'`, first.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM nodes WHERE id = $1 AND lifecycle = 'quarantined'`, first.NodeID)
	assertCount(t, s, 1, `SELECT count(*) FROM events WHERE node_id = $1 AND code = $2 AND severity = 'CRITICAL'`,
		first.NodeID, proto.CodeFingerprintChanged)

	// The token from the refused attempt must NOT have been consumed: the
	// transaction rolled back, and a field tech should be able to retry with it
	// once an operator releases the node.
	assertCount(t, s, 1, `SELECT count(*) FROM enrollment_tokens WHERE consumed_at IS NULL`)
}

func TestApiTokenLookupHonoursRevocationAndExpiry(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	ps := NewPrincipalStore(s)

	insert := func(secret, name, role string, expires *time.Time, revoked *time.Time) []byte {
		sum := sha256.Sum256([]byte(secret))
		_, err := s.pool.Exec(ctx, `
			INSERT INTO api_tokens (token_hash, token_prefix, name, role, created_by, expires_at, revoked_at)
			VALUES ($1, $2, $3, $4, 'test', $5, $6)`,
			sum[:], secret[:4], name, role, expires, revoked)
		if err != nil {
			t.Fatal(err)
		}
		return sum[:]
	}

	past := time.Now().Add(-time.Hour)
	live := insert("live-token", "live", "operator", nil, nil)
	expired := insert("expired-token", "expired", "admin", &past, nil)
	revoked := insert("revoked-token", "revoked", "admin", nil, &past)

	if p, err := ps.PrincipalByTokenHash(ctx, live); err != nil || p == nil || p.Role != model.RoleOperator {
		t.Fatalf("live token: %+v %v", p, err)
	}
	for name, hash := range map[string][]byte{"expired": expired, "revoked": revoked} {
		p, err := ps.PrincipalByTokenHash(ctx, hash)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p != nil {
			t.Errorf("%s token must not resolve, got %+v", name, p)
		}
	}
}

func TestSigningKeySurvivesRestart(t *testing.T) {
	s := testDB(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// A controller that mints a new key on every boot silently tells every
	// enrolled node to reject its commands.
	first, err := LoadOrCreateSigningKey(ctx, s, "ck-dev-1", "", true, log)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateSigningKey(ctx, s, "ck-dev-1", "", true, log)
	if err != nil {
		t.Fatal(err)
	}
	if first.Seed() != second.Seed() {
		t.Fatal("the dev signing key must be reused across restarts, not regenerated")
	}

	// And outside dev, generation is refused outright.
	if _, err := LoadOrCreateSigningKey(ctx, s, "ck-prod", "", false, log); err == nil {
		t.Fatal("key generation must be refused when it is not permitted")
	}
}

func TestBootstrapAdminIsRefusedOutsideDev(t *testing.T) {
	s := testDB(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := EnsureBootstrapAdmin(context.Background(), s, "letmein", false, log); err == nil {
		t.Fatal("a shared bootstrap admin token must never be seeded outside dev")
	}
}

func assertCount(t *testing.T, s *Store, want int, query string, args ...any) {
	t.Helper()
	var got int
	if err := s.pool.QueryRow(context.Background(), query, args...).Scan(&got); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if got != want {
		t.Errorf("%s\n  got %d, want %d", query, got, want)
	}
}
