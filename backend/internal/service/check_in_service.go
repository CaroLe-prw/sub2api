package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	CheckInRewardMinDefault = 0.01
	CheckInRewardMaxDefault = 0.15
	CheckInRewardMaxAllowed = 100.0
	checkInRewardScale      = 100_000_000
)

var ErrCheckInDisabled = infraerrors.Forbidden("CHECK_IN_DISABLED", "daily check-in is disabled")

type CheckInRecord struct {
	Date      string    `json:"date"`
	Reward    float64   `json:"reward"`
	CreatedAt time.Time `json:"created_at"`
}

type CheckInRepositoryOverview struct {
	MonthRecords []CheckInRecord
	TodayRecord  *CheckInRecord
	AllDates     []string
	TotalDays    int
	TotalReward  float64
	Balance      float64
}

type CheckInRepository interface {
	Claim(ctx context.Context, userID int64, businessDate time.Time, reward float64) (record CheckInRecord, balance float64, created bool, err error)
	Overview(ctx context.Context, userID int64, monthStart, monthEnd, today time.Time) (CheckInRepositoryOverview, error)
}

type CheckInOverview struct {
	Today          string          `json:"today"`
	Timezone       string          `json:"timezone"`
	Year           int             `json:"year"`
	Month          int             `json:"month"`
	CheckedInToday bool            `json:"checked_in_today"`
	TodayReward    float64         `json:"today_reward"`
	CurrentStreak  int             `json:"current_streak"`
	TotalDays      int             `json:"total_days"`
	MonthDays      int             `json:"month_days"`
	MonthReward    float64         `json:"month_reward"`
	TotalReward    float64         `json:"total_reward"`
	Balance        float64         `json:"balance"`
	RewardMin      float64         `json:"reward_min"`
	RewardMax      float64         `json:"reward_max"`
	Records        []CheckInRecord `json:"records"`
}

type CheckInClaimResult struct {
	Created bool          `json:"created"`
	Record  CheckInRecord `json:"record"`
	Balance float64       `json:"balance"`
}

type checkInRewardGenerator func(minUnits, maxUnits int64) (int64, error)

type CheckInService struct {
	repo            CheckInRepository
	settings        *SettingService
	now             func() time.Time
	rewardGenerator checkInRewardGenerator
}

func NewCheckInService(repo CheckInRepository, settings *SettingService) *CheckInService {
	return &CheckInService{
		repo:            repo,
		settings:        settings,
		now:             timezone.Now,
		rewardGenerator: secureCheckInReward,
	}
}

func (s *CheckInService) Claim(ctx context.Context, userID int64) (*CheckInClaimResult, error) {
	if !s.settings.IsCheckInEnabled(ctx) {
		return nil, ErrCheckInDisabled
	}
	minReward, maxReward := s.settings.GetCheckInRewardRange(ctx)
	minUnits := int64(math.Round(minReward * checkInRewardScale))
	maxUnits := int64(math.Round(maxReward * checkInRewardScale))
	units, err := s.rewardGenerator(minUnits, maxUnits)
	if err != nil {
		return nil, fmt.Errorf("generate check-in reward: %w", err)
	}
	reward := float64(units) / checkInRewardScale
	now := s.now().In(timezone.Location())
	record, balance, created, err := s.repo.Claim(ctx, userID, timezone.StartOfDay(now), reward)
	if err != nil {
		return nil, err
	}
	return &CheckInClaimResult{Created: created, Record: record, Balance: balance}, nil
}

func (s *CheckInService) GetOverview(ctx context.Context, userID int64, year int, month time.Month) (*CheckInOverview, error) {
	if !s.settings.IsCheckInEnabled(ctx) {
		return nil, ErrCheckInDisabled
	}
	loc := timezone.Location()
	monthStart := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	monthEnd := monthStart.AddDate(0, 1, 0)
	todayDate := timezone.StartOfDay(s.now().In(loc))
	data, err := s.repo.Overview(ctx, userID, monthStart, monthEnd, todayDate)
	if err != nil {
		return nil, err
	}

	records := append([]CheckInRecord(nil), data.MonthRecords...)
	if records == nil {
		records = []CheckInRecord{}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Date < records[j].Date })
	today := todayDate.Format("2006-01-02")
	checkedToday := data.TodayRecord != nil
	todayReward := 0.0
	if data.TodayRecord != nil {
		todayReward = data.TodayRecord.Reward
	}
	monthReward := 0.0
	for _, record := range records {
		monthReward += record.Reward
	}
	minReward, maxReward := s.settings.GetCheckInRewardRange(ctx)
	return &CheckInOverview{
		Today:          today,
		Timezone:       timezone.Name(),
		Year:           year,
		Month:          int(month),
		CheckedInToday: checkedToday,
		TodayReward:    todayReward,
		CurrentStreak:  currentCheckInStreak(data.AllDates, today, loc),
		TotalDays:      data.TotalDays,
		MonthDays:      len(records),
		MonthReward:    monthReward,
		TotalReward:    data.TotalReward,
		Balance:        data.Balance,
		RewardMin:      minReward,
		RewardMax:      maxReward,
		Records:        records,
	}, nil
}

func secureCheckInReward(minUnits, maxUnits int64) (int64, error) {
	if minUnits <= 0 || maxUnits < minUnits {
		return 0, fmt.Errorf("invalid reward range")
	}
	span := big.NewInt(maxUnits - minUnits + 1)
	n, err := rand.Int(rand.Reader, span)
	if err != nil {
		return 0, err
	}
	return minUnits + n.Int64(), nil
}

func currentCheckInStreak(dates []string, today string, loc *time.Location) int {
	dateSet := make(map[string]struct{}, len(dates))
	for _, date := range dates {
		dateSet[date] = struct{}{}
	}
	anchor, err := time.ParseInLocation("2006-01-02", today, loc)
	if err != nil {
		return 0
	}
	if _, ok := dateSet[today]; !ok {
		anchor = anchor.AddDate(0, 0, -1)
	}
	streak := 0
	for {
		if _, ok := dateSet[anchor.Format("2006-01-02")]; !ok {
			break
		}
		streak++
		anchor = anchor.AddDate(0, 0, -1)
	}
	return streak
}
