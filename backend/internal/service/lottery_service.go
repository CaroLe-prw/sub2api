package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	LotteryFirstPrizeName  = "天选之子"
	LotterySecondPrizeName = "鸿运锦鲤"
	LotteryThirdPrizeName  = "幸运新星"

	LotteryFirstPrizeRewardDefault  = 1.0
	LotterySecondPrizeRewardDefault = 0.5
	LotteryThirdPrizeRewardDefault  = 0.2
	LotteryFirstPrizeWeightDefault  = 10
	LotterySecondPrizeWeightDefault = 30
	LotteryThirdPrizeWeightDefault  = 60
	LotteryPrizeWinnerCountDefault  = 1

	lotteryRewardMax      = 10_000.0
	lotteryWeightMax      = 1_000_000
	lotteryWinnerCountMax = 10_000
)

var (
	ErrLotteryDisabled    = infraerrors.Forbidden("LOTTERY_DISABLED", "lottery is disabled")
	ErrLotteryNotOpen     = infraerrors.Conflict("LOTTERY_NOT_OPEN", "there is no lottery open for participation")
	ErrLotteryRoundLocked = infraerrors.Conflict("LOTTERY_ROUND_LOCKED", "the current lottery already has participants and can no longer be changed")
	ErrLotteryInvalid     = infraerrors.BadRequest("LOTTERY_CONFIG_INVALID", "invalid lottery configuration")
)

type LotteryPrize struct {
	Tier        int     `json:"tier"`
	Name        string  `json:"name"`
	Reward      float64 `json:"reward"`
	Weight      int     `json:"weight"`
	WinnerCount int     `json:"winner_count"`
}

type LotteryRound struct {
	ID               int64          `json:"id"`
	StartsAt         time.Time      `json:"starts_at"`
	EndsAt           time.Time      `json:"ends_at"`
	SettledAt        *time.Time     `json:"settled_at"`
	CreatedAt        time.Time      `json:"created_at"`
	ParticipantCount int            `json:"participant_count"`
	Status           string         `json:"status"`
	Prizes           []LotteryPrize `json:"prizes"`
}

type LotteryEntry struct {
	ID            int64      `json:"id"`
	RoundID       int64      `json:"round_id"`
	EnteredAt     time.Time  `json:"entered_at"`
	RoundStartsAt time.Time  `json:"round_starts_at"`
	RoundEndsAt   time.Time  `json:"round_ends_at"`
	PrizeTier     *int       `json:"prize_tier"`
	PrizeName     string     `json:"prize_name"`
	Reward        float64    `json:"reward"`
	BalanceAfter  *float64   `json:"balance_after"`
	SettledAt     *time.Time `json:"settled_at"`
	CancelledAt   *time.Time `json:"cancelled_at"`
}

type LotteryOverview struct {
	Enabled      bool           `json:"enabled"`
	ServerTime   time.Time      `json:"server_time"`
	Timezone     string         `json:"timezone"`
	CurrentRound *LotteryRound  `json:"current_round"`
	MyEntries    []LotteryEntry `json:"my_entries"`
}

type LotteryEnterResult struct {
	Created bool         `json:"created"`
	Round   LotteryRound `json:"round"`
	Entry   LotteryEntry `json:"entry"`
}

type LotteryConfigureInput struct {
	Enabled                bool
	StartsAt               time.Time
	EndsAt                 time.Time
	FirstPrizeReward       float64
	FirstPrizeWeight       int
	FirstPrizeWinnerCount  int
	SecondPrizeReward      float64
	SecondPrizeWeight      int
	SecondPrizeWinnerCount int
	ThirdPrizeReward       float64
	ThirdPrizeWeight       int
	ThirdPrizeWinnerCount  int
}

type LotteryAdminConfig struct {
	Enabled      bool           `json:"enabled"`
	ServerTime   time.Time      `json:"server_time"`
	Timezone     string         `json:"timezone"`
	CurrentRound *LotteryRound  `json:"current_round"`
	Defaults     []LotteryPrize `json:"defaults"`
}

