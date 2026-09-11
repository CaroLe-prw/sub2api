ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS response_diagnostics JSONB;
COMMENT ON COLUMN usage_logs.response_diagnostics IS 'Admin response diagnostics: bounded upstream and downstream captures and per-frame JSON validation';
