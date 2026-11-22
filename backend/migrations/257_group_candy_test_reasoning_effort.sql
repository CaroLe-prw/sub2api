-- Preserve the previous medium effort for existing candy-test groups.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS candy_test_reasoning_effort VARCHAR(20) NOT NULL DEFAULT 'medium'
    CHECK (candy_test_reasoning_effort IN ('', 'low', 'medium', 'high', 'xhigh', 'max'));

COMMENT ON COLUMN groups.candy_test_reasoning_effort IS
    'OpenAI candy-test reasoning effort for OAuth and API Key accounts; empty uses medium';
