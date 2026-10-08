package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// These tests run real PostgreSQL SQL and concurrency without relying on the
// Docker integration harness. The opt-in DSN is deliberately restricted to a
// disposable local Unix socket; no existing remote database is ever contacted.
func newPaymentVerificationPostgresTest(t *testing.T) (*sql.DB, *paymentVerificationRepository) {
	t.Helper()
	dsn := os.Getenv("SUB2API_PAYMENT_VERIFICATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUB2API_PAYMENT_VERIFICATION_TEST_DSN to a disposable local PostgreSQL Unix socket")
	}
	allowedKeys := map[string]bool{"host": true, "port": true, "dbname": true, "user": true, "sslmode": true}
	fields := map[string]string{}
	for _, field := range strings.Fields(dsn) {
		pair := strings.SplitN(field, "=", 2)
		require.Len(t, pair, 2, "use a plain key=value DSN, not a URI")
		require.True(t, allowedKeys[pair[0]], "unsupported test DSN option")
		_, duplicate := fields[pair[0]]
		require.False(t, duplicate, "duplicate test DSN option")
		fields[pair[0]] = pair[1]
	}
	host := fields["host"]
	parent := filepath.Dir(host)
	validSocket := filepath.Clean(host) == host && (parent == "/private/tmp" || parent == "/tmp") && strings.HasPrefix(filepath.Base(host), "sub2api-payment-verification.")
	require.True(t, validSocket, "payment verification PostgreSQL tests require their dedicated temporary Unix socket")
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "payment_verification_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(context.Background(), `CREATE SCHEMA `+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, dropErr := admin.ExecContext(context.Background(), `DROP SCHEMA `+pq.QuoteIdentifier(schema)+` CASCADE`)
		require.NoError(t, dropErr)
		require.NoError(t, admin.Close())
	})
	_, err = db.ExecContext(context.Background(), `
CREATE TABLE users (id BIGSERIAL PRIMARY KEY, role TEXT NOT NULL DEFAULT 'user', status TEXT NOT NULL DEFAULT 'active', deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TABLE payment_orders (id BIGSERIAL PRIMARY KEY,user_id BIGINT NOT NULL,status TEXT NOT NULL,order_type TEXT NOT NULL,
refund_amount DECIMAL(20,8) NOT NULL DEFAULT 0,refund_at TIMESTAMPTZ,refund_requested_at TIMESTAMPTZ,pay_amount DECIMAL(20,8) NOT NULL,
provider_key TEXT,provider_instance_id TEXT,provider_snapshot JSONB,completed_at TIMESTAMPTZ);`)
	require.NoError(t, err)
	for _, name := range []string{"093_payment_audit_logs.sql", "243_payment_verification.sql"} {
		migration, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		_, err = db.ExecContext(context.Background(), string(migration))
		require.NoError(t, err)
	}
	// Migration 131 makes all order/action audit pairs unique.
	_, err = db.ExecContext(context.Background(), `CREATE UNIQUE INDEX audit_order_action_unique ON payment_audit_logs(order_id,action)`)
	require.NoError(t, err)
	return db, &paymentVerificationRepository{db: db}
}

func insertPaymentVerificationPostgresRecord(t *testing.T, db *sql.DB, repo *paymentVerificationRepository, role, result, state string, now time.Time) *service.PaymentVerificationRecord {
	t.Helper()
	var uid, oid int64
	require.NoError(t, db.QueryRowContext(context.Background(), `INSERT INTO users (role) VALUES ($1) RETURNING id`, role).Scan(&uid))
	require.NoError(t, db.QueryRowContext(context.Background(), `INSERT INTO payment_orders (user_id,status,order_type,pay_amount,provider_key,provider_instance_id,completed_at)
VALUES ($1,'COMPLETED','balance',80,'easypay','1',$2) RETURNING id`, uid, now.Add(-time.Hour)).Scan(&oid))
	record, err := repo.SaveCheck(context.Background(), &service.PaymentVerificationRecord{OrderID: oid, UserID: uid, Email: "user@example.com", ProviderName: "merchant",
		OutTradeNo: fmt.Sprintf("test_%d", oid), Amount: 80, PayAmount: 80, Currency: "CNY", OrderStatus: "COMPLETED", Result: result, State: state,
		Reason: "test evidence", CheckedAt: now, ActionToken: strings.ReplaceAll(uuid.NewString(), "-", ""), ExpiresAt: now.Add(24 * time.Hour)}, now.Add(5*time.Minute))
	require.NoError(t, err)
	return record
}

