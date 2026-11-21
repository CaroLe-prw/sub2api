-- Backfill the account-deletion cleanup for existing tombstones. Only candy
-- history/answers and scheduling verdicts are removed; unrelated extra survives.
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) - 'candy_test' - 'candy_test_blocked'
WHERE deleted_at IS NOT NULL
  AND (extra ? 'candy_test' OR extra ? 'candy_test_blocked');

-- Test failures include legacy candy checks that have no separate candy tag.
-- Keep business-request logs and audit history, and never touch active accounts.
DELETE FROM ops_system_logs l
WHERE l.account_id IS NOT NULL
  AND l.component = 'service.account_test'
  AND l.message = 'account_test.failed'
  AND NOT EXISTS (
    SELECT 1 FROM accounts a WHERE a.id = l.account_id AND a.deleted_at IS NULL
  );
