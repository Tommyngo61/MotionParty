package meternodeproto

import (
	"errors"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

// Kind discriminates what an Envelope's payload holds.
type Kind uint8

const (
	KindHeartbeat Kind = iota
	KindMetrics
	KindEvent
	KindCommandResult
)

var kindNames = map[Kind]string{
	KindHeartbeat:     "HEARTBEAT",
	KindMetrics:       "METRICS",
	KindEvent:         "EVENT",
	KindCommandResult: "COMMAND_RESULT",
}

func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("KIND(%d)", uint8(k))
}

// Valid reports whether k is a kind this schema version defines. Unknown kinds
// from a newer agent are rejected at the ingest gateway rather than ignored,
// so a rollout that gets ahead of the controller shows up as an error rate
// instead of as silently missing telemetry.
func (k Kind) Valid() bool {
	_, ok := kindNames[k]
	return ok
}

// ParseKind converts the wire-stable uppercase name back to a Kind. Used by
// operator tooling and tests; the wire itself carries the uint8.
func ParseKind(s string) (Kind, error) {
	for k, n := range kindNames {
		if n == s {
			return k, nil
		}
	}
	return 0, fmt.Errorf("meternode: unknown envelope kind %q", s)
}

// Envelope is the single frame type on the wire. Every message an agent sends
// is one Envelope, whether it arrives over the WebSocket or the HTTP batch
// fallback.
//
// Payload is left as opaque bytes so the ingest gateway can validate identity,
// sequence, and version before it spends CPU decoding a body — at 10k sockets
// per replica that ordering matters.
//
// # Why this one type is hand-encoded
//
// The envelope is serialised as a positional MessagePack ARRAY, not the named
// map used everywhere else in this package. That is not premature
// optimisation, it is arithmetic: the contract caps a heartbeat envelope at
// 100 bytes and sends one every 15 s forever. Named keys ("node_id", "sent_at",
// "payload", ...) cost 33 bytes per frame, which alone is a third of the
// budget and ~5.7 MB/month/node of pure key names. Payload bodies keep named
// maps, because they are zstd-compressed in bulk where repeated keys cost
// almost nothing.
//
// Field order is therefore API. Fields may only be APPENDED (a minor bump);
// reordering or removing one is a major bump. The decoder reads the array
// length and tolerates both a short array from an older agent and a long one
// from a newer agent it is willing to accept.
type Envelope struct {
	V       uint8  // schema version
	NodeID  string // uuid
	Seq     uint64 // monotonic per node, for gap detection
	SentAt  int64  // unix ms, agent clock
	Kind    Kind
	Payload []byte

	// C is the compression applied to Payload.
	//
	// This is not in the original schema sketch, which said only that batches
	// are "compressed (zstd)". It has to be explicit and per-envelope: zstd
	// costs ~13 bytes of frame overhead, which would inflate the sub-100-byte
	// heartbeat. So metric batches compress and heartbeats do not, and the
	// receiver has to be told which it got.
	C Compression
}

// envelopeFields is the number of array elements this build writes. Appending
// a field means bumping this and SchemaVersion's minor.
const envelopeFields = 7

// maxEnvelopeFields bounds how long an array from a newer agent may be before
// the decoder treats it as garbage rather than as forward compatibility.
const maxEnvelopeFields = 32

// EncodeMsgpack implements msgpack.CustomEncoder.
func (e *Envelope) EncodeMsgpack(enc *msgpack.Encoder) error {
	if err := enc.EncodeArrayLen(envelopeFields); err != nil {
		return err
	}
	if err := enc.EncodeUint8(e.V); err != nil {
		return err
	}
	if err := enc.EncodeString(e.NodeID); err != nil {
		return err
	}
	if err := enc.EncodeUint64(e.Seq); err != nil {
		return err
	}
	if err := enc.EncodeInt64(e.SentAt); err != nil {
		return err
	}
	if err := enc.EncodeUint8(uint8(e.Kind)); err != nil {
		return err
	}
	if err := enc.EncodeBytes(e.Payload); err != nil {
		return err
	}
	return enc.EncodeUint8(uint8(e.C))
}

