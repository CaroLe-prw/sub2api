CREATE TABLE IF NOT EXISTS payment_verification_records (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL UNIQUE REFERENCES payment_orders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    email VARCHAR(255) NOT NULL DEFAULT '',
    provider_name VARCHAR(255) NOT NULL DEFAULT '',
    out_trade_no VARCHAR(64) NOT NULL DEFAULT '',
    amount NUMERIC NOT NULL,
    pay_amount NUMERIC NOT NULL,
    currency VARCHAR(10) NOT NULL,
    order_status VARCHAR(30) NOT NULL,
    result VARCHAR(30) NOT NULL CHECK (result IN ('verified', 'unpaid', 'amount_mismatch', 'trade_mismatch', 'query_error')),
    reason VARCHAR(500) NOT NULL DEFAULT '',
    upstream_amount NUMERIC,
    state VARCHAR(20) NOT NULL CHECK (state IN ('pending', 'ignored', 'banned', 'resolved')),
    checked_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    handled_at TIMESTAMPTZ,
    handled_by VARCHAR(100) NOT NULL DEFAULT '',
    action_token VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    template_id VARCHAR(128) NOT NULL DEFAULT '',
    config_generation VARCHAR(64) NOT NULL DEFAULT '',
    bot_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    telegram_chat_id BIGINT NOT NULL DEFAULT 0,
    telegram_message_id BIGINT NOT NULL DEFAULT 0,
    telegram_topic_id BIGINT NOT NULL DEFAULT 0,
    notified_at TIMESTAMPTZ,
    next_check_at TIMESTAMPTZ NOT NULL,
    notification_available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notification_claimed_at TIMESTAMPTZ,
    notification_claimed_by VARCHAR(64)
);

CREATE INDEX IF NOT EXISTS idx_payment_verification_next_check
    ON payment_verification_records (next_check_at, order_id)
    WHERE state NOT IN ('ignored', 'banned');
CREATE INDEX IF NOT EXISTS idx_payment_verification_notification
    ON payment_verification_records (notification_available_at, id)
    WHERE state = 'pending' AND notified_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_payment_orders_completed_verification
    ON payment_orders (completed_at, id)
    WHERE status = 'COMPLETED' AND order_type = 'balance' AND refund_amount = 0;
