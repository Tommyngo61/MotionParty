# MeterNode Agent — Architecture

The supervisor process on a company-owned GPU node in a private residence. It
owns telemetry, container lifecycle, and self-update.

This document explains the shape of the agent and why it is that shape. Every
significant decision has an ADR in [`ADR/`](ADR/); this is the map, those are
the reasons.

## The constraints that drive everything

The agent's design is dictated almost entirely by *where it runs*:

| Constraint | What it forces |
| --- | --- |
| No inbound connectivity (consumer NAT, often CGNAT) | Every connection is agent-initiated, outbound, TCP/443. Nothing dials in — and nothing listens. |
| Asymmetric bandwidth: 10–35 Mbps up, some as low as 5 | Upstream is the scarce resource. Batch, compress, rate-limit backfill. |
| Metered links, ~1.2 TB/month caps | 150 MB/month/node target. The agent counts its own control-plane bytes and degrades before it breaches. |
| Homeowners power-cycle and unplug machines | Disconnection is normal. Restarts are normal. A *crash loop* is not, and has to be distinguishable from both. |
| Untrusted LAN shared with personal devices | No listening TCP port. Workload egress to RFC1918/RFC6598 is dropped. See [SECURITY.md](SECURITY.md). |
| Nobody on site administers the machine | Everything must degrade rather than fail, and a field tech with a laptop must be able to find out why in one command. |

## Layout

```
cmd/agent/            the supervisor binary
cmd/agentctl/         meternodectl — local diagnostics for a field tech
internal/
  agent/              the supervisor loop, event queue, status
  buffer/             the bounded sample ring
  collect/            the collector framework and every collector
  config/             YAML + env, validation, SIGHUP reload rules
  enroll/             enrollment (M3)
  localapi/           the unix socket, and its client
  logging/            structured logging
  supervise/          systemd watchdog, boot counter, crash-loop detection
  version/            build identity
packaging/
  systemd/            the unit file and the example config
  deb/                the .deb build, using nothing but Go and dpkg-deb
test/netem/           tc netem harness for the transport (M3)
ADR/                  decision records
```

## The sampling loop

```
                    ┌─────────── every 10s ───────────┐
                    ▼                                 │
   ┌────────────────────────────────┐                 │
   │ Registry.Collect(ctx, now)     │                 │
   │   every collector, in parallel │                 │
   │   each under a 3s timeout      │                 │
   └───────────┬────────────────────┘                 │
               │                                      │
       ┌───────┴────────┐                             │
       ▼                ▼                             │
   Sample           Failures ──► EventQueue ──────────┤
       │                                              │
       ▼                                              │
   Ring buffer (bounded, oldest-first eviction)       │
       │                                              │
       ▼                                              │
   Transport (M3) ──► controller                      │
                                                      │
   localapi (unix socket) ◄── meternodectl ───────────┘
```

Three properties of this loop are load-bearing:

**It never blocks.** Every collector runs in its own goroutine under its own
timeout. A `statfs` against a dying consumer SSD or a wedged NVML call during a
driver upgrade can block for tens of seconds in uninterruptible sleep; the
framework abandons it and moves on. The abandoned goroutine exits when its
syscall eventually returns — what matters is that the agent does not wait.

**A panic in a collector is contained.** The recovery is on the goroutine that
actually calls `Collect` (getting this wrong is easy, and a test pins it). A nil
map in a disk parser must not take down an agent on a machine nobody can reach.

**It degrades rather than fails.** A failed collector is recorded on the sample,
emitted as an event, and skipped — the rest of the sample ships. There is no
failure mode in `collect` that stops the agent sampling.

## Absent is not zero

`Sample`'s field groups are pointers, and this is the single most important
thing to know before editing a collector. A node whose NVIDIA driver is
mid-upgrade reports **no GPU block at all**, and that must not decode as a GPU
sitting idle at 0 W — one is a maintenance window, the other is a dead card. The
same applies to `smart_ok`, which is `NULL` when SMART could not be read and
`false` only when SMART says the disk is failing. Only the second raises an
alert. See `meternode-console/ADR/0003`.

## Reporting failures without causing an alert storm

A collector that is permanently broken — a driver removed, a disk pulled —
reports on its **first** failure and on **recovery**, and nothing in between.
Otherwise a node with no GPU emits an event every ten seconds forever, which is
both an operator problem and, on a metered residential link, a bandwidth problem
we inflicted on ourselves.

