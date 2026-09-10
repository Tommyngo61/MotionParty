# ADR 0001 — linux/amd64 only, but collection stays behind an interface

**Status:** accepted · **Date:** 2026-09-10

## Context

Salad and similar networks run on consumer-owned gaming PCs, which means
Windows, which means WSL2 for containers, which means a whole second code path
and a class of failures that only exist because of it.

We are not doing that. We own the hardware. We buy the machines, image them
before they ship, and place them in homes. The homeowner supplies power and
internet and does not administer the box.

## Decision

The agent targets **linux/amd64 only**. Ubuntu Server LTS, headless, imaged by
us. No Windows code path, no WSL2, no `runtime.GOOS` branches.

But platform-specific collection sits behind `collect.Collector` anyway. That is
not hedging: a future bring-your-own-hardware tier is a plausible business
direction, and the difference between "add a collector" and "rewrite the
sampling loop" is worth one interface.

The interface is also what makes the collectors testable. Every degraded-path
test in `internal/collect` works by substituting a `Collector` or a `GPUSource`,
which would not be possible if collection were a package of free functions
calling into sysfs.

## Consequences

- `nvidia-smi` parsing can assume Linux conventions and the Linux flag set.
- `/proc` and `/sys` are read directly rather than through an abstraction that
  would have to pretend Windows has them.
- CGO is off, so the binary is static and does not care which libc the base
  image has — nor can it be surprised by a glibc upgrade on a node we cannot
  reach.
- If a BYO tier ever happens, it adds collectors. It does not touch
  `internal/agent`.
- Adding a Windows code path now would be actively wrong: it would double the
  test matrix for hardware we do not own and do not plan to.
