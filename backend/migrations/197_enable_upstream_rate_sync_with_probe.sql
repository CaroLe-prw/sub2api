UPDATE accounts
SET extra = jsonb_set(
        COALESCE(extra, '{}'::jsonb),
        '{upstream_billing_rate_sync_enabled}',
        'true'::jsonb,
        true
    ),
    updated_at = NOW()
WHERE type = 'apikey'
  AND extra @> '{"upstream_billing_probe_enabled": true}'::jsonb
  AND NOT extra @> '{"newapi_sync_enabled": true}'::jsonb
  AND extra -> 'upstream_billing_rate_sync_enabled' IS DISTINCT FROM 'true'::jsonb;
