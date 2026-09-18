-- Error-only diagnostic snapshots. This does not restore request replay.
ALTER TABLE ops_error_logs
  ADD COLUMN IF NOT EXISTS request_diagnostics JSONB;
COMMENT ON COLUMN ops_error_logs.request_diagnostics IS
  'Bounded redacted client request and upstream attempt payloads; admin diagnostics only, no credentials or replay.';
