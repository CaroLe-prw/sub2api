package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLotteryOverviewPrivacyContractDoesNotExposeWinnerDirectory(t *testing.T) {
	payload, err := json.Marshal(LotteryOverview{
		MyEntries: []LotteryEntry{{ID: 9, RoundID: 3, PrizeName: LotteryFirstPrizeName}},
	})
	require.NoError(t, err)
	jsonText := strings.ToLower(string(payload))
	require.NotContains(t, jsonText, "email")
	require.NotContains(t, jsonText, "username")
	require.NotContains(t, jsonText, "past_rounds")
	require.Contains(t, jsonText, "my_entries")
}

type lotteryRepoStub struct {
	dueIDs       []int64
	configured   int
	settledPrize LotteryPrize
}

func (s *lotteryRepoStub) GetCurrentRound(context.Context) (*LotteryRound, error) { return nil, nil }
func (s *lotteryRepoStub) ConfigureRound(_ context.Context, round LotteryRound) (LotteryRound, error) {
	s.configured++
	return round, nil
}
func (s *lotteryRepoStub) Enter(context.Context, int64, time.Time) (LotteryRound, LotteryEntry, bool, error) {
	return LotteryRound{}, LotteryEntry{}, false, nil
}
func (s *lotteryRepoStub) ListUserEntries(context.Context, int64, int) ([]LotteryEntry, error) {
	return nil, nil
}
func (s *lotteryRepoStub) AdminListResults(context.Context, int, int) ([]LotteryAdminResult, int64, error) {
	return nil, 0, nil
}
func (s *lotteryRepoStub) FindDueRoundIDs(context.Context, time.Time, int) ([]int64, error) {
	return s.dueIDs, nil
}
func (s *lotteryRepoStub) CancelCurrentRound(context.Context, time.Time) (bool, error) {
	return true, nil
}
func (s *lotteryRepoStub) SettleRound(_ context.Context, roundID int64, _ time.Time, picker LotteryPrizePicker) (LotterySettlementSummary, error) {
	prize, err := picker(defaultLotteryPrizes())
	if err != nil {
		return LotterySettlementSummary{}, err
	}
	s.settledPrize = prize
	return LotterySettlementSummary{RoundID: roundID, Entrants: 1, TotalAward: prize.Reward, Settled: true}, nil
}

func TestPickLotteryPrizeByRollUsesConfiguredWeightBoundaries(t *testing.T) {
	prizes := defaultLotteryPrizes()

	first, err := pickLotteryPrizeByRoll(prizes, 9)
	require.NoError(t, err)
	require.Equal(t, 1, first.Tier)

	second, err := pickLotteryPrizeByRoll(prizes, 10)
	require.NoError(t, err)
	require.Equal(t, 2, second.Tier)

	third, err := pickLotteryPrizeByRoll(prizes, 99)
	require.NoError(t, err)
	require.Equal(t, 3, third.Tier)
}

func TestLotterySingleEntrantCanReceiveThirdPrize(t *testing.T) {
	repo := &lotteryRepoStub{dueIDs: []int64{17}}
	service := NewLotteryService(repo, nil)
	service.picker = func(prizes []LotteryPrize) (LotteryPrize, error) {
		return prizes[2], nil
	}

	results, err := service.SettleDueRounds(context.Background(), time.Now())
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, 1, results[0].Entrants)
	require.Equal(t, 3, repo.settledPrize.Tier)
	require.Equal(t, LotteryThirdPrizeName, repo.settledPrize.Name)
}

func TestLotteryConfigValidationRejectsInvalidTimeRange(t *testing.T) {
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	_, err := validateLotteryConfig(LotteryConfigureInput{
		StartsAt:               now.Add(time.Hour),
		EndsAt:                 now,
		FirstPrizeReward:       1,
		FirstPrizeWeight:       10,
		FirstPrizeWinnerCount:  1,
		SecondPrizeReward:      0.5,
		SecondPrizeWeight:      30,
		SecondPrizeWinnerCount: 1,
		ThirdPrizeReward:       0.2,
		ThirdPrizeWeight:       60,
		ThirdPrizeWinnerCount:  1,
	}, now)
	require.ErrorIs(t, err, ErrLotteryInvalid)
}

func TestLotteryConfigMatchesAllowsReopeningLockedRoundWithoutChanges(t *testing.T) {
	start := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	input := LotteryConfigureInput{
		Enabled:                true,
		StartsAt:               start,
		EndsAt:                 start.Add(24 * time.Hour),
		FirstPrizeReward:       1,
		FirstPrizeWeight:       10,
		FirstPrizeWinnerCount:  1,
		SecondPrizeReward:      0.5,
		SecondPrizeWeight:      30,
		SecondPrizeWinnerCount: 1,
		ThirdPrizeReward:       0.2,
		ThirdPrizeWeight:       60,
		ThirdPrizeWinnerCount:  1,
	}
	prizes, err := validateLotteryConfig(input, start.Add(-time.Minute))
	require.NoError(t, err)
	round := LotteryRound{StartsAt: input.StartsAt, EndsAt: input.EndsAt, Prizes: prizes, ParticipantCount: 1}

	require.True(t, lotteryConfigMatches(round, input, prizes))
	input.ThirdPrizeWeight = 61
	changedPrizes := append([]LotteryPrize(nil), prizes...)
	changedPrizes[2].Weight = 61
	require.False(t, lotteryConfigMatches(round, input, changedPrizes))
}
