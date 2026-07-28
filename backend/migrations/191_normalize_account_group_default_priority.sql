-- account_groups.priority was historically populated as 1..N according to the
-- order in which an account's groups were selected. Per-group scheduling now
-- treats this column as the account priority within that group, so migrate only
-- those legacy sequential sets to the account's existing global priority.
WITH ranked AS (
    SELECT
        account_id,
        priority,
        ROW_NUMBER() OVER (
            PARTITION BY account_id
            ORDER BY priority, group_id
        ) AS expected_priority
    FROM account_groups
),
legacy_accounts AS (
    SELECT account_id
    FROM ranked
    GROUP BY account_id
    HAVING BOOL_AND(priority = expected_priority)
)
UPDATE account_groups AS ag
SET priority = a.priority
FROM accounts AS a
JOIN legacy_accounts AS legacy ON legacy.account_id = a.id
WHERE ag.account_id = a.id;
