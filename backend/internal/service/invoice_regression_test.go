package service

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func reviewInvoiceDB(t *testing.T) (*sql.DB, *InvoiceService) {
	t.Helper()
	socket := os.Getenv("SUB2API_INVOICE_TEST_SOCKET")
	if socket == "" {
		t.Skip("set SUB2API_INVOICE_TEST_SOCKET to a disposable local invoice test PostgreSQL socket")
	}
	require.NotContains(t, socket, " ")
	require.NotContains(t, socket, "'")
	require.True(t, filepath.Clean(socket) == socket && filepath.Dir(socket) == "/private/tmp" && strings.HasPrefix(filepath.Base(socket), "sub2api-invoice-"))
	dsn := "host=" + socket + " port=55481 dbname=postgres sslmode=disable"
	admin, e := sql.Open("postgres", dsn)
	require.NoError(t, e)
	schema := "invreview_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, e = admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema))
	require.NoError(t, e)
	db, e := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, e)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		_ = admin.Close()
	})
	_, e = db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY);INSERT INTO users VALUES(1),(2);
 CREATE TABLE payment_orders(id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL, out_trade_no TEXT NOT NULL DEFAULT '',
 paid_at TIMESTAMPTZ,completed_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW(),expires_at TIMESTAMPTZ DEFAULT NOW()+interval '1 hour',payment_type TEXT DEFAULT 'alipay',pay_url TEXT,qr_code TEXT,amount NUMERIC DEFAULT 0,
 pay_amount NUMERIC NOT NULL DEFAULT 102,fee_rate NUMERIC NOT NULL DEFAULT 2,provider_snapshot JSONB,
 status TEXT NOT NULL DEFAULT 'COMPLETED',order_type TEXT NOT NULL DEFAULT 'balance',
 refund_amount NUMERIC NOT NULL DEFAULT 0,refund_at TIMESTAMPTZ,refund_requested_at TIMESTAMPTZ,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`)
	require.NoError(t, e)
	migration, e := migrations.FS.ReadFile("247_invoice_management.sql")
	require.NoError(t, e)
	_, e = db.Exec(string(migration))
	require.NoError(t, e)
	mailMigration, e := migrations.FS.ReadFile("248_invoice_admin_notifications.sql")
	require.NoError(t, e)
	_, e = db.Exec(string(mailMigration))
	require.NoError(t, e)
	feeMigration, e := migrations.FS.ReadFile("249_invoice_fee_payments.sql")
	require.NoError(t, e)
	_, e = db.Exec(string(feeMigration))
	require.NoError(t, e)
	_, e = db.Exec(`UPDATE invoice_settings SET enabled=true`)
	require.NoError(t, e)
	s := NewInvoiceService(db)

	return db, s
}
func reviewInvoiceCreate(t *testing.T, db *sql.DB, s *InvoiceService) *InvoiceRecord {
	t.Helper()
	var oid int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id)VALUES(1)RETURNING id`).Scan(&oid))
	r, e := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "测试公司", TaxNumber: "913100000000000000"}, OrderIDs: []int64{oid}, InvoiceItem: "技术服务费"})
	require.NoError(t, e)
	return r
}

func TestInvoiceReviewNormalOwnershipAndRefund(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, s)
	require.EqualValues(t, 10000, r.AmountCent)
	require.EqualValues(t, 200, r.Orders[0].PaymentFeeCent)
	_, e := s.get(context.Background(), r.ID, 2)
	require.Error(t, e)
	_, e = db.Exec(`UPDATE payment_orders SET status='REFUND_REQUESTED' WHERE id=$1`, r.Orders[0].ID)
	require.Error(t, e)
	_, e = s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: r.InvoiceTitle, OrderIDs: []int64{r.Orders[0].ID}, InvoiceItem: "技术服务费"})
	require.Error(t, e)
	_, e = s.Action(context.Background(), r.ID, 1, 1, "withdraw", "", false)
	require.NoError(t, e)
	_, e = db.Exec(`UPDATE payment_orders SET status='REFUND_REQUESTED' WHERE id=$1`, r.Orders[0].ID)
	require.NoError(t, e)
}
func TestInvoiceReviewOrderPagination(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	_, e := db.Exec(`INSERT INTO payment_orders(user_id)SELECT 1 FROM generate_series(1,201)`)
	require.NoError(t, e)
	a, e := s.EligibleOrders(context.Background(), 1, 0)
	require.NoError(t, e)
	require.Len(t, a.Items, 200)
	require.True(t, a.HasMore)
	b, e := s.EligibleOrders(context.Background(), 1, a.NextAfter)
	require.NoError(t, e)
	require.Len(t, b.Items, 1)
	require.False(t, b.HasMore)
}
func TestInvoiceReviewFeeRecovery(t *testing.T) {
	for _, rate := range []float64{0, 0.1, 2, 6, 99.9999} {
		for n := int64(1); n < 10000; n += 37 {
			gross := payment.CalculatePayAmount(float64(n)/100, rate)
			got, _, e := invoicePrincipal(gross, strconv.FormatFloat(rate, 'f', -1, 64), "")
			require.NoError(t, e)
			require.Equal(t, n, got)
		}
	}
}
func TestInvoicePaginationAdvancesPastInvalidAmounts(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	_, err := db.Exec(`INSERT INTO payment_orders(user_id,pay_amount)SELECT 1,0 FROM generate_series(1,200); INSERT INTO payment_orders(user_id)VALUES(1)`)
	require.NoError(t, err)
	page, err := s.EligibleOrders(context.Background(), 1, 0)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.True(t, page.HasMore)
	require.EqualValues(t, 200, page.NextAfter)
	next, err := s.EligibleOrders(context.Background(), 1, page.NextAfter)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.EqualValues(t, 201, next.Items[0].ID)
}

