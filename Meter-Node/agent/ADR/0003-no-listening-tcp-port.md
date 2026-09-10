# ADR 0003 — The agent never opens a listening TCP port

**Status:** accepted · **Date:** 2026-09-10

## Context

The node shares a LAN with the homeowner's laptop, phone, TV, thermostat, and
whatever else is plugged in. They did not consent to being part of a compute
fleet's attack surface, they will never read a runbook, and they cannot be asked
to configure their router.

A field tech still needs to stand at the house with a laptop on the node's
console and find out what is wrong.

## Decision

The agent's only local interface is a **unix socket** at
`/run/meternode/agent.sock`, mode 0660, group `nodeagent`.

Not "a unix socket by default". Not "a unix socket unless configured otherwise".
There is no configuration in this codebase that opens a TCP listener, and adding
one would be a security regression rather than a feature.

Filesystem permissions are the authentication: a tech runs `meternodectl` as a
member of the agent's group, or via `sudo`. 0666 would let any process on the
box read it — including, once workloads exist, a customer container that escaped
its mount namespace.

The unit file enforces this from the other side:
`RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK` permits the
outbound controller connection and the netlink call that enumerates interfaces,
and nothing else.

## Alternatives considered

**A localhost-bound HTTP port.** The usual choice, and wrong here. "Localhost
only" is one misconfiguration, one container with `--net=host`, or one
`0.0.0.0` typo away from being LAN-reachable — and the blast radius of that
mistake is a stranger's home network.

**No local interface at all.** Considered seriously: it is the most defensible
posture. Rejected because a field tech at the house needs an answer, and
`journalctl` alone does not give one — the interesting state (enrolled or not,
connected or not, which collector is failing) is in memory, not in the log.

**SSH plus a status file on disk.** A status file goes stale the moment the
agent wedges, which is exactly when it is needed.

## Consequences

- `meternodectl` is a unix-socket HTTP client. The URL host is a placeholder;
  the transport dials the socket.
- A stale socket from an unclean shutdown — a power cut, which on these machines
  is routine — is removed on start. An agent that fails with "address already in
  use" on a node nobody can log into is a site visit.
- Remote diagnostics go through the controller: `collect_diagnostics` and
  `tail_logs` are signed commands over the outbound connection, not an inbound
  path.
- The local API is read-only. Nothing a tech can do over this socket changes the
  node's state; anything that changes state is a signed command from the
  controller, which is auditable.
