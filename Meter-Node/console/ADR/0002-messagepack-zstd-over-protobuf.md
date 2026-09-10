# ADR 0002 — MessagePack + zstd for the wire schema, not protobuf

**Status:** accepted · **Date:** 2026-09-10

## Context

The shared contract allows "MessagePack or protobuf". The choice matters more
than usual because of the bandwidth budget: every node must stay under
150 MB/month of control-plane traffic against a residential cap of roughly
1.2 TB, and some ISPs prohibit commercial use, so the traffic profile has to
stay defensible as well as small.

The traffic is dominated by two things:

- A heartbeat every 15 s, forever. At that rate a single byte costs
  ~172 KB/month/node.
- A metric batch every 60 s carrying six samples of highly repetitive,
  keyed data.

## Decision

MessagePack for encoding, zstd for compression, with three refinements:

1. **Payload bodies use named maps.** Field names cost bytes, but the batch is
   zstd-compressed in bulk where repeated keys compress to almost nothing, and
   named fields make version skew trivial: an older agent simply omits a key.

2. **The envelope is a positional array, hand-encoded.** The envelope is not
   compressed (see 3), and named keys on it cost 33 bytes per frame — a third of
   the 100-byte heartbeat budget, or ~5.7 MB/month/node of pure key names.
   `Envelope` implements `EncodeMsgpack`/`DecodeMsgpack` by hand. Field order is
   API; fields may only be appended.

3. **Compression is per-envelope and explicit.** zstd's frame overhead (~13
   bytes) would inflate a sub-100-byte heartbeat, so batches compress and
   heartbeats do not, and the envelope carries which choice was made.

## Alternatives considered

**Protobuf.** Comparable on the wire, and its field-number scheme handles skew
well. Rejected on toolchain cost: it adds `protoc`, a code-generation step, and
a generated-code-in-git question to a two-repo project whose whole schema is
about 400 lines of Go. The compatibility rules we need (append-only, accept two
releases back) are enforced by tests here rather than by a compiler, and the
tests are ones we would have written anyway.

**JSON.** Rejected outright: roughly 3x the wire size before compression, and no
distinction between an absent GPU block and a zero-valued one — a distinction
this schema depends on (see ADR 0003).

**CBOR.** Broadly equivalent to MessagePack. MessagePack was chosen for the
maturity of `vmihailenco/msgpack` and its `CustomEncoder` seam, which is what
makes the hand-encoded envelope clean rather than a fork.

## Consequences

- The measured result: 75–93 MB/month/node for 1–4 GPUs, against a 150 MB
  target. `TestMonthlyBudget` asserts it and prints the accounting in CI.
- Reordering an envelope field is a breaking change that no compiler will catch.
  `TestSchemaSkewOldAgent` and `TestSchemaSkewNewAgent` guard both directions.
- The decompression path is bounded (`WithDecoderMaxMemory`) because the ingest
  gateway decompresses data from machines in houses we do not control.
- Non-Go consumers of the schema would need a MessagePack library. Acceptable:
  both consumers are ours and both are Go.
