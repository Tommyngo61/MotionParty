-- Continuous aggregates: 1 m / 5 m / 1 h rollups, retained 2 years.
--
-- NO TRANSACTION because TimescaleDB refuses to create a continuous aggregate
-- inside a transaction block, and goose wraps every other migration in one.
-- That also means this migration is not atomic: if it fails halfway, re-running
-- it after fixing the cause is safe, because each view is created once and
-- goose records the version only on success.
--
-- Why three resolutions rather than one: the fleet grid draws a one-hour
-- sparkline per node for up to 200 nodes at once (the 1 m view), node detail
-- draws a day (the 5 m view), and the bandwidth-against-cap and energy accrual
-- panels reason in months (the 1 h view). Serving any of those from raw
-- samples is a scan we would pay for on every page load.

-- +goose NO TRANSACTION

-- +goose Up

CREATE MATERIALIZED VIEW metric_samples_1m
WITH (timescaledb.continuous) AS
SELECT
    node_id,
    time_bucket(INTERVAL '1 minute', t) AS bucket,
    avg(cpu_util_pct)              AS cpu_util_pct_avg,
    max(cpu_util_pct)              AS cpu_util_pct_max,
    avg(cpu_temp_c)                AS cpu_temp_c_avg,
    max(cpu_temp_c)                AS cpu_temp_c_max,
    bool_or(cpu_throttled)         AS cpu_throttled,
    avg(mem_used_mb)               AS mem_used_mb_avg,
    max(mem_total_mb)              AS mem_total_mb,
    avg(net_rx_mbps)               AS net_rx_mbps_avg,
    avg(net_tx_mbps)               AS net_tx_mbps_avg,
    max(net_tx_mbps)               AS net_tx_mbps_max,
    max(net_rx_total_gb_month)     AS net_rx_total_gb_month,
    max(net_tx_total_gb_month)     AS net_tx_total_gb_month,
    max(net_control_plane_bytes)   AS net_control_plane_bytes,
    max(host_uptime_s)             AS host_uptime_s,
    max(abs(host_clock_skew_ms))   AS clock_skew_ms_max,
    count(*)                       AS samples
FROM metric_samples
GROUP BY node_id, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW metric_samples_5m
WITH (timescaledb.continuous) AS
SELECT
    node_id,
    time_bucket(INTERVAL '5 minutes', t) AS bucket,
    avg(cpu_util_pct)            AS cpu_util_pct_avg,
    max(cpu_util_pct)            AS cpu_util_pct_max,
    avg(cpu_temp_c)              AS cpu_temp_c_avg,
    max(cpu_temp_c)              AS cpu_temp_c_max,
    bool_or(cpu_throttled)       AS cpu_throttled,
    avg(mem_used_mb)             AS mem_used_mb_avg,
    max(mem_total_mb)            AS mem_total_mb,
    avg(net_rx_mbps)             AS net_rx_mbps_avg,
    avg(net_tx_mbps)             AS net_tx_mbps_avg,
    max(net_control_plane_bytes) AS net_control_plane_bytes,
    count(*)                     AS samples
FROM metric_samples
GROUP BY node_id, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW metric_samples_1h
WITH (timescaledb.continuous) AS
SELECT
    node_id,
    time_bucket(INTERVAL '1 hour', t) AS bucket,
    avg(cpu_util_pct)            AS cpu_util_pct_avg,
    max(cpu_util_pct)            AS cpu_util_pct_max,
    avg(cpu_temp_c)              AS cpu_temp_c_avg,
    max(cpu_temp_c)              AS cpu_temp_c_max,
    bool_or(cpu_throttled)       AS cpu_throttled,
    avg(mem_used_mb)             AS mem_used_mb_avg,
    avg(net_rx_mbps)             AS net_rx_mbps_avg,
    avg(net_tx_mbps)             AS net_tx_mbps_avg,
    -- These three are the inputs to the bandwidth-against-cap panel, which is
    -- the metric the whole residential-hosting argument rests on.
    max(net_rx_total_gb_month)   AS net_rx_total_gb_month,
    max(net_tx_total_gb_month)   AS net_tx_total_gb_month,
    max(net_control_plane_bytes) AS net_control_plane_bytes,
    count(*)                     AS samples
FROM metric_samples
GROUP BY node_id, bucket
WITH NO DATA;

