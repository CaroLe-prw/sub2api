-- Per-group account priority is a new scheduling setting. Before it had an
-- editing UI, account_groups.priority only reflected group selection order
-- (including sequences with gaps after removals). Initialize every existing
-- binding from the account's global priority once; later explicit per-group
-- edits are preserved because this migration runs only once.
UPDATE account_groups AS ag
SET priority = a.priority
FROM accounts AS a
WHERE ag.account_id = a.id;
