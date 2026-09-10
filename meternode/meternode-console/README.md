# MeterNode Console

The control plane for a fleet of company-owned GPU compute nodes hosted in
private residences. Phase 1 is monitoring only.

Part of MeterNode, alongside [`meternode-agent`](../meternode-agent) (the
host-side supervisor) and [`meternode-proto`](../meternode-proto) (the shared
wire schema).

## Status

| Milestone | | |
| --- | --- | --- |
| M0 | scaffold, migrations, health endpoint, CI, compose | ✅ |
| M1 | enrollment, credential issuance, hardware fingerprint binding | ✅ |
| M2 | WebSocket ingest, Timescale writes, full simulator | next |
| M3 | operator UI | blocked — see [DESIGN-INHERITANCE.md](DESIGN-INHERITANCE.md) |
| M4 | health scoring, alerting | |
| M5 | command dispatch, agent releases | |

## Quick start

```bash
make dev          # Postgres/Timescale + Redis + the controller
make simulate     # 25 simulated nodes enrol against it
```

Then:

```bash
curl -s localhost:8080/healthz | jq
curl -s localhost:8080/v1/schema | jq

# Mint an enrollment token (shown once, never recoverable)
curl -s -X POST localhost:8080/v1/admin/enrollment-tokens \
  -H 'Authorization: Bearer dev-admin-token' \
  -H 'Content-Type: application/json' \
  -d '{"label":"Ridgefield install"}' | jq
```

## Development

```bash
make help              # every target
make test              # unit tests, no database needed
make test-integration  # against the compose database
make lint              # vet + gofmt check
make ci                # what CI runs, minus the database job
make migrate           # apply migrations to the dev database
```

The unit tests need nothing but Go. That is deliberate: `internal/store` is the
only package that imports a database driver, so the decisions worth testing —
what happens when a fingerprint changes, whether a token can be replayed, what a
viewer may do — run in milliseconds against in-memory fakes. What a fake cannot
honestly test (transaction atomicity, unique constraints, row locking under
concurrency) lives in `internal/store/integration_test.go` behind the
`integration` build tag.

## The simulator

Most of this system's failure modes only appear under bad network conditions,
and none of them appear with one agent on loopback.

```bash
go run ./cmd/simulator -n 200 -concurrency 64 \
  -latency 300ms -jitter 100ms -fail-pct 5 \
  -admin-token dev-admin-token
```

It enrols against the real endpoint, verifies every issued credential the way a
real agent does before storing it, and reports latency percentiles. Simulated
nodes are deterministic per index, so a re-run enrols the *same* machines —
which is what makes re-enrollment and re-attestation reproducible rather than a
fresh fleet each time.

## Configuration

Environment only; no config file. Defaults work for local development.

| Variable | Default | Notes |
| --- | --- | --- |
| `METERNODE_ENV` | `dev` | `dev` \| `staging` \| `prod`. Gates every developer convenience. |
| `METERNODE_HTTP_ADDR` | `:8080` | |
| `METERNODE_PUBLIC_URL` | `http://localhost:8080` | The hostname agents connect to. Must be https outside dev. |
| `METERNODE_DATABASE_URL` | local dev DSN | |
| `METERNODE_REDIS_URL` | `redis://localhost:6379/0` | |
| `METERNODE_SIGNING_KEY_ID` | `ck-dev-1` | |
| `METERNODE_SIGNING_KEY_SEED` | — | base64 ed25519 seed. **Required outside dev.** |
| `METERNODE_ENROLL_TOKEN_TTL` | `72h` | |
| `METERNODE_CREDENTIAL_TTL` | `0` (no expiry) | A node offline for a week must not lock itself out. |
| `METERNODE_BOOTSTRAP_ADMIN_TOKEN` | — | Dev only; refused elsewhere by two independent checks. |
| `METERNODE_LOG_LEVEL` / `_FORMAT` | `info` / `json` | |

`config.Validate` refuses configurations that are unsafe rather than merely
unusual: a generated signing key outside dev (a restart would silently
invalidate every enrolled agent), a bootstrap admin token outside dev, a
plaintext public URL outside dev.

## Endpoints

**Open** — `GET /healthz`, `GET /readyz`, `GET /v1/schema`.
Liveness never touches the database; readiness does. See ARCHITECTURE.md for why
that distinction matters here.

**Agent-facing** — `POST /v1/enroll`.
Unauthenticated by necessity: the one-time token in the body *is* the
authentication.

**Operator** — `/v1/admin/*`, bearer token, role-gated.

| | |
| --- | --- |
| `GET /v1/admin/whoami` | |
| `GET /v1/admin/enrollment-tokens` | viewer |
| `POST /v1/admin/enrollment-tokens` | operator |
| `POST /v1/admin/enrollment-tokens/{id}/revoke` | operator |

## Reading further

- [ARCHITECTURE.md](ARCHITECTURE.md) — the shape of the system and why.
- [ADR/](ADR/) — the decisions, one file each, with the alternatives we rejected.
- [DESIGN-INHERITANCE.md](DESIGN-INHERITANCE.md) — what Console inherits from the
  RMS portal, and why the UI is currently blocked.
