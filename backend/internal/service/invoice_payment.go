package service

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

func (s *InvoiceService) PaymentAmount(ctx context.Context, id string, user int64) (int64, error) {
	r, err := s.get(ctx, id, user)
	if err != nil {
		return 0, err
	}
	if r.Status != InvoicePendingPayment || r.FeePaid || r.ServiceFeeCent <= 0 {
		return 0, invoiceConflict("该申请无需支付或已支付手续费")
	}
	return r.ServiceFeeCent, nil
}
func (s *PaymentService) lockInvoiceForFeeOrder(ctx context.Context, tx *dbent.Tx, req CreateOrderRequest, amount float64) error {
	rows, err := tx.QueryContext(ctx, invoiceSelect+` WHERE id=$1 AND user_id=$2 FOR UPDATE`, req.InvoiceID, req.UserID)
	if err != nil {
		return err
	}
	if !rows.Next() {
		_ = rows.Close()
		return invoiceBad("发票申请不存在")
	}
	r, err := scanInvoice(rows)
	_ = rows.Close()
	if err != nil {
		return err
	}
	if r.Status != InvoicePendingPayment || r.FeePaid || r.ServiceFeeCent <= 0 || !decimal.NewFromFloat(amount).Mul(decimal.NewFromInt(100)).Equal(decimal.NewFromInt(r.ServiceFeeCent)) {
		return invoiceConflict("开票手续费或申请状态已变化")
	}
	rows, err = tx.QueryContext(ctx, `SELECT p.id FROM invoice_fee_payments f JOIN payment_orders p ON p.id=f.payment_order_id WHERE f.application_id=$1 AND (p.status IN ('PENDING','PAID','RECHARGING') OR p.status='FAILED' AND p.paid_at IS NOT NULL) LIMIT 1`, req.InvoiceID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		return infraerrors.Conflict("INVOICE_PAYMENT_EXISTS", "已有支付订单，请继续支付或取消原支付订单").WithMetadata(map[string]string{"order_id": strconv.FormatInt(id, 10)})
	}
	return rows.Err()
}

// The callback and invoice state change commit together. No balance credit,
// recharge bonus, subscription or affiliate logic runs for invoice fee orders.
func (s *PaymentService) ExecuteInvoiceFeeFulfillment(ctx context.Context, oid int64) error {
	if s.invoiceService == nil {
		return invoiceBad("发票服务不可用")
	}
	return s.invoiceService.completeFeePayment(ctx, oid)
}
func (s *InvoiceService) completeFeePayment(ctx context.Context, oid int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var uid int64
	var state, orderType, amount, currency string
	err = tx.QueryRowContext(ctx, `SELECT user_id,status,order_type,pay_amount::text,COALESCE(provider_snapshot->>'currency','CNY') FROM payment_orders WHERE id=$1 FOR UPDATE`, oid).Scan(&uid, &state, &orderType, &amount, &currency)
	if err != nil {
		return err
	}
	if orderType != payment.OrderTypeInvoiceFee {
		return invoiceBad("不是开票手续费订单")
	}
	if state == payment.OrderStatusCompleted {
		return nil
	}
	if state != payment.OrderStatusPaid && state != payment.OrderStatusRecharging {
		return invoiceConflict("订单尚未确认支付")
	}
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT application_id FROM invoice_fee_payments WHERE payment_order_id=$1`, oid).Scan(&id); err != nil {
		return err
	}
	r, err := scanInvoice(tx.QueryRowContext(ctx, invoiceSelect+` WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	paid, parseErr := decimal.NewFromString(amount)
	if parseErr != nil || currency != "CNY" || r.UserID != uid || !paid.Mul(decimal.NewFromInt(100)).Equal(decimal.NewFromInt(r.ServiceFeeCent)) {
		return invoiceConflict("手续费支付金额、币种或用户不匹配")
	}
	first := r.Status == InvoicePendingPayment && !r.FeePaid
	r.FeePaid = true
	if r.FeePaymentOrderID == 0 {
		r.FeePaymentOrderID = oid
	}
	if first {
		r.Status = InvoiceReview
		if _, err = tx.ExecContext(ctx, `INSERT INTO invoice_admin_notifications(application_id) VALUES($1) ON CONFLICT(application_id) DO NOTHING`, id); err != nil {
			return err
		}
	} else if r.FeePaymentOrderID != oid || r.Status == InvoiceRejected || r.Status == InvoiceWithdrawn || r.Status == InvoiceCancelled || r.Status == InvoiceFailed {
		if _, err = tx.ExecContext(ctx, `UPDATE invoice_fee_payments SET refund_required=TRUE WHERE payment_order_id=$1`, oid); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE payment_orders SET status='COMPLETED',completed_at=NOW(),updated_at=NOW() WHERE id=$1`, oid); err != nil {
		return err
	}
	if err = saveInvoice(ctx, tx, r, "system", "FEE_PAID"); err != nil {
		return err
	}
	return tx.Commit()
}

// Resume QR/redirect checkouts after closing the dialog. Provider client
// secrets remain in the existing browser payment recovery storage.
func (s *InvoiceService) FeePayment(ctx context.Context, id string, user int64) (*CreateOrderResponse, error) {
	if _, err := s.get(ctx, id, user); err != nil {
		return nil, err
	}
	var r CreateOrderResponse
	err := s.db.QueryRowContext(ctx, `SELECT p.id,p.amount,p.pay_amount,p.fee_rate,p.status,p.payment_type,p.out_trade_no,COALESCE(p.pay_url,''),COALESCE(p.qr_code,''),p.expires_at,COALESCE(p.provider_snapshot->>'currency','CNY') FROM invoice_fee_payments f JOIN payment_orders p ON p.id=f.payment_order_id WHERE f.application_id=$1 ORDER BY p.id DESC LIMIT 1`, id).Scan(&r.OrderID, &r.Amount, &r.PayAmount, &r.FeeRate, &r.Status, &r.PaymentType, &r.OutTradeNo, &r.PayURL, &r.QRCode, &r.ExpiresAt, &r.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
func (s *InvoiceService) RefundableFeeOrders(ctx context.Context, id string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id FROM invoice_fee_payments f JOIN payment_orders p ON p.id=f.payment_order_id WHERE f.application_id=$1 AND f.refund_required AND p.status IN ('COMPLETED','REFUND_FAILED','REFUND_PENDING') ORDER BY p.id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
