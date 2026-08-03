//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// resetQuotaUserSubRepoStub 支持 GetByID、ResetUsageWindows，
// 其余方法继承 userSubRepoNoop（panic）。
type resetQuotaUserSubRepoStub struct {
	userSubRepoNoop

	sub *UserSubscription

	resetDailyCalled   bool
	resetWeeklyCalled  bool
	resetMonthlyCalled bool
	resetDailyErr      error
	resetWeeklyErr     error
	resetMonthlyErr    error
	windowStart        *time.Time
}

type resetQuotaBillingCacheStub struct {
	*billingCacheStub
	invalidateErr    error
	publishErr       error
	invalidateCalled bool
	publishedKeys    []string
}

func (s *resetQuotaBillingCacheStub) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	s.invalidateCalled = true
	return s.invalidateErr
}

func (s *resetQuotaBillingCacheStub) PublishSubscriptionCacheInvalidation(_ context.Context, cacheKey string) error {
	s.publishedKeys = append(s.publishedKeys, cacheKey)
	return s.publishErr
}

func (s *resetQuotaBillingCacheStub) SubscribeSubscriptionCacheInvalidation(context.Context, func(string)) error {
	return nil
}

func (r *resetQuotaUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *resetQuotaUserSubRepoStub) ResetUsageWindows(_ context.Context, _ int64, resetDaily, resetWeekly, resetMonthly bool, windowStart *time.Time) error {
	r.resetDailyCalled = resetDaily
	r.resetWeeklyCalled = resetWeekly
	r.resetMonthlyCalled = resetMonthly
	if windowStart != nil {
		copy := *windowStart
		r.windowStart = &copy
	} else {
		r.windowStart = nil
	}
	if resetDaily && r.resetDailyErr != nil {
		return r.resetDailyErr
	}
	if resetWeekly && r.resetWeeklyErr != nil {
		return r.resetWeeklyErr
	}
	if resetMonthly && r.resetMonthlyErr != nil {
		return r.resetMonthlyErr
	}
	if r.sub == nil {
		return nil
	}
	if resetDaily {
		r.sub.DailyUsageUSD = 0
		if windowStart != nil {
			copy := *windowStart
			r.sub.DailyWindowStart = &copy
		}
	}
	if resetWeekly {
		r.sub.WeeklyUsageUSD = 0
		if windowStart != nil {
			copy := *windowStart
			r.sub.WeeklyWindowStart = &copy
		}
	}
	if resetMonthly {
		r.sub.MonthlyUsageUSD = 0
		if windowStart != nil {
			copy := *windowStart
			r.sub.MonthlyWindowStart = &copy
		}
	}
	return nil
}

func (r *resetQuotaUserSubRepoStub) ResetDailyUsage(_ context.Context, _ int64, _ *time.Time, windowStart time.Time) error {
	r.resetDailyCalled = true
	if r.resetDailyErr == nil && r.sub != nil {
		r.sub.DailyUsageUSD = 0
		r.sub.DailyWindowStart = &windowStart
	}
	return r.resetDailyErr
}

func (r *resetQuotaUserSubRepoStub) ResetWeeklyUsage(_ context.Context, _ int64, _ *time.Time, _ time.Time) error {
	r.resetWeeklyCalled = true
	return r.resetWeeklyErr
}

func (r *resetQuotaUserSubRepoStub) ResetMonthlyUsage(_ context.Context, _ int64, _ *time.Time, _ time.Time) error {
	r.resetMonthlyCalled = true
	return r.resetMonthlyErr
}

func newResetQuotaSvc(stub *resetQuotaUserSubRepoStub) *SubscriptionService {
	return NewSubscriptionService(groupRepoNoop{}, stub, nil, nil, nil)
}

func resetQuotaTestSub(id int64) *UserSubscription {
	daily := time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)
	weekly := time.Date(2024, 12, 30, 9, 0, 0, 0, time.UTC)
	monthly := time.Date(2024, 12, 1, 10, 0, 0, 0, time.UTC)
	return &UserSubscription{
		ID:                 id,
		UserID:             10,
		GroupID:            20,
		DailyWindowStart:   &daily,
		WeeklyWindowStart:  &weekly,
		MonthlyWindowStart: &monthly,
		DailyUsageUSD:      10,
		WeeklyUsageUSD:     20,
		MonthlyUsageUSD:    30,
	}
}

func TestAdminResetQuota_ResetBoth(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: resetQuotaTestSub(1),
	}
	svc := newResetQuotaSvc(stub)
	resetAt := time.Date(2026, 7, 1, 10, 37, 42, 123, time.UTC)
	svc.now = func() time.Time { return resetAt }

	result, err := svc.AdminResetQuota(context.Background(), 1, true, true, false, QuotaWindowStartNaturalDay)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, stub.resetDailyCalled, "应调用 ResetDailyUsage")
	require.True(t, stub.resetWeeklyCalled, "应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
	require.NotNil(t, stub.windowStart)
	require.Equal(t, startOfDay(resetAt), *stub.windowStart)
	require.Equal(t, startOfDay(resetAt), *result.DailyWindowStart)
	require.Equal(t, startOfDay(resetAt), *result.WeeklyWindowStart)
}

