package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "modernc.org/sqlite"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCheckInRepositoryClaimCreditsBalanceInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &checkInRepository{db: db}
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO user_check_ins (user_id, business_date, reward_amount)")).
		WithArgs(int64(7), "2026-08-15", 0.05).
		WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at"}).AddRow("2026-08-15", 0.05, now))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE users")).
		WithArgs(0.05, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(3.25))
	mock.ExpectCommit()

	record, balance, created, err := repo.Claim(context.Background(), 7, now, 0.05, 0)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "2026-08-15", record.Date)
	require.Equal(t, 3.25, balance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryClaimReturnsExistingWithoutSecondCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &checkInRepository{db: db}
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO user_check_ins (user_id, business_date, reward_amount)")).
		WithArgs(int64(7), "2026-08-15", 0.09).
		WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT c.business_date::text, c.reward_amount, c.created_at, u.balance")).
		WithArgs(int64(7), "2026-08-15").
		WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at", "balance"}).AddRow("2026-08-15", 0.04, now, 3.2))
	mock.ExpectCommit()

	record, balance, created, err := repo.Claim(context.Background(), 7, now, 0.09, 0)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, 0.04, record.Reward)
	require.Equal(t, 3.2, balance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryOverviewLoadsTodayOutsideSelectedMonth(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &checkInRepository{db: db}
	monthStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT business_date::text, reward_amount, created_at")).
		WithArgs(int64(7), "2026-07-01", "2026-08-01").
		WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at"}).
			AddRow("2026-07-31", 0.03, createdAt))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT business_date::text, reward_amount, created_at")).
		WithArgs(int64(7), "2026-08-15").
		WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at"}).
			AddRow("2026-08-15", 0.05, createdAt))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.balance, COUNT(c.id), COALESCE(SUM(c.reward_amount), 0)")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "count", "reward"}).AddRow(3.25, 2, 0.08))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT business_date::text")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"business_date"}).AddRow("2026-08-15").AddRow("2026-07-31"))

	overview, err := repo.Overview(context.Background(), 7, monthStart, monthEnd, today)
	require.NoError(t, err)
	require.Len(t, overview.MonthRecords, 1)
	require.NotNil(t, overview.TodayRecord)
	require.Equal(t, "2026-08-15", overview.TodayRecord.Date)
	require.Equal(t, 2, overview.TotalDays)
	require.InDelta(t, 0.08, overview.TotalReward, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryAdminListRecordsIncludesUserAndReward(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &checkInRepository{db: db}
	createdAt := time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM user_check_ins")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT c.id, c.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "email", "username", "date", "reward", "created_at"}).
			AddRow(9, 7, "winner@example.com", "winner", "2026-08-15", 0.0123, createdAt))

	records, total, err := repo.AdminListRecords(context.Background(), 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, "winner@example.com", records[0].Email)
	require.InDelta(t, 0.0123, records[0].Reward, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryRechargeGate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		total     float64
		lookupErr error
		allowed   bool
	}{
		{name: "never recharged", total: 0},
		{name: "below minimum", total: 9.99},
		{name: "exact minimum", total: 10, allowed: true},
		{name: "above minimum", total: 20, allowed: true},
		{name: "lookup failed", lookupErr: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &checkInRepository{db: db}
			now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
			mock.ExpectBegin()
			q := mock.ExpectQuery(regexp.QuoteMeta(checkInPaidRechargeTotalQuery)).WithArgs(int64(7))
			if tc.lookupErr != nil {
				q.WillReturnError(tc.lookupErr)
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(tc.total))
			}
			if tc.allowed {
				mock.ExpectQuery("INSERT INTO user_check_ins").WithArgs(int64(7), "2026-09-11", 0.05).
					WillReturnRows(sqlmock.NewRows([]string{"business_date", "reward_amount", "created_at"}).AddRow("2026-09-11", 0.05, now))
				mock.ExpectQuery("UPDATE users").WithArgs(0.05, int64(7)).
					WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(3.25))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			_, _, created, err := repo.Claim(context.Background(), 7, now, 0.05, 10)
			if tc.allowed {
				require.NoError(t, err)
				require.True(t, created)
			} else {
				require.False(t, created)
				if tc.lookupErr != nil {
					require.ErrorIs(t, err, tc.lookupErr)
				} else {
					require.ErrorIs(t, err, service.ErrCheckInRechargeRequired)
				}
			}
			require.NoError(t, mock.ExpectationsWereMet(), "ineligible users must not insert records or credit rewards")
		})
	}
}

func TestCheckInPaidRechargeTotalCountsOnlySuccessfulBalancePurchases(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE payment_orders (
  user_id INTEGER, order_type TEXT, status TEXT, amount NUMERIC,
  refund_amount NUMERIC, pay_amount NUMERIC, paid_at TEXT
 )`)
	require.NoError(t, err)
	for _, row := range []struct {
		user                 int
		kind, status         string
		amount, refund, paid float64
		paidAt               any
	}{
		{7, "balance", "COMPLETED", 10, 0, 70, "2026-09-11"},
		{7, "balance", "COMPLETED", 2.5, 0, 2.5, "2026-09-11"},
		{7, "balance", "PARTIALLY_REFUNDED", 5, 3, 35, "2026-09-11"},
		{7, "balance", "PARTIALLY_REFUNDED", 1, 2, 7, "2026-09-11"},
		{7, "balance", "REFUNDED", 100, 100, 700, "2026-09-11"},
		{7, "balance", "PENDING", 100, 0, 700, nil},
		{7, "balance", "FAILED", 100, 0, 700, "2026-09-11"},
		{7, "subscription", "COMPLETED", 100, 0, 700, "2026-09-11"},
		{7, "balance", "COMPLETED", 100, 0, 0, "2026-09-11"},
		{7, "balance", "COMPLETED", 100, 0, 700, nil},
		{8, "balance", "COMPLETED", 100, 0, 700, "2026-09-11"},
	} {
		_, err = db.Exec(`INSERT INTO payment_orders VALUES (?, ?, ?, ?, ?, ?, ?)`, row.user, row.kind, row.status, row.amount, row.refund, row.paid, row.paidAt)
		require.NoError(t, err)
	}
	repo := &checkInRepository{db: db}
	total, err := repo.PaidRechargeTotal(context.Background(), 7)
	require.NoError(t, err)
	require.InDelta(t, 14.5, total, 1e-8)
	total, err = repo.PaidRechargeTotal(context.Background(), 9)
	require.NoError(t, err)
	require.Zero(t, total)
}
