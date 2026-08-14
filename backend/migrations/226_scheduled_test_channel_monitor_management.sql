-- 226: Mark scheduled probes created by channel-monitor pool reconciliation.
-- Manual scheduled tests remain independent and are never disabled/deleted by
-- the automatic account-pool monitor.
ALTER TABLE scheduled_test_plans
    ADD COLUMN IF NOT EXISTS managed_by VARCHAR(40) NOT NULL DEFAULT 'manual';

CREATE UNIQUE INDEX IF NOT EXISTS idx_stp_channel_monitor_account_model
    ON scheduled_test_plans(account_id, model_id)
    WHERE managed_by = 'channel_monitor';

CREATE INDEX IF NOT EXISTS idx_stp_channel_monitor_enabled
    ON scheduled_test_plans(managed_by, enabled, account_id);
