# ADR 0005 — No capabilities, and an honest note about the docker group

**Status:** accepted · **Date:** 2026-09-10

## Context

The agent runs on a machine in a stranger's home. Whatever it can reach, a bug
in it can reach — and the thing on the other side of that boundary is somebody's
personal network.

The default for an agent like this is to run as root "because it needs to read
hardware". Most of that need turns out to be imagined.

## Decision

The agent runs as the unprivileged `nodeagent` user and holds **no Linux
capabilities**: `CapabilityBoundingSet=` and `AmbientCapabilities=` are both
empty in the unit file, with `NoNewPrivileges=yes`.

Everything it actually needs is reachable without them on the image we ship:

| Needs | How |
| --- | --- |
| `/proc`, `/sys` for host telemetry | world-readable |
| DMI `product_uuid` for the fingerprint | readable on our image; falls back to GPU UUIDs + NIC MAC otherwise |
| Interface enumeration | `AF_NETLINK`, no capability |
| NVML / `nvidia-smi` | driver device nodes are world-readable |
| `/var/lib/meternode`, `/run/meternode` | `StateDirectory=` / `RuntimeDirectory=` |

### What we give up rather than take a capability

**SMART.** Reading it needs `CAP_SYS_RAWIO` or a privileged ioctl. The agent
does not take it. `smart_ok` is reported as `NULL` — "could not be read" — which
the schema deliberately distinguishes from `false` ("SMART says this disk is
failing"). Only the second raises an alert.

That is the trade, made explicitly: a weaker disk-failure signal in exchange for
an agent that holds no capabilities at all. Disk health is inferred from usage,
I/O errors, and throughput anomalies instead. A privilege-separated SMART helper
is the way to get it back if the weaker signal proves insufficient.

### The docker group, stated plainly

The agent is in the `docker` group so it can supervise workload containers (M5)
without being root.

**Membership in the `docker` group is root-equivalent on this host.** Anyone who
can talk to the Docker socket can start a privileged container that mounts `/`.
Claiming "the agent runs unprivileged" without saying this would be a lie by
omission, and `SECURITY.md` says it in the same words.

What it does and does not buy:

- It is *not* the same as running as root. A bug in a metric parser still cannot
  write to `/etc`; an attacker has to reach the Docker API deliberately.
- The exposure is bounded by the agent's attack surface, which is small: no
  listening TCP port (ADR 0003), one unix socket, and an outbound-only TLS
  connection to a controller it authenticates.
- The right long-term answer is a privilege-separated helper brokering a narrow
  set of container operations, or rootless Docker/Podman. The container
  interface is written so Podman can be swapped in. This is deferred, not
  forgotten.

## Consequences

- The unit file is hardened well past the default: `ProtectSystem=strict`,
  `ProtectKernelTunables`, `RestrictNamespaces`, `MemoryDenyWriteExecute`, a
  `@system-service` seccomp filter with `~@privileged @resources` subtracted,
  and `RestrictAddressFamilies` limited to what the agent demonstrably uses.
- `MemoryMax=512M` and `CPUQuota=25%` cap the agent because the GPU and the CPU
  belong to the workload, not to us. A runaway agent becomes a restarted agent
  rather than a node that needs a site visit.
- Loosening any of this needs a reason written down here. The default answer to
  "the agent needs one more capability" is to find another way to get the data,
  or to do without it as we did with SMART.