// DecodeMsgpack implements msgpack.CustomDecoder.
//
// It decodes as many fields as both sides know about and skips any extra ones
// a newer agent appended, so a controller mid-rollout keeps ingesting from
// agents that are one schema minor ahead of it instead of dropping their
// telemetry on the floor.
func (e *Envelope) DecodeMsgpack(dec *msgpack.Decoder) error {
	n, err := dec.DecodeArrayLen()
	if err != nil {
		return err
	}
	if n < 0 || n > maxEnvelopeFields {
		return fmt.Errorf("meternode: envelope array length %d out of range", n)
	}

	// Each decode step is guarded by the array length so a truncated frame
	// yields a validation error rather than a panic or a silent zero value.
	steps := []func() error{
		func() (err error) { e.V, err = dec.DecodeUint8(); return },
		func() (err error) { e.NodeID, err = dec.DecodeString(); return },
		func() (err error) { e.Seq, err = dec.DecodeUint64(); return },
		func() (err error) { e.SentAt, err = dec.DecodeInt64(); return },
		func() error {
			k, err := dec.DecodeUint8()
			e.Kind = Kind(k)
			return err
		},
		func() (err error) { e.Payload, err = dec.DecodeBytes(); return },
		func() error {
			c, err := dec.DecodeUint8()
			e.C = Compression(c)
			return err
		},
	}
	for i := 0; i < n; i++ {
		if i < len(steps) {
			if err := steps[i](); err != nil {
				return fmt.Errorf("meternode: envelope field %d: %w", i, err)
			}
			continue
		}
		if err := dec.Skip(); err != nil {
			return fmt.Errorf("meternode: skip envelope field %d: %w", i, err)
		}
	}
	return nil
}

// Envelope validation errors. These are values rather than formatted strings
// so the gateway can count them by class without parsing messages.
var (
	ErrUnsupportedVersion = errors.New("meternode: unsupported schema version")
	ErrMissingNodeID      = errors.New("meternode: envelope has no node_id")
	ErrUnknownKind        = errors.New("meternode: unknown envelope kind")
	ErrNoTimestamp        = errors.New("meternode: envelope has no sent_at")
	ErrPayloadTooLarge    = errors.New("meternode: envelope payload exceeds MaxPayloadBytes")
)

// MaxPayloadBytes caps a single decompressed envelope payload.
//
// A 60 s batch at a 10 s sample interval is 6 samples; even a node with 8 GPUs
// and 12 mounts encodes well under 64 KiB. The cap exists so a malformed or
// hostile agent cannot make the gateway allocate without bound, and it is
// enforced on the decompressed size, not the wire size, because zstd bombs are
// the interesting case.
const MaxPayloadBytes = 1 << 20 // 1 MiB

// Validate performs the cheap, allocation-free checks the ingest gateway runs
// before it decodes a payload or touches the database.
//
// It deliberately does NOT check the signature or that node_id exists: those
// need the credential store, and live one layer up.
func (e *Envelope) Validate() error {
	if !SupportsVersion(e.V) {
		return fmt.Errorf("%w: got v%d, accept v%d..v%d", ErrUnsupportedVersion, e.V, MinSupportedVersion, SchemaVersion)
	}
	if e.NodeID == "" {
		return ErrMissingNodeID
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: %d", ErrUnknownKind, uint8(e.Kind))
	}
	if e.SentAt <= 0 {
		return ErrNoTimestamp
	}
	if len(e.Payload) > MaxPayloadBytes {
		return fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, len(e.Payload))
	}
	return nil
}

// Heartbeat is the payload of a KindHeartbeat envelope.
//
// The whole envelope must stay under 100 bytes on the wire (see
// TestHeartbeatFitsBudget), because it goes out every 15 s forever. At 15 s
// intervals, 100 bytes costs ~17 MB/month/node before compression — already a
// tenth of the whole control-plane budget, so nothing may be added here
// casually.
type Heartbeat struct {
	// UptimeS is the agent process uptime, not host uptime. A resetting value
	// here with a stable BootID means the agent is crash-looping.
	UptimeS uint32 `msgpack:"up"`
	// ClockSkewMS is the agent's estimate of its own clock error against the
	// controller, signed. Residential machines drift and some lose RTC across
	// power cuts.
	ClockSkewMS int32 `msgpack:"skew"`
	// Degraded is set when the agent has an active self-reported problem (a
	// collector failing, bandwidth cap approached). It is a hint for
	// prioritisation only: the controller derives real health from telemetry,
	// never from a status the agent asserts.
	Degraded bool `msgpack:"deg,omitempty"`
}
