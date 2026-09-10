# MeterNode

Software for a fleet of company-owned x86 machines with NVIDIA RTX PRO-class
GPUs, physically located in homeowners' residences. The homeowner supplies power
and internet; they do not administer the machine.

Phase 1 is **monitoring only**: enrollment, telemetry, storage, health scoring,
alerting, and an operator UI. Part of the MeterHome brand family.

## The three components

| | | |
| --- | --- | --- |
| [`meternode-proto`](meternode-proto/) | The shared wire schema | Imported by both sides. Neither hand-rolls a struct that crosses the network. |
| [`meternode-console`](meternode-console/) | The control plane | Go + chi, PostgreSQL/TimescaleDB, Redis. Owns node identity, telemetry storage, health scoring, alerting, and the operator UI. |
| [`meternode-agent`](meternode-agent/) | The host software | Go, single static binary, systemd. Owns telemetry, container lifecycle, and self-update. |

Three separate Go modules living in one directory during development.
`meternode-proto` is reached by a `replace` directive from the other two;
splitting it into its own repository is a one-line change in each `go.mod`, and
it is a separate module precisely so that split stays cheap.

## Why the design looks the way it does

Every unusual decision in these repositories traces back to one of six
environmental constraints. They are worth reading before deciding something here
looks over-engineered:

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

The contract's numbers, enforced in the agent and asserted by
`meternode-proto`'s `TestMonthlyBudget`:

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

The headroom to 150 MB is what pays for events, command results, and reconnect
backfill. A change that pushes those numbers up fails the test.

## Status

| | Console | Agent |
| --- | --- | --- |
| M0 | ✅ scaffold, migrations, health, CI, compose | ✅ scaffold, config, systemd, .deb, CI |
| M1 | ✅ enrollment, credentials, fingerprint binding | ✅ host collectors, `meternodectl status` |
| M2 | WebSocket ingest, Timescale writes, simulator | NVML GPU collector (nvidia-smi source and degraded paths done) |
| M3 | operator UI — **blocked**, see below | enrollment, transport, SQLite buffer, backfill |
| M4 | health scoring, alerting | bandwidth accounting, adaptive degradation |
| M5 | command dispatch, agent releases | container supervision, isolation enforcement |
| M6 | — | self-update with rollback |

**Console's UI is blocked.** It is meant to inherit its design system from the
existing MeterHome RMS portal, which could not be read from this session. The
brief says to stop rather than invent one, so
[`meternode-console/DESIGN-INHERITANCE.md`](meternode-console/DESIGN-INHERITANCE.md)
records the blocker, exactly what will be extracted once the repository is
reachable, and the three questions that need a decision rather than a guess.

## Getting started

```bash
# Control plane: Postgres/Timescale + Redis + the controller
cd meternode-console && make dev
make simulate              # 25 simulated nodes enrol against it

# Agent, against a scratch config, no root needed
cd meternode-agent && make run
make status
```

## Decision records

Both repositories keep an `ADR/` directory. The ones worth reading first, if you
are trying to understand why this is not shaped like ordinary infrastructure:

- [Console 0002](meternode-console/ADR/0002-messagepack-zstd-over-protobuf.md) —
  why the heartbeat envelope is hand-encoded.
- [Console 0003](meternode-console/ADR/0003-absent-is-not-zero.md) — why absent
  telemetry and zero telemetry must never collapse into each other.
- [Console 0005](meternode-console/ADR/0005-fingerprint-change-never-auto-rebinds.md) —
  why a swapped GPU stops the line and asks a human.
- [Agent 0002](meternode-agent/ADR/0002-collectors-behind-timeouts.md) — why
  every collector has its own timeout.
- [Agent 0005](meternode-agent/ADR/0005-unprivileged-and-the-docker-group.md) —
  what the agent gives up to hold no capabilities, and the one caveat we state
  plainly.

## Explicit non-goals for phase 1

Job scheduling, workload placement, customer-facing inference routing, billing,
homeowner payouts, marketplace, autoscaling, multi-tenant isolation guarantees.
The interfaces exist; the implementations are empty.
