# MeterNode Console — Architecture

The control plane for a fleet of company-owned GPU nodes living in private
residences. Phase 1 is monitoring: enrollment, telemetry ingest, storage, health
scoring, alerting, and an operator UI.

This document explains the shape of the system and why it is that shape. Every
significant decision has an ADR in [`ADR/`](ADR/); this is the map, those are
the reasons.

## The constraints that drive everything

Nothing here is a normal infrastructure problem, because the machines are not in
a datacenter:

| Constraint | What it forces |
| --- | --- |
| No inbound connectivity (consumer NAT, often CGNAT) | Every connection is agent-initiated over TCP/443. The controller can never dial a node. |
| Asymmetric bandwidth: 200–500↓ / 10–35↑ Mbps, some 50/5 | Upstream is the scarce resource. Telemetry is batched and compressed; backfill is rate-limited. |
| Metered links, ~1.2 TB/month caps | A hard control-plane budget: 150 MB/month/node target, 500 MB ceiling, asserted by tests. |
| Homeowners power-cycle and unplug machines | Disconnection is normal operation, not an error. Reconnect is exponential backoff with full jitter. |
| Untrusted LAN shared with personal devices | Workload isolation is enforced on the node and verified at startup (see `agent/`). |
| Some residential ISPs prohibit commercial use | Per-node bandwidth is a first-class metric so the traffic profile stays measurable. |

## Layout

| | |
| --- | --- |
| [`proto/`](../proto) | the wire schema — imported by both sides |
| [`console/`](.) | this directory: the controller |
| [`agent/`](../agent) | the host-side supervisor |

All three live in one repository and one Go module, because schema drift
between a control plane and a fleet of self-updating agents is silent until it
is an outage — and in one module a mismatch is a compile error caught by the
same `go build` that would have shipped it.

Go's `internal` rule keeps the boundary honest even so: `console/internal/...`
is importable only from `console/...`, and `agent/internal/...` only from
`agent/...`. Neither can reach into the other; both share `proto/`.

## Layout

```
cmd/controller/        the single binary: migrate + serve
cmd/simulator/         fake agents, for the failure modes that only appear under load
internal/
  api/                 HTTP surface: routing, auth, middleware, handlers
  config/              environment configuration and its validation
  enroll/              node identity: tokens, credentials, fingerprints, re-attestation
  keys/                the controller's command-signing identity
  logging/             structured logging
  model/               persistent domain types; depends on nothing
  store/               the only package that knows about PostgreSQL
migrations/            goose SQL, embedded into the binary
deploy/                Dockerfile and the development stack
ADR/                   decision records
```

The dependency rule is one-directional and load-bearing: **`internal/store` is
the only package that imports a database driver.** Everything else talks to an
interface its own package defines — `enroll.Repo`, `api.PrincipalStore`,
`api.Health` — and `store` satisfies them. That is what makes the interesting
decisions unit-testable: what happens when a fingerprint changes, what a viewer
may do, whether a token can be replayed. Those tests run in milliseconds with no
container.

The fake repository in those tests **does** roll back on error, and that matters:
an earlier version did not, and the gap hid a real bug — the re-attestation
record, the node quarantine, and the CRITICAL event were written inside the
enrollment transaction, which then rolled back because the enrollment was
refused, silently discarding the entire operator-review flow. A fake whose
transactions always commit is not a simplification, it is a blind spot.

What a fake still cannot test — unique constraints, row locking under
concurrency, real rollback semantics — is covered by
`internal/store/integration_test.go` behind a build tag, against a real
TimescaleDB.

## Request paths

### Enrollment — `POST /v1/enroll`

Necessarily unauthenticated: a node has no credential yet. Its authentication
*is* the one-time token in the body, and that is why the endpoint has a 64 KiB
body cap — it is the only place an unauthenticated caller can make the
controller allocate.

The whole exchange is one transaction. Consuming a token, creating a node,
binding hardware, and issuing a credential either all happen or none do: a
partial enrollment strands a machine in a homeowner's house with a spent token
and no identity, and fixing that costs a site visit.

Four ways a node arrives:

1. **New hardware, valid token** → create the node, bind the fingerprint, issue.
2. **Known hardware, fingerprint matches** → re-issue. The normal recovery path
   for a re-imaged node or a lost credential file. Revokes the old credential in
   the same transaction, so a revoked machine cannot keep reporting under it.
3. **Known node, fingerprint CHANGED** → refuse, quarantine, open a
   re-attestation for a human. Never re-bind silently. See ADR 0005.
4. **Known hardware bound to a different node** → refuse. The clone case.