func TestPaymentVerificationPostgresSaveDedupeEscalationAndRecovery(t *testing.T) {
	db, repo := newPaymentVerificationPostgresTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := insertPaymentVerificationPostgresRecord(t, db, repo, "user", service.PaymentVerificationQueryError, service.PaymentVerificationPending, now)
	warnings, err := repo.ClaimNotifications(ctx, "worker", now, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	binding := service.PaymentVerificationNotificationBinding{TemplateID: "template", ConfigGeneration: "generation", BotFingerprint: "fingerprint", TelegramChatID: 12, TelegramTopicID: 34}
	require.NoError(t, repo.PrepareNotification(ctx, record.ID, "worker", binding))
	require.NoError(t, repo.MarkNotified(ctx, record.ID, "worker", 56, now))
	warningToken := record.ActionToken
	record.CheckedAt = now.Add(time.Minute)
	record.ActionToken = strings.ReplaceAll(uuid.NewString(), "-", "")
	record, err = repo.SaveCheck(ctx, record, now.Add(5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, warningToken, record.ActionToken)
	require.NotNil(t, record.NotifiedAt)
	require.Equal(t, int64(56), record.TelegramMessageID)
	claims, err := repo.ClaimNotifications(ctx, "worker", now.Add(time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, claims)
	// Confirmed mismatch upgrades the earlier warning and invalidates its button.
	record.CheckedAt = now.Add(2 * time.Minute)
	record.Result = service.PaymentVerificationUnpaid
	record.ActionToken = strings.ReplaceAll(uuid.NewString(), "-", "")
	record, err = repo.SaveCheck(ctx, record, now.Add(7*time.Minute))
	require.NoError(t, err)
	require.NotEqual(t, warningToken, record.ActionToken)
	require.Nil(t, record.NotifiedAt)
	require.Zero(t, record.TelegramMessageID)
	_, err = repo.GetByToken(ctx, warningToken)
	require.ErrorIs(t, err, service.ErrPaymentVerificationNotFound)
	loaded, err := repo.GetByToken(ctx, record.ActionToken)
	require.NoError(t, err)
	require.Equal(t, record.ID, loaded.ID)
	claims, err = repo.ClaimNotifications(ctx, "worker", now.Add(2*time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	require.NoError(t, repo.RetryNotification(ctx, record.ID, "worker", now.Add(5*time.Minute)))
	claims, err = repo.ClaimNotifications(ctx, "worker", now.Add(3*time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, claims)
	claims, err = repo.ClaimNotifications(ctx, "worker", now.Add(5*time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	record.CheckedAt = now.Add(6 * time.Minute)
	record.Result = service.PaymentVerificationVerified
	record.State = service.PaymentVerificationResolved
	record, err = repo.SaveCheck(ctx, record, now.Add(10*time.Minute))
	require.NoError(t, err)
	require.Equal(t, service.PaymentVerificationResolved, record.State)
	require.Equal(t, "system", record.HandledBy)
	_, err = repo.Resolve(ctx, record.ID, record.CheckedAt, "ban", "admin:1", now.Add(7*time.Minute))
	require.ErrorIs(t, err, service.ErrPaymentVerificationChanged)
	alerts, err := repo.ListAlerts(ctx, 100)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
}

func TestPaymentVerificationPostgresConcurrentIgnoreAndBan(t *testing.T) {
	db, repo := newPaymentVerificationPostgresTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := insertPaymentVerificationPostgresRecord(t, db, repo, "user", service.PaymentVerificationUnpaid, service.PaymentVerificationPending, now)
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, action := range []string{"ban", "ignore"} {
		wg.Add(1)
		go func(action string) {
			defer wg.Done()
			<-start
			_, err := repo.Resolve(ctx, record.ID, record.CheckedAt, action, "admin:1", now)
			errorsCh <- err
		}(action)
	}
	close(start)
	wg.Wait()
	close(errorsCh)
	success, conflict := 0, 0
	for err := range errorsCh {
		if err == nil {
			success++
		} else if errors.Is(err, service.ErrPaymentVerificationChanged) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	current, err := repo.GetByID(ctx, record.ID)
	require.NoError(t, err)
	var userStatus string
	var audits int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=$1`, record.UserID).Scan(&userStatus))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_audit_logs`).Scan(&audits))
	require.Equal(t, 1, audits)
	if current.State == service.PaymentVerificationBanned {
		require.Equal(t, "disabled", userStatus)
	} else {
		require.Equal(t, "active", userStatus)
	}
	// Later scans cannot reopen either human decision or re-send its warning.
	record.CheckedAt = now.Add(time.Minute)
	record.ActionToken = strings.ReplaceAll(uuid.NewString(), "-", "")
	saved, err := repo.SaveCheck(ctx, record, now.Add(5*time.Minute))
	require.NoError(t, err)
	require.Equal(t, current.State, saved.State)
	claims, err := repo.ClaimNotifications(ctx, "worker", now.Add(2*time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, claims)
}

func TestPaymentVerificationPostgresRejectsAdminRefundAndStaleEvidence(t *testing.T) {
	for _, kind := range []string{"admin", "refund", "state changed", "query error", "newer check"} {
		t.Run(kind, func(t *testing.T) {
			db, repo := newPaymentVerificationPostgresTest(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Microsecond)
			role := "user"
			if kind == "admin" {
				role = "admin"
			}
			record := insertPaymentVerificationPostgresRecord(t, db, repo, role, service.PaymentVerificationUnpaid, service.PaymentVerificationPending, now)
			switch kind {
			case "refund":
				_, err := db.ExecContext(ctx, `UPDATE payment_orders SET refund_amount=1 WHERE id=$1`, record.OrderID)
				require.NoError(t, err)
			case "state changed":
				_, err := db.ExecContext(ctx, `UPDATE payment_orders SET status='REFUND_PENDING' WHERE id=$1`, record.OrderID)
				require.NoError(t, err)
			case "query error":
				_, err := db.ExecContext(ctx, `UPDATE payment_verification_records SET result='query_error' WHERE id=$1`, record.ID)
				require.NoError(t, err)
			case "newer check":
				_, err := db.ExecContext(ctx, `UPDATE payment_verification_records SET checked_at=$2 WHERE id=$1`, record.ID, now.Add(time.Minute))
				require.NoError(t, err)
			}
			_, err := repo.Resolve(ctx, record.ID, record.CheckedAt, "ban", "admin:1", now.Add(time.Second))
			require.Error(t, err)
			var status string
			var audits int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=$1`, record.UserID).Scan(&status))
			require.Equal(t, "active", status)
			require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_audit_logs`).Scan(&audits))
			require.Zero(t, audits)
		})
	}
}

func TestPaymentVerificationPostgresAuditFailureRollsBackBan(t *testing.T) {
	db, repo := newPaymentVerificationPostgresTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := insertPaymentVerificationPostgresRecord(t, db, repo, "user", service.PaymentVerificationUnpaid, service.PaymentVerificationPending, now)
	_, err := db.ExecContext(ctx, `ALTER TABLE payment_audit_logs ADD CONSTRAINT reject_verification CHECK(action NOT LIKE 'PAYMENT_VERIFICATION_%')`)
	require.NoError(t, err)
	_, err = repo.Resolve(ctx, record.ID, record.CheckedAt, "ban", "admin:1", now)
	require.Error(t, err)
	current, err := repo.GetByID(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, service.PaymentVerificationPending, current.State)
	var status string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM users WHERE id=$1`, record.UserID).Scan(&status))
	require.Equal(t, "active", status)
}

func TestPaymentVerificationPostgresNotificationLeaseOwnership(t *testing.T) {
	db, repo := newPaymentVerificationPostgresTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	record := insertPaymentVerificationPostgresRecord(t, db, repo, "user", service.PaymentVerificationUnpaid, service.PaymentVerificationPending, now)
	first, err := repo.ClaimNotifications(ctx, "first", now, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := repo.ClaimNotifications(ctx, "second", now, 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, second)
	second, err = repo.ClaimNotifications(ctx, "second", now.Add(2*time.Minute), 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, second, 1)
	binding := service.PaymentVerificationNotificationBinding{TemplateID: "test", ConfigGeneration: "gen", BotFingerprint: "fingerprint", TelegramChatID: 123, TelegramTopicID: 2}
	require.Error(t, repo.PrepareNotification(ctx, record.ID, "first", binding))
	require.Error(t, repo.MarkNotified(ctx, record.ID, "first", 456, now))
	require.Error(t, repo.RetryNotification(ctx, record.ID, "first", now))
	require.NoError(t, repo.PrepareNotification(ctx, record.ID, "second", binding))
	require.NoError(t, repo.MarkNotified(ctx, record.ID, "second", 789, now.Add(2*time.Minute)))
	current, err := repo.GetByID(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, int64(123), current.TelegramChatID)
	require.Equal(t, int64(789), current.TelegramMessageID)
	require.Equal(t, int64(2), current.TelegramTopicID)
	require.Equal(t, "gen", current.ConfigGeneration)
	require.NotNil(t, current.NotifiedAt)
}

func TestPaymentVerificationPostgresCandidatesAreBoundedAndScoped(t *testing.T) {
	db, repo := newPaymentVerificationPostgresTest(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	for range 12 {
		insertPaymentVerificationPostgresRecord(t, db, repo, "user", service.PaymentVerificationVerified, service.PaymentVerificationResolved, now.Add(-10*time.Minute))
	}
	ids, err := repo.ListCandidates(ctx, now.Add(-24*time.Hour), now, 1000)
	require.NoError(t, err)
	require.Len(t, ids, 10)
	_, err = db.ExecContext(ctx, `UPDATE payment_orders SET provider_instance_id=NULL WHERE id=$1`, ids[0])
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE payment_orders SET refund_requested_at=NOW() WHERE id=$1`, ids[1])
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE payment_orders SET provider_key='stripe' WHERE id=$1`, ids[2])
	require.NoError(t, err)
	ids, err = repo.ListCandidates(ctx, now.Add(-24*time.Hour), now, 10)
	require.NoError(t, err)
	require.Len(t, ids, 9)
	for _, id := range ids {
		_, err = db.ExecContext(ctx, `UPDATE payment_verification_records SET state='ignored' WHERE order_id=$1`, id)
		require.NoError(t, err)
	}
	ids, err = repo.ListCandidates(ctx, now.Add(-24*time.Hour), now, 10)
	require.NoError(t, err)
	require.Empty(t, ids)
}
