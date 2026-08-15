package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLotterySettlementCreditsSingleEntrantWithThirdPrize(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}
	now := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Hour)
	end := now.Add(-time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT r.id, r.starts_at, r.ends_at,")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "starts_at", "ends_at",
			"first_name", "first_reward", "first_weight", "first_winner_count",
			"second_name", "second_reward", "second_weight", "second_winner_count",
			"third_name", "third_reward", "third_weight", "third_winner_count",
			"settled_at", "created_at", "participant_count",
		}).AddRow(
			17, start, end,
			service.LotteryFirstPrizeName, 1.0, 10, 1,
			service.LotterySecondPrizeName, 0.5, 30, 1,
			service.LotteryThirdPrizeName, 0.2, 60, 1,
			nil, start, 1,
		))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id")).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id"}).AddRow(29, 7))
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE users")).
		WithArgs(0.2, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(4.2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE lottery_entries")).
		WithArgs(3, service.LotteryThirdPrizeName, 0.2, 4.2, now, int64(29)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE lottery_rounds")).
		WithArgs(now, int64(17)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.SettleRound(context.Background(), 17, now, func(prizes []service.LotteryPrize) (service.LotteryPrize, error) {
		return prizes[2], nil
	})
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.Equal(t, 1, result.Entrants)
	require.InDelta(t, 0.2, result.TotalAward, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLotterySettlementRespectsWinnerQuotaAndSettlesNonWinners(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}
	now := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Hour)
	end := now.Add(-time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT r.id, r.starts_at, r.ends_at,")).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "starts_at", "ends_at",
			"first_name", "first_reward", "first_weight", "first_winner_count",
			"second_name", "second_reward", "second_weight", "second_winner_count",
			"third_name", "third_reward", "third_weight", "third_winner_count",
			"settled_at", "created_at", "participant_count",
		}).AddRow(
			17, start, end,
			service.LotteryFirstPrizeName, 1.0, 10, 1,
			service.LotterySecondPrizeName, 0.5, 30, 1,
			service.LotteryThirdPrizeName, 0.2, 60, 1,
			nil, start, 4,
		))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, user_id")).
		WithArgs(int64(17)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id"}).
			AddRow(29, 7).
			AddRow(30, 8).
			AddRow(31, 9).
			AddRow(32, 10))

	for _, winner := range []struct {
		entryID int64
		userID  int64
		balance float64
		prize   service.LotteryPrize
	}{
		{29, 7, 11, service.LotteryPrize{Tier: 1, Name: service.LotteryFirstPrizeName, Reward: 1}},
		{30, 8, 20.5, service.LotteryPrize{Tier: 2, Name: service.LotterySecondPrizeName, Reward: 0.5}},
		{31, 9, 30.2, service.LotteryPrize{Tier: 3, Name: service.LotteryThirdPrizeName, Reward: 0.2}},
	} {
		mock.ExpectQuery(regexp.QuoteMeta("UPDATE users")).
			WithArgs(winner.prize.Reward, winner.userID).
			WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(winner.balance))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE lottery_entries")).
			WithArgs(winner.prize.Tier, winner.prize.Name, winner.prize.Reward, winner.balance, now, winner.entryID).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec(regexp.QuoteMeta("UPDATE lottery_entries")).
		WithArgs(now, int64(32)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE lottery_rounds")).
		WithArgs(now, int64(17)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	pickIndex := 0
	result, err := repo.SettleRound(context.Background(), 17, now, func(prizes []service.LotteryPrize) (service.LotteryPrize, error) {
		index := pickIndex
		if index >= len(prizes) {
			index = 0
		}
		pickIndex++
		return prizes[index], nil
	})
	require.NoError(t, err)
	require.True(t, result.Settled)
	require.Equal(t, 4, result.Entrants)
	require.InDelta(t, 1.7, result.TotalAward, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLotteryRepositoryAdminListResultsShowsPrizeWinner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}
	settledAt := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM lottery_entries WHERE prize_tier IS NOT NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT e.id, e.round_id, e.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"entry_id", "round_id", "user_id", "email", "username", "entered_at", "settled_at",
			"prize_tier", "prize_name", "reward", "balance_after",
		}).AddRow(29, 17, 7, "winner@example.com", "winner", settledAt.Add(-time.Hour), settledAt, 1, service.LotteryFirstPrizeName, 1.5, 8.5))

	results, total, err := repo.AdminListResults(context.Background(), 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, results, 1)
	require.Equal(t, 1, results[0].PrizeTier)
	require.Equal(t, service.LotteryFirstPrizeName, results[0].PrizeName)
	require.Equal(t, "winner@example.com", results[0].Email)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLotteryRepositoryCancelCurrentRoundVoidsOnlyRoundBeforeEnd(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}
	now := time.Date(2026, 8, 15, 15, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("UPDATE lottery_rounds")).
		WithArgs(now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(18))

	cancelled, err := repo.CancelCurrentRound(context.Background(), now)
	require.NoError(t, err)
	require.True(t, cancelled)
	require.NoError(t, mock.ExpectationsWereMet())
}