Identification runs in descending order of confidence: a controller-signed prior
credential, then an exact fingerprint match, then a partial hardware match
(shared motherboard, NIC, or GPU). The last one catches a node that was
re-imaged *and* had a part swapped — without it, one physical machine enrolls as
two node records.

### Telemetry — `wss://<DOMAIN>/v1/stream` (M2)

One long-lived WebSocket per node, multiplexed for telemetry up and commands
down, with `POST /v1/telemetry` as the batch fallback for networks that will not
keep a socket open (middleboxes and captive portals are real). Authentication is
the credential plus a per-request signature; see ADR 0004.

### Operator API — `/v1/admin/*`

Bearer-authenticated, role-gated (`viewer` / `operator` / `admin`), OIDC-ready.
Every operator-initiated action writes an `audit_log` row with actor, node,
command, and result.

## Storage

PostgreSQL 16 with TimescaleDB, which is a hard requirement rather than an
optimisation — every screen in the operator UI is a time-bucketed aggregate. See
ADR 0007.

| Table | Kind | Retention |
| --- | --- | --- |
| `sites`, `nodes`, `node_hardware` | regular | — |
| `enrollment_tokens`, `node_credentials`, `node_reattestations` | regular | — |
| `controller_keys`, `operators`, `api_tokens`, `audit_log` | regular | — |
| `heartbeats` | hypertable | 7 days |
| `metric_samples`, `gpu_samples`, `disk_samples`, `container_samples` | hypertable | 30 days |
| `metric_samples_1m` / `_5m` / `_1h`, `gpu_samples_1m` / `_1h` | continuous aggregate | 14d / 90d / 2y |
| `alert_rules`, `alerts`, `notification_sinks`, `maintenance_windows` | regular | — |
| `commands`, `command_results`, `agent_releases`, `agent_rollouts` | regular | — |

GPU samples get their own hypertable rather than a JSONB column because GPU
telemetry is the point of this product: "every node whose GPU throttled in the
last hour" must be an index scan.

Migrations are goose SQL embedded into the binary. The controller migrates
itself — there is no separate migration image to drift out of sync, and no
hand-run SQL.

## Two things that are deliberately separate

**Lifecycle vs. health.** `nodes.lifecycle` is the administrative state an
operator sets (`unenrolled` / `enrolled` / `quarantined` / `retired`). Health is
*derived* from telemetry by the controller and never stored as something the
agent asserts — an agent that is wedged or crash-looping is exactly the one
whose self-assessment is worth least. See ADR 0006.

**Liveness vs. readiness.** `/healthz` never touches the database; `/readyz`
does. Conflating them means a database blip has the orchestrator restart every
replica, dropping every agent socket at once — and the whole fleet reconnects
together, which is precisely the storm the transport design exists to avoid.

## Security posture

- Enrollment tokens are stored as SHA-256 only. The raw token exists once, in
  the mint response. A database dump cannot be turned into a fleet enrollment.
- The token alphabet excludes `I`, `L`, `O`, and `U` — a field tech reading a
  token off a printed sheet in a dim utility room should not be able to confuse
  `0` with `O`.
- Node credentials bind a public key the agent generated and never transmitted,
  and every request carries a fresh signature. The credential is not a bearer
  token. See ADR 0004.
- Commands are signed by the controller and carry the target node id **inside**
  the signed body; without that, a command captured from one node's socket is
  valid for every node in the fleet.
- The controller's signing key must survive restarts or every enrolled node
  rejects its commands. Generation is refused outside dev, and the dev key is
  persisted.
- The zstd decompression path is bounded. The gateway decompresses data from
  machines in houses we do not control.

## Milestones

| | | |
| --- | --- | --- |
| **M0** | repo scaffold, migrations, health endpoint, CI, compose | ✅ done |
| **M1** | enrollment, credential issuance, fingerprint binding | ✅ done |
| M2 | WebSocket ingest, Timescale writes, the full agent simulator | next |
| M3 | node list and node detail UI with live charts | blocked — see [DESIGN-INHERITANCE.md](DESIGN-INHERITANCE.md) |
| M4 | health scoring, alert rules, notification sinks | |
| M5 | command dispatch, agent release channels | |

The schema for M2–M5 already exists in the migrations, and the wire types
already exist in `proto`. The tables are not speculative: getting the
schema wrong later means migrating a hypertable with a month of fleet data in
it, which is a genuinely different cost from adding a column now.

## Explicit non-goals for phase 1

Job scheduling, workload placement, customer-facing inference routing, billing,
homeowner payouts, marketplace, autoscaling, multi-tenant isolation guarantees.
The interfaces exist; the implementations are empty.
