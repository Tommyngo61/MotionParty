# Security posture — MeterNode Agent

This document exists because of one fact: **the node is a company-owned machine
sitting in a private residence, on a LAN shared with the homeowner's laptop,
phone, TV, thermostat, and whatever else is plugged in.** The homeowner supplies
power and internet. They do not administer the machine, they did not consent to
being part of a compute fleet's attack surface, and they will never read a
runbook.

Everything below is written to be checkable. Where a claim is not yet true in
phase 1, it says so.

---

## What a compromised workload can and cannot reach

This is the section that matters. Workloads do not run in phase 1 — the
reconciliation loop runs against an always-empty desired set — but the
enforcement is being built now, and it is the contract the whole product rests
on. Anything marked **(M5)** is designed and specified but not yet enforced in
code; until M5 ships, the honest answer is that the agent refuses to run
workloads at all.

### Cannot reach

| | Why |
| --- | --- |
| **The homeowner's LAN** — their laptop, NAS, printer, cameras, thermostat | Workload containers attach to a dedicated Docker network with egress-only rules, and all traffic to RFC1918 (`10/8`, `172.16/12`, `192.168/16`) and RFC6598 (`100.64/10`, CGNAT) destinations is dropped **(M5)** |
| **The homeowner's router** | The host's default gateway is an explicit DROP target, so a workload cannot reach the admin interface even though it is technically off-LAN from the container's view **(M5)** |
| **The host's own services** | No `--net=host`. The agent's local socket is a unix socket at `/run/meternode/agent.sock`, mode 0660, owned by `nodeagent` — not reachable from a container's mount namespace **(M5)** |
| **The node's identity** | `/var/lib/meternode/node.key` and `/var/lib/meternode/credential` are 0600, owned by `nodeagent`. Containers do not run as `nodeagent` and do not mount that directory **(M5)** |
| **Other workloads** | Each workload gets its own network attachment; inter-container traffic on the workload network is denied **(M5)** |
| **The kernel** | seccomp profile applied, all capabilities dropped, `no-new-privileges`, read-only rootfs, no host PID or IPC namespace **(M5)** |
| **Unbounded host resources** | cgroups v2 limits on CPU, memory, and PIDs; a workload cannot starve the agent or the host **(M5)** |

### Can reach

| | Note |
| --- | --- |
| **The public internet, outbound** | This is the point of the product. Egress is unrestricted to non-private addresses |
| **Its assigned GPU(s)** | Via the NVIDIA container runtime, which is a privileged device passthrough by nature. A workload with GPU access can read GPU memory it allocated and can crash the driver |
| **Its own filesystem and resource allocation** | Within its cgroup limits |
| **The homeowner's bandwidth** | Metered and reported, not blocked. A workload's traffic counts against the residential cap; that is a commercial question, not a containment one |

### What we do not claim

- **This is not multi-tenant isolation.** Phase 1's non-goals say so explicitly.
  A workload sharing a GPU with another workload can, through driver bugs and
  side channels, potentially affect it. Do not schedule mutually distrusting
  tenants onto one node until that is designed for.
- **A kernel escape defeats all of it.** Container isolation is a kernel
  boundary. A local privilege escalation in the Linux kernel gives a workload
  the host, and from the host it has the homeowner's LAN. This is mitigated by
  keeping the base image patched and by the blast-radius limits below, not
  eliminated.
- **The GPU is not a security boundary.** NVIDIA's driver is a large privileged
  surface. Treat GPU passthrough as trusting the workload not to be actively
  hostile at the driver level.

### Verified, not assumed

The agent checks its isolation invariants **at startup** and refuses to run
workloads if it cannot enforce them, logging the specific failing invariant
(`isolation.invariant_failed`, CRITICAL) rather than a generic error. There is
no configuration flag that turns this off; `runtime.enforce_isolation` defaults
to true and a config that sets it false requires a restart, which is refused.

M5 ships an integration test that starts a container and asserts it cannot reach
a simulated LAN host. Until that test passes, workloads do not run.

---

## Privileges the agent itself holds

The agent runs as the unprivileged `nodeagent` user under systemd. It holds
**no Linux capabilities** — `CapabilityBoundingSet=` and `AmbientCapabilities=`
are both empty in the unit file.

| Needs | Why | How it gets it |
| --- | --- | --- |
| Read `/proc`, `/sys` | Host telemetry: CPU, memory, thermal, DMI | World-readable on the Ubuntu image we ship |
| Read `/sys/class/dmi/id/product_uuid` | The motherboard half of the hardware fingerprint | Readable on our image. On a stock Ubuntu it is 0400 root-only, and the agent then falls back to GPU UUIDs and the NIC MAC, reporting an incomplete fingerprint rather than failing |
| Enumerate network interfaces | The NIC half of the fingerprint, and bandwidth accounting | `AF_NETLINK`, no capability required |
| Run `nvidia-smi` / load NVML | GPU telemetry | The driver's device nodes are world-readable by default |
| Write `/var/lib/meternode`, `/run/meternode` | Credentials, buffer, socket | `StateDirectory=` / `RuntimeDirectory=`, owned by `nodeagent` |
| Talk to `/var/run/docker.sock` | Workload supervision (M5) | Membership in the `docker` group |

