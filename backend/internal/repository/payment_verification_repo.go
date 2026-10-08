package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type paymentVerificationRepository struct{ db *sql.DB }

func NewPaymentVerificationRepository(db *sql.DB) service.PaymentVerificationRepository {
	return &paymentVerificationRepository{db: db}
}

const paymentVerificationColumns = `id, order_id, user_id, email, provider_name, out_trade_no,
amount, pay_amount, currency, order_status, result, reason, upstream_amount, state,
checked_at, created_at, handled_at, handled_by, action_token, expires_at, template_id,
config_generation, bot_fingerprint, telegram_chat_id, telegram_message_id,
telegram_topic_id, notified_at`

type paymentVerificationScanner interface{ Scan(...any) error }

// A warning about an unavailable query has no ban button. When a later query
// establishes a mismatch, invalidate that warning's token and send actionable
// evidence once. Repeated observations of the same mismatch remain deduplicated.
const paymentVerificationReopen = `((payment_verification_records.state='resolved' AND EXCLUDED.state='pending')
 OR (payment_verification_records.state='pending' AND payment_verification_records.result='query_error'
     AND EXCLUDED.state='pending' AND EXCLUDED.result IN ('unpaid','amount_mismatch','trade_mismatch')))`

func scanPaymentVerification(row paymentVerificationScanner) (*service.PaymentVerificationRecord, error) {
	r := &service.PaymentVerificationRecord{}
	err := row.Scan(&r.ID, &r.OrderID, &r.UserID, &r.Email, &r.ProviderName, &r.OutTradeNo,
		&r.Amount, &r.PayAmount, &r.Currency, &r.OrderStatus, &r.Result, &r.Reason, &r.UpstreamAmount, &r.State,
		&r.CheckedAt, &r.CreatedAt, &r.HandledAt, &r.HandledBy, &r.ActionToken, &r.ExpiresAt, &r.TemplateID,
		&r.ConfigGeneration, &r.BotFingerprint, &r.TelegramChatID, &r.TelegramMessageID,
		&r.TelegramTopicID, &r.NotifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrPaymentVerificationNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (r *paymentVerificationRepository) ListCandidates(ctx context.Context, since, now time.Time, limit int) ([]int64, error) {
	if limit < 1 || limit > 10 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT o.id FROM payment_orders o
LEFT JOIN payment_verification_records v ON v.order_id = o.id
WHERE o.status = 'COMPLETED' AND o.order_type = 'balance'
  AND o.refund_amount = 0 AND o.refund_at IS NULL AND o.refund_requested_at IS NULL
  AND o.completed_at >= $1 AND o.completed_at <= $2
  AND COALESCE(NULLIF(o.provider_snapshot->>'provider_key', ''), o.provider_key, '') = 'easypay'
  AND (NULLIF(o.provider_instance_id, '') IS NOT NULL OR NULLIF(o.provider_snapshot->>'provider_instance_id', '') IS NOT NULL)
  AND (v.id IS NULL OR (v.state NOT IN ('ignored', 'banned') AND v.next_check_at <= $2))
ORDER BY COALESCE(v.checked_at, o.completed_at), o.id LIMIT $3`, since, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *paymentVerificationRepository) SaveCheck(ctx context.Context, record *service.PaymentVerificationRecord, nextCheckAt time.Time) (*service.PaymentVerificationRecord, error) {
	// Human resolutions are terminal for this order; background checks must not
	// overwrite them or recreate an ignored alert. The conflict predicate also
	// prevents a slow older query from replacing fresher evidence.
	row := r.db.QueryRowContext(ctx, `
INSERT INTO payment_verification_records
(order_id,user_id,email,provider_name,out_trade_no,amount,pay_amount,currency,order_status,
 result,reason,upstream_amount,state,checked_at,created_at,action_token,expires_at,next_check_at,notification_available_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14,$15,$16,$17,$14)
ON CONFLICT (order_id) DO UPDATE SET
 user_id=EXCLUDED.user_id, email=EXCLUDED.email, provider_name=EXCLUDED.provider_name,
 out_trade_no=EXCLUDED.out_trade_no, amount=EXCLUDED.amount, pay_amount=EXCLUDED.pay_amount,
 currency=EXCLUDED.currency, order_status=EXCLUDED.order_status,
 result=EXCLUDED.result, reason=EXCLUDED.reason, upstream_amount=EXCLUDED.upstream_amount,
 state=EXCLUDED.state, checked_at=EXCLUDED.checked_at, next_check_at=EXCLUDED.next_check_at,
 handled_at=CASE WHEN payment_verification_records.state='pending' AND EXCLUDED.state='resolved' THEN EXCLUDED.checked_at
                 WHEN EXCLUDED.state='pending' THEN NULL ELSE payment_verification_records.handled_at END,
 handled_by=CASE WHEN payment_verification_records.state='pending' AND EXCLUDED.state='resolved' THEN 'system'
                 WHEN EXCLUDED.state='pending' THEN '' ELSE payment_verification_records.handled_by END,
 action_token=CASE WHEN `+paymentVerificationReopen+`
                    OR (payment_verification_records.notified_at IS NULL AND payment_verification_records.expires_at <= EXCLUDED.checked_at
                        AND payment_verification_records.notification_claimed_at IS NULL)
                   THEN EXCLUDED.action_token ELSE payment_verification_records.action_token END,
 expires_at=CASE WHEN `+paymentVerificationReopen+`
                  OR (payment_verification_records.notified_at IS NULL AND payment_verification_records.expires_at <= EXCLUDED.checked_at
                      AND payment_verification_records.notification_claimed_at IS NULL)
                 THEN EXCLUDED.expires_at ELSE payment_verification_records.expires_at END,
 notified_at=CASE WHEN `+paymentVerificationReopen+` THEN NULL ELSE payment_verification_records.notified_at END,
 telegram_message_id=CASE WHEN `+paymentVerificationReopen+` THEN 0 ELSE payment_verification_records.telegram_message_id END,
 notification_available_at=CASE WHEN `+paymentVerificationReopen+` THEN EXCLUDED.checked_at ELSE payment_verification_records.notification_available_at END,
 notification_claimed_at=CASE WHEN `+paymentVerificationReopen+` THEN NULL ELSE payment_verification_records.notification_claimed_at END,
 notification_claimed_by=CASE WHEN `+paymentVerificationReopen+` THEN NULL ELSE payment_verification_records.notification_claimed_by END
WHERE payment_verification_records.state NOT IN ('ignored','banned')
  AND payment_verification_records.checked_at <= EXCLUDED.checked_at
RETURNING `+paymentVerificationColumns,
		record.OrderID, record.UserID, record.Email, record.ProviderName, record.OutTradeNo,
		record.Amount, record.PayAmount, record.Currency, record.OrderStatus, record.Result, record.Reason,
		record.UpstreamAmount, record.State, record.CheckedAt, record.ActionToken, record.ExpiresAt, nextCheckAt)
	saved, err := scanPaymentVerification(row)
	if errors.Is(err, service.ErrPaymentVerificationNotFound) {
		return scanPaymentVerification(r.db.QueryRowContext(ctx, `SELECT `+paymentVerificationColumns+` FROM payment_verification_records WHERE order_id=$1`, record.OrderID))
	}
	return saved, err
}

func (r *paymentVerificationRepository) ListAlerts(ctx context.Context, limit int) ([]service.PaymentVerificationRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+paymentVerificationColumns+` FROM payment_verification_records
WHERE result <> 'verified' OR notified_at IS NOT NULL OR handled_by <> ''
ORDER BY (state='pending') DESC, checked_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return readPaymentVerificationRows(rows)
}

func readPaymentVerificationRows(rows *sql.Rows) ([]service.PaymentVerificationRecord, error) {
	records := []service.PaymentVerificationRecord{}
	for rows.Next() {
		record, err := scanPaymentVerification(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, rows.Err()
}

func (r *paymentVerificationRepository) GetByID(ctx context.Context, id int64) (*service.PaymentVerificationRecord, error) {
	return scanPaymentVerification(r.db.QueryRowContext(ctx, `SELECT `+paymentVerificationColumns+` FROM payment_verification_records WHERE id=$1`, id))
}

func (r *paymentVerificationRepository) GetByToken(ctx context.Context, token string) (*service.PaymentVerificationRecord, error) {
	return scanPaymentVerification(r.db.QueryRowContext(ctx, `SELECT `+paymentVerificationColumns+` FROM payment_verification_records WHERE action_token=$1`, token))
}

func (r *paymentVerificationRepository) Resolve(ctx context.Context, id int64, checkedAt time.Time, action, actor string, now time.Time) (*service.PaymentVerificationRecord, error) {
	if action != "ban" && action != "ignore" {
		return nil, service.ErrPaymentVerificationChanged
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	record, err := scanPaymentVerification(tx.QueryRowContext(ctx, `SELECT `+paymentVerificationColumns+` FROM payment_verification_records WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}
	if record.State != service.PaymentVerificationPending || !record.CheckedAt.Equal(checkedAt) {
		return nil, service.ErrPaymentVerificationChanged
	}
	state, audit := service.PaymentVerificationIgnored, "PAYMENT_VERIFICATION_IGNORED"
	if action == "ban" {
		if record.Result != service.PaymentVerificationUnpaid && record.Result != service.PaymentVerificationAmountMismatch && record.Result != service.PaymentVerificationTradeMismatch {
			return nil, service.ErrPaymentVerificationUnsafeBan
		}
		// Lock and recheck the live order after the network verification: a refund
		// or order state change during that call must not trigger an account ban.
		var orderUserID int64
		var status, orderType string
		var refundAmount, payAmount float64
		var refundAt, requestedAt sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT user_id,status,order_type,refund_amount,refund_at,refund_requested_at,pay_amount
FROM payment_orders WHERE id=$1 FOR UPDATE`, record.OrderID).
			Scan(&orderUserID, &status, &orderType, &refundAmount, &refundAt, &requestedAt, &payAmount)
		if err != nil {
			return nil, err
		}
		if orderUserID != record.UserID || status != service.OrderStatusCompleted || orderType != "balance" ||
			refundAmount != 0 || refundAt.Valid || requestedAt.Valid || payAmount != record.PayAmount {
			return nil, service.ErrPaymentVerificationChanged
		}
		var role string
		err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, record.UserID).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		if role == service.RoleAdmin {
			return nil, service.ErrPaymentVerificationUnsafeBan
		}
		updated, err := tx.ExecContext(ctx, `UPDATE users SET status='disabled',updated_at=$2 WHERE id=$1 AND role <> 'admin' AND deleted_at IS NULL`, record.UserID, now)
		if err != nil {
			return nil, err
		}
		changed, err := updated.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed != 1 {
			return nil, service.ErrPaymentVerificationChanged
		}
		state, audit = service.PaymentVerificationBanned, "PAYMENT_VERIFICATION_BANNED"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_verification_records SET state=$2,handled_at=$3,handled_by=$4,
notification_claimed_at=NULL,notification_claimed_by=NULL WHERE id=$1`, record.ID, state, now, actor); err != nil {
		return nil, err
	}
	detail, err := json.Marshal(map[string]any{"alert_id": record.ID, "user_id": record.UserID, "result": record.Result})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_audit_logs (order_id,action,detail,operator,created_at) VALUES ($1,$2,$3,$4,$5)`,
		strconv.FormatInt(record.OrderID, 10), audit, string(detail), actor, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	record.State, record.HandledAt, record.HandledBy = state, &now, actor
	return record, nil
}

func (r *paymentVerificationRepository) ClaimNotifications(ctx context.Context, owner string, now time.Time, limit int, lease time.Duration) ([]service.PaymentVerificationRecord, error) {
	if limit < 1 || limit > 10 {
		limit = 10
	}
	if lease <= 0 {
		lease = time.Minute
	}
	rows, err := r.db.QueryContext(ctx, `
WITH candidates AS (
 SELECT id FROM payment_verification_records
 WHERE state='pending' AND result<>'verified' AND notified_at IS NULL
 AND notification_available_at <= $2 AND expires_at > $2
 AND (notification_claimed_at IS NULL OR notification_claimed_at < $3)
 ORDER BY notification_available_at,id LIMIT $4 FOR UPDATE SKIP LOCKED
), claimed AS (
 UPDATE payment_verification_records v SET notification_claimed_at=$2,notification_claimed_by=$1
 FROM candidates c WHERE v.id=c.id RETURNING v.*
)
SELECT `+paymentVerificationColumns+` FROM claimed`, owner, now, now.Add(-lease), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return readPaymentVerificationRows(rows)
}

func (r *paymentVerificationRepository) PrepareNotification(ctx context.Context, id int64, owner string, binding service.PaymentVerificationNotificationBinding) error {
	result, err := r.db.ExecContext(ctx, `UPDATE payment_verification_records
SET template_id=$3,config_generation=$4,bot_fingerprint=$5,telegram_chat_id=$6,telegram_topic_id=$7,telegram_message_id=0
WHERE id=$1 AND notification_claimed_by=$2 AND state='pending' AND notified_at IS NULL`,
		id, owner, binding.TemplateID, binding.ConfigGeneration, binding.BotFingerprint, binding.TelegramChatID, binding.TelegramTopicID)
	return paymentVerificationClaimUpdated(result, err)
}

func (r *paymentVerificationRepository) MarkNotified(ctx context.Context, id int64, owner string, messageID int64, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE payment_verification_records SET telegram_message_id=$3,notified_at=$4,
notification_claimed_at=NULL,notification_claimed_by=NULL
WHERE id=$1 AND notification_claimed_by=$2 AND state='pending' AND notified_at IS NULL`, id, owner, messageID, now)
	return paymentVerificationClaimUpdated(result, err)
}

func (r *paymentVerificationRepository) RetryNotification(ctx context.Context, id int64, owner string, retryAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE payment_verification_records SET notification_available_at=$3,
notification_claimed_at=NULL,notification_claimed_by=NULL WHERE id=$1 AND notification_claimed_by=$2`, id, owner, retryAt)
	return paymentVerificationClaimUpdated(result, err)
}

func paymentVerificationClaimUpdated(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("payment verification notification claim is no longer owned")
	}
	return nil
}