var manualInvoicePDF = []byte("%PDF-1.7\n1 0 obj<</Type/Catalog>>endobj\n%%EOF\n")

func TestInvoiceManualApprovalUploadDownload(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, s)
	ctx := context.Background()
	var payload []byte
	r, err := s.Action(ctx, r.ID, 0, 2, "approve", "reviewed", false)
	require.NoError(t, err)
	require.Equal(t, InvoiceApproved, r.Status)
	require.True(t, r.Manual)
	require.NoError(t, db.QueryRow(`SELECT payload FROM invoice_applications WHERE id=$1`, r.ID).Scan(&payload))
	require.Nil(t, payload)
	_, _, _, err = s.Download(ctx, r.ID, 1)
	require.Error(t, err)
	r, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "INV-2026-1", false)
	require.NoError(t, err)
	require.Equal(t, InvoiceCompleted, r.Status)
	require.True(t, r.Downloadable)
	require.EqualValues(t, 2, r.UploadedBy)
	require.NotNil(t, r.CompletedAt)
	content, kind, name, err := s.Download(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, manualInvoicePDF, content)
	require.Equal(t, "application/pdf", kind)
	require.Equal(t, r.ID+".pdf", name)
	_, _, _, err = s.Download(ctx, r.ID, 2)
	require.Error(t, err)
	_, err = s.Upload(ctx, r.ID, 2, []byte("%PDF-1.7\nreplacement\n%%EOF"), "other", false)
	require.Error(t, err)
	content, _, _, err = s.Download(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, manualInvoicePDF, content)
	_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDING' WHERE id=$1`, r.Orders[0].ID)
	require.Error(t, err)
	detail, err := s.Detail(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, "UPLOAD", detail.Events[len(detail.Events)-1].Action)
}

func TestInvoiceManualUploadRequiresApprovalAndValidFile(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, s)
	ctx := context.Background()
	_, err := s.Upload(ctx, r.ID, 2, manualInvoicePDF, "", false)
	require.Error(t, err)
	_, err = s.Action(ctx, r.ID, 0, 2, "approve", "", false)
	require.NoError(t, err)
	for _, body := range [][]byte{nil, []byte("<svg><script>alert(1)</script></svg>"), []byte("%PDF-1.7 incomplete"), make([]byte, MaxInvoiceFileSize+1)} {
		_, err = s.Upload(ctx, r.ID, 2, body, "", false)
		require.Error(t, err)
	}
	current, err := s.get(ctx, r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, InvoiceApproved, current.Status)
	require.False(t, current.Downloadable)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_files`).Scan(&count))
	require.Zero(t, count)
}

func TestInvoiceManualCancelRequiresConfirmationAndReleases(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, s)
	ctx := context.Background()
	_, err := s.Action(ctx, r.ID, 0, 2, "approve", "", false)
	require.NoError(t, err)
	_, err = s.Action(ctx, r.ID, 0, 2, "cancel", "not issued", false)
	require.Error(t, err)
	r, err = s.Action(ctx, r.ID, 0, 2, "cancel", "not issued", true)
	require.NoError(t, err)
	require.Equal(t, InvoiceCancelled, r.Status)
	_, err = db.Exec(`UPDATE payment_orders SET status='REFUND_REQUESTED' WHERE id=$1`, r.Orders[0].ID)
	require.NoError(t, err)
	_, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "", false)
	require.Error(t, err)
}

func TestInvoiceLegacyUnknownRemainsHeldUntilManualVerification(t *testing.T) {
	for _, action := range []string{"cancel", "upload"} {
		t.Run(action, func(t *testing.T) {
			db, s := reviewInvoiceDB(t)
			r := reviewInvoiceCreate(t, db, s)
			ctx := context.Background()
			_, err := db.Exec(`UPDATE invoice_applications SET status='UNKNOWN',payload='{}',data=data || '{"manual":false}'::jsonb WHERE id=$1`, r.ID)
			require.NoError(t, err)
			r, err = s.get(ctx, r.ID, 1)
			require.NoError(t, err)
			require.True(t, r.RequiresManualCheck)
			_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDING' WHERE id=$1`, r.Orders[0].ID)
			require.Error(t, err)
			if action == "cancel" {
				_, err = s.Action(ctx, r.ID, 0, 2, "cancel", "verified", false)
				require.Error(t, err)
				r, err = s.Action(ctx, r.ID, 0, 2, "cancel", "verified", true)
				require.NoError(t, err)
				require.Equal(t, InvoiceCancelled, r.Status)
			} else {
				_, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "legacy", false)
				require.Error(t, err)
				r, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "legacy", true)
				require.NoError(t, err)
				require.Equal(t, InvoiceCompleted, r.Status)
			}
			require.False(t, r.RequiresManualCheck)
		})
	}
}

