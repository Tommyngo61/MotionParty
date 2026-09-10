package meternodeproto

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	batch := MetricBatch{Samples: []Sample{{T: 1730000000000, CPU: &CPU{UtilPct: 42.5, FreqMHz: 4200}}}}
	env, err := NewEnvelope("node-1", 7, 1730000000123, KindMetrics, batch)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalEnvelope(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalEnvelope(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeID != "node-1" || got.Seq != 7 || got.SentAt != 1730000000123 || got.Kind != KindMetrics {
		t.Fatalf("header mismatch: %+v", got)
	}
	var back MetricBatch
	if err := got.DecodePayload(&back); err != nil {
		t.Fatal(err)
	}
	if len(back.Samples) != 1 || back.Samples[0].CPU.UtilPct != 42.5 {
		t.Fatalf("payload mismatch: %+v", back)
	}
}

// TestSchemaSkewOldAgent is the compatibility promise: a controller built at
// v1 must keep ingesting from an agent that predates a field being added.
// Simulated by encoding a SHORT envelope array, which is exactly what an older
// agent's encoder emits.
func TestSchemaSkewOldAgent(t *testing.T) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	// A hypothetical v1.0 agent that predates the compression field: 6
	// elements instead of 7.
	if err := enc.EncodeArrayLen(6); err != nil {
		t.Fatal(err)
	}
	for _, f := range []func() error{
		func() error { return enc.EncodeUint8(1) },
		func() error { return enc.EncodeString("legacy-node") },
		func() error { return enc.EncodeUint64(99) },
		func() error { return enc.EncodeInt64(1730000000000) },
		func() error { return enc.EncodeUint8(uint8(KindHeartbeat)) },
		func() error { raw, _ := Marshal(Heartbeat{UptimeS: 5}); return enc.EncodeBytes(raw) },
	} {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	got, err := UnmarshalEnvelope(buf.Bytes())
	if err != nil {
		t.Fatalf("controller must accept a short envelope from a lagging agent: %v", err)
	}
	if got.C != CompressionNone {
		t.Fatalf("absent compression field must default to none, got %s", got.C)
	}
	var hb Heartbeat
	if err := got.DecodePayload(&hb); err != nil || hb.UptimeS != 5 {
		t.Fatalf("payload from lagging agent: %v %+v", err, hb)
	}
}

// TestSchemaSkewNewAgent is the other direction: an agent one minor ahead
// appends a field. The controller ignores what it does not know rather than
// dropping the node's telemetry mid-rollout.
func TestSchemaSkewNewAgent(t *testing.T) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	if err := enc.EncodeArrayLen(9); err != nil {
		t.Fatal(err)
	}
	_ = enc.EncodeUint8(1)
	_ = enc.EncodeString("future-node")
	_ = enc.EncodeUint64(4)
	_ = enc.EncodeInt64(1730000000000)
	_ = enc.EncodeUint8(uint8(KindHeartbeat))
	raw, _ := Marshal(Heartbeat{UptimeS: 11})
	_ = enc.EncodeBytes(raw)
	_ = enc.EncodeUint8(uint8(CompressionNone))
	_ = enc.EncodeString("a-field-we-do-not-know")
	_ = enc.EncodeUint64(1234)

	got, err := UnmarshalEnvelope(buf.Bytes())
	if err != nil {
		t.Fatalf("controller must tolerate appended fields: %v", err)
	}
	var hb Heartbeat
	if err := got.DecodePayload(&hb); err != nil || hb.UptimeS != 11 {
		t.Fatalf("payload: %v %+v", err, hb)
	}
}

