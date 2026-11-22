-- Channel price labels retain their original denomination; no FX conversion.
-- Existing channels continue to use USD without changing any stored prices.
ALTER TABLE channels ADD COLUMN IF NOT EXISTS price_currency VARCHAR(3) NOT NULL DEFAULT 'USD';
ALTER TABLE channels ADD CONSTRAINT channels_price_currency_check
    CHECK (price_currency IN ('USD', 'CNY', 'EUR', 'GBP', 'JPY', 'HKD', 'SGD', 'AUD', 'CAD', 'CHF', 'KRW'));