func TestInvoiceLegacyCompletedWithoutLocalFileCanUpload(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, s)
	ctx := context.Background()
	_, err := db.Exec(`UPDATE invoice_applications SET status='COMPLETED',payload='{}',data=data || '{"manual":false,"downloadable":true}'::jsonb WHERE id=$1`, r.ID)
	require.NoError(t, err)
	r, err = s.get(ctx, r.ID, 1)
	require.NoError(t, err)
	require.False(t, r.Downloadable)
	r, err = s.Upload(ctx, r.ID, 2, manualInvoicePDF, "legacy", true)
	require.NoError(t, err)
	require.True(t, r.Downloadable)
}

func TestInvoiceManualSettingsNeedNoKeyOrProduct(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	ctx := context.Background()
	c, err := s.UpdateConfig(ctx, InvoiceConfig{Enabled: true})
	require.NoError(t, err)
	require.True(t, c.Enabled)
	r := reviewInvoiceCreate(t, db, s)
	require.True(t, r.Manual)
	c, err = s.UpdateConfig(ctx, InvoiceConfig{Enabled: false})
	require.NoError(t, err)
	require.False(t, c.Enabled)
	_, err = s.Action(ctx, r.ID, 0, 2, "approve", "", false)
	require.Error(t, err)
	_, err = s.Create(ctx, 1, InvoiceCreate{InvoiceTitle: r.InvoiceTitle, OrderIDs: []int64{r.Orders[0].ID}})
	require.Error(t, err)
}

func TestInvoiceManualMergesPrincipalAndPreservesBillingDetails(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	var first, second int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id,pay_amount)VALUES(1,102)RETURNING id`).Scan(&first))
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id,pay_amount)VALUES(1,51)RETURNING id`).Scan(&second))
	r, err := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd", TaxNumber: "913100000000000000"}, OrderIDs: []int64{second, first}, InvoiceItem: "service", Specification: "API", Unit: "month", Remark: "billing note", SaveTitle: true})
	require.NoError(t, err)
	require.EqualValues(t, 15000, r.AmountCent)
	require.Len(t, r.Orders, 2)
	require.Equal(t, "API", r.Specification)
	require.Equal(t, "month", r.Unit)
	require.Equal(t, "billing note", r.Remark)
	titles, err := s.Titles(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, titles, 1)
}

func TestInvoiceOptionalTaxAndExtraDetails(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	var oid int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id)VALUES(1)RETURNING id`).Scan(&oid))
	price := int64(320)
	r, err := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example overseas"}, OrderIDs: []int64{oid}, InvoiceType: "NORMAL", Quantity: "2.5", UnitPriceCent: &price, Specification: "service detail", Unit: "month"})
	require.NoError(t, err)
	require.Empty(t, r.TaxNumber)
	require.Equal(t, "2.5", r.Quantity)
	require.EqualValues(t, 320, *r.UnitPriceCent)
	require.EqualValues(t, 10000, r.AmountCent)
	require.Equal(t, "NORMAL", r.InvoiceType)
	detail, err := s.Detail(context.Background(), r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, r.Quantity, detail.Quantity)
	require.EqualValues(t, price, *detail.UnitPriceCent)
}

func TestInvoiceRejectsInvalidExtraDetails(t *testing.T) {
	db, s := reviewInvoiceDB(t)
	var oid int64
	require.NoError(t, db.QueryRow(`INSERT INTO payment_orders(user_id)VALUES(1)RETURNING id`).Scan(&oid))
	for _, quantity := range []string{"-1", "0", "1e3", "1.0000001", "1234567890123"} {
		_, err := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd"}, OrderIDs: []int64{oid}, Quantity: quantity})
		require.Error(t, err)
	}
	zero := int64(0)
	_, err := s.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: InvoiceTitle{CompanyName: "Example Ltd"}, OrderIDs: []int64{oid}, UnitPriceCent: &zero})
	require.Error(t, err)
}
