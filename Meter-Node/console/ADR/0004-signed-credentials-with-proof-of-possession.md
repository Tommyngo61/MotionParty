# ADR 0004 — Signed node credentials with proof of possession, not mTLS

**Status:** accepted · **Date:** 2026-09-10

## Context

The shared contract allows either a per-node client certificate (mTLS) or a
long-lived signed credential. Whichever we pick has to survive the environment:
a node that has been unplugged for three weeks must be able to come back and
report without a human touching it, and a machine sitting in a stranger's house
is physically accessible to someone we have not vetted.

## Decision

A controller-signed credential that binds a public key **the agent generated
locally and never transmitted**, plus a per-request signature made with the
matching private key.

- At enrollment the agent generates an ed25519 keypair. Only the public half is
  sent.
- The controller returns `mnc1.<body>.<signature>`, where the body binds
  `node_id`, `node_pub`, the hardware fingerprint hash, and the signing key id.
- Every subsequent request carries `X-MeterNode-Auth: <unix_ms>.<signature>`
  over a canonical string that includes the node id, HTTP method, path, and
  timestamp.

The credential is therefore **not a bearer token**. Capturing it from a log, a
proxy, or a support bundle does not let anyone speak as the node.

The timestamp window is 5 minutes, which is generous on purpose: residential
machines lose their RTC across power cuts and come back minutes off until NTP
catches up. A node that cannot authenticate because the homeowner unplugged it
is exactly the failure this system exists to avoid, and replay inside the window
is bounded by telemetry being idempotent on `seq`.

## Alternatives considered

**mTLS with per-node client certificates.** The stronger option, and the likely
phase-2 destination. Rejected for phase 1 on operational cost: it needs a CA, a
revocation mechanism that works for nodes that are offline when we revoke
(CRL/OCSP are both awkward here), a renewal path that cannot strand a node, and
TLS-terminator configuration that varies by deployment. The credential scheme
above gets most of the security properties with none of that, and the
`Signer` interface is already the seam a KMS or CA integration slots into.

**A plain bearer token.** Rejected. The credential file sits on a machine in a
stranger's house and gets copied into support bundles.

## Consequences

- Revocation is a database check on the controller, not a certificate
  mechanism. Cheap and immediate, but it means an ingest replica must be able to
  reach the credential store — a cached negative is planned for M2.
- Key rotation is supported: the controller holds several public keys, and an
  agent pins the whole set at enrollment. `trustRetiredKeys` loads
  previously-active keys so a node that has been offline through a rotation
  still verifies.
- The controller's signing key must survive restarts, or every enrolled node
  rejects its commands. `LoadOrCreateSigningKey` persists the dev key and
  `config.Validate` refuses a generated key outside dev.
- Signing is behind an interface, so an HSM or KMS that will sign a message but
  never hand over a private key drops in without touching `internal/enroll`.