Timeouts are reported at ERROR, clean errors at WARN. A timeout means something
on the *host* is wedged, which is worse than a collector that returned an error
it understood.

## Telling apart three kinds of restart

From the controller's side these look identical, and they need very different
responses:

| | Signal |
| --- | --- |
| The homeowner power-cycled the machine | `boot_id` changes, host uptime resets |
| The agent restarted cleanly (an update) | `boot_id` unchanged, `CleanShutdown` was recorded |
| The agent is crash-looping | `CleanShutdown` false on start, repeatedly, with runs shorter than 90 s |

The third is the one that must not be silent: a node restarting every thirty
seconds heartbeats often enough to look alive. Three consecutive unclean exits
raise `agent.crash_loop` at CRITICAL, and a run that survives 90 seconds clears
the streak so one bad restart does not follow a node around forever.

The systemd watchdog covers the other invisible failure: an agent that is
running, answering its socket, and not sampling. `Agent.Healthy` reports false
after three missed sample intervals, the watchdog pings stop, and systemd
restarts a process that a liveness check would have called fine.

## The buffer is bounded, always

An unbounded queue during a week-long ISP outage is an OOM kill, which turns a
network problem into a machine that needs a site visit. The ring evicts
oldest-first and counts the drops, because on reconnect the controller needs
*recent* telemetry first — and a node whose buffer has overflowed is itself a
fact worth reporting.

M1 is an in-memory ring sized for six hours. M3 adds the SQLite WAL-mode buffer
behind the same interface, so a node that loses power mid-outage does not lose
what it had queued.

## The local interface is a unix socket

Not "a unix socket by default". A unix socket, full stop. The node shares a LAN
with the homeowner's laptop, phone, and TV, and none of those may reach anything
the agent exposes. There is no configuration in this codebase that opens a
listening TCP port, and adding one would be a security regression rather than a
feature.

The socket is mode 0660, group `nodeagent`. Filesystem permissions *are* the
authentication. A stale socket left by a power cut is removed on start, because
an agent that fails with "address already in use" on a node nobody can log into
is a site visit.

## Configuration and reload

One YAML file, environment overrides, a working default for everything —
including no file at all, because a node that boots before anyone configured it
must still be diagnosable.

SIGHUP applies what can be applied live: intervals, collector timeout, log
level, disk mounts, network interface. It refuses what cannot: the controller
URL, the state directory, the socket path, isolation enforcement. Reconnecting
to apply a changed controller URL would drop a socket that may have taken
minutes of backoff to establish on a flaky residential link.

An invalid config on reload is rejected and the running one is kept. A typo must
never take a node off the network.

Validation refuses configurations that are *unsafe*, not merely unusual: a
heartbeat faster than 5 s (which alone would eat the monthly budget), a
collector timeout at or above the sample interval (which defeats the timeout's
purpose), a budget above the 500 MB hard ceiling, TLS verification disabled
against a non-localhost controller.

## Privileges

The agent holds **no Linux capabilities**. It runs as `nodeagent` with
membership in the `docker` group — which is root-equivalent on this host, and
[SECURITY.md](SECURITY.md) says so plainly rather than pretending otherwise.

The one thing it gives up for this is SMART, which needs a privileged ioctl. It
reports `smart_ok: NULL` instead of taking `CAP_SYS_RAWIO`.

## What is designed but not yet built

The seams are in place so the remaining milestones add packages rather than
rewrite this one:

| | Seam that already exists |
| --- | --- |
| Enrollment (M3) | `collect.Fingerprint` and `collect.Inventory` produce the enrollment payload today |
| Transport (M3) | `agent.Buffer()` and `agent.Events()` are what it drains |
| Clock skew (M3) | `collect.NewHostCollector` takes a skew source; nil today |
| Bandwidth accounting (M4) | `collect.ControlPlaneCounter`, wired into the net collector; nil today |
| NVML (M2) | `collect.GPUSource`, an ordered preference list with probe throttling and re-promotion on failure. NVML goes at the front |
| Container supervision (M5) | `config.Runtime`, and `Sample.Containers` in the schema |
| Self-update (M6) | `supervise.BootCounter` already distinguishes a clean update-restart from a crash |

## Explicit non-goals for phase 1

Job scheduling, workload placement, customer-facing inference routing, billing,
homeowner payouts, marketplace, autoscaling, multi-tenant isolation guarantees.
The interfaces exist; the implementations are empty. Phase 1's reconciliation
loop runs against an always-empty desired set — deliberately, so the loop itself
is exercised before there is anything to lose.
