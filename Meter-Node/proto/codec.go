package meternodeproto

import (
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

// Compression names how an Envelope's payload is compressed.
type Compression uint8

const (
	CompressionNone Compression = 0
	CompressionZstd Compression = 1
)

func (c Compression) String() string {
	switch c {
	case CompressionNone:
		return "none"
	case CompressionZstd:
		return "zstd"
	default:
		return fmt.Sprintf("COMPRESSION(%d)", uint8(c))
	}
}

// CompressThresholdBytes is the payload size at which compression starts
// paying for itself.
//
// Below roughly this size zstd's frame overhead makes the result bigger, and
// the two things the agent sends most often — heartbeats and single events —
// are both below it. A metric batch is always well above it.
const CompressThresholdBytes = 256

// ErrCompressionUnknown is returned when an envelope names a compression
// algorithm this build does not implement. That means an agent is ahead of its
// controller, so it is an error rather than a fallback to raw bytes.
var ErrCompressionUnknown = errors.New("meternode: unknown payload compression")

// zstd level 3 is the default and the right pick here: it gets within a few
// percent of the higher levels on this data (highly repetitive keyed maps)
// while costing an order of magnitude less CPU. Nodes are GPU boxes but their
// CPU belongs to the workload, not to us.
var (
	encOnce sync.Once
	encoder *zstd.Encoder
	encErr  error

	decOnce sync.Once
	decoder *zstd.Decoder
	decErr  error
)

func getEncoder() (*zstd.Encoder, error) {
	encOnce.Do(func() {
		encoder, encErr = zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedDefault),
			zstd.WithEncoderConcurrency(1),
		)
	})
	return encoder, encErr
}

func getDecoder() (*zstd.Decoder, error) {
	decOnce.Do(func() {
		// WithDecoderMaxMemory is the zstd-bomb guard: a hostile or corrupt
		// agent must not be able to make the ingest gateway allocate an
		// unbounded buffer from a few kilobytes of wire data.
		decoder, decErr = zstd.NewReader(nil,
			zstd.WithDecoderMaxMemory(MaxPayloadBytes),
			zstd.WithDecoderConcurrency(1),
		)
	})
	return decoder, decErr
}

// Marshal encodes v as MessagePack.
func Marshal(v any) ([]byte, error) { return msgpack.Marshal(v) }

// Unmarshal decodes MessagePack into v.
func Unmarshal(b []byte, v any) error { return msgpack.Unmarshal(b, v) }

// SetPayload encodes v into e.Payload, compressing when it is worth it and
// recording which choice was made.
func (e *Envelope) SetPayload(v any) error {
	raw, err := Marshal(v)
	if err != nil {
		return fmt.Errorf("meternode: encode payload: %w", err)
	}
	if len(raw) < CompressThresholdBytes {
		e.Payload, e.C = raw, CompressionNone
		return nil
	}
	enc, err := getEncoder()
	if err != nil {
		return fmt.Errorf("meternode: zstd encoder: %w", err)
	}
	packed := enc.EncodeAll(raw, make([]byte, 0, len(raw)/3))
	// Incompressible payloads do happen (already-compressed diagnostics blobs
	// in a command result). Shipping the larger of the two would be silly.
	if len(packed) >= len(raw) {
		e.Payload, e.C = raw, CompressionNone
		return nil
	}
	e.Payload, e.C = packed, CompressionZstd
	return nil
}

// DecodePayload decompresses and decodes e.Payload into v.
func (e *Envelope) DecodePayload(v any) error {
	raw, err := e.rawPayload()
	if err != nil {
		return err
	}
	if err := Unmarshal(raw, v); err != nil {
		return fmt.Errorf("meternode: decode payload: %w", err)
	}
	return nil
}

func (e *Envelope) rawPayload() ([]byte, error) {
	switch e.C {
	case CompressionNone:
		return e.Payload, nil
	case CompressionZstd:
		dec, err := getDecoder()
		if err != nil {
			return nil, fmt.Errorf("meternode: zstd decoder: %w", err)
		}
		raw, err := dec.DecodeAll(e.Payload, nil)
		if err != nil {
			return nil, fmt.Errorf("meternode: decompress payload: %w", err)
		}
		if len(raw) > MaxPayloadBytes {
			return nil, fmt.Errorf("%w: %d bytes decompressed", ErrPayloadTooLarge, len(raw))
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("%w: %d", ErrCompressionUnknown, uint8(e.C))
	}
}

// MarshalEnvelope encodes a complete frame. One frame is one WebSocket binary
// message, or one element of an HTTP batch body.
func MarshalEnvelope(e *Envelope) ([]byte, error) { return Marshal(e) }

// UnmarshalEnvelope decodes a frame and runs the cheap validations. It does
// not decode the payload — the gateway checks identity and sequence first.
func UnmarshalEnvelope(b []byte) (*Envelope, error) {
	var e Envelope
	if err := Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("meternode: decode envelope: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return &e, nil
}

// Batch is the body of the HTTP fallback POST /v1/telemetry. The WebSocket
// carries one envelope per message; the fallback carries several per request
// because a request costs a TLS round trip and those are expensive on a
// high-latency residential link.
type Batch struct {
	Envelopes []Envelope `msgpack:"envelopes"`
}

// NewEnvelope builds an envelope of the given kind with the payload encoded.
func NewEnvelope(nodeID string, seq uint64, sentAtMS int64, kind Kind, payload any) (*Envelope, error) {
	e := &Envelope{V: SchemaVersion, NodeID: nodeID, Seq: seq, SentAt: sentAtMS, Kind: kind}
	if err := e.SetPayload(payload); err != nil {
		return nil, err
	}
	return e, nil
}
