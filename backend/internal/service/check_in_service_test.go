package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type checkInSettingsRepoStub struct {
	values map[string]string
	getErr error
}

func (s *checkInSettingsRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (s *checkInSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (s *checkInSettingsRepoStub) Set(context.Context, string, string) error { return nil }
func (s *checkInSettingsRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		result[key] = s.values[key]
	}
	return result, nil
}
func (s *checkInSettingsRepoStub) SetMultiple(context.Context, map[string]string) error { return nil }
func (s *checkInSettingsRepoStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *checkInSettingsRepoStub) Delete(context.Context, string) error { return nil }

type checkInRepoStub struct {
	overview        CheckInRepositoryOverview
	claimRecord     CheckInRecord
	claimBalance    float64
	claimCreated    bool
	claimedReward   float64
	claimedDate     time.Time
	claimCalls      int
	minRecharge     float64
	paidRecharge    float64
	paidRechargeErr error
	claimErr        error
}

func (s *checkInRepoStub) Claim(_ context.Context, _ int64, businessDate time.Time, reward, minRecharge float64) (CheckInRecord, float64, bool, error) {
	s.minRecharge = minRecharge
	s.claimCalls++
	s.claimedDate = businessDate
	s.claimedReward = reward
	return s.claimRecord, s.claimBalance, s.claimCreated, s.claimErr
}

func (s *checkInRepoStub) Overview(context.Context, int64, time.Time, time.Time, time.Time) (CheckInRepositoryOverview, error) {
	return s.overview, nil
}

func (s *checkInRepoStub) AdminListRecords(context.Context, int, int) ([]CheckInAdminRecord, int64, error) {
	return nil, 0, nil
}

func newCheckInTestService(repo CheckInRepository, values map[string]string, now time.Time) *CheckInService {
	settings := NewSettingService(&checkInSettingsRepoStub{values: values}, &config.Config{})
	service := NewCheckInService(repo, settings)
	service.now = func() time.Time { return now }
	return service
}

func TestCheckInOverviewCalculatesCurrentStreakAndMonthlyStats(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	todayRecord := CheckInRecord{Date: "2026-08-15", Reward: 0.05}
	repo := &checkInRepoStub{overview: CheckInRepositoryOverview{
		MonthRecords: []CheckInRecord{
			{Date: "2026-08-13", Reward: 0.03},
			{Date: "2026-08-14", Reward: 0.04},
			{Date: "2026-08-15", Reward: 0.05},
		},
		TodayRecord: &todayRecord,
		AllDates:    []string{"2026-08-15", "2026-08-14", "2026-08-13", "2026-08-10"},
		TotalDays:   4,
		TotalReward: 0.2,
		Balance:     12.5,
	}}
	service := newCheckInTestService(repo, map[string]string{
		SettingKeyCheckInEnabled:   "true",
		SettingKeyCheckInRewardMin: "0.01000000",
		SettingKeyCheckInRewardMax: "0.15000000",
	}, now)

	overview, err := service.GetOverview(context.Background(), 7, 2026, time.August)
	require.NoError(t, err)
	require.True(t, overview.CheckedInToday)
	require.Equal(t, 3, overview.CurrentStreak)
	require.Equal(t, 3, overview.MonthDays)
	require.InDelta(t, 0.12, overview.MonthReward, 1e-9)
	require.Equal(t, 4, overview.TotalDays)
	require.Equal(t, "2026-08-15", overview.Today)
}

func TestCheckInOverviewKeepsTodayStatusWhenViewingAnotherMonth(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	todayRecord := CheckInRecord{Date: "2026-08-15", Reward: 0.05}
	repo := &checkInRepoStub{overview: CheckInRepositoryOverview{
		MonthRecords: []CheckInRecord{{Date: "2026-07-31", Reward: 0.03}},
		TodayRecord:  &todayRecord,
		AllDates:     []string{"2026-08-15", "2026-08-14"},
	}}
	service := newCheckInTestService(repo, map[string]string{SettingKeyCheckInEnabled: "true"}, now)

	overview, err := service.GetOverview(context.Background(), 7, 2026, time.July)
	require.NoError(t, err)
	require.True(t, overview.CheckedInToday)
	require.InDelta(t, 0.05, overview.TodayReward, 1e-9)
	require.Equal(t, 1, overview.MonthDays)
	require.InDelta(t, 0.03, overview.MonthReward, 1e-9)
}

func TestCheckInOverviewKeepsStreakWhenTodayNotYetClaimed(t *testing.T) {
	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repo := &checkInRepoStub{overview: CheckInRepositoryOverview{
		MonthRecords: []CheckInRecord{{Date: "2026-08-13"}, {Date: "2026-08-14"}},
		AllDates:     []string{"2026-08-14", "2026-08-13"},
	}}
	service := newCheckInTestService(repo, map[string]string{SettingKeyCheckInEnabled: "true"}, now)

	overview, err := service.GetOverview(context.Background(), 7, 2026, time.August)
	require.NoError(t, err)
	require.False(t, overview.CheckedInToday)
	require.Equal(t, 2, overview.CurrentStreak)
}

func TestCheckInClaimUsesConfiguredRangeAndBusinessDate(t *testing.T) {
	now := time.Date(2026, time.August, 15, 23, 59, 0, 0, time.UTC)
	repo := &checkInRepoStub{
		claimRecord:  CheckInRecord{Date: "2026-08-15", Reward: 0.02},
		claimBalance: 2.5,
		claimCreated: true,
	}
	service := newCheckInTestService(repo, map[string]string{
		SettingKeyCheckInEnabled:   "true",
		SettingKeyCheckInRewardMin: "0.02000000",
		SettingKeyCheckInRewardMax: "0.03000000",
	}, now)
	service.rewardGenerator = func(minUnits, maxUnits int64) (int64, error) {
		require.Equal(t, int64(2_000_000), minUnits)
		require.Equal(t, int64(3_000_000), maxUnits)
		return minUnits, nil
	}

	result, err := service.Claim(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.InDelta(t, 0.02, repo.claimedReward, 1e-9)
	require.Equal(t, 0, repo.claimedDate.Hour())
	require.Equal(t, 1, repo.claimCalls)
}

func TestCheckInClaimStopsWhenFeatureDisabled(t *testing.T) {
	repo := &checkInRepoStub{}
	service := newCheckInTestService(repo, map[string]string{SettingKeyCheckInEnabled: "false"}, time.Now())

	_, err := service.Claim(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInDisabled)
	require.Zero(t, repo.claimCalls)
}

func TestNormalizeCheckInRewardRangeClampsAndOrders(t *testing.T) {
	minReward, maxReward := normalizeCheckInRewardRange(5, 1)
	require.Equal(t, 5.0, minReward)
	require.Equal(t, 5.0, maxReward)

	minReward, maxReward = normalizeCheckInRewardRange(-1, 200)
	require.Equal(t, CheckInRewardMinDefault, minReward)
	require.Equal(t, CheckInRewardMaxAllowed, maxReward)
}

func (s *checkInRepoStub) PaidRechargeTotal(context.Context, int64) (float64, error) {
	return s.paidRecharge, s.paidRechargeErr
}

func TestCheckInRechargeEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, setting string
		paid          float64
		eligible      bool
	}{
		{"default unrestricted", "", 0, true},
		{"disabled requirement", "0", 0, true},
		{"never recharged", "10", 0, false},
		{"below threshold", "10", 9.99, false},
		{"exact threshold", "10", 10, true},
		{"above threshold", "10", 15, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &checkInRepoStub{paidRecharge: tc.paid, overview: CheckInRepositoryOverview{Balance: 1000}}
			svc := newCheckInTestService(repo, map[string]string{SettingKeyCheckInMinRecharge: tc.setting}, time.Now())
			overview, err := svc.GetOverview(context.Background(), 7, 2026, time.September)
			require.NoError(t, err)
			require.Equal(t, tc.eligible, overview.Eligible, "gift balance must not establish recharge eligibility")
			require.InDelta(t, math.Max(0, overview.MinRecharge-tc.paid), overview.RechargeRemaining, 1e-8)
		})
	}
}

