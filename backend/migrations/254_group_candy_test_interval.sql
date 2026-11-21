ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS candy_test_interval_seconds INTEGER NOT NULL DEFAULT 0
    CHECK (candy_test_interval_seconds BETWEEN 0 AND 86400);

COMMENT ON COLUMN groups.candy_test_interval_seconds IS
    'OpenAI OAuth automatic candy-test interval in seconds; 0 disables automatic checks';
