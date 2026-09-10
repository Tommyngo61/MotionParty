# proto — the MeterNode wire schema

The MeterNode wire schema. Imported by both [`console/`](../console)
(the controller) and [`agent/`](../agent). Neither side hand-rolls a
struct that crosses the network.

It exists as its own module for one reason: schema drift between a control
plane and a fleet of self-updating agents in other people's houses is silent
until it is an outage. A shared module makes a mismatch a compile error.

## What's in here

| File | Contents |
| --- | --- |
| `version.go` | Schema version and the compatibility window a controller must honour |
| `envelope.go` | `Envelope`, `Heartbeat`, and the hand-rolled positional encoding |
| `metrics.go` | `MetricBatch`, `Sample`, and the per-subsystem metric groups |
| `event.go` | `Event`, `Severity`, and the stable event-code catalogue |
| `command.go` | `Command`, `CommandResult`, kinds, allowlist, ed25519 signing |
| `credential.go` | Node credential format and per-request proof of possession |
| `fingerprint.go` | Hardware fingerprint normalisation and hashing |
| `enroll.go` | `POST /v1/enroll` request/response types |
| `codec.go` | MessagePack + zstd, with a decompression bound |
| `budget.go` | The control-plane bandwidth budget, as code |

## Three decisions worth knowing before you edit anything

**The envelope is a positional array, not a named map.** The contract caps a
heartbeat at 100 bytes and sends one every 15 seconds forever. Named keys cost
33 bytes a frame — a third of the budget spent on the letters in `"payload"`.
So `Envelope` implements `EncodeMsgpack`/`DecodeMsgpack` by hand and **field
order is API**. Append fields; never reorder or remove one. Payload bodies keep
named maps, because they are zstd-compressed in bulk where repeated keys are
nearly free.

**Absent is not zero.** `Sample`'s field groups are pointers. A node whose
NVIDIA driver is mid-upgrade reports no GPU block at all, and that must not
decode as a GPU sitting idle at 0 W — one is a maintenance window, the other is
a dead card. `TestAbsentIsNotZero` pins this.

**`Command` and `Credential` carry a node id inside the signed body.** Without
it, a signed command captured from one node's socket is a valid signed command
for every node in the fleet. `TestCommandSignAndVerify` pins the cross-node
replay case.

## The budget is a test, not a comment

`TestMonthlyBudget` encodes a full month of traffic the way the agent actually
sends it and asserts the total lands under the 150 MB target:

```
1 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1322 B x 43200 = 54.5 MB; total 74.9 MB
2 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1466 B x 43200 = 60.4 MB; total 80.8 MB
4 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1750 B x 43200 = 72.1 MB; total 92.5 MB
```

The remaining headroom to 150 MB is what pays for events, command results, and
reconnect backfill. If a change to this package pushes those numbers up, the
test fails and the change needs a better reason than convenience.

## Compatibility rules

- Metric names are stable and `snake_case`.
- Adding a field is a **minor** bump. Removing or retyping one is a **major** bump.
- A controller accepts envelopes from agents down to `MinSupportedVersion`,
  which must lag by at least two releases. Nodes update on their own schedule
  in houses we do not control; the controller always talks to a spread of
  versions at once.

`TestSchemaSkewOldAgent` and `TestSchemaSkewNewAgent` cover both directions.

## Usage

```go
env, err := meternodeproto.NewEnvelope(nodeID, seq, time.Now().UnixMilli(),
    meternodeproto.KindMetrics, batch)   // compresses if it is worth it
wire, err := meternodeproto.MarshalEnvelope(env)
```

```go
env, err := meternodeproto.UnmarshalEnvelope(wire) // validates the header only
// ... check identity and seq before spending CPU on the body ...
var batch meternodeproto.MetricBatch
err = env.DecodePayload(&batch)
```

## Test

```
go test ./...
go test -v -run TestMonthlyBudget ./...   # prints the byte accounting above
```
