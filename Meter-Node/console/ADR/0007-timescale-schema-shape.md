# ADR 0007 — TimescaleDB is a hard requirement, and GPUs get their own hypertable

**Status:** accepted · **Date:** 2026-09-10

## Context

The stack specifies PostgreSQL 16 with TimescaleDB for metrics. Two shape
questions follow from that: whether Timescale is optional, and whether the
per-GPU and per-disk arrays inside a `Sample` live as JSONB on one row or as
their own tables.

## Decision

**Timescale is required, not optional.** An early draft guarded every
hypertable behind an extension check so plain `postgres:16` would still produce
a working schema. That was dropped: the continuous aggregates cannot be created
inside a transaction (so they need their own `NO TRANSACTION` migration, where a
conditional guard is not available anyway), and every screen in the operator UI
is a time-bucketed aggregate. A "working" schema without the rollups is one that
falls over the first time it is pointed at a month of fleet data. Both
`deploy/docker-compose.yml` and CI run `timescale/timescaledb:2.17.2-pg16`.

**GPU and disk samples get their own hypertables**, rather than JSONB columns on
`metric_samples`. GPU telemetry is the point of this product. "Show me every
node whose GPU throttled in the last hour", "which cards are downtraining to
x8", "what is the ECC trend on this serial" must be index scans, not a JSONB
unnest across a month of samples. The contract names `metric_samples`; this
keeps that table as the host-level one and adds `gpu_samples`, `disk_samples`,
and `container_samples` beside it.

**Retention** follows the contract — raw samples 30 days, hourly rollups 2
years — with two deliberate departures:

- `heartbeats` is retained 7 days, not 30. It is the highest-volume table in the
  system (35 M rows/month for 200 nodes) and answers a question the rollups
  answer nearly as well after a week.
- The 1 m and 5 m rollups are retained 14 and 90 days. They exist to serve
  recent charts; a minute-resolution view kept for two years would cost more
  than the raw samples it summarises.

## Continuous aggregate refresh windows

`start_offset` is set much wider than the flush interval (2 hours for the 1 m
views, 3 days for the hourly ones). A node returning from an outage backfills
into buckets that are already materialised, and a narrow window would leave
those buckets permanently wrong — a node that was offline would show as a
permanent hole rather than a filled-in one.

## Consequences

- A developer cannot point the controller at a bare Postgres. Accepted: `make
  dev` is one command.
- Ingest writes to up to four tables per sample. Mitigated by batching: the
  gateway accumulates and writes via `COPY` (M2).
- A query that wants host and GPU metrics together joins on `(node_id, t)`.
  Both are 1-day-chunked hypertables so the chunks line up.
- `TestMigrationsApplyCleanly` asserts the hypertables and continuous aggregates
  actually exist, because a plain table with the right columns looks fine right
  up until the first `time_bucket` over real data.
