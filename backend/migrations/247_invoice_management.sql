CREATE TABLE invoice_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    api_key_encrypted TEXT NOT NULL DEFAULT '',
    allowed_option_ids JSONB NOT NULL DEFAULT '[]',
    options JSONB NOT NULL DEFAULT '[]',
    options_updated_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO invoice_settings(id) VALUES (1);

CREATE TABLE invoice_applications (
    id VARCHAR(64) PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    status VARCHAR(32) NOT NULL,
    data JSONB NOT NULL,
    payload JSONB,
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lease_until TIMESTAMPTZ,
    lease_token TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX invoice_applications_user ON invoice_applications(user_id, created_at DESC);
CREATE INDEX invoice_applications_work ON invoice_applications(next_check_at) WHERE status IN ('APPROVED','SUBMITTING','PROCESSING','UNKNOWN','CANCELLING','COMPLETED');

CREATE TABLE invoice_order_links (
    application_id VARCHAR(64) NOT NULL REFERENCES invoice_applications(id),
    order_id BIGINT NOT NULL REFERENCES payment_orders(id),
    amount_cent BIGINT NOT NULL CHECK (amount_cent > 0),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY(application_id, order_id)
);
CREATE UNIQUE INDEX invoice_order_one_active ON invoice_order_links(order_id) WHERE active;

CREATE TABLE invoice_events (
    id BIGSERIAL PRIMARY KEY,
    application_id VARCHAR(64) NOT NULL REFERENCES invoice_applications(id),
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX invoice_events_application ON invoice_events(application_id, id);

CREATE TABLE invoice_titles (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    company_name TEXT NOT NULL,
    tax_number VARCHAR(20) NOT NULL,
    delivery_email TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, tax_number)
);
CREATE TABLE invoice_files (
    application_id VARCHAR(64) PRIMARY KEY REFERENCES invoice_applications(id),
    content BYTEA NOT NULL CHECK (octet_length(content) <= 20971520),
    mime_type TEXT NOT NULL,
    file_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE invoice_api_limits (bucket TEXT PRIMARY KEY, next_at TIMESTAMPTZ NOT NULL);

-- Both invoice acquisition and refund updates lock the payment order row.
-- Protect every refund entry point, including force refunds and concurrent requests.
CREATE FUNCTION guard_invoice_order_refund() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.status LIKE 'REFUND%' OR NEW.status = 'PARTIALLY_REFUNDED' OR NEW.refund_amount > 0
        OR NEW.refund_requested_at IS NOT NULL OR NEW.refund_at IS NOT NULL)
       AND EXISTS(SELECT 1 FROM invoice_order_links WHERE order_id = NEW.id AND active) THEN
        RAISE EXCEPTION '订单已关联发票申请，请先撤回或确认取消；已开票订单需要人工处理'
            USING ERRCODE = '23514', CONSTRAINT = 'invoice_refund_guard';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER invoice_refund_guard BEFORE UPDATE OF status, refund_amount, refund_requested_at, refund_at
ON payment_orders FOR EACH ROW EXECUTE FUNCTION guard_invoice_order_refund();