type LotterySettlementSummary struct {
	RoundID    int64
	Entrants   int
	TotalAward float64
	Settled    bool
}

type LotteryAdminResult struct {
	EntryID      int64      `json:"entry_id"`
	RoundID      int64      `json:"round_id"`
	UserID       int64      `json:"user_id"`
	Email        string     `json:"email"`
	Username     string     `json:"username"`
	EnteredAt    time.Time  `json:"entered_at"`
	SettledAt    *time.Time `json:"settled_at"`
	PrizeTier    int        `json:"prize_tier"`
	PrizeName    string     `json:"prize_name"`
	Reward       float64    `json:"reward"`
	BalanceAfter float64    `json:"balance_after"`
}

type LotteryPrizePicker func(prizes []LotteryPrize) (LotteryPrize, error)

type LotteryRepository interface {
	GetCurrentRound(ctx context.Context) (*LotteryRound, error)
	ConfigureRound(ctx context.Context, round LotteryRound) (LotteryRound, error)
	CancelCurrentRound(ctx context.Context, now time.Time) (bool, error)
	Enter(ctx context.Context, userID int64, now time.Time) (LotteryRound, LotteryEntry, bool, error)
	ListUserEntries(ctx context.Context, userID int64, limit int) ([]LotteryEntry, error)
	AdminListResults(ctx context.Context, page, pageSize int) ([]LotteryAdminResult, int64, error)
	FindDueRoundIDs(ctx context.Context, now time.Time, limit int) ([]int64, error)
	SettleRound(ctx context.Context, roundID int64, now time.Time, picker LotteryPrizePicker) (LotterySettlementSummary, error)
}

type LotteryService struct {
	repo       LotteryRepository
	settings   *SettingService
	now        func() time.Time
	picker     LotteryPrizePicker
	interval   time.Duration
	stopCh     chan struct{}
	stopOnce   sync.Once
	workerOnce sync.Once
	wg         sync.WaitGroup
}

func NewLotteryService(repo LotteryRepository, settings *SettingService) *LotteryService {
	return &LotteryService{
		repo:     repo,
		settings: settings,
		now:      timezone.Now,
		picker:   secureLotteryPrizePicker,
		interval: 30 * time.Second,
		stopCh:   make(chan struct{}),
	}
}

