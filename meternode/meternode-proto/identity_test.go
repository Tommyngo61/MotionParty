package meternodeproto

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func TestFingerprintNormalisation(t *testing.T) {
	// The same machine, described by dmidecode in one enrollment and by sysfs
	// in the next, with NVML returning the GPUs in a different order. These
	// must produce the same hash: any jitter here looks like a hardware swap
	// and drags a healthy node into re-attestation.
	a := HardwareFingerprint{
		MotherboardUUID: "4C4C4544-0037-5A10-8054-B4C04F503733",
		GPUUUIDs:        []string{"GPU-aaaa1111-2222-3333-4444-555566667777", "GPU-bbbb1111-2222-3333-4444-555566667777"},
		PrimaryNICMAC:   "A4:BB:6D:11:22:33",
	}
	b := HardwareFingerprint{
		MotherboardUUID: "  4c4c4544-0037-5a10-8054-b4c04f503733 ",
		GPUUUIDs:        []string{"gpu-bbbb1111-2222-3333-4444-555566667777", "GPU-AAAA1111-2222-3333-4444-555566667777", ""},
		PrimaryNICMAC:   "a4:bb:6d:11:22:33",
	}
	if a.Hash() != b.Hash() {
		t.Fatalf("normalisation failed:\n a=%s\n b=%s", a.Hash(), b.Hash())
	}

	// A genuinely different GPU must change the hash.
	c := a
	c.GPUUUIDs = []string{"GPU-cccc1111-2222-3333-4444-555566667777"}
	if c.Hash() == a.Hash() {
		t.Fatal("a swapped GPU must change the fingerprint")
	}
}

// TestFingerprintNoDelimiterCollision: the length-delimited hash input must
// not let a value rearrangement produce the same digest.
func TestFingerprintNoDelimiterCollision(t *testing.T) {
	a := HardwareFingerprint{MotherboardUUID: "aa", PrimaryNICMAC: "bb:cc", GPUUUIDs: []string{"g1"}}
	b := HardwareFingerprint{MotherboardUUID: "aa\nbb", PrimaryNICMAC: "cc", GPUUUIDs: []string{"g1"}}
	if a.Hash() == b.Hash() {
		t.Fatal("fingerprint components must be unambiguously delimited")
	}
}

func TestFingerprintCompleteness(t *testing.T) {
	full := HardwareFingerprint{
		MotherboardUUID: "4C4C4544-0037-5A10-8054-B4C04F503733",
		GPUUUIDs:        []string{"GPU-aaaa"},
		PrimaryNICMAC:   "a4:bb:6d:11:22:33",
	}
	if !full.Complete() {
		t.Fatal("a fully populated fingerprint should be complete")
	}
	// Consumer boards that report a vendor placeholder must not be treated as
	// identity — otherwise every affected node in the fleet shares one.
	for _, placeholder := range []string{
		"00000000-0000-0000-0000-000000000000",
		"03000200-0400-0500-0006-000700080009",
		"Default string",
		"",
	} {
		p := full
		p.MotherboardUUID = placeholder
		if p.Complete() {
			t.Errorf("placeholder motherboard UUID %q must not count as complete", placeholder)
		}
		// It still hashes to something usable off the GPU and NIC.
		if p.Hash() == "" {
			t.Errorf("placeholder %q should still yield a hash", placeholder)
		}
	}
}

func TestFingerprintDiff(t *testing.T) {
	a := HardwareFingerprint{MotherboardUUID: "mb1", GPUUUIDs: []string{"g1", "g2"}, PrimaryNICMAC: "m1"}
	b := HardwareFingerprint{MotherboardUUID: "mb1", GPUUUIDs: []string{"g2", "g1"}, PrimaryNICMAC: "m1"}
	if d := a.Diff(b); len(d) != 0 {
		t.Fatalf("reordered GPUs are not a change, got %v", d)
	}
	c := HardwareFingerprint{MotherboardUUID: "mb2", GPUUUIDs: []string{"g1"}, PrimaryNICMAC: "m1"}
	d := a.Diff(c)
	if len(d) != 2 || d[0] != "motherboard_uuid" || d[1] != "gpu_uuids" {
		t.Fatalf("expected motherboard + gpu change, got %v", d)
	}
}

func TestCommandSignAndVerify(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cmd := &Command{
		ID:        "cmd-1",
		NodeID:    "node-a",
		Kind:      CmdSetSampleInterval,
		Args:      map[string]string{"interval_s": "30", "reason": "bandwidth"},
		IssuedAt:  now.UnixMilli(),
		ExpiresAt: now.Add(10 * time.Minute).UnixMilli(),
	}
	cmd.Sign("key-1", priv)

	if err := cmd.Verify(pub, "node-a", now, Phase1Commands); err != nil {
		t.Fatalf("valid command should verify: %v", err)
	}

	// Signature must cover args: flipping one must break verification.
	tampered := *cmd
	tampered.Args = map[string]string{"interval_s": "1", "reason": "bandwidth"}
	if err := tampered.Verify(pub, "node-a", now, Phase1Commands); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered args must fail, got %v", err)
	}

	// A command captured from one node's socket must not be executable on
	// another node. This is why NodeID is inside the signed body.
	if err := cmd.Verify(pub, "node-b", now, Phase1Commands); !errors.Is(err, ErrWrongNode) {
		t.Fatalf("cross-node replay must fail, got %v", err)
	}

	// A queued command that lands after its TTL is dropped, never executed.
	late := now.Add(11 * time.Minute)
	if err := cmd.Verify(pub, "node-a", late, Phase1Commands); !errors.Is(err, ErrCommandExpired) {
		t.Fatalf("expired command must fail, got %v", err)
	}

	// A different controller key must not pass.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := cmd.Verify(otherPub, "node-a", now, Phase1Commands); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong key must fail, got %v", err)
	}
}

