# MeterNode

Software for a fleet of company-owned x86 machines with NVIDIA RTX PRO-class
GPUs, physically located in homeowners' residences. The homeowner supplies power
and internet; they do not administer the machine.

Phase 1 is **monitoring only**: enrollment, telemetry, storage, health scoring,
alerting, and an operator UI. Part of the MeterHome brand family.

```
proto/     the wire schema both sides import
console/   the control plane — Go, PostgreSQL/TimescaleDB, Redis
agent/     the host software — Go, single static binary, systemd
```

One repository, one Go module. The two binaries and the schema always ship from
the same commit, so a mismatch between them is a compile error rather than a
silent outage in someone's house. Go's `internal` rule still keeps the boundary
honest: `console/internal/...` is importable only from `console/...`, and
`agent/internal/...` only from `agent/...`.

## Quick start

```bash
make test          # every unit test in the repo
make dev           # Postgres/Timescale + Redis + the controller
make simulate      # simulated nodes enrol against it
make agent         # run the host agent locally, no root needed
make budget        # print the projected traffic per node
```

## Opening in VS Code

`.vscode/` is committed, so the repo is ready to work in as soon as it is
cloned. Accept the extension prompt (the Go extension is the one that matters)
and you get:

- **Run and Debug** — the controller, the simulator (normal and a 200-node
  bad-network run), the agent, and `meternodectl`, each with the environment a
  local dev stack needs already set. The agent's config is written by a
  pre-launch task, so it runs without root and without touching `/etc`.
- **Tasks** (⇧⌘P → *Run Task*) — every `make` target, including the ones that
  need the dev database and the one that prints the bandwidth accounting.
- Tests run with `-race`, a timeout that suits the agent's real sampling loops,
  and `staticcheck` on.

The `integration`-tagged store tests are excluded from analysis by default;
`settings.json` says which line to uncomment if you want gopls to see them.

## Why the design looks the way it does

Every unusual decision here traces back to one of six environmental
constraints. They are worth reading before deciding something looks
over-engineered:

1. **No inbound connectivity.** Consumer NAT, often CGNAT. No port forwarding,
   no UPnP, no static IPs, no homeowner router configuration of any kind. Every
   connection is agent-initiated, outbound, over TCP/443.
2. **Asymmetric, modest bandwidth.** 200–500 Mbps down / 10–35 Mbps up is
   typical; some nodes are 50/5. Upstream is the scarce resource.
3. **Metered links.** Many US residential plans cap around 1.2 TB/month.
   Control-plane traffic has to be a rounding error against that.
4. **Unreliable uptime.** Homeowners power-cycle, unplug, lose ISP service, and
   move furniture. Disconnection is normal operation, not an error condition.
5. **Untrusted LAN.** The node shares a network with the homeowner's personal
   devices. Neither may reach the other.
6. **Residential ISP terms.** Some prohibit commercial use. The traffic profile
   should look like a normal heavy consumer device, and per-node bandwidth is a
   first-class metric so that stays measurable rather than asserted.

## The bandwidth budget, as code

Enforced in the agent and asserted by `proto`'s `TestMonthlyBudget`:

| | |
| --- | --- |
| Metric sampling | every 10 s |
| Telemetry batch | every 60 s, zstd + MessagePack |
| Heartbeat | every 15 s, **under 100 bytes** |
| Target | **< 150 MB/month/node**, all control-plane traffic |
| Hard ceiling | 500 MB/month |

Measured today, printed by CI on every run:

```
1 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1322 B x 43200 = 54.5 MB; total 74.9 MB
2 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1466 B x 43200 = 60.4 MB; total 80.8 MB
4 GPU: heartbeat 124 B x 172800 = 20.4 MB; batch 1750 B x 43200 = 72.1 MB; total 92.5 MB
```

The headroom to 150 MB pays for events, command results, and reconnect
backfill. A change that pushes those numbers up fails the test.

## Status

| | console | agent |
| --- | --- | --- |
| M0 | ✅ scaffold, migrations, health, CI, compose | ✅ scaffold, config, systemd, .deb, CI |
| M1 | ✅ enrollment, credentials, fingerprint binding | ✅ host collectors, `meternodectl status` |
| M2 | WebSocket ingest, Timescale writes, simulator | NVML GPU collector (nvidia-smi source and degraded paths done) |
| M3 | operator UI — **blocked**, see below | enrollment, transport, SQLite buffer, backfill |
| M4 | health scoring, alerting | bandwidth accounting, adaptive degradation |
| M5 | command dispatch, agent releases | container supervision, isolation enforcement |
| M6 | — | self-update with rollback |

**The operator UI is blocked.** It inherits its design system from the existing
MeterHome RMS portal, which could not be read from the session that built this.
Rather than invent one, [`console/DESIGN-INHERITANCE.md`](console/DESIGN-INHERITANCE.md)
records the blocker, what will be extracted once the repository is reachable,
and three questions that need a decision rather than a guess.

## Decision records

`console/ADR/` and `agent/ADR/`. The ones to read first if you are trying to
understand why this is not shaped like ordinary infrastructure:

- [console 0002](console/ADR/0002-messagepack-zstd-over-protobuf.md) — why the
  heartbeat envelope is hand-encoded.
- [console 0003](console/ADR/0003-absent-is-not-zero.md) — why absent telemetry
  and zero telemetry must never collapse into each other.
- [console 0005](console/ADR/0005-fingerprint-change-never-auto-rebinds.md) —
  why a swapped GPU stops the line and asks a human.
- [console 0008](console/ADR/0008-recording-and-refusing-are-separate-transactions.md) —
  rejecting something and recording that you rejected it are different jobs.
- [agent 0002](agent/ADR/0002-collectors-behind-timeouts.md) — why every
  collector has its own timeout.
- [agent 0005](agent/ADR/0005-unprivileged-and-the-docker-group.md) — what the
  agent gives up to hold no capabilities, and the one caveat stated plainly.

## Security

[`agent/SECURITY.md`](agent/SECURITY.md) leads with what a compromised workload
can and cannot reach, and marks the phase-5 enforcement as
designed-but-not-yet-enforced rather than implying it already holds.

## Explicit non-goals for phase 1

Job scheduling, workload placement, customer-facing inference routing, billing,
homeowner payouts, marketplace, autoscaling, multi-tenant isolation guarantees.
The interfaces exist; the implementations are empty.
