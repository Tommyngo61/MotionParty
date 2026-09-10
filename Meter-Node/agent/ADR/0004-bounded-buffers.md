# ADR 0004 — Every buffer is bounded, and overflow is reported

**Status:** accepted · **Date:** 2026-09-10

## Context

Disconnection is normal operation. A homeowner unplugs the machine to vacuum. An
ISP has a bad afternoon. Someone moves furniture and knocks out the ethernet
cable and does not notice for four days.

The obvious design queues telemetry until the connection comes back. The obvious
design is also how a four-day outage becomes an OOM kill — which turns a network
problem into a machine that needs a site visit, on hardware we own, in a house
we need permission to enter.

## Decision

Every queue in the agent is fixed-capacity with oldest-first eviction, and the
eviction count is itself reported to the controller.

**Sample ring** (`buffer.Ring`): sized for six hours at the configured sample
interval — 2,160 samples at 10 s. Six hours covers an evening's outage without
the memory cost of trying to cover a multi-day one.

**Event queue** (`agent.EventQueue`): 256 events, with one refinement. When it
is full it evicts the oldest **INFO** event in preference to the oldest event
overall. An hour of "collector recovered" notices must not push out the one
CRITICAL that explains why a node is being replaced.

On reconnect the agent uploads **most recent first**, then backfills older data
downsampled and rate-limited (M3). That ordering is why oldest-first eviction is
the right policy: the controller needs current state before it needs history,
and a returning node must not saturate the homeowner's upstream.

## Why six hours and not more

The memory is not the binding constraint — 2,160 samples is a few megabytes. The
reasoning is about what the data is *for*:

- Under an hour: the controller wants all of it, at full resolution.
- Hours: it wants recent detail and a downsampled trend.
- Days: it wants to know the node was down, and roughly what it looked like. The
  rollups on the controller side answer that; a per-10-second replay of a
  four-day outage is 34,560 samples nobody will look at, shipped over a 5 Mbps
  upstream we are trying to stay invisible on.

M3 adds the SQLite WAL-mode buffer behind the same `Buffer` interface for
longer outages and to survive a power cut mid-outage. It is size-capped and
oldest-first for the same reasons.

## Consequences

- A node that has been offline long enough to overflow reports
  `buffer_dropped > 0`, and that is a health signal — it means this node's
  upstream cannot keep up with its own telemetry, which is real on a 5 Mbps
  link.
- `meternodectl status` surfaces overflow as a warning in plain words, because a
  tech at the house should be told "this node has been unable to reach the
  controller for a long time" rather than a number.
- `Ring.Drain` zeroes the slots it drains so the sample's slices can be
  collected rather than pinned until the ring wraps. On a process that runs for
  months that is the difference between flat memory and a slow leak.
- `NewRing(0)` yields a usable one-element ring rather than panicking. A
  misconfiguration must not take down a machine nobody can reach.
