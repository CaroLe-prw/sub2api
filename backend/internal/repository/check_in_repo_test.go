package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCheckInRepositoryClaimCreditsBalanceInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
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

	record, balance, created, err := repo.Claim(context.Background(), 7, now, 0.05)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "2026-08-15", record.Date)
	require.Equal(t, 3.25, balance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryClaimReturnsExistingWithoutSecondCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
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

	record, balance, created, err := repo.Claim(context.Background(), 7, now, 0.09)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, 0.04, record.Reward)
	require.Equal(t, 3.2, balance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckInRepositoryOverviewLoadsTodayOutsideSelectedMonth(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
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
