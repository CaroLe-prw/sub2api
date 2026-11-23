-- Preserve legacy single-model values while allowing an ordered candidate list.
ALTER TABLE groups ALTER COLUMN candy_test_model TYPE VARCHAR(2048);

COMMENT ON COLUMN groups.candy_test_model IS
    'Ordered OpenAI candy-test candidate model IDs separated by newlines; select only the first allowed by the account; empty uses gpt-6.2-sol';
