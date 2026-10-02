-- Keep diagnostics for incomplete inbound uploads, but exclude them from SLA.
-- Do not match upstream read/stream failures even if their message is identical.
-- The changed-row snapshot makes counter repair idempotent and preserves totals.
CREATE TABLE IF NOT EXISTS channel_monitor_v2_health_repairs (
    bucket_date DATE PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TEMP TABLE ops_inbound_body_read_health_repair ON COMMIT DROP AS
WITH changed AS (
    UPDATE ops_error_logs e
    SET is_business_limited = TRUE
    WHERE NOT e.is_business_limited
      AND e.status_code = 400
      AND lower(btrim(COALESCE(e.error_message, ''))) = 'failed to read request body'
      AND COALESCE(e.error_phase, '') NOT IN ('upstream', 'network', 'account_auth')
      AND COALESCE(e.error_owner, '') <> 'provider'
      AND COALESCE(e.error_source, '') <> 'upstream_http'
      AND e.upstream_status_code IS NULL
      AND COALESCE(NULLIF(e.upstream_errors, 'null'::jsonb), '[]'::jsonb) = '[]'::jsonb
    RETURNING created_at, platform, group_id, is_count_tokens
)
SELECT * FROM changed WHERE NOT is_count_tokens;

-- Repair existing hourly/daily scores without discarding success/latency data
-- or relying on source rows which may already have expired in these buckets.
WITH dimensions AS (
    SELECT date_trunc('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
           COALESCE(platform, 'unknown') AS platform, group_id
    FROM ops_inbound_body_read_health_repair
), corrections AS (
    SELECT bucket_start,
           CASE WHEN GROUPING(platform) = 1 THEN NULL ELSE platform END AS platform,
           CASE WHEN GROUPING(group_id) = 1 THEN NULL ELSE group_id END AS group_id,
           COUNT(*) AS excluded
    FROM dimensions
    GROUP BY GROUPING SETS ((bucket_start), (bucket_start, platform), (bucket_start, platform, group_id))
    HAVING GROUPING(group_id) = 1 OR group_id IS NOT NULL
)
UPDATE ops_metrics_hourly m
SET error_count_sla = GREATEST(0, m.error_count_sla - c.excluded),
    business_limited_count = LEAST(m.error_count_total, m.business_limited_count + c.excluded),
    computed_at = NOW()
FROM corrections c
WHERE m.bucket_start = c.bucket_start
  AND m.platform IS NOT DISTINCT FROM c.platform
  AND m.group_id IS NOT DISTINCT FROM c.group_id;

WITH dimensions AS (
    SELECT (created_at AT TIME ZONE 'UTC')::date AS bucket_date,
           COALESCE(platform, 'unknown') AS platform, group_id
    FROM ops_inbound_body_read_health_repair
), corrections AS (
    SELECT bucket_date,
           CASE WHEN GROUPING(platform) = 1 THEN NULL ELSE platform END AS platform,
           CASE WHEN GROUPING(group_id) = 1 THEN NULL ELSE group_id END AS group_id,
           COUNT(*) AS excluded
    FROM dimensions
    GROUP BY GROUPING SETS ((bucket_date), (bucket_date, platform), (bucket_date, platform, group_id))
    HAVING GROUPING(group_id) = 1 OR group_id IS NOT NULL
)
UPDATE ops_metrics_daily m
SET error_count_sla = GREATEST(0, m.error_count_sla - c.excluded),
    business_limited_count = LEAST(m.error_count_total, m.business_limited_count + c.excluded),
    computed_at = NOW()
FROM corrections c
WHERE m.bucket_date = c.bucket_date
  AND m.platform IS NOT DISTINCT FROM c.platform
  AND m.group_id IS NOT DISTINCT FROM c.group_id;

-- Queue only affected UTC days, newest first in the worker. Leave live coverage
-- and progress intact so current channel status remains available. A full day
-- rebuild also repairs 12h/1d rollups after the older 1m facts have expired.
INSERT INTO channel_monitor_v2_health_repairs (bucket_date)
SELECT DISTINCT (created_at AT TIME ZONE 'UTC')::date
FROM ops_inbound_body_read_health_repair
WHERE created_at >= date_trunc('minute', NOW()) - INTERVAL '90 days'
ON CONFLICT (bucket_date) DO NOTHING;

DROP TABLE ops_inbound_body_read_health_repair;