### The one honest caveat: the docker group

**Membership in the `docker` group is root-equivalent on this host.** Anyone who
can talk to the Docker socket can start a privileged container that mounts `/`.
Saying "the agent runs unprivileged" without saying this would be a lie by
omission.

What that buys and costs:

- It is *not* the same as running the agent as root. A bug in a metric parser
  still cannot write to `/etc`; an attacker needs to reach the Docker API
  deliberately.
- The exposure is bounded by the agent's own attack surface, which is small:
  no listening TCP port, one unix socket, and an outbound-only TLS connection to
  a controller it authenticates.
- The alternative — a privilege-separated helper that brokers a narrow set of
  container operations — is the right long-term answer and is deliberately
  deferred rather than forgotten. Rootless Docker or Podman is the other path,
  and the container interface is designed so Podman can be swapped in.

### What the agent gives up rather than take a capability

**SMART data.** Reading SMART needs `CAP_SYS_RAWIO` or a privileged ioctl. The
agent does not take it. Instead `smart_ok` is reported as `NULL` — "could not be
read" — which the schema deliberately distinguishes from `false` ("SMART says
this disk is failing"). Only the second raises an alert. Disk health is inferred
from usage, I/O errors, and throughput anomalies instead.

That is the trade being made explicitly: a slightly weaker disk-failure signal
in exchange for an agent that holds no capabilities at all.

---

## Node identity and credentials

- The node generates an **ed25519 keypair locally at enrollment**. The private
  half never leaves the machine — not at enrollment, not ever. It is stored at
  `/var/lib/meternode/node.key`, mode 0600, owned by `nodeagent`.
- The controller issues a signed credential that binds the node id, the *public*
  key, and the hardware fingerprint hash.
- Every request carries a fresh signature over the method, path, and a
  timestamp. **The credential is therefore not a bearer token**: capturing it
  from a log, a proxy, or a support bundle does not let anyone speak as the
  node. See `meternode-console/ADR/0004`.
- Enrollment tokens are **single-use and short-lived**. The token file is
  deleted after a successful enrollment.
- A **hardware fingerprint change never re-binds silently.** It quarantines the
  node and opens a re-attestation for a human, because a swapped GPU and a
  cloned provisioning image are indistinguishable from the controller's side.
  See `meternode-console/ADR/0005`.
- Credentials are **revocable centrally**. A revoked node stops reporting and
  refuses to start workloads.

## Commands from the controller

- Every command is **signed by the controller** and verified against keys the
  agent pinned at enrollment.
- The **target node id is inside the signed body**. Without that, a command
  captured from one node's socket would be a valid signed command for every node
  in the fleet.
- Commands carry an expiry. A queued command that arrives after its TTL is
  dropped, never executed.
- The agent maintains an **allowlist of command kinds it will execute**. Phase 1:
  `ping`, `set_sample_interval`, `collect_diagnostics`, `tail_logs`,
  `restart_agent`, `update_agent`. Anything else — including the declared but
  unimplemented `run_workload`, `stop_workload`, `drain`, `reboot_host` — is
  logged and rejected with `command.rejected`.

## Network posture

- **No listening TCP port. Ever.** The only local interface is a unix socket at
  `/run/meternode/agent.sock`, mode 0660, group `nodeagent`. Filesystem
  permissions are the authentication. There is no configuration in this codebase
  that opens a TCP listener, and adding one would be a security regression, not
  a feature.
- All controller traffic is **agent-initiated, outbound, TLS 1.3 over TCP/443**.
  Nodes sit behind consumer NAT, often CGNAT; nothing dials in.
- `controller.insecure_skip_verify` is **refused unless the controller URL is
  localhost**, and a plaintext `http://` controller URL is refused outright. A
  node on a stranger's LAN is the last place to trust an unverified certificate.

## Self-update (M6)

- The agent verifies the **signature on a downloaded build** before staging it.
  An unsigned or mis-signed release is refused on the node, not merely on the
  controller.
- Updates stage, swap atomically, restart under systemd, and **roll back
  automatically** if the new binary does not reach a healthy connected state
  within a timeout.
- Updates respect the bandwidth budget and defer while a workload is running.

## Reporting a vulnerability

Email `security@meterhome.example`. Please do not open a public issue.

Include the agent version (`meternodectl version`), what you observed, and
whether it required local access to a node. A finding that lets a workload reach
the homeowner's LAN is the highest-severity class of bug in this codebase and
will be treated as such.
