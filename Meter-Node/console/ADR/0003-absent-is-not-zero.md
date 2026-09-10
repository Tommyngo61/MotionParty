# ADR 0003 — Absent telemetry is not zero telemetry

**Status:** accepted · **Date:** 2026-09-10

## Context

Nodes run in houses. Collectors fail in ways that do not happen in a
datacenter: the NVIDIA driver is mid-upgrade and NVML returns nothing, a USB
disk enclosure stops answering SMART queries, a wedged sysfs read hangs past its
timeout and the collector is abandoned.

The health scorer derives node state from telemetry rather than trusting a
status the agent asserts. That only works if it can tell "no data" from "a
reading of zero".

## Decision

Every field group in `Sample` that a collector can legitimately fail to produce
is a pointer, and `smart_ok` is a `*bool`. Failed collectors are named in
`Sample.CollectorErrors` and also emit an `agent.collector_failed` event.

Concretely:

- No GPU block ≠ a GPU idling at 0 W and 0 °C. The first is a driver upgrade or
  a card that fell off the bus; the second is a healthy node with nothing to do.
- `smart_ok = NULL` ≠ `smart_ok = false`. The first means SMART could not be
  read; only the second should raise an alert. The database column is nullable
  for the same reason.
- A sample with one failed collector is still shipped. Dropping the whole sample
  because one collector timed out would turn a minor fault into a telemetry gap.

## Consequences

- More nil checks in the ingest path and the health scorer. Accepted.
- Alert rules must be written to treat missing data as missing, not as a value.
  The rule engine (M4) distinguishes "no data for N minutes" from "value below
  threshold for N minutes"; they are different alerts with different runbooks.
- `TestAbsentIsNotZero` pins the encoding behaviour so a future switch to
  non-pointer fields fails loudly.