func TestEnvelopeValidate(t *testing.T) {
	base := func() Envelope {
		return Envelope{V: SchemaVersion, NodeID: "n", Seq: 1, SentAt: 1, Kind: KindHeartbeat}
	}
	cases := []struct {
		name string
		mut  func(*Envelope)
		want error
	}{
		{"ok", func(*Envelope) {}, nil},
		{"version 0", func(e *Envelope) { e.V = 0 }, ErrUnsupportedVersion},
		{"version ahead", func(e *Envelope) { e.V = SchemaVersion + 1 }, ErrUnsupportedVersion},
		{"no node", func(e *Envelope) { e.NodeID = "" }, ErrMissingNodeID},
		{"bad kind", func(e *Envelope) { e.Kind = Kind(200) }, ErrUnknownKind},
		{"no timestamp", func(e *Envelope) { e.SentAt = 0 }, ErrNoTimestamp},
		{"huge payload", func(e *Envelope) { e.Payload = make([]byte, MaxPayloadBytes+1) }, ErrPayloadTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := base()
			c.mut(&e)
			err := e.Validate()
			if c.want == nil && err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// TestZstdBombIsBounded: a hostile agent must not be able to turn a few KB of
// wire data into an unbounded allocation in the ingest gateway.
func TestZstdBombIsBounded(t *testing.T) {
	enc, err := getEncoder()
	if err != nil {
		t.Fatal(err)
	}
	bomb := enc.EncodeAll(make([]byte, 64<<20), nil)
	t.Logf("64 MiB of zeroes compresses to %d bytes", len(bomb))
	e := &Envelope{V: SchemaVersion, NodeID: "n", Seq: 1, SentAt: 1, Kind: KindMetrics, Payload: bomb, C: CompressionZstd}
	var batch MetricBatch
	if err := e.DecodePayload(&batch); err == nil {
		t.Fatal("expected the decompression bound to reject a zstd bomb")
	}
}

func TestUnknownCompressionIsAnError(t *testing.T) {
	e := &Envelope{V: SchemaVersion, NodeID: "n", Seq: 1, SentAt: 1, Kind: KindMetrics, Payload: []byte{1}, C: Compression(9)}
	var batch MetricBatch
	if err := e.DecodePayload(&batch); !errors.Is(err, ErrCompressionUnknown) {
		t.Fatalf("got %v, want ErrCompressionUnknown", err)
	}
}

// TestAbsentIsNotZero pins the reason the Sample field groups are pointers:
// a node mid-driver-upgrade reporting no GPU block must be distinguishable
// from a GPU sitting idle at 0 W.
func TestAbsentIsNotZero(t *testing.T) {
	env, err := NewEnvelope("n", 1, 1, KindMetrics, MetricBatch{Samples: []Sample{{T: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := MarshalEnvelope(env)
	got, err := UnmarshalEnvelope(wire)
	if err != nil {
		t.Fatal(err)
	}
	var back MetricBatch
	if err := got.DecodePayload(&back); err != nil {
		t.Fatal(err)
	}
	if back.Samples[0].GPUs != nil || back.Samples[0].CPU != nil {
		t.Fatal("an absent collector must decode as nil, not as a zero-valued struct")
	}
}

func TestKindAndSeverityNamesAreStable(t *testing.T) {
	// These strings appear in alert rules, dashboards, and runbooks. If this
	// test fails, something renamed API.
	for k, want := range map[Kind]string{
		KindHeartbeat: "HEARTBEAT", KindMetrics: "METRICS",
		KindEvent: "EVENT", KindCommandResult: "COMMAND_RESULT",
	} {
		if k.String() != want {
			t.Errorf("kind %d = %q, want %q", uint8(k), k.String(), want)
		}
		if got, err := ParseKind(want); err != nil || got != k {
			t.Errorf("ParseKind(%q) = %v, %v", want, got, err)
		}
	}
	for s, want := range map[Severity]string{
		SeverityInfo: "INFO", SeverityWarn: "WARN",
		SeverityError: "ERROR", SeverityCritical: "CRITICAL",
	} {
		if s.String() != want {
			t.Errorf("severity %d = %q, want %q", uint8(s), s.String(), want)
		}
		if got, err := ParseSeverity(want); err != nil || got != s {
			t.Errorf("ParseSeverity(%q) = %v, %v", want, got, err)
		}
	}
}

func TestEventCodesAreSnakeCaseAndDotted(t *testing.T) {
	codes := []string{
		CodeGPUECCError, CodeGPUThrottled, CodeGPUFellOffBus, CodeGPUPCIeDowntrained,
		CodeGPUDriverMissing, CodeGPUDriverUpgrading, CodeHostThermalThrottle,
		CodeHostDiskSMARTFail, CodeHostDiskFull, CodeHostClockSkew, CodeHostBooted,
		CodeAgentStarted, CodeAgentStopping, CodeAgentPanic, CodeAgentCrashLoop,
		CodeAgentConfigReload, CodeAgentUpdateStaged, CodeAgentUpdateApplied,
		CodeAgentRolledBack, CodeCollectorFailed, CodeNetCapApproaching,
		CodeNetCapExceeded, CodeNetDegradedSampling, CodeNetCaptivePortal,
		CodeNetUpstreamLow, CodeNetReconnected, CodeEnrollCompleted, CodeEnrollFailed,
		CodeFingerprintChanged, CodeCredentialRevoked, CodeCommandRejected,
		CodeCommandFailed, CodeIsolationInvariantFailed,
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Errorf("duplicate event code %q", c)
		}
		seen[c] = true
		if c != strings.ToLower(c) || strings.ContainsAny(c, " -") {
			t.Errorf("event code %q must be lowercase snake_case", c)
		}
		if !strings.Contains(c, ".") {
			t.Errorf("event code %q must be subsystem-qualified", c)
		}
	}
}
