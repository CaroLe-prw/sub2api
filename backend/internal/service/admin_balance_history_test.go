package service

import (
	"context"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestMergeBalanceHistoryCodesIncludesAffiliateTransfersByDefault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(time.Hour)

	usedBy := int64(10)
	redeemCodes := []RedeemCode{
		{
			ID:        1,
			Type:      RedeemTypeBalance,
			Value:     8,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        2,
			Type:      RedeemTypeConcurrency,
			Value:     1,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &older,
			CreatedAt: older,
		},
	}
	affiliateCodes := []RedeemCode{
		{
			ID:        -20,
			Type:      RedeemTypeAffiliateBalance,
			Value:     3.5,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &newer,
			CreatedAt: newer,
		},
	}

	got := mergeBalanceHistoryCodes(redeemCodes, affiliateCodes, nil, nil, pagination.PaginationParams{
		Page:     1,
		PageSize: 2,
	})

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeAffiliateBalance, got[0].Type)
	require.Equal(t, RedeemTypeBalance, got[1].Type)
}

func TestMergeBalanceHistoryCodesPaginatesAfterCombiningSources(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	usedBy := int64(10)
	at := func(hours int) *time.Time {
		v := base.Add(time.Duration(hours) * time.Hour)
		return &v
	}

	got := mergeBalanceHistoryCodes(
		[]RedeemCode{
			{ID: 1, Type: RedeemTypeBalance, UsedBy: &usedBy, UsedAt: at(4), CreatedAt: *at(4)},
			{ID: 2, Type: RedeemTypeConcurrency, UsedBy: &usedBy, UsedAt: at(2), CreatedAt: *at(2)},
		},
		[]RedeemCode{
			{ID: -3, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(3), CreatedAt: *at(3)},
			{ID: -4, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(1), CreatedAt: *at(1)},
		},
		nil,
		nil,
		pagination.PaginationParams{Page: 2, PageSize: 2},
	)

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeConcurrency, got[0].Type)
	require.Equal(t, int64(-4), got[1].ID)
}

func TestMergeBalanceHistoryCodesIncludesLotteryRewardsByDefault(t *testing.T) {
	t.Parallel()

	settledAt := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	usedBy := int64(42)
	lotteryCodes := []RedeemCode{{
		ID:        91,
		Code:      "LOTTERY-7-91",
		Type:      RedeemTypeLotteryBalance,
		Value:     0.5,
		Status:    StatusUsed,
		UsedBy:    &usedBy,
		UsedAt:    &settledAt,
		CreatedAt: settledAt.Add(-time.Hour),
	}}

	got := mergeBalanceHistoryCodes(nil, nil, lotteryCodes, nil, pagination.PaginationParams{Page: 1, PageSize: 20})

	require.Equal(t, lotteryCodes, got)
}

func TestListLotteryBalanceHistoryMapsWinningEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	enteredAt := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	settledAt := enteredAt.Add(2 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT e.id, e.round_id, COALESCE(e.prize_name, ''),")).
		WithArgs(int64(42), 0, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "round_id", "prize_name", "reward_amount", "entered_at", "settled_at"}).
			AddRow(91, 7, "鸿运锦鲤", 0.5, enteredAt, settledAt))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM lottery_entries")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	svc := &adminServiceImpl{entClient: client}
	got, total, err := svc.listLotteryBalanceHistory(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, got, 1)
	require.Equal(t, RedeemTypeLotteryBalance, got[0].Type)
	require.Equal(t, "LOTTERY-7-91", got[0].Code)
	require.Equal(t, "鸿运锦鲤", got[0].Notes)
	require.InDelta(t, 0.5, got[0].Value, 1e-9)
	require.Equal(t, settledAt, *got[0].UsedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMergeBalanceHistoryCodesIncludesCheckInRewardsByDefault(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 8, 31, 8, 30, 0, 0, time.UTC)
	usedBy := int64(42)
	checkInCodes := []RedeemCode{{
		ID:        19,
		Code:      "CHECKIN-2026-08-31-19",
		Type:      RedeemTypeCheckInBalance,
		Value:     0.08,
		Status:    StatusUsed,
		UsedBy:    &usedBy,
		UsedAt:    &createdAt,
		CreatedAt: createdAt,
	}}

	got := mergeBalanceHistoryCodes(nil, nil, nil, checkInCodes, pagination.PaginationParams{Page: 1, PageSize: 20})

	require.Equal(t, checkInCodes, got)
}

func TestListCheckInBalanceHistoryMapsRewardRecords(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	createdAt := time.Date(2026, 8, 31, 8, 30, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, business_date::text,")).
		WithArgs(int64(42), 0, 20).
		WillReturnRows(sqlmock.NewRows([]string{"id", "business_date", "reward_amount", "created_at"}).
			AddRow(19, "2026-08-31", 0.08, createdAt))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM user_check_ins")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	svc := &adminServiceImpl{entClient: client}
	got, total, err := svc.listCheckInBalanceHistory(context.Background(), 42, pagination.PaginationParams{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, got, 1)
	require.Equal(t, RedeemTypeCheckInBalance, got[0].Type)
	require.Equal(t, "CHECKIN-2026-08-31-19", got[0].Code)
	require.Equal(t, "2026-08-31", got[0].Notes)
	require.InDelta(t, 0.08, got[0].Value, 1e-9)
	require.Equal(t, createdAt, *got[0].UsedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}
