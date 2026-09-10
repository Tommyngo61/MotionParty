# ADR 0008 — Refusing an enrollment and recording why are separate transactions

**Status:** accepted · **Date:** 2026-09-10 · **Supersedes part of** ADR 0005

## Context

ADR 0005 established that a changed hardware fingerprint never re-binds: it
opens a re-attestation record, quarantines the node, raises a CRITICAL event,
and refuses the enrollment with a 409.

The first implementation did all of that inside the enrollment transaction. It
was wrong, and the way it was wrong is worth writing down, because the mistake
is a natural one.

Refusing the enrollment means returning an error from the transaction closure,
which rolls the transaction back. That rollback is **necessary**: the one-time
token must stay unspent, so a field tech can retry with it once an operator
releases the node. A consumed token there is a second site visit.

But the rollback also discarded the re-attestation record, the quarantine, and
the event — the exact evidence an operator needs in order to make that decision.
The operator queue stayed empty. The node stayed `enrolled`. Nothing was logged
at CRITICAL. From the outside the node simply retried forever and no human ever
learned why.

It survived unit testing because the in-memory fake's `InTx` ran the closure
against live maps and rolled nothing back, so the writes appeared to stick. It
was found by running the real controller against real PostgreSQL and looking at
the tables.

## Decision

Detection and recording are separate operations in separate transactions.

1. **Detect**, inside the enrollment transaction: `detectReattestation` compares
   the bound fingerprint to the presented one and returns a
   `*ReattestationError`. It writes nothing, because everything it wrote would
   be discarded.
2. **Refuse**: the error propagates, the transaction rolls back, the token stays
   unspent.
3. **Record**, in its own committed transaction: `recordReattestation` inserts
   the re-attestation row, quarantines the node, raises the event, and writes
   the audit entry.

If step 3 fails, the enrollment is *still* refused — a failure to write the
audit trail must never turn into a successful enrollment. The error returned
wraps both.

Step 3 re-checks for a pending record under its own transaction, so two agents
(or one agent retrying quickly) that both reach step 1 before either reaches
step 3 produce one row on the operator queue, not two.

## Consequences

- Two round trips to the database on the refusal path. This path is rare and
  already a human-scale event; correctness wins easily.
- There is a window where a mismatch has been detected but not yet recorded. If
  the process dies inside it, the next enrollment attempt detects and records
  it again — the agent retries with backoff forever, which makes this
  self-healing rather than a lost event.
- **The in-memory fake now performs real rollback.** That was the actual root
  cause: a fake whose transactions always commit is not a simplification, it is
  a blind spot. `TestRefusedEnrollmentStillRecordsTheReattestation` and
  `TestFingerprintChangeQuarantinesInTheDatabase` both fail against the old
  implementation.
- The general rule this establishes: **rejecting something and recording that
  you rejected it are different jobs, and they do not share a transaction.**
  The same shape will appear in command dispatch (M5) when a node rejects a
  signed command, and in the ingest gateway when it drops a malformed envelope.
