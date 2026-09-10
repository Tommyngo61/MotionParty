# ADR 0005 — A changed hardware fingerprint never re-binds automatically

**Status:** accepted · **Date:** 2026-09-10

## Context

Node identity is bound to a hardware fingerprint: motherboard UUID + GPU
UUID(s) + primary NIC MAC, normalised and hashed. When a node presents a
fingerprint that does not match the one bound to it, there are two readings:

1. **Benign.** A GPU was replaced under warranty, a NIC was swapped, a
   motherboard was RMA'd. Routine.
2. **Serious.** The provisioning image was cloned onto a second machine, or
   someone is attempting to assume an enrolled node's identity.

From the controller's side these are indistinguishable. Auto-re-binding turns
case 2 into two machines sharing one identity, which corrupts every metric,
every alert, and every command routed to that identity — and does so silently.

## Decision

A fingerprint mismatch **never** re-binds. It:

1. Opens a `node_reattestations` row recording the bound hash, the presented
   hash, exactly which components changed, the presented inventory, and the
   source IP.
2. Quarantines the node. Not merely refuses it — until a human decides, we do
   not know that the machine reporting under this identity is ours, and a
   quarantined node must not start workloads.
3. Emits `identity.fingerprint_changed` at CRITICAL and writes an audit row.
4. Returns HTTP 409 with the changed field list, so a field tech standing in the
   utility room is told what actually happened rather than "unauthorized".

Retries reuse the pending row rather than stacking one per attempt — a node that
cannot enroll retries with backoff forever, and one row per attempt would bury
the operator queue.

## Identifying the node at all

A changed fingerprint means the obvious lookup misses. Identification runs in
descending order of confidence:

1. **A prior credential the controller signed.** The strongest claim, and the
   only one that survives a hardware change. It is a claim to be *verified*, not
   authorisation: one signed by an unknown key is ignored, not honoured
   (`TestForgedPriorCredentialIsIgnored`).
2. **An exact fingerprint match.** Nothing changed; a plain re-enrollment.
3. **A partial hardware match** — shared motherboard UUID, NIC MAC, or GPU UUID.
   This catches the case with nothing else to lean on: a node that was re-imaged
   (losing its credential) *and* had a part swapped. Without it, one physical
   machine enrolls as two node records.

A shared GPU UUID is the weakest signal, since a card really can be moved
between machines. That case still deserves a human look, so matching on it and
raising re-attestation is the right outcome either way.

## Consequences

- A legitimate GPU swap needs an operator to approve a re-attestation. Accepted:
  a hardware change on a machine in someone's house is a thing we should know
  about, and it is rare.
- A quarantined node keeps retrying and keeps being refused with the same
  actionable 409 until someone acts. The re-attestation queue is therefore a
  work queue with real urgency, and the operator UI (M3) must surface it
  prominently.
- `node_hardware.fingerprint_hash` carries a unique index, so even a bug in this
  logic cannot produce two nodes sharing a fingerprint.
- The token from a refused attempt is **not** consumed — the transaction rolls
  back — so a field tech can retry with it once the node is released.
