-- Upgrade the previous one-wrong-answer gate without touching manual scheduling.
-- Unknown answers displaced by the bounded history prove at most one failure.
-- Only gate transitions emit outbox events; unchanged pools/stickiness stay warm.
WITH legacy AS (
    SELECT id, extra,
           CASE WHEN jsonb_typeof(extra->'candy_test'->'recent') = 'array'
                THEN extra->'candy_test'->'recent' ELSE '[]'::jsonb END AS recent
    FROM accounts
    WHERE deleted_at IS NULL AND platform = 'openai' AND type IN ('oauth', 'apikey')
      AND jsonb_typeof(extra->'candy_test') = 'object'
      AND NOT (extra->'candy_test' ? 'consecutive_failures')
), facts AS (
    SELECT legacy.*,
           COALESCE(
               CASE WHEN extra->'candy_test'->>'last_verdict' IN ('passed', 'failed')
                    THEN extra->'candy_test'->>'last_verdict' END,
               (SELECT sample->>'status'
                FROM jsonb_array_elements(recent) WITH ORDINALITY AS history(sample, pos)
                WHERE sample->>'status' IN ('passed', 'failed') ORDER BY pos DESC LIMIT 1),
               '') AS verdict,
           (SELECT COUNT(*)::integer
            FROM jsonb_array_elements(recent) WITH ORDINALITY AS history(sample, pos)
            WHERE sample->>'status' = 'failed'
              AND pos > COALESCE(
                  (SELECT MAX(pos) FROM jsonb_array_elements(recent) WITH ORDINALITY AS history(sample, pos)
                   WHERE sample->>'status' = 'passed'), 0)) AS trailing_failures
    FROM legacy
), resolved AS (
    SELECT id, extra,
           CASE WHEN verdict = 'failed' THEN GREATEST(trailing_failures, 1) ELSE 0 END AS streak,
           CASE WHEN jsonb_typeof(extra->'candy_test_blocked') = 'boolean'
                THEN (extra->>'candy_test_blocked')::boolean ELSE verdict = 'failed' END AS was_blocked
    FROM facts
), updated AS (
    UPDATE accounts AS a
    SET extra = jsonb_set(
            jsonb_set(r.extra, '{candy_test,consecutive_failures}', to_jsonb(r.streak), true),
            '{candy_test_blocked}', to_jsonb(r.streak >= 2), true),
        updated_at = NOW()
    FROM resolved r WHERE a.id = r.id
    RETURNING a.id, r.was_blocked IS DISTINCT FROM (r.streak >= 2) AS gate_changed
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM updated WHERE gate_changed;
