# ADR 0001 — Record architecture decisions

**Status:** accepted · **Date:** 2026-09-10

## Context

MeterNode Console is being built against a set of environmental constraints
that are unusual enough that most decisions here will look wrong to someone who
has only worked on datacenter infrastructure. Nodes sit behind consumer NAT with
no inbound connectivity, on metered residential links, in houses where the power
gets cycled by someone who does not know the machine is there.

Six months from now the reasoning behind "why is the heartbeat hand-encoded" or
"why does a GPU swap quarantine a node" will not be obvious from the code, and
the most likely outcome is that someone removes the strange-looking thing and
reintroduces the problem it solved.

## Decision

Record every significant decision as a short ADR in `ADR/`, numbered
sequentially. An ADR states the context, the decision, and the consequences
we accepted — including the ones we did not like.

An ADR is warranted when a decision constrains future work, when a reasonable
engineer would choose differently without knowing the constraint, or when we
rejected an obvious alternative. Routine choices do not need one.

ADRs are immutable once accepted. A decision that changes gets a new ADR that
supersedes the old one, and the old one is marked superseded rather than edited.
The record of what we believed at the time is the point.

## Consequences

- Some overhead per decision. Accepted; it is measured in minutes.
- The `ADR/` directory becomes the first thing a new contributor reads.
- Disagreements get relitigated against a written rationale rather than against
  someone's memory of a conversation.
