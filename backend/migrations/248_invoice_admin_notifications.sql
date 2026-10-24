-- Only new applications enqueue notifications. Do not email historical invoices.
CREATE TABLE invoice_admin_notifications (
    application_id VARCHAR(64) PRIMARY KEY REFERENCES invoice_applications(id),
    state VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sent','skipped')),
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    recipient_id BIGINT,
    sent_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX invoice_admin_notifications_pending ON invoice_admin_notifications(next_attempt_at,created_at) WHERE state='pending';
