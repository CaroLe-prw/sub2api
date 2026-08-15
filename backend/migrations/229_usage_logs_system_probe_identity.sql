-- Allow system-owned channel probes to be reconciled in usage_logs without
-- attributing them to (or charging) an arbitrary user/API key.
ALTER TABLE usage_logs
    ALTER COLUMN user_id DROP NOT NULL,
    ALTER COLUMN api_key_id DROP NOT NULL;

-- Preserve usage records for reconciliation when their former user/API key is
-- deleted. Cover both migration-created and Ent-created constraint names.
ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_user_id_fkey,
    DROP CONSTRAINT IF EXISTS usage_logs_users_usage_logs,
    DROP CONSTRAINT IF EXISTS usage_logs_api_key_id_fkey,
    DROP CONSTRAINT IF EXISTS usage_logs_api_keys_usage_logs;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL,
    ADD CONSTRAINT usage_logs_api_key_id_fkey
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL;

ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_request_type_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_request_type_check
    CHECK (request_type >= 0 AND request_type <= 6);

COMMENT ON COLUMN usage_logs.user_id IS
    'Nullable for system-owned usage such as channel monitor probes';
COMMENT ON COLUMN usage_logs.api_key_id IS
    'Nullable for system-owned usage such as channel monitor probes';
