ALTER TABLE scheduled_test_results
    ADD COLUMN IF NOT EXISTS ttft_ms BIGINT;
