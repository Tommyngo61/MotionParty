# ADR 0006 — Health is derived from telemetry, never asserted by the agent

**Status:** accepted · **Date:** 2026-09-10

## Context

The obvious design has the agent report its own status: it knows about its own
throttling, its own ECC counters, its own disk. It is also the design that fails
in exactly the cases that matter.

An agent that is wedged, crash-looping, out of disk, or running an old build
with a known scoring bug is precisely the agent whose self-assessment is worth
least. Worse, a node reporting `healthy` and a node reporting nothing at all are
different only if the controller is doing the deciding.

## Decision

The controller derives node health. The schema has no field for the agent to
assert a status.

Two separate concepts, kept in separate columns of thought:

- **Lifecycle** (`nodes.lifecycle`, an enum in the database): the administrative
  state an operator sets — `unenrolled`, `enrolled`, `quarantined`, `retired`.
- **Health** (derived, M4, never stored as truth): `healthy`, `degraded`,
  `unreachable`, `unenrolled`, `quarantined`, computed from raw telemetry.

An operator can quarantine a perfectly healthy node; an enrolled node can be
reporting ECC errors. Collapsing the two would make both wrong.

The one thing the agent may hint at is `Heartbeat.Degraded`, and it is
documented as a prioritisation hint only — the controller does not read it as
truth.

## Consequences

- Silence is a signal the controller can act on, because it owns the timeout.
- Fixing a scoring rule is a controller deploy, not a fleet-wide agent rollout
  to machines that update on their own schedule.
- The scorer must be tolerant of missing data, which is why ADR 0003 exists.
- Degradation signals to score on (M4): thermal throttling, ECC errors, PCIe
  link downtraining, sustained low GPU utilisation while assigned work, disk
  SMART failure, clock skew, repeated agent restarts, bandwidth budget overrun,
  and upstream bandwidth below a floor.
