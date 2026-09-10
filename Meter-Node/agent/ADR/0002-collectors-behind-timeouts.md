# ADR 0002 — Every collector runs behind a timeout, and a failure is data

**Status:** accepted · **Date:** 2026-09-10

## Context

A node is a machine in someone's house. Its consumer SSD may be failing. Its
NVIDIA driver may be mid-upgrade, with NVML's shared library and the kernel
module briefly disagreeing about version. A USB disk enclosure may have stopped
answering.

Any of those makes a read block — sometimes for tens of seconds, in
uninterruptible sleep where the process cannot even be signalled. On a 10-second
sampling interval, one such collector serialised into the loop stops the agent
entirely.

And an agent that stops sampling is, from the controller's side, indistinguishable
from a node the homeowner unplugged. That confusion is the expensive failure:
one needs a driver investigation, the other needs a phone call.

## Decision

`collect.Registry.Collect` runs every collector:

1. **In parallel**, each in its own goroutine. Three collectors that each take
   100 ms finish in 100 ms, not 300.
2. **Under an individual timeout** (`collect.collector_timeout`, default 3 s,
   validated to be strictly shorter than the sample interval — a timeout at or
   above the interval defeats its own purpose).
3. **With panic recovery on the goroutine that actually calls `Collect`.** This
   is easy to get wrong: a `recover()` on the outer goroutine does not catch a
   panic on the inner one, and the process dies. A test pins it.

A collector that fails or times out is **abandoned, not waited for**. Its
goroutine may still be blocked in a syscall — that is exactly the case this
exists for — and it will exit when the syscall returns.

The failure becomes data:

- The collector's name goes into `Sample.CollectorErrors`, which ships with the
  sample.
- An `agent.collector_failed` event is emitted — at WARN for a clean error, at
  ERROR for a timeout, because a timeout means something on the *host* is wedged.
- The rest of the sample ships regardless.

## Reporting cadence

An event on the **first** failure of a run, and an event on **recovery**.
Nothing in between.

A permanently broken collector — a driver removed, a disk pulled — would
otherwise emit an event every ten seconds forever. That is an alert storm, and
on a metered residential link it is also a bandwidth problem we inflicted on
ourselves: 8,640 events a day at even 100 bytes is 26 MB/month, a sixth of the
entire control-plane budget, spent telling the controller the same thing.

## Alternatives considered

**A global timeout on the whole sampling pass.** Simpler, and wrong: one slow
collector would poison the whole sample, losing the CPU and memory readings that
are almost always fine and are what the health scorer needs most.

**Dropping the sample when any collector fails.** Turns a minor fault into a
telemetry gap, which is the one thing the controller cannot distinguish from an
unplugged node.

**Running collectors serially with a shared deadline.** Cheaper on goroutines,
but a slow disk then delays the GPU read, and at a 10 s interval that compounds.

## Consequences

- A wedged collector costs one goroutine per sample until its syscall returns.
  Bounded in practice by the collector count and the timeout; `MemoryMax=512M`
  in the unit file is the backstop.
- The registry holds a per-collector consecutive-failure count, which is what
  makes the reporting cadence possible.
- Tests substitute collectors, so the wedged, panicking, and permanently-broken
  cases are all covered without needing a dying disk.