func TestCheckInClaimPassesRequirementToTransaction(t *testing.T) {
	repo := &checkInRepoStub{claimErr: ErrCheckInRechargeRequired}
	svc := newCheckInTestService(repo, map[string]string{SettingKeyCheckInMinRecharge: "10"}, time.Now())
	result, err := svc.Claim(context.Background(), 7)
	require.ErrorIs(t, err, ErrCheckInRechargeRequired)
	require.Nil(t, result)
	require.Equal(t, 10.0, repo.minRecharge)
}

func TestCheckInRequirementLookupFailureDoesNotAllowClaim(t *testing.T) {
	repo := &checkInRepoStub{}
	svc := newCheckInTestService(repo, map[string]string{SettingKeyCheckInMinRecharge: "invalid"}, time.Now())
	_, err := svc.Claim(context.Background(), 7)
	require.Error(t, err)
	require.Zero(t, repo.claimCalls)
}

func TestCheckInOverviewRejectsFailedRechargeLookup(t *testing.T) {
	repo := &checkInRepoStub{paidRechargeErr: errors.New("database unavailable")}
	svc := newCheckInTestService(repo, map[string]string{SettingKeyCheckInMinRecharge: "10"}, time.Now())
	_, err := svc.GetOverview(context.Background(), 7, 2026, time.September)
	require.ErrorContains(t, err, "database unavailable")
	// Clearing the threshold removes the dependency on payment history.
	svc = newCheckInTestService(repo, map[string]string{SettingKeyCheckInMinRecharge: "0"}, time.Now())
	overview, err := svc.GetOverview(context.Background(), 7, 2026, time.September)
	require.NoError(t, err)
	require.True(t, overview.Eligible)
}

func TestCheckInMinRechargeSettingsRoundTrip(t *testing.T) {
	// Parsing system settings publishes process-wide Grok mapping defaults.
	// Restore them so this settings test cannot alter later routing tests.
	originalMapping := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(originalMapping) })
	svc := NewSettingService(&checkInSettingsRepoStub{values: map[string]string{}}, &config.Config{})
	require.Zero(t, svc.parseSettings(map[string]string{}).CheckInMinRecharge)
	for _, amount := range []float64{10.25, 0} {
		settings := svc.parseSettings(map[string]string{})
		settings.CheckInMinRecharge = amount
		updates, err := svc.buildSystemSettingsUpdates(context.Background(), settings)
		require.NoError(t, err)
		require.Equal(t, amount, svc.parseSettings(updates).CheckInMinRecharge)
	}
	for _, raw := range []string{"-1", "NaN", "+Inf", "1000000000001", "invalid"} {
		_, err := parseCheckInMinRecharge(raw)
		require.Error(t, err)
	}
}

func TestCheckInSettingsStorageFailureDoesNotAllowClaim(t *testing.T) {
	repo := &checkInRepoStub{}
	settings := NewSettingService(&checkInSettingsRepoStub{getErr: errors.New("settings unavailable")}, &config.Config{})
	svc := NewCheckInService(repo, settings)
	_, err := svc.Claim(context.Background(), 7)
	require.ErrorContains(t, err, "settings unavailable")
	require.Zero(t, repo.claimCalls)
}
