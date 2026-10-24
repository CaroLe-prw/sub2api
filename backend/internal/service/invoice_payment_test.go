package service

import (
	"context"
	"database/sql"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/migrations"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInvoiceTierBoundaries(t *testing.T) {
	cfg := &InvoiceConfig{MinAmountCent: 10000, FeeTiers: []InvoiceFeeTier{{10000, 0}, {20000, 600}, {30000, 700}}, PricingVersion: 1}
	for _, tc := range []struct{ amount, fee int64 }{{10000, 0}, {19999, 0}, {20000, 1200}, {29999, 1800}, {30000, 2100}, {50000, 3500}, {50001, 3501}, {80000, 5600}} {
		q, err := invoiceFeeQuote(cfg, tc.amount)
		require.NoError(t, err)
		require.Equal(t, tc.fee, q.FeeCent)
	}
	_, err := invoiceFeeQuote(cfg, 9999)
	require.Error(t, err)
	for _, tiers := range [][]InvoiceFeeTier{nil, {{10000, 0}, {10000, 600}}, {{10001, 0}}, {{10000, -1}}, {{10000, 10001}}} {
		require.Error(t, validateInvoicePricing(10000, tiers))
	}
}
func createChargedTestInvoice(t *testing.T, db *sql.DB, s *InvoiceService) *InvoiceRecord {
	t.Helper()
	var oid int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id,pay_amount)VALUES(1,306)RETURNING id`).Scan(&oid))
	q, err := s.Quote(context.Background(), 1, []int64{oid})
	require.NoError(t, err)
	r, err := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd"}, OrderIDs: []int64{oid}, ExpectedAmountCent: &q.AmountCent, ExpectedFeeCent: &q.FeeCent, PricingVersion: q.PricingVersion})
	require.NoError(t, err)
	return r
}
func insertTestFeePayment(t *testing.T, db *sql.DB, r *InvoiceRecord, amount int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id,order_type,status,amount,pay_amount,fee_rate,paid_at,provider_snapshot)VALUES(1,'invoice_fee','PAID',$1,$1,0,NOW(),'{"currency":"CNY"}')RETURNING id`, decimal.NewFromInt(amount).Div(decimal.NewFromInt(100)).String()).Scan(&id))
	_, err := db.Exec(`INSERT INTO invoice_fee_payments(payment_order_id,application_id)VALUES($1,$2)`, id, r.ID)
	require.NoError(t, err)
	return id
}
func TestInvoiceFeePaymentGatesReviewAndMail(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := createChargedTestInvoice(t, db, s)
	ctx := context.Background()
	require.Equal(t, InvoicePendingPayment, r.Status)
	require.EqualValues(t, 2100, r.ServiceFeeCent)
	_, err := s.Action(ctx, r.ID, 0, 2, "approve", "", false)
	require.Error(t, err)
	_, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "", false)
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_admin_notifications`).Scan(&count))
	require.Zero(t, count)
	id := insertTestFeePayment(t, db, r, 2100)
	p := &PaymentService{invoiceService: s}
	require.NoError(t, p.ExecuteInvoiceFeeFulfillment(ctx, id))
	require.NoError(t, p.ExecuteInvoiceFeeFulfillment(ctx, id))
	r, err = s.get(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, InvoiceReview, r.Status)
	require.True(t, r.FeePaid)
	require.Equal(t, id, r.FeePaymentOrderID)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_admin_notifications`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_events WHERE action='FEE_PAID'`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = s.Action(ctx, r.ID, 0, 2, "approve", "", false)
	require.NoError(t, err)
}
func TestInvoiceFeeWrongAmountNeverEntersReview(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := createChargedTestInvoice(t, db, s)
	id := insertTestFeePayment(t, db, r, 2099)
	require.Error(t, s.completeFeePayment(context.Background(), id))
	r, err := s.get(context.Background(), r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, InvoicePendingPayment, r.Status)
	require.False(t, r.FeePaid)
}
func TestInvoiceLateAndDuplicatePaymentsRequireRefund(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate", true: "withdrawn"}[closed], func(t *testing.T) {
			db, s := reviewInvoiceDB(t)
			r := createChargedTestInvoice(t, db, s)
			ctx := context.Background()
			if closed {
				_, err := s.Action(ctx, r.ID, 1, 1, "withdraw", "", false)
				require.NoError(t, err)
			}
			first := insertTestFeePayment(t, db, r, 2100)
			require.NoError(t, s.completeFeePayment(ctx, first))
			second := insertTestFeePayment(t, db, r, 2100)
			require.NoError(t, s.completeFeePayment(ctx, second))
			r, err := s.get(ctx, r.ID, 1)
			require.NoError(t, err)
			require.True(t, r.FeeRefundRequired)
			require.Equal(t, first, r.FeePaymentOrderID)
			if closed {
				require.Equal(t, InvoiceWithdrawn, r.Status)
			} else {
				require.Equal(t, InvoiceReview, r.Status)
				_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDING' WHERE id=$1`, first)
				require.Error(t, err)
			}
			_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDING' WHERE id=$1`, second)
			require.NoError(t, err)
			var mailCount int
			require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_admin_notifications`).Scan(&mailCount))
			if closed {
				require.Zero(t, mailCount)
			} else {
				require.Equal(t, 1, mailCount)
			}
		})
	}
}
func TestInvoicePaidRejectionAllowsOriginalRefund(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := createChargedTestInvoice(t, db, s)
	ctx := context.Background()
	id := insertTestFeePayment(t, db, r, 2100)
	require.NoError(t, s.completeFeePayment(ctx, id))
	_, err := s.Action(ctx, r.ID, 0, 2, "reject", "invalid buyer", false)
	require.NoError(t, err)
	ids, err := s.RefundableFeeOrders(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{id}, ids)
	_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDED',refund_amount=21 WHERE id=$1`, id)
	require.NoError(t, err)
	r, err = s.get(ctx, r.ID, 1)
	require.NoError(t, err)
	require.False(t, r.FeeRefundRequired)
	require.Equal(t, "REFUNDED", r.FeePaymentStatus)
}
func TestInvoicePricingSnapshotAndQuoteConflict(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := createChargedTestInvoice(t, db, s)
	ctx := context.Background()
	_, err := s.UpdateConfig(ctx, InvoiceConfig{Enabled: true, MinAmountCent: 10000, FeeTiers: []InvoiceFeeTier{{10000, 1000}}})
	require.NoError(t, err)
	amount, err := s.PaymentAmount(ctx, r.ID, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2100, amount)
	_, err = s.PaymentAmount(ctx, r.ID, 2)
	require.Error(t, err)
	var oid int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id,pay_amount)VALUES(1,306)RETURNING id`).Scan(&oid))
	old := int64(2100)
	_, err = s.Create(ctx, 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd"}, OrderIDs: []int64{oid}, ExpectedFeeCent: &old, PricingVersion: 1})
	require.Error(t, err)
	_, err = s.Create(ctx, 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd"}, OrderIDs: []int64{oid}})
	require.Error(t, err)
}

type invoiceFeeTestUsers struct{ UserRepository }

func (invoiceFeeTestUsers) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Email: "payer@example.com", Username: "payer", Status: StatusActive}, nil
}

type invoiceFeeTestLB struct{ config map[string]string }

func (l invoiceFeeTestLB) GetInstanceConfig(context.Context, int64) (map[string]string, error) {
	return l.config, nil
}
func (l invoiceFeeTestLB) SelectInstance(context.Context, string, string, payment.Strategy, float64) (*payment.InstanceSelection, error) {
	return &payment.InstanceSelection{InstanceID: "1", ProviderKey: payment.TypeEasyPay, PaymentMode: "popup", Config: l.config}, nil
}

func TestInvoiceFeeCreatesExistingGatewayOrderAndVerifiedCallback(t *testing.T) {
	db, invoices := reviewInvoiceDB(t)
	ctx := context.Background()
	r := createChargedTestInvoice(t, db, invoices)
	_, err := db.Exec(`ALTER TABLE payment_orders
 ADD COLUMN user_email VARCHAR(255) NOT NULL DEFAULT '', ADD COLUMN user_name VARCHAR(100) NOT NULL DEFAULT '', ADD COLUMN user_notes TEXT,
 ADD COLUMN bonus_amount NUMERIC NOT NULL DEFAULT 0,ADD COLUMN recharge_code VARCHAR(64) NOT NULL DEFAULT '',ADD COLUMN payment_trade_no VARCHAR(128) NOT NULL DEFAULT '',ADD COLUMN qr_code_img TEXT,
 ADD COLUMN plan_id BIGINT,ADD COLUMN subscription_group_id BIGINT,ADD COLUMN subscription_days INTEGER,ADD COLUMN provider_instance_id VARCHAR(64),ADD COLUMN provider_key VARCHAR(30),
 ADD COLUMN refund_reason TEXT,ADD COLUMN force_refund BOOLEAN NOT NULL DEFAULT FALSE,ADD COLUMN refund_request_reason TEXT,ADD COLUMN refund_requested_by VARCHAR(20),
 ADD COLUMN failed_at TIMESTAMPTZ,ADD COLUMN failed_reason TEXT,ADD COLUMN client_ip VARCHAR(50) NOT NULL DEFAULT '',ADD COLUMN src_host VARCHAR(255) NOT NULL DEFAULT '',ADD COLUMN src_url TEXT;`)
	require.NoError(t, err)
	for _, name := range []string{"096_payment_provider_instances.sql", "093_payment_audit_logs.sql"} {
		raw, e := migrations.FS.ReadFile(name)
		require.NoError(t, e)
		_, e = db.Exec(string(raw))
		require.NoError(t, e)
	}
	_, err = db.Exec(`ALTER TABLE payment_provider_instances ADD COLUMN payment_mode VARCHAR(20) DEFAULT 'popup',ADD COLUMN allow_user_refund BOOLEAN DEFAULT FALSE; ALTER TABLE payment_audit_logs ALTER COLUMN order_id TYPE BIGINT USING order_id::bigint`)
	require.NoError(t, err)
	gatewayConfig := map[string]string{"pid": "test", "pkey": "test-only-key", "apiBase": "https://cashier.invalid", "notifyUrl": "https://site.invalid/notify", "returnUrl": "https://site.invalid/return", "paymentMode": "popup"}
	_, err = db.Exec(`INSERT INTO payment_provider_instances(provider_key,name,config,supported_types,enabled,payment_mode)VALUES('easypay','test',$1,'alipay',TRUE,'popup')`, invoiceJSON(gatewayConfig))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settings := newNotificationEmailMemorySettingRepo()
	for k, v := range map[string]string{SettingPaymentEnabled: "true", SettingBalancePayDisabled: "true", SettingMinRechargeAmount: "100", SettingRechargeFeeRate: "20", SettingBalanceRechargeMult: "10"} {
		require.NoError(t, settings.Set(ctx, k, v))
	}
	svc := &PaymentService{entClient: client, invoiceService: invoices, configService: NewPaymentConfigService(client, settings, nil), userRepo: invoiceFeeTestUsers{}, loadBalancer: invoiceFeeTestLB{gatewayConfig}}
	result, err := svc.CreateOrder(ctx, CreateOrderRequest{UserID: 1, InvoiceID: r.ID, OrderType: payment.OrderTypeInvoiceFee, Amount: 9999, PaymentType: "alipay"})
	require.NoError(t, err)
	require.Equal(t, 21.0, result.PayAmount)
	require.Zero(t, result.FeeRate)
	require.Zero(t, result.BonusAmount)
	require.Contains(t, result.PayURL, "money=21.00")
	order, err := client.PaymentOrder.Get(ctx, result.OrderID)
	require.NoError(t, err)
	require.Equal(t, payment.OrderTypeInvoiceFee, order.OrderType)
	require.Equal(t, r.ID, order.ProviderSnapshot["invoice_application_id"])
	_, err = svc.CreateOrder(ctx, CreateOrderRequest{UserID: 1, InvoiceID: r.ID, OrderType: payment.OrderTypeInvoiceFee, Amount: 1, PaymentType: "alipay"})
	require.Error(t, err)
	require.NoError(t, svc.confirmPayment(ctx, result.OrderID, "verified-trade", 21, payment.TypeEasyPay, map[string]string{"pid": "test"}, true))
	r, err = invoices.get(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, InvoiceReview, r.Status)
	require.True(t, r.FeePaid)
	require.NoError(t, svc.confirmPayment(ctx, result.OrderID, "verified-trade", 21, payment.TypeEasyPay, map[string]string{"pid": "test"}, true))
	var jobs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_admin_notifications WHERE application_id=$1`, r.ID).Scan(&jobs))
	require.Equal(t, 1, jobs)
	// A verified late payment must be recorded and queued for refund, even after
	// the normal recharge expiry grace window has elapsed.
	late := insertTestFeePayment(t, db, r, 2100)
	_, err = db.Exec(`UPDATE payment_orders SET status='EXPIRED',updated_at=NOW()-interval '2 days',provider_key='easypay',provider_instance_id='1' WHERE id=$1`, late)
	require.NoError(t, err)
	require.NoError(t, svc.confirmPayment(ctx, late, "late-trade", 21, payment.TypeEasyPay, map[string]string{"pid": "test"}, true))
	var refundRequired bool
	require.NoError(t, db.QueryRow(`SELECT refund_required FROM invoice_fee_payments WHERE payment_order_id=$1`, late).Scan(&refundRequired))
	require.True(t, refundRequired)
	// Competing pay clicks may create only one still-payable order.
	another := createChargedTestInvoice(t, db, invoices)
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, e := svc.CreateOrder(ctx, CreateOrderRequest{UserID: 1, InvoiceID: another.ID, OrderType: payment.OrderTypeInvoiceFee, Amount: 1, PaymentType: "alipay"})
			outcomes <- e
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if <-outcomes == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var attempts int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_fee_payments WHERE application_id=$1`, another.ID).Scan(&attempts))
	require.Equal(t, 1, attempts)
}

func TestInvoiceFeeRefundDoesNotDeductWallet(t *testing.T) {
	svc := &PaymentService{}
	p := &RefundPlan{DeductBalance: true, RefundAmount: 21}
	result := svc.prepDeduct(context.Background(), &dbent.PaymentOrder{OrderType: payment.OrderTypeInvoiceFee}, p, false)
	require.Nil(t, result)
	require.False(t, p.DeductBalance)
	require.Equal(t, payment.DeductionTypeNone, p.DeductionType)
	require.Zero(t, p.BalanceToDeduct)
}