// TestPhase2CommandsAreRejected: reboot_host and the workload commands are
// declared so the agent can reject them by name, but a correctly signed one
// must still not run in a phase-1 build.
func TestPhase2CommandsAreRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now()
	for _, kind := range []CommandKind{CmdRunWorkload, CmdStopWorkload, CmdDrain, CmdRebootHost} {
		cmd := &Command{ID: "c", NodeID: "n", Kind: kind, IssuedAt: now.UnixMilli(), ExpiresAt: now.Add(time.Minute).UnixMilli()}
		cmd.Sign("key-1", priv)
		if !kind.Declared() {
			t.Errorf("%s should be declared", kind)
		}
		if kind.ImplementedInPhase1() {
			t.Errorf("%s must not be implemented in phase 1", kind)
		}
		if err := cmd.Verify(pub, "n", now, Phase1Commands); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s must be refused by the phase-1 allowlist, got %v", kind, err)
		}
	}
}

func TestCredentialRoundTripAndTamper(t *testing.T) {
	ctrlPub, ctrlPriv, _ := ed25519.GenerateKey(nil)
	nodePub, nodePriv, _ := ed25519.GenerateKey(nil)
	now := time.Now()

	cred := &Credential{
		V: 1, NodeID: "node-a", SiteID: "site-1", NodePub: nodePub,
		FingerprintHash: "abc123", IssuedAt: now.UnixMilli(), KeyID: "ck-1",
	}
	encoded, err := cred.Encode(ctrlPriv)
	if err != nil {
		t.Fatal(err)
	}

	parsed, sig, err := ParseCredential(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.KeyID != "ck-1" {
		t.Fatalf("key id must be readable before verification, got %q", parsed.KeyID)
	}
	if err := parsed.Verify(ctrlPub, sig, now); err != nil {
		t.Fatalf("valid credential should verify: %v", err)
	}

	// Rewriting the node id inside the credential must invalidate it.
	parsed.NodeID = "node-b"
	if err := parsed.Verify(ctrlPub, sig, now); !errors.Is(err, ErrCredentialSig) {
		t.Fatalf("tampered credential must fail, got %v", err)
	}

	// Expiry is enforced.
	expired := &Credential{V: 1, NodeID: "n", NodePub: nodePub, IssuedAt: now.Add(-2 * time.Hour).UnixMilli(), ExpiresAt: now.Add(-time.Hour).UnixMilli(), KeyID: "ck-1"}
	enc2, _ := expired.Encode(ctrlPriv)
	p2, s2, _ := ParseCredential(enc2)
	if err := p2.Verify(ctrlPub, s2, now); !errors.Is(err, ErrCredentialExpired) {
		t.Fatalf("expired credential must fail, got %v", err)
	}

	for _, bad := range []string{"", "nope", "mnc1.only-two", "mnc2.aa.bb", "mnc1.!!!.bb"} {
		if _, _, err := ParseCredential(bad); err == nil {
			t.Errorf("ParseCredential(%q) should fail", bad)
		}
	}
	_ = nodePriv
}

// TestAuthenticatorIsNotABearerToken is the point of the whole credential
// design: holding the credential string is not enough to speak as the node.
func TestAuthenticatorIsNotABearerToken(t *testing.T) {
	nodePub, nodePriv, _ := ed25519.GenerateKey(nil)
	now := time.Now()

	auth := MakeAuthenticator("node-a", "GET", StreamPath, now, nodePriv)
	if err := VerifyAuthenticator(auth, "node-a", "GET", StreamPath, nodePub, now); err != nil {
		t.Fatalf("fresh authenticator should verify: %v", err)
	}

	// Captured from a telemetry POST, replayed against enrollment: must fail,
	// because method and path are signed.
	if err := VerifyAuthenticator(auth, "node-a", "POST", EnrollPath, nodePub, now); !errors.Is(err, ErrAuthSig) {
		t.Fatalf("cross-route replay must fail, got %v", err)
	}

	// Replayed later, outside the skew window.
	if err := VerifyAuthenticator(auth, "node-a", "GET", StreamPath, nodePub, now.Add(AuthSkew+time.Minute)); !errors.Is(err, ErrAuthStale) {
		t.Fatalf("stale authenticator must fail, got %v", err)
	}

	// A homeowner power-cut leaves the RTC behind; inside the window we still
	// accept it, because a node that cannot authenticate after an unplug is
	// exactly the failure this system exists to avoid.
	if err := VerifyAuthenticator(auth, "node-a", "GET", StreamPath, nodePub, now.Add(-4*time.Minute)); err != nil {
		t.Fatalf("clock drift inside the skew window must be tolerated: %v", err)
	}

	// Someone else's key must not pass.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := VerifyAuthenticator(auth, "node-a", "GET", StreamPath, otherPub, now); !errors.Is(err, ErrAuthSig) {
		t.Fatalf("wrong key must fail, got %v", err)
	}

	for _, bad := range []string{"", "no-dot", "abc.def", "123.!!!"} {
		if err := VerifyAuthenticator(bad, "node-a", "GET", StreamPath, nodePub, now); err == nil {
			t.Errorf("VerifyAuthenticator(%q) should fail", bad)
		}
	}
}
