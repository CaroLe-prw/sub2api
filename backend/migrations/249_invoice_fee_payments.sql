ALTER TABLE invoice_settings ADD COLUMN min_amount_cent BIGINT NOT NULL DEFAULT 10000;
ALTER TABLE invoice_settings ADD COLUMN fee_tiers JSONB NOT NULL DEFAULT '[{"min_amount_cent":10000,"rate_bps":0},{"min_amount_cent":20000,"rate_bps":600},{"min_amount_cent":30000,"rate_bps":700}]';
ALTER TABLE invoice_settings ADD COLUMN pricing_version BIGINT NOT NULL DEFAULT 1;

CREATE TABLE invoice_fee_payments (
    payment_order_id BIGINT PRIMARY KEY REFERENCES payment_orders(id),
    application_id VARCHAR(64) NOT NULL REFERENCES invoice_applications(id),
    refund_required BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX invoice_fee_payments_application ON invoice_fee_payments(application_id,payment_order_id DESC);

-- Fee payments never credit balances. Refunds are allowed only after the
-- invoice is closed, or for an extra/late payment explicitly flagged for return.
CREATE FUNCTION guard_invoice_fee_refund() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allowed BOOLEAN;
BEGIN
 IF NEW.order_type='invoice_fee' AND NEW.status IN ('REFUND_REQUESTED','REFUNDING','REFUND_PENDING','PARTIALLY_REFUNDED','REFUNDED') THEN
   SELECT p.refund_required INTO allowed FROM invoice_fee_payments p
   JOIN invoice_applications a ON a.id=p.application_id
   WHERE p.payment_order_id=NEW.id FOR UPDATE OF a;
   IF NOT COALESCE(allowed,FALSE) THEN
     RAISE EXCEPTION '发票申请仍在处理中，不允许退还开票手续费'
       USING ERRCODE='23514', CONSTRAINT='invoice_fee_refund_guard';
   END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER invoice_fee_refund_guard BEFORE UPDATE OF status ON payment_orders
FOR EACH ROW EXECUTE FUNCTION guard_invoice_fee_refund();