-- GPU rollups. Separate from the host rollups because they are per-device, and
-- because the degradation signals the health scorer cares about — throttling,
-- ECC, PCIe downtraining — are all GPU-side.
CREATE MATERIALIZED VIEW gpu_samples_1m
WITH (timescaledb.continuous) AS
SELECT
    node_id,
    gpu_uuid,
    time_bucket(INTERVAL '1 minute', t) AS bucket,
    min(gpu_index)          AS gpu_index,
    avg(util_pct)           AS util_pct_avg,
    max(util_pct)           AS util_pct_max,
    avg(mem_used_mb)        AS mem_used_mb_avg,
    max(mem_total_mb)       AS mem_total_mb,
    avg(temp_c)             AS temp_c_avg,
    max(temp_c)             AS temp_c_max,
    avg(power_w)            AS power_w_avg,
    max(power_w)            AS power_w_max,
    max(power_limit_w)      AS power_limit_w,
    avg(fan_pct)            AS fan_pct_avg,
    max(ecc_errors)         AS ecc_errors_max,
    -- min(), not max(): a link that dropped to x8 for part of the minute is
    -- the fact worth surfacing. Averaging would hide it.
    min(pcie_gen)           AS pcie_gen_min,
    min(pcie_width)         AS pcie_width_min,
    count(*) FILTER (WHERE array_length(throttle_reasons, 1) > 0) AS throttled_samples,
    count(*)                AS samples
FROM gpu_samples
GROUP BY node_id, gpu_uuid, bucket
WITH NO DATA;

CREATE MATERIALIZED VIEW gpu_samples_1h
WITH (timescaledb.continuous) AS
SELECT
    node_id,
    gpu_uuid,
    time_bucket(INTERVAL '1 hour', t) AS bucket,
    min(gpu_index)     AS gpu_index,
    avg(util_pct)      AS util_pct_avg,
    max(util_pct)      AS util_pct_max,
    avg(temp_c)        AS temp_c_avg,
    max(temp_c)        AS temp_c_max,
    avg(power_w)       AS power_w_avg,
    max(power_w)       AS power_w_max,
    max(power_limit_w) AS power_limit_w,
    max(ecc_errors)    AS ecc_errors_max,
    min(pcie_gen)      AS pcie_gen_min,
    min(pcie_width)    AS pcie_width_min,
    count(*) FILTER (WHERE array_length(throttle_reasons, 1) > 0) AS throttled_samples,
    count(*)           AS samples
FROM gpu_samples
GROUP BY node_id, gpu_uuid, bucket
WITH NO DATA;

-- Refresh policies.
--
-- start_offset is deliberately wider than the flush interval: a node coming
-- back from an outage backfills into buckets that are already materialised, and
-- a narrow window would leave those buckets permanently wrong. Two hours covers
-- the recent-first-then-backfill upload the agent performs on reconnect.
SELECT add_continuous_aggregate_policy('metric_samples_1m',
    start_offset => INTERVAL '2 hours', end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '1 minute');
SELECT add_continuous_aggregate_policy('metric_samples_5m',
    start_offset => INTERVAL '6 hours', end_offset => INTERVAL '5 minutes',
    schedule_interval => INTERVAL '5 minutes');
SELECT add_continuous_aggregate_policy('metric_samples_1h',
    start_offset => INTERVAL '3 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');
SELECT add_continuous_aggregate_policy('gpu_samples_1m',
    start_offset => INTERVAL '2 hours', end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '1 minute');
SELECT add_continuous_aggregate_policy('gpu_samples_1h',
    start_offset => INTERVAL '3 days', end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '30 minutes');

-- Rollup retention: the contract's 2 years on the hourly views. The 1 m and
-- 5 m views exist to serve recent charts, so they are trimmed well before that
-- — keeping a minute-resolution view for two years would cost more than the
-- raw samples it summarises.
SELECT add_retention_policy('metric_samples_1m', INTERVAL '14 days');
SELECT add_retention_policy('metric_samples_5m', INTERVAL '90 days');
SELECT add_retention_policy('metric_samples_1h', INTERVAL '2 years');
SELECT add_retention_policy('gpu_samples_1m',    INTERVAL '14 days');
SELECT add_retention_policy('gpu_samples_1h',    INTERVAL '2 years');

-- +goose Down
DROP MATERIALIZED VIEW IF EXISTS gpu_samples_1h;
DROP MATERIALIZED VIEW IF EXISTS gpu_samples_1m;
DROP MATERIALIZED VIEW IF EXISTS metric_samples_1h;
DROP MATERIALIZED VIEW IF EXISTS metric_samples_5m;
DROP MATERIALIZED VIEW IF EXISTS metric_samples_1m;
