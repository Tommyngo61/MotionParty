# MeterNode Agent

The host-side supervisor for a company-owned GPU compute node hosted in a
private residence. Phase 1 is telemetry and supervision only.

Part of MeterNode, alongside [`console/`](../console) (the control
plane) and [`proto/`](../proto) (the shared wire schema).


## Status

| Milestone | | |
| --- | --- | --- |
| M0 | scaffold, config, logging, systemd unit, CI, cross-compile | ✅ |
| M1 | CPU/mem/disk/net/host collectors + `meternodectl status` | ✅ |
| M2 | NVML GPU collector with full degraded-path handling | partial — the `nvidia-smi` source and the degraded paths are done; NVML itself is next |
| M3 | enrollment, WebSocket transport, SQLite buffering, reconnect/backfill | |
| M4 | bandwidth accounting and adaptive degradation | |
| M5 | container supervision, isolation enforcement, invariant checks | |
| M6 | self-update with rollback | |

## What it does today

Samples the host every 10 seconds, buffers into a bounded ring, and answers a
field tech over a unix socket. It does not yet enrol or ship telemetry — that is
M3 — but everything it collects is the real wire schema, and everything it
reports about itself is honest about not being connected.

```console
$ meternodectl status
PROBLEMS
  ! This node is not enrolled. Place a one-time token at
    /etc/meternode/enroll.token and restart the agent.
  ! No usable NVIDIA driver. GPU telemetry is unavailable; check `nvidia-smi`
    and whether a driver upgrade is in progress.

AGENT
  version        0.1.0
  uptime         6s
  pid            19894
  restarts       1 boots, 0 crashes
IDENTITY
  enrolled       NO
  token file     /etc/meternode/enroll.token
  controller     https://control.example.com
CONNECTION
  connected      no
COLLECTION
  collectors     cpu, disk, gpu, host, mem, net
  failing        gpu
  gpu source     none
  interval       10s
  samples        3 taken, last took 4 ms
BUFFER
  queued         3 of 2160 samples
BANDWIDTH
  control plane  0.0 MB month-to-date
  budget         150 MB/month (ok)
```

Problems come first, before the identity block. A tech at the house has one
question — "what is wrong with this box" — and making them read past twenty
lines of healthy fields to find the answer is the difference between a
five-minute visit and a twenty-minute one.

## Quick start

```bash
make build              # ./bin/meternode-agent and ./bin/meternodectl
make test               # unit tests, no hardware needed
make run                # run against ./dev/agent.yaml in a scratch directory
```

In a second terminal:

```bash
./bin/meternodectl -socket ./dev/run/agent.sock status
./bin/meternodectl -socket ./dev/run/agent.sock sample
```

## Build and package

```bash
make build              # host build
make cross              # linux/amd64, the only target
make deb                # dist/meternode-agent_<version>_amd64.deb
make image              # OCI image for development
```

`linux/amd64` only, and deliberately so. We own the hardware and image it
ourselves, so there is no reason to pay the Windows/WSL2 tax that a
consumer-BYO-hardware fleet pays. Platform-specific collection still sits behind
an interface (`collect.Collector`) so a future bring-your-own-hardware tier does
not need a rewrite — but there is no Windows code path and there should not be
one.

## How it runs on a node

Under systemd as the unprivileged `nodeagent` user:

```bash
sudo dpkg -i meternode-agent_0.1.0_amd64.deb
sudo systemctl status meternode-agent
sudo journalctl -u meternode-agent -f
```

The unit file is hardened well past the default — no capabilities, `ProtectSystem=strict`,
a seccomp filter, `MemoryDenyWriteExecute`, and a `WatchdogSec` the agent stops
pinging when its sampling loop stalls. Every directive is justified in
[SECURITY.md](SECURITY.md); read that before loosening any of them.

## Configuration

One YAML file at `/etc/meternode/agent.yaml`, with environment overrides and a
working default for everything. See
[`packaging/systemd/agent.yaml.example`](packaging/systemd/agent.yaml.example),
which is installed as the seed on first install.

```bash
meternode-agent -check-config          # validate before restarting
systemctl reload meternode-agent       # SIGHUP: intervals, logging, mounts
```

A missing config file is **not** an error. A node that boots before anyone has
configured it must still start, sample, and answer `meternodectl status` — that
is how a field tech finds out what is wrong.

SIGHUP applies intervals, the collector timeout, log level, disk mounts, and the
network interface without dropping anything. Changes to the controller URL, the
state directory, the socket path, or isolation enforcement need a restart and
are refused with a message saying so — reconnecting to apply a changed
controller URL would drop a socket that may have taken minutes of backoff to
establish on a flaky residential link. An invalid config on reload is rejected
and the old one keeps running: a typo must never take a node off the network.

## Design notes worth knowing

**Sampling never blocks.** Every collector runs in parallel behind a timeout. A
hung SMART read against a dying consumer SSD, or a wedged NVML call during a
driver upgrade, is abandoned — its failure is recorded on the sample and emitted
as an event, and the rest of the sample ships anyway. Dropping a whole sample
because one collector wedged would turn a minor fault into a telemetry gap,
which is the one thing the controller cannot distinguish from a node being
unplugged.

**A collector that is permanently broken reports once.** First failure, and
recovery — nothing in between. Otherwise a node with no GPU driver emits an
event every ten seconds forever, which is both an alert storm and, on a metered
residential link, a self-inflicted bandwidth problem.

**Absent is not zero.** A failed GPU collector leaves `Sample.GPUs` nil, never
an empty slice or a zero-valued struct. A driver mid-upgrade must not look like
a GPU idling at 0 W. Same for `smart_ok`, which is `NULL` when SMART could not
be read and `false` only when SMART says the disk is failing.

**A panic in a collector does not kill the agent.** The recovery is on the
goroutine that actually calls `Collect` — a nil map in a disk parser is not
worth a truck roll.

**Crash loops are loud.** A node that restarts every thirty seconds still
heartbeats often enough to look alive. The boot counter distinguishes a
homeowner power-cycling the machine, a clean restart for an update, and an agent
that is genuinely looping — and the third becomes a CRITICAL event.

**The buffer is bounded.** A week-long ISP outage on an unbounded queue is an
OOM kill, which turns a network problem into a machine that needs a site visit.
The ring evicts oldest-first and reports the drop count, because recent
telemetry is what the controller needs first on reconnect.

## Testing

```bash
make test        # unit tests
make test-race   # the same, under the race detector
make lint        # vet + gofmt
make ci          # everything CI runs
```

The collector tests run against faked inputs including the degraded cases:
driver absent, GPU fallen off the bus, ECC errors present, thermal throttle
active, `nvidia-smi` emitting `[N/A]` per field. The agent tests exercise the
real sampling loop and the real unix socket, on whatever machine they run on —
which on a build box with no GPU means the no-driver path is exercised for real
rather than stubbed.

`test/netem/` holds the harness for M3's network conditions (5 Mbps upstream,
300 ms latency, 5% loss, hard disconnects). It is a scaffold until the transport
exists to test.

## Reading further

- [ARCHITECTURE.md](ARCHITECTURE.md) — the shape of the agent and why.
- [SECURITY.md](SECURITY.md) — **what a compromised workload can and cannot
  reach**, and what privileges the agent itself holds.
- [ADR/](ADR/) — the decisions, one file each.