func TestAdminResetQuota_ResetDailyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: resetQuotaTestSub(2),
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 2, true, false, false, QuotaWindowStartNaturalDay)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, stub.resetDailyCalled, "应调用 ResetDailyUsage")
	require.False(t, stub.resetWeeklyCalled, "不应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_ResetWeeklyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: resetQuotaTestSub(3),
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 3, false, true, false, QuotaWindowStartNaturalDay)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, stub.resetDailyCalled, "不应调用 ResetDailyUsage")
	require.True(t, stub.resetWeeklyCalled, "应调用 ResetWeeklyUsage")
	require.False(t, stub.resetMonthlyCalled, "不应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_BothFalseReturnsError(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: resetQuotaTestSub(7),
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 7, false, false, false, QuotaWindowStartNaturalDay)

	require.ErrorIs(t, err, ErrInvalidInput)
	require.False(t, stub.resetDailyCalled)
	require.False(t, stub.resetWeeklyCalled)
	require.False(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_SubscriptionNotFound(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{sub: nil}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 999, true, true, true, QuotaWindowStartNaturalDay)

	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	require.False(t, stub.resetDailyCalled)
	require.False(t, stub.resetWeeklyCalled)
	require.False(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_ResetDailyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:           resetQuotaTestSub(4),
		resetDailyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 4, true, true, false, QuotaWindowStartNaturalDay)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetDailyCalled)
	require.True(t, stub.resetWeeklyCalled, "原子重置应在一次调用中提交所选窗口")
}

func TestAdminResetQuota_ResetWeeklyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:            resetQuotaTestSub(5),
		resetWeeklyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 5, false, true, false, QuotaWindowStartNaturalDay)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetWeeklyCalled)
}

func TestAdminResetQuota_ResetMonthlyOnly(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{
		sub: resetQuotaTestSub(8),
	}
	svc := newResetQuotaSvc(stub)

	result, err := svc.AdminResetQuota(context.Background(), 8, false, false, true, QuotaWindowStartNaturalDay)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, stub.resetDailyCalled, "不应调用 ResetDailyUsage")
	require.False(t, stub.resetWeeklyCalled, "不应调用 ResetWeeklyUsage")
	require.True(t, stub.resetMonthlyCalled, "应调用 ResetMonthlyUsage")
}

func TestAdminResetQuota_BeforeStartsAtSameDayPreservesAutomaticBoundary(t *testing.T) {
	startsAt := time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 7, 1, 10, 37, 42, 123, time.UTC)
	stub := &resetQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:                 10,
			UserID:             10,
			GroupID:            20,
			StartsAt:           startsAt,
			ExpiresAt:          startsAt.Add(45 * 24 * time.Hour),
			DailyWindowStart:   &startsAt,
			WeeklyWindowStart:  &startsAt,
			MonthlyWindowStart: &startsAt,
		},
	}
	svc := newResetQuotaSvc(stub)
	svc.now = func() time.Time { return resetAt }

	result, err := svc.AdminResetQuota(context.Background(), 10, false, false, true, QuotaWindowStartCurrent)

	require.NoError(t, err)
	require.Equal(t, resetAt, *result.MonthlyWindowStart)
	boundary, ok := result.automaticWindowStartAt(result.MonthlyWindowStart, 30*24*time.Hour, resetAt.Add(30*24*time.Hour))
	require.True(t, ok)
	require.Equal(t, resetAt.Add(30*24*time.Hour), boundary)
}

func TestAdminResetQuota_ResetMonthlyUsageError(t *testing.T) {
	dbErr := errors.New("db error")
	stub := &resetQuotaUserSubRepoStub{
		sub:             resetQuotaTestSub(9),
		resetMonthlyErr: dbErr,
	}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 9, false, false, true, QuotaWindowStartNaturalDay)

	require.ErrorIs(t, err, dbErr)
	require.True(t, stub.resetMonthlyCalled)
}

func TestAdminResetQuota_ReturnsRefreshedSub(t *testing.T) {
	sub := resetQuotaTestSub(6)
	sub.DailyUsageUSD = 99.9
	stub := &resetQuotaUserSubRepoStub{sub: sub}

	svc := newResetQuotaSvc(stub)
	result, err := svc.AdminResetQuota(context.Background(), 6, true, false, false, QuotaWindowStartNaturalDay)

	require.NoError(t, err)
	// ResetUsageWindows stub 会将 sub.DailyUsageUSD 归零，
	// 服务应返回第二次 GetByID 的刷新值而非初始的 99.9
	require.Equal(t, float64(0), result.DailyUsageUSD, "返回的订阅应反映已归零的用量")
	require.True(t, stub.resetDailyCalled)
}

