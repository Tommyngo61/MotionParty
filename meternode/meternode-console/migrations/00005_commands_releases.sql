-- Command dispatch and agent release management.

-- +goose Up

-- Command lifecycle. `expired` is a terminal state distinct from `failed`
-- because a command that outlived its TTL was never delivered — a queued
-- restart_agent that reaches a node three days after an operator issued it is
-- a bug, not a late success, and it must be dropped rather than replayed.
CREATE TYPE command_state AS ENUM ('queued', 'sent', 'acked', 'succeeded', 'failed', 'expired', 'rejected', 'cancelled');

CREATE TABLE commands (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id     uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind        text NOT NULL,
    args        jsonb NOT NULL DEFAULT '{}'::jsonb,
    state       command_state NOT NULL DEFAULT 'queued',

    issued_by   text NOT NULL,
    issued_at   timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    sent_at     timestamptz,
    finished_at timestamptz,

    -- The signature is stored so an operator can prove after the fact exactly
    -- what was authorised, and so a redelivery over a reconnected socket sends
    -- the identical signed bytes rather than re-signing (which would move
    -- issued_at and change what the agent verifies).
    key_id      text NOT NULL REFERENCES controller_keys(key_id),
    signature   bytea NOT NULL,
    signed_body bytea NOT NULL,

    CHECK (expires_at > issued_at)
);

CREATE INDEX commands_node_idx    ON commands (node_id, issued_at DESC);
CREATE INDEX commands_pending_idx ON commands (node_id, expires_at) WHERE state IN ('queued', 'sent');

CREATE TABLE command_results (
    command_id  uuid PRIMARY KEY REFERENCES commands(id) ON DELETE CASCADE,
    node_id     uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    started_at  timestamptz,
    finished_at timestamptz,
    received_at timestamptz NOT NULL DEFAULT now(),
    ok          boolean NOT NULL,
    code        text NOT NULL,
    detail      jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Bounded. Large diagnostics bundles are uploaded out of band; a node on a
    -- 5 Mbps upstream cannot ship a log file inside a telemetry frame.
    output      text NOT NULL DEFAULT ''
);

CREATE TYPE release_channel AS ENUM ('canary', 'stable');

CREATE TABLE agent_releases (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    version      text NOT NULL,
    channel      release_channel NOT NULL,
    -- The agent verifies this signature before it stages a build. An unsigned
    -- or mis-signed release is refused on the node, not just here.
    sha256       bytea NOT NULL,
    signature    bytea NOT NULL,
    signing_key_id text NOT NULL REFERENCES controller_keys(key_id),
    size_bytes   bigint NOT NULL,
    url          text NOT NULL,
    notes        text NOT NULL DEFAULT '',

    -- Staged rollout. rollout_pct is the share of the channel's nodes eligible
    -- so far; the halt columns record an automatic stop when the canary cohort
    -- regresses on crash rate or reconnect rate.
    rollout_pct  smallint NOT NULL DEFAULT 0 CHECK (rollout_pct BETWEEN 0 AND 100),
    halted       boolean NOT NULL DEFAULT false,
    halted_at    timestamptz,
    halted_reason text,

    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE (version, channel)
);

CREATE INDEX agent_releases_channel_idx ON agent_releases (channel, published_at DESC);

-- Which release each node was told to move to, and what happened. This is the
-- table the automatic-halt check reads: a canary cohort whose nodes fail to
-- reach a healthy connected state is what stops a bad build reaching the fleet.
CREATE TABLE agent_rollouts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id  uuid NOT NULL REFERENCES agent_releases(id) ON DELETE CASCADE,
    node_id     uuid NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    command_id  uuid REFERENCES commands(id) ON DELETE SET NULL,
    offered_at  timestamptz NOT NULL DEFAULT now(),
    applied_at  timestamptz,
    healthy_at  timestamptz,
    rolled_back_at timestamptz,
    failure_reason text,
    UNIQUE (release_id, node_id)
);

CREATE INDEX agent_rollouts_release_idx ON agent_rollouts (release_id);

-- +goose Down
DROP TABLE IF EXISTS agent_rollouts;
DROP TABLE IF EXISTS agent_releases;
DROP TYPE  IF EXISTS release_channel;
DROP TABLE IF EXISTS command_results;
DROP TABLE IF EXISTS commands;
DROP TYPE  IF EXISTS command_state;
