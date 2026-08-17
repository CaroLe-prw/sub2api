-- Add multiple Shanghai-time price override periods to each channel model price.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS time_pricing JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN channel_model_pricing.time_pricing IS
    '模型分时价格覆盖；按 Asia/Shanghai 判断，区间为 [start,end)，未填写字段继承默认/阶梯价格';

ALTER TABLE channel_model_pricing
    DROP CONSTRAINT IF EXISTS channel_model_pricing_time_pricing_array;

ALTER TABLE channel_model_pricing
    ADD CONSTRAINT channel_model_pricing_time_pricing_array
    CHECK (jsonb_typeof(time_pricing) = 'array');