func TestAdminResetQuota_WindowStartModes(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	resetAt := time.Date(2025, 2, 3, 14, 15, 16, 123, location)

	tests := []struct {
		name          string
		mode          QuotaWindowStartMode
		expectedStart *time.Time
	}{
		{name: "current", mode: QuotaWindowStartCurrent, expectedStart: &resetAt},
		{name: "natural day", mode: QuotaWindowStartNaturalDay, expectedStart: timePointer(startOfDay(resetAt))},
		{name: "preserve", mode: QuotaWindowStartPreserve, expectedStart: nil},
		{name: "empty defaults to natural day", mode: "", expectedStart: timePointer(startOfDay(resetAt))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := resetQuotaTestSub(1)
			originalDaily := *sub.DailyWindowStart
			originalWeekly := *sub.WeeklyWindowStart
			originalMonthly := *sub.MonthlyWindowStart
			stub := &resetQuotaUserSubRepoStub{sub: sub}
			svc := newResetQuotaSvc(stub)

			result, err := svc.adminResetQuotaAt(context.Background(), sub.ID, true, true, true, tt.mode, resetAt)

			require.NoError(t, err)
			require.NotNil(t, result)
			if tt.expectedStart == nil {
				require.Nil(t, stub.windowStart)
				require.Equal(t, originalDaily, *result.DailyWindowStart)
				require.Equal(t, originalWeekly, *result.WeeklyWindowStart)
				require.Equal(t, originalMonthly, *result.MonthlyWindowStart)
				return
			}
			require.NotNil(t, stub.windowStart)
			require.Equal(t, *tt.expectedStart, *stub.windowStart)
			require.Equal(t, *tt.expectedStart, *result.DailyWindowStart)
			require.Equal(t, *tt.expectedStart, *result.WeeklyWindowStart)
			require.Equal(t, *tt.expectedStart, *result.MonthlyWindowStart)
		})
	}
}

func TestAdminResetQuota_InvalidWindowStartMode(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{sub: resetQuotaTestSub(1)}
	svc := newResetQuotaSvc(stub)

	_, err := svc.AdminResetQuota(context.Background(), 1, true, false, false, "invalid")

	require.ErrorIs(t, err, ErrInvalidQuotaWindowStartMode)
	require.False(t, stub.resetDailyCalled)
}

func TestAdminResetQuota_SelectedWindowMustBeActivated(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*UserSubscription)
		daily   bool
		weekly  bool
		monthly bool
	}{
		{
			name:    "daily",
			prepare: func(sub *UserSubscription) { sub.DailyWindowStart = nil },
			daily:   true,
		},
		{
			name:    "weekly",
			prepare: func(sub *UserSubscription) { sub.WeeklyWindowStart = nil },
			weekly:  true,
		},
		{
			name:    "monthly",
			prepare: func(sub *UserSubscription) { sub.MonthlyWindowStart = nil },
			monthly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := resetQuotaTestSub(1)
			tt.prepare(sub)
			stub := &resetQuotaUserSubRepoStub{sub: sub}
			svc := newResetQuotaSvc(stub)

			_, err := svc.AdminResetQuota(context.Background(), sub.ID, tt.daily, tt.weekly, tt.monthly, QuotaWindowStartPreserve)

			require.ErrorIs(t, err, ErrQuotaWindowNotActivated)
			require.False(t, stub.resetDailyCalled)
			require.False(t, stub.resetWeeklyCalled)
			require.False(t, stub.resetMonthlyCalled)
		})
	}
}

func TestAdminResetQuota_AllWindowsCanActivateNewSubscription(t *testing.T) {
	sub := resetQuotaTestSub(1)
	sub.DailyWindowStart = nil
	sub.WeeklyWindowStart = nil
	sub.MonthlyWindowStart = nil
	stub := &resetQuotaUserSubRepoStub{sub: sub}
	svc := newResetQuotaSvc(stub)
	resetAt := time.Date(2026, 7, 22, 13, 14, 15, 0, time.Local)

	result, err := svc.adminResetQuotaAt(context.Background(), sub.ID, true, true, true, "", resetAt)

	require.NoError(t, err)
	require.NotNil(t, result.DailyWindowStart)
	require.NotNil(t, result.WeeklyWindowStart)
	require.NotNil(t, result.MonthlyWindowStart)
	require.Equal(t, startOfDay(resetAt), *result.DailyWindowStart)
}

func TestAdminResetQuota_CacheFailuresAreBestEffortAndPublishInvalidation(t *testing.T) {
	stub := &resetQuotaUserSubRepoStub{sub: resetQuotaTestSub(1)}
	cache := &resetQuotaBillingCacheStub{
		billingCacheStub: newBillingCacheStub(1),
		invalidateErr:    errors.New("invalidate failed"),
		publishErr:       errors.New("publish failed"),
	}
	svc := NewSubscriptionService(
		groupRepoNoop{},
		stub,
		&BillingCacheService{cache: cache},
		nil,
		nil,
	)

	result, err := svc.AdminResetQuota(context.Background(), 1, true, false, false, QuotaWindowStartPreserve)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, cache.invalidateCalled)
	require.Equal(t, []string{subCacheKey(10, 20)}, cache.publishedKeys)
}

func timePointer(value time.Time) *time.Time {
	return &value
}