func (s *LotteryService) Start() {
	if s == nil || s.repo == nil || s.interval <= 0 {
		return
	}
	s.workerOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runSettlementCycle()
			ticker := time.NewTicker(s.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.runSettlementCycle()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *LotteryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *LotteryService) GetOverview(ctx context.Context, userID int64) (*LotteryOverview, error) {
	now := s.now().In(timezone.Location())
	s.settleDueBestEffort(ctx, now)
	enabled := s.settings.IsLotteryEnabled(ctx)
	round, err := s.repo.GetCurrentRound(ctx)
	if err != nil {
		return nil, err
	}
	decorateLotteryRound(round, now, enabled)
	entries, err := s.repo.ListUserEntries(ctx, userID, 20)
	if err != nil {
		return nil, err
	}
	return &LotteryOverview{
		Enabled:      enabled,
		ServerTime:   now,
		Timezone:     timezone.Name(),
		CurrentRound: round,
		MyEntries:    entries,
	}, nil
}

func (s *LotteryService) Enter(ctx context.Context, userID int64) (*LotteryEnterResult, error) {
	if !s.settings.IsLotteryEnabled(ctx) {
		return nil, ErrLotteryDisabled
	}
	now := s.now().In(timezone.Location())
	round, entry, created, err := s.repo.Enter(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	decorateLotteryRound(&round, now, true)
	return &LotteryEnterResult{Created: created, Round: round, Entry: entry}, nil
}

func (s *LotteryService) GetAdminConfig(ctx context.Context) (*LotteryAdminConfig, error) {
	now := s.now().In(timezone.Location())
	s.settleDueBestEffort(ctx, now)
	enabled := s.settings.IsLotteryEnabled(ctx)
	round, err := s.repo.GetCurrentRound(ctx)
	if err != nil {
		return nil, err
	}
	decorateLotteryRound(round, now, enabled)
	return &LotteryAdminConfig{
		Enabled:      enabled,
		ServerTime:   now,
		Timezone:     timezone.Name(),
		CurrentRound: round,
		Defaults:     defaultLotteryPrizes(),
	}, nil
}

func (s *LotteryService) AdminListResults(ctx context.Context, page, pageSize int) ([]LotteryAdminResult, int64, error) {
	now := s.now().In(timezone.Location())
	s.settleDueBestEffort(ctx, now)
	return s.repo.AdminListResults(ctx, page, pageSize)
}

func (s *LotteryService) Configure(ctx context.Context, input LotteryConfigureInput) (*LotteryAdminConfig, error) {
	// Closing before the configured end time voids the current round. If the end
	// time has already passed, settle first so an administrator cannot erase a
	// result that is already due.
	if !input.Enabled {
		now := s.now().In(timezone.Location())
		s.settleDueBestEffort(ctx, now)
		if _, err := s.repo.CancelCurrentRound(ctx, now); err != nil {
			return nil, err
		}
		if err := s.settings.SetLotteryEnabled(ctx, false); err != nil {
			return nil, err
		}
		return s.GetAdminConfig(ctx)
	}

	now := s.now().In(timezone.Location())
	s.settleDueBestEffort(ctx, now)
	prizes, err := validateLotteryConfig(input, now)
	if err != nil {
		return nil, err
	}
	current, err := s.repo.GetCurrentRound(ctx)
	if err != nil {
		return nil, err
	}
	if current != nil && current.ParticipantCount > 0 {
		if !lotteryConfigMatches(*current, input, prizes) {
			return nil, ErrLotteryRoundLocked
		}
		if err := s.settings.SetLotteryEnabled(ctx, true); err != nil {
			return nil, err
		}
		return s.GetAdminConfig(ctx)
	}
	round := LotteryRound{StartsAt: input.StartsAt, EndsAt: input.EndsAt, Prizes: prizes}
	if _, err := s.repo.ConfigureRound(ctx, round); err != nil {
		return nil, err
	}
	if err := s.settings.SetLotteryEnabled(ctx, true); err != nil {
		return nil, err
	}
	return s.GetAdminConfig(ctx)
}

func lotteryConfigMatches(round LotteryRound, input LotteryConfigureInput, prizes []LotteryPrize) bool {
	if !round.StartsAt.Equal(input.StartsAt) || !round.EndsAt.Equal(input.EndsAt) || len(round.Prizes) != len(prizes) {
		return false
	}
	for i := range prizes {
		left, right := round.Prizes[i], prizes[i]
		if left.Tier != right.Tier || left.Name != right.Name || left.Weight != right.Weight || left.WinnerCount != right.WinnerCount || math.Abs(left.Reward-right.Reward) > 1e-8 {
			return false
		}
	}
	return true
}

func (s *LotteryService) SettleDueRounds(ctx context.Context, now time.Time) ([]LotterySettlementSummary, error) {
	ids, err := s.repo.FindDueRoundIDs(ctx, now, 20)
	if err != nil {
		return nil, err
	}
	results := make([]LotterySettlementSummary, 0, len(ids))
	for _, id := range ids {
		result, err := s.repo.SettleRound(ctx, id, now, s.picker)
		if err != nil {
			return results, fmt.Errorf("settle lottery round %d: %w", id, err)
		}
		if result.Settled {
			results = append(results, result)
		}
	}
	return results, nil
}

func (s *LotteryService) runSettlementCycle() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results, err := s.SettleDueRounds(ctx, s.now().In(timezone.Location()))
	if err != nil {
		slog.Error("lottery automatic settlement failed", "error", err)
		return
	}
	for _, result := range results {
		slog.Info("lottery round settled", "round_id", result.RoundID, "entrants", result.Entrants, "total_award", result.TotalAward)
	}
}

func (s *LotteryService) settleDueBestEffort(ctx context.Context, now time.Time) {
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.SettleDueRounds(settleCtx, now); err != nil {
		slog.Warn("lottery on-demand settlement failed", "error", err)
	}
}

func validateLotteryConfig(input LotteryConfigureInput, now time.Time) ([]LotteryPrize, error) {
	if input.StartsAt.IsZero() || input.EndsAt.IsZero() || !input.EndsAt.After(input.StartsAt) || !input.EndsAt.After(now) {
		return nil, ErrLotteryInvalid.WithMetadata(map[string]string{"field": "time_range"})
	}
	prizes := []LotteryPrize{
		{Tier: 1, Name: LotteryFirstPrizeName, Reward: input.FirstPrizeReward, Weight: input.FirstPrizeWeight, WinnerCount: input.FirstPrizeWinnerCount},
		{Tier: 2, Name: LotterySecondPrizeName, Reward: input.SecondPrizeReward, Weight: input.SecondPrizeWeight, WinnerCount: input.SecondPrizeWinnerCount},
		{Tier: 3, Name: LotteryThirdPrizeName, Reward: input.ThirdPrizeReward, Weight: input.ThirdPrizeWeight, WinnerCount: input.ThirdPrizeWinnerCount},
	}
	for _, prize := range prizes {
		if strings.TrimSpace(prize.Name) == "" || math.IsNaN(prize.Reward) || math.IsInf(prize.Reward, 0) || prize.Reward <= 0 || prize.Reward > lotteryRewardMax || prize.Weight <= 0 || prize.Weight > lotteryWeightMax || prize.WinnerCount <= 0 || prize.WinnerCount > lotteryWinnerCountMax {
			return nil, ErrLotteryInvalid.WithMetadata(map[string]string{"field": fmt.Sprintf("prize_%d", prize.Tier)})
		}
	}
	return prizes, nil
}

func secureLotteryPrizePicker(prizes []LotteryPrize) (LotteryPrize, error) {
	if len(prizes) == 0 {
		return LotteryPrize{}, fmt.Errorf("no lottery prizes configured")
	}
	total := int64(0)
	for _, prize := range prizes {
		if prize.Weight <= 0 {
			return LotteryPrize{}, fmt.Errorf("invalid lottery prize weight")
		}
		total += int64(prize.Weight)
	}
	roll, err := rand.Int(rand.Reader, big.NewInt(total))
	if err != nil {
		return LotteryPrize{}, err
	}
	return pickLotteryPrizeByRoll(prizes, roll.Int64())
}

func pickLotteryPrizeByRoll(prizes []LotteryPrize, roll int64) (LotteryPrize, error) {
	if roll < 0 {
		return LotteryPrize{}, fmt.Errorf("invalid lottery roll")
	}
	cursor := int64(0)
	for _, prize := range prizes {
		cursor += int64(prize.Weight)
		if roll < cursor {
			return prize, nil
		}
	}
	return LotteryPrize{}, fmt.Errorf("lottery roll outside configured weight")
}

func decorateLotteryRound(round *LotteryRound, now time.Time, enabled bool) {
	if round == nil {
		return
	}
	switch {
	case round.SettledAt != nil:
		round.Status = "settled"
	case now.Before(round.StartsAt):
		round.Status = "scheduled"
	case !now.Before(round.EndsAt):
		round.Status = "drawing"
	case !enabled:
		round.Status = "paused"
	default:
		round.Status = "open"
	}
}

func defaultLotteryPrizes() []LotteryPrize {
	return []LotteryPrize{
		{Tier: 1, Name: LotteryFirstPrizeName, Reward: LotteryFirstPrizeRewardDefault, Weight: LotteryFirstPrizeWeightDefault, WinnerCount: LotteryPrizeWinnerCountDefault},
		{Tier: 2, Name: LotterySecondPrizeName, Reward: LotterySecondPrizeRewardDefault, Weight: LotterySecondPrizeWeightDefault, WinnerCount: LotteryPrizeWinnerCountDefault},
		{Tier: 3, Name: LotteryThirdPrizeName, Reward: LotteryThirdPrizeRewardDefault, Weight: LotteryThirdPrizeWeightDefault, WinnerCount: LotteryPrizeWinnerCountDefault},
	}
}
