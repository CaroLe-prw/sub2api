-- Keep system-owned channel probe traffic visible without mixing it into
-- user-request dashboard totals. Existing aggregate rows default to zero;
-- the periodic aggregator will repopulate the affected window from usage_logs.
ALTER TABLE usage_dashboard_hourly
    ADD COLUMN IF NOT EXISTS probe_requests BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_input_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_output_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_total_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_account_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_duration_ms BIGINT NOT NULL DEFAULT 0;

ALTER TABLE usage_dashboard_daily
    ADD COLUMN IF NOT EXISTS probe_requests BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_input_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_output_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_total_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_account_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS probe_duration_ms BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN usage_dashboard_hourly.probe_requests IS 'System-owned channel probe requests in this bucket';
COMMENT ON COLUMN usage_dashboard_hourly.probe_account_cost IS 'Estimated upstream account cost of system probes in this bucket';
COMMENT ON COLUMN usage_dashboard_daily.probe_requests IS 'System-owned channel probe requests on this date';
COMMENT ON COLUMN usage_dashboard_daily.probe_account_cost IS 'Estimated upstream account cost of system probes on this date';
