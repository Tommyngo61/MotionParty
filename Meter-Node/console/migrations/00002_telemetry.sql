-- Telemetry storage: the hypertables, their retention, and their rollups.
--
-- TimescaleDB is a hard requirement, not an optimisation. Everything the fleet
-- grid does is a time-bucketed aggregate over a few hundred nodes, and the
-- continuous aggregates below are what make that a lookup instead of a scan.
-- Both `deploy/docker-compose.yml` and CI run timescale/timescaledb-ha:pg16.

-- +goose Up

CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Heartbeats. Every 15 s per node, forever: 200 nodes is ~35 M rows/month, so
-- this is a hypertable with short retention. It answers "was this node alive
-- at 14:32 last Tuesday" — beyond a week that question is answered from events
-- and metric rollups instead.
CREATE TABLE heartbeats (
    node_id       uuid        NOT NULL,
    t             timestamptz NOT NULL,
    seq           bigint      NOT NULL,
    uptime_s      integer     NOT NULL,
    clock_skew_ms integer     NOT NULL DEFAULT 0,
    degraded      boolean     NOT NULL DEFAULT false,
    -- Which controller replica held the socket. A node that keeps landing on a
    -- different replica is reconnecting, and that is a signal.
    replica       text
);

-- Host-level samples. One row per node per 10 s.
--
-- GPU and disk arrays get their own hypertables below rather than a jsonb
-- column here. GPU telemetry is the point of this product: "show me every node
-- whose GPU throttled in the last hour" must be an index scan, not a jsonb
-- unnest across a month of samples.
CREATE TABLE metric_samples (
    node_id            uuid        NOT NULL,
    t                  timestamptz NOT NULL,

    cpu_util_pct       real,
    cpu_load1          real,
    cpu_temp_c         real,
    cpu_freq_mhz       integer,
    cpu_throttled      boolean,

    mem_used_mb        integer,
    mem_total_mb       integer,
    swap_used_mb       integer,

    net_rx_mbps        real,
    net_tx_mbps        real,
    net_rx_total_gb_month real,
    net_tx_total_gb_month real,
    net_control_plane_bytes bigint,
    net_iface          text,

    host_uptime_s      bigint,
    host_agent_version text,
    host_kernel        text,
    host_os            text,
    host_boot_id       text,
    host_clock_skew_ms integer,

    -- Set when the sample arrived as reconnect backfill rather than live, and
    -- how many raw samples it stands for. Alert rules must not fire on an
    -- hour-old value just because it landed now.
    backfill           boolean NOT NULL DEFAULT false,
    downsample_factor  smallint NOT NULL DEFAULT 1,
    -- Collectors that failed or timed out for this sample.
    collector_errors   text[]
);

CREATE TABLE gpu_samples (
    node_id          uuid        NOT NULL,
    t                timestamptz NOT NULL,
    gpu_index        smallint    NOT NULL,
    gpu_uuid         text        NOT NULL,
    name             text,
    util_pct         real,
    mem_used_mb      integer,
    mem_total_mb     integer,
    temp_c           real,
    power_w          real,
    power_limit_w    real,
    fan_pct          real,
    sm_clock_mhz     integer,
    -- Current link state, not capability. PCIe downtraining under a hot
    -- residential desk is a real failure mode and is invisible if only the
    -- maximum is recorded.
    pcie_gen         smallint,
    pcie_width       smallint,
    ecc_errors       bigint,
    throttle_reasons text[],
    driver_version   text,
    backfill         boolean NOT NULL DEFAULT false
);

CREATE TABLE disk_samples (
    node_id    uuid        NOT NULL,
    t          timestamptz NOT NULL,
    mount      text        NOT NULL,
    used_gb    real,
    total_gb   real,
    read_mbps  real,
    write_mbps real,
    -- NULL means SMART could not be read, which is a different fact from SMART
    -- reporting a failure. Only the second should raise an alert.
    smart_ok   boolean,
    backfill   boolean NOT NULL DEFAULT false
);

CREATE TABLE container_samples (
    node_id      uuid        NOT NULL,
    t            timestamptz NOT NULL,
    container_id text        NOT NULL,
    image        text,
    state        text,
    cpu_pct      real,
    mem_mb       integer,
    restarts     integer
);

-- Events are discrete and low volume compared with samples, but they are what
-- an operator actually reads, so they are indexed for the timeline view.
CREATE TABLE events (
    id       bigserial   PRIMARY KEY,
    node_id  uuid        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    t        timestamptz NOT NULL,
    severity text        NOT NULL,   -- INFO | WARN | ERROR | CRITICAL
    code     text        NOT NULL,   -- stable, e.g. gpu.ecc_error
    message  text        NOT NULL DEFAULT '',
    detail   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Set for events the controller derived rather than the agent reported
    -- (health transitions, missed heartbeats).
    source   text        NOT NULL DEFAULT 'agent',
    received_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX events_node_t_idx  ON events (node_id, t DESC);
CREATE INDEX events_code_t_idx  ON events (code, t DESC);
CREATE INDEX events_sev_t_idx   ON events (severity, t DESC) WHERE severity IN ('ERROR', 'CRITICAL');

-- Hypertables and retention.

-- 1-day chunks. At the contract's 10 s resolution a 200-node fleet writes
-- roughly 1.7 M host samples a day, which keeps a chunk comfortably in memory
-- for the queries the fleet grid runs.
SELECT create_hypertable('heartbeats',        't', chunk_time_interval => INTERVAL '1 day');
SELECT create_hypertable('metric_samples',    't', chunk_time_interval => INTERVAL '1 day');
SELECT create_hypertable('gpu_samples',       't', chunk_time_interval => INTERVAL '1 day');
SELECT create_hypertable('disk_samples',      't', chunk_time_interval => INTERVAL '1 day');
SELECT create_hypertable('container_samples', 't', chunk_time_interval => INTERVAL '1 day');

CREATE INDEX heartbeats_node_t_idx        ON heartbeats (node_id, t DESC);
CREATE INDEX metric_samples_node_t_idx    ON metric_samples (node_id, t DESC);
CREATE INDEX gpu_samples_node_t_idx       ON gpu_samples (node_id, t DESC);
CREATE INDEX gpu_samples_uuid_t_idx       ON gpu_samples (gpu_uuid, t DESC);
CREATE INDEX disk_samples_node_t_idx      ON disk_samples (node_id, t DESC);
CREATE INDEX container_samples_node_t_idx ON container_samples (node_id, t DESC);

-- Retention, per the contract: raw samples 30 days, rollups 2 years (set on
-- the continuous aggregates in 00003).
SELECT add_retention_policy('metric_samples',    INTERVAL '30 days');
SELECT add_retention_policy('gpu_samples',       INTERVAL '30 days');
SELECT add_retention_policy('disk_samples',      INTERVAL '30 days');
SELECT add_retention_policy('container_samples', INTERVAL '30 days');
-- Heartbeats are the exception: 35 M rows a month to answer a question the
-- rollups answer nearly as well after a week.
SELECT add_retention_policy('heartbeats',        INTERVAL '7 days');

-- Continuous aggregates (1 m / 5 m / 1 h) live in 00003. Timescale refuses to
-- create them inside a transaction, and goose wraps each migration in one, so
-- they need their own NO TRANSACTION migration.

-- +goose Down
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS container_samples;
DROP TABLE IF EXISTS disk_samples;
DROP TABLE IF EXISTS gpu_samples;
DROP TABLE IF EXISTS metric_samples;
DROP TABLE IF EXISTS heartbeats;
