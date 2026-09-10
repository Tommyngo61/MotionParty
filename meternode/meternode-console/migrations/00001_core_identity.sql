-- Core identity: who a node is, which house it lives in, and what it may do.
--
-- Everything in this migration is a regular table. Telemetry (00002) is where
-- the hypertables live.

-- +goose Up

-- pgcrypto gives us gen_random_uuid() and digest() for token hashing.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- A site is a residence. One site may eventually hold several nodes, so the
-- relationship is one-to-many from the start even though today it is one-to-one.
--
-- The homeowner columns are deliberately minimal. This table will accumulate
-- payout and contract data later; for phase 1 it holds only what an operator
-- needs to answer "whose house is this and how do I reach them".
CREATE TABLE sites (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    short_name      text NOT NULL UNIQUE,          -- shown on every node tile; keep it short
    name            text NOT NULL,
    timezone        text NOT NULL DEFAULT 'UTC',   -- maintenance windows are local to the house
    address_line1   text,
    address_line2   text,
    city            text,
    region          text,
    postal_code     text,
    country         text NOT NULL DEFAULT 'US',
    contact_name    text,
    contact_email   text,
    contact_phone   text,
    -- The ISP and plan matter operationally: the monthly cap and the upstream
    -- floor are per-site facts, and "does this ISP forbid commercial use" is a
    -- question we have to be able to answer per site.
    isp             text,
    plan_down_mbps  integer,
    plan_up_mbps    integer,
    monthly_cap_gb  integer,
    notes           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Node lifecycle state. This is the ADMINISTRATIVE state an operator sets, not
-- the health state the controller derives from telemetry. The two are
-- deliberately separate columns of thought: an operator can quarantine a
-- perfectly healthy node, and a healthy-by-policy node can still be reporting
-- ECC errors.
CREATE TYPE node_lifecycle AS ENUM (
    'unenrolled',   -- a row exists (pre-provisioned) but no credential issued
    'enrolled',     -- normal
    'quarantined',  -- operator- or system-held; must not run workloads
    'retired'       -- decommissioned; keeps its history
);

CREATE TABLE nodes (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    site_id          uuid REFERENCES sites(id) ON DELETE RESTRICT,
    name             text NOT NULL,
    lifecycle        node_lifecycle NOT NULL DEFAULT 'unenrolled',
    agent_version    text,
    enrolled_at      timestamptz,
    last_seen_at     timestamptz,
    last_seq         bigint NOT NULL DEFAULT 0,   -- for gap detection at ingest
    boot_id          text,
    quarantine_reason text,
    labels           jsonb NOT NULL DEFAULT '{}'::jsonb,
    notes            text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (site_id, name)
);

CREATE INDEX nodes_site_idx      ON nodes (site_id);
CREATE INDEX nodes_lifecycle_idx ON nodes (lifecycle);
-- The fleet grid sorts by last-seen constantly; NULLS FIRST puts the nodes
-- that have never reported at the top, which is where they belong.
CREATE INDEX nodes_last_seen_idx ON nodes (last_seen_at DESC NULLS FIRST);

-- The hardware a node identity is bound to.
--
-- Both the hash and its components are stored. The hash is what enrollment
-- compares; the components are what a human reads when the hash changes,
-- because "the GPU was swapped" and "the motherboard was swapped" are very
-- different conversations and only one of them is routine.
CREATE TABLE node_hardware (
    node_id           uuid PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    fingerprint_hash  text NOT NULL,
    motherboard_uuid  text,
    gpu_uuids         text[] NOT NULL DEFAULT '{}',
    primary_nic_mac   text,
    -- False when the board reports a vendor placeholder UUID, so a weaker
    -- identity binding is visible rather than assumed.
    fingerprint_complete boolean NOT NULL DEFAULT false,
    inventory         jsonb NOT NULL DEFAULT '{}'::jsonb,  -- HardwareInventory
    bound_at          timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- A fingerprint must map to at most one node. Without this constraint a cloned
-- provisioning image silently becomes two nodes sharing one machine's identity,
-- which is the single worst outcome available to this subsystem.
CREATE UNIQUE INDEX node_hardware_fingerprint_key ON node_hardware (fingerprint_hash);

-- One-time enrollment tokens.
--
-- Only the hash is stored. A token is shown exactly once, at mint time, and is
-- unrecoverable afterwards — an operator who loses it mints another. That is
-- cheap; a token sitting in plaintext in a table that gets dumped into a
-- support bundle is not.
CREATE TABLE enrollment_tokens (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash       bytea NOT NULL UNIQUE,   -- sha256 of the token
    token_prefix     text NOT NULL,           -- first 8 chars, so a human can identify it in a list
    label            text,                    -- e.g. "Ridgefield install, 2026-09-14"
    site_id          uuid REFERENCES sites(id) ON DELETE SET NULL,
    node_name_hint   text,
    created_by       text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz,
    consumed_by_node uuid REFERENCES nodes(id) ON DELETE SET NULL,
    revoked_at       timestamptz,
    revoked_by       text,
    revoke_reason    text
);

CREATE INDEX enrollment_tokens_open_idx ON enrollment_tokens (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

-- The controller's command-signing keys.
--
-- More than one may be active at a time: a fleet is always partly offline, so
-- rotating a signing key means both keys must verify for as long as the
-- slowest node takes to come back and re-enroll its pinned key set.
CREATE TABLE controller_keys (
    key_id      text PRIMARY KEY,
    public_key  bytea NOT NULL,
    -- The private half is NOT stored here in production; it comes from the
    -- environment or a KMS. This column exists only for the dev/compose
    -- profile, which generates a throwaway key on first boot.
    private_key bytea,
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    retired_at  timestamptz
);

-- Per-node credentials. The credential itself is self-contained and signed, so
-- this table exists for revocation and for answering "which key signed this
-- node's identity" during a rotation.
CREATE TABLE node_credentials (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id           uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    key_id            text NOT NULL REFERENCES controller_keys(key_id),
    node_pub          bytea NOT NULL,   -- ed25519 public key the agent generated
    fingerprint_hash  text NOT NULL,
    credential_sha256 bytea NOT NULL,   -- identifies a specific issued credential
    issued_at         timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz,
    revoked_at        timestamptz,
    revoked_by        text,
    revoke_reason     text
);

CREATE INDEX node_credentials_node_idx ON node_credentials (node_id);
-- A node has at most one credential that is not revoked. Re-enrollment revokes
-- the previous one in the same transaction that issues the new one.
CREATE UNIQUE INDEX node_credentials_active_key ON node_credentials (node_id)
    WHERE revoked_at IS NULL;

-- Re-attestation: raised when an enrolled node presents a fingerprint that does
-- not match the one bound to it.
--
-- This is never resolved automatically. Either the hardware changed and we
-- should know, or a provisioning image was cloned onto a second machine — and
-- silently re-enrolling turns the second case into two nodes sharing an
-- identity.
CREATE TYPE reattestation_state AS ENUM ('pending', 'approved', 'rejected');

CREATE TABLE node_reattestations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id         uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    state           reattestation_state NOT NULL DEFAULT 'pending',
    bound_hash      text NOT NULL,
    presented_hash  text NOT NULL,
    changed_fields  text[] NOT NULL DEFAULT '{}',
    presented_inventory jsonb NOT NULL DEFAULT '{}'::jsonb,
    presented_from_ip inet,
    created_at      timestamptz NOT NULL DEFAULT now(),
    resolved_at     timestamptz,
    resolved_by     text,
    resolution_note text
);

CREATE INDEX node_reattestations_pending_idx ON node_reattestations (node_id)
    WHERE state = 'pending';

-- Operators. OIDC-ready: subject/issuer identify a federated identity, and the
-- role is ours. No shared logins — every audit row names a person.
CREATE TYPE operator_role AS ENUM ('viewer', 'operator', 'admin');

CREATE TABLE operators (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE,
    display_name  text NOT NULL,
    role          operator_role NOT NULL DEFAULT 'viewer',
    oidc_issuer   text,
    oidc_subject  text,
    disabled_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    UNIQUE (oidc_issuer, oidc_subject)
);

-- Service tokens for CI, the simulator, and scripts. Hashed, never stored raw.
-- These are not a login path for humans.
CREATE TABLE api_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash  bytea NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    name        text NOT NULL,
    role        operator_role NOT NULL DEFAULT 'viewer',
    created_by  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked_at  timestamptz,
    last_used_at timestamptz
);

-- Every operator-initiated action lands here: actor, node, command, result.
-- Append-only by convention; nothing in the application updates or deletes a row.
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor       text NOT NULL,          -- email, token name, or "system"
    actor_role  text,
    action      text NOT NULL,          -- stable verb, e.g. "enroll_token.mint"
    node_id     uuid REFERENCES nodes(id) ON DELETE SET NULL,
    site_id     uuid REFERENCES sites(id) ON DELETE SET NULL,
    command_id  uuid,
    target      text,                   -- free-form secondary target
    result      text NOT NULL,          -- "ok" | "denied" | "error"
    detail      jsonb NOT NULL DEFAULT '{}'::jsonb,
    request_id  text,
    remote_addr inet
);

CREATE INDEX audit_log_at_idx     ON audit_log (at DESC);
CREATE INDEX audit_log_node_idx   ON audit_log (node_id, at DESC);
CREATE INDEX audit_log_actor_idx  ON audit_log (actor, at DESC);
CREATE INDEX audit_log_action_idx ON audit_log (action, at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS operators;
DROP TYPE  IF EXISTS operator_role;
DROP TABLE IF EXISTS node_reattestations;
DROP TYPE  IF EXISTS reattestation_state;
DROP TABLE IF EXISTS node_credentials;
DROP TABLE IF EXISTS controller_keys;
DROP TABLE IF EXISTS enrollment_tokens;
DROP TABLE IF EXISTS node_hardware;
DROP TABLE IF EXISTS nodes;
DROP TYPE  IF EXISTS node_lifecycle;
DROP TABLE IF EXISTS sites;
