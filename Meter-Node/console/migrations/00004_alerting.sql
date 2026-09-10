-- Alerting.
--
-- The design constraint here is not expressiveness, it is alert fatigue. A
-- fleet of a few hundred residential machines generates a constant drizzle of
-- transient conditions — a homeowner unplugs a node to vacuum, an ISP blips, a
-- GPU touches its thermal limit for four seconds — and a rule engine that
-- pages on any of those is one an operator will mute within a week.
--
-- So every rule carries a REQUIRED sustain duration, and hysteresis is
-- structural rather than optional: a rule clears at a different threshold from
-- the one that opened it. There is no way to express "fire on a single sample"
-- in this schema, and that is the point.

-- +goose Up

CREATE TYPE alert_state AS ENUM ('pending', 'firing', 'resolved', 'suppressed');

CREATE TABLE alert_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL UNIQUE,
    description   text NOT NULL DEFAULT '',
    enabled       boolean NOT NULL DEFAULT true,
    severity      text NOT NULL DEFAULT 'WARN',   -- INFO | WARN | ERROR | CRITICAL

    -- What is evaluated. `subject` names the stream (metric path like
    -- "gpu.temp_c", or "event" for code-driven rules); `comparison` and
    -- `threshold` form the predicate.
    subject       text NOT NULL,
    comparison    text NOT NULL,                  -- gt | gte | lt | lte | eq | ne | present
    threshold     double precision,
    event_code    text,                           -- for subject = 'event'

    -- Sustain, and the hysteresis that stops a value sitting on the threshold
    -- from flapping. clear_threshold defaults to threshold when null, but a
    -- rule that pages should always set it apart.
    for_duration_s   integer NOT NULL CHECK (for_duration_s >= 60),
    clear_threshold  double precision,
    clear_duration_s integer NOT NULL DEFAULT 300 CHECK (clear_duration_s >= 60),

    -- Scope. Empty means the whole fleet.
    site_ids      uuid[] NOT NULL DEFAULT '{}',
    node_labels   jsonb  NOT NULL DEFAULT '{}'::jsonb,

    -- Grouping. Site is the default because a whole-house outage is ONE
    -- problem: an ISP failure at a site with four nodes should page once, not
    -- four times.
    group_by      text NOT NULL DEFAULT 'site',   -- site | node | fleet
    sinks         uuid[] NOT NULL DEFAULT '{}',

    created_by    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notification_sinks (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    kind       text NOT NULL,                     -- webhook | email | slack
    -- Endpoint secrets are referenced by name and resolved from the
    -- environment at send time; a Slack webhook URL is a credential and does
    -- not belong in a table that gets dumped into a support bundle.
    config     jsonb NOT NULL DEFAULT '{}'::jsonb,
    secret_ref text,
    enabled    boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Maintenance windows suppress notification without suppressing evaluation:
-- an alert still opens and still shows in the UI, it just does not page. An
-- operator who silences a window then wants to know what happened in it.
CREATE TABLE maintenance_windows (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    site_id    uuid REFERENCES sites(id) ON DELETE CASCADE,
    node_id    uuid REFERENCES nodes(id) ON DELETE CASCADE,
    starts_at  timestamptz NOT NULL,
    ends_at    timestamptz NOT NULL,
    reason     text NOT NULL DEFAULT '',
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at)
);

CREATE INDEX maintenance_windows_active_idx ON maintenance_windows (starts_at, ends_at);

CREATE TABLE alerts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id      uuid NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    node_id      uuid REFERENCES nodes(id) ON DELETE CASCADE,
    site_id      uuid REFERENCES sites(id) ON DELETE CASCADE,
    state        alert_state NOT NULL DEFAULT 'pending',
    severity     text NOT NULL,

    -- dedup_key is what makes this table idempotent under a rule that keeps
    -- matching. One open alert per (rule, scope); re-matching updates
    -- last_value and last_seen_at instead of inserting.
    dedup_key    text NOT NULL,

    first_seen_at timestamptz NOT NULL DEFAULT now(),
    opened_at     timestamptz,        -- when it crossed for_duration_s
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at   timestamptz,
    last_value    double precision,
    notified_at   timestamptz,
    notify_count  integer NOT NULL DEFAULT 0,
    suppressed_by uuid REFERENCES maintenance_windows(id) ON DELETE SET NULL,
    detail        jsonb NOT NULL DEFAULT '{}'::jsonb,
    acknowledged_at timestamptz,
    acknowledged_by text
);

CREATE UNIQUE INDEX alerts_open_dedup_key ON alerts (dedup_key) WHERE resolved_at IS NULL;
CREATE INDEX alerts_node_idx  ON alerts (node_id, first_seen_at DESC);
CREATE INDEX alerts_state_idx ON alerts (state) WHERE resolved_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS maintenance_windows;
DROP TABLE IF EXISTS notification_sinks;
DROP TABLE IF EXISTS alert_rules;
DROP TYPE  IF EXISTS alert_state;
