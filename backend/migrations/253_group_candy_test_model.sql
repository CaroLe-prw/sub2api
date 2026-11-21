-- Empty keeps existing groups on the diagnostic default, gpt-6.2-sol.
ALTER TABLE groups ADD COLUMN IF NOT EXISTS candy_test_model VARCHAR(100) NOT NULL DEFAULT '';
COMMENT ON COLUMN groups.candy_test_model IS 'OpenAI OAuth candy-test model; empty uses gpt-6.2-sol';
