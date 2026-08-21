-- Normalize the legacy top-level period array to the current object shape.
-- The update is idempotent: once wrapped, jsonb_typeof becomes "object" and
-- subsequent migration replays leave the row unchanged.
UPDATE channel_model_pricing
SET time_pricing = jsonb_build_object(
    'timezone', 'Asia/Shanghai',
    'periods', time_pricing
)
WHERE time_pricing IS NOT NULL
  AND jsonb_typeof(time_pricing) = 'array';
