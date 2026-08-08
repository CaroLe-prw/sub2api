//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type quotaResetSettingRepoStub struct {
	SettingRepository
	mu       sync.Mutex
	settings map[string]*Setting
}

func newQuotaResetSettingRepoStub() *quotaResetSettingRepoStub {
	return &quotaResetSettingRepoStub{settings: make(map[string]*Setting)}
}

func (r *quotaResetSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	setting := r.settings[key]
	if setting == nil {
		return nil, ErrSettingNotFound
	}
	copy := *setting
	return &copy, nil
}

func (r *quotaResetSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings[key] = &Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return nil
}

type quotaResetGroupRepoStub struct {
	groupRepoNoop
	groups map[int64]*Group
}

func (r *quotaResetGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	copy := *group
	return &copy, nil
}

type quotaResetUserSubRepoStub struct {
	userSubRepoNoop
	mu          sync.Mutex
	subs        map[int64]*UserSubscription
	resetErr    map[int64]error
	resetStarts map[int64]*time.Time
}

func (r *quotaResetUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub := r.subs[id]
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	copy := *sub
	return &copy, nil
}

func (r *quotaResetUserSubRepoStub) List(
	_ context.Context,
	params pagination.PaginationParams,
	_, groupID *int64,
	_, _, _, _ string,
) ([]UserSubscription, *pagination.PaginationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]UserSubscription, 0)
	for _, sub := range r.subs {
		if groupID != nil && sub.GroupID != *groupID {
			continue
		}
		copy := *sub
		result = append(result, copy)
	}
	return result, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Pages: 1, Total: int64(len(result))}, nil
}

func (r *quotaResetUserSubRepoStub) ResetUsageWindows(_ context.Context, id int64, daily, weekly, monthly bool, dailyStart, periodicStart *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.resetErr[id]; err != nil {
		return err
	}
	sub := r.subs[id]
	if sub == nil {
		return ErrSubscriptionNotFound
	}
	if dailyStart != nil {
		copy := *dailyStart
		r.resetStarts[id] = &copy
	} else if periodicStart != nil {
		copy := *periodicStart
		r.resetStarts[id] = &copy
	} else {
		r.resetStarts[id] = nil
	}
	if daily {
		sub.DailyUsageUSD = 0
		if dailyStart != nil {
			copy := *dailyStart
			sub.DailyWindowStart = &copy
		}
	}
	if weekly {
		sub.WeeklyUsageUSD = 0
		if periodicStart != nil {
			copy := *periodicStart
			sub.WeeklyWindowStart = &copy
		}
	}
	if monthly {
		sub.MonthlyUsageUSD = 0
		if periodicStart != nil {
			copy := *periodicStart
			sub.MonthlyWindowStart = &copy
		}
	}
	return nil
}

func newQuotaResetServiceForTest(subs ...*UserSubscription) (*SubscriptionQuotaResetService, *quotaResetUserSubRepoStub) {
	repo := &quotaResetUserSubRepoStub{
		subs:        make(map[int64]*UserSubscription),
		resetErr:    make(map[int64]error),
		resetStarts: make(map[int64]*time.Time),
	}
	for _, sub := range subs {
		repo.subs[sub.ID] = sub
	}
	groupRepo := &quotaResetGroupRepoStub{groups: map[int64]*Group{
		7: {ID: 7, SubscriptionType: SubscriptionTypeSubscription},
		8: {ID: 8, SubscriptionType: SubscriptionTypeSubscription},
	}}
	settings := newQuotaResetSettingRepoStub()
	subscriptionService := NewSubscriptionService(groupRepo, repo, nil, nil, nil)
	return NewSubscriptionQuotaResetService(subscriptionService, repo, groupRepo, settings), repo
}

func quotaResetBatchSub(id int64, daily *time.Time) *UserSubscription {
	weekly := time.Date(2026, 7, 1, 8, 0, 0, 0, time.Local)
	monthly := time.Date(2026, 6, 1, 9, 0, 0, 0, time.Local)
	return &UserSubscription{
		ID:                 id,
		UserID:             id + 100,
		GroupID:            7,
		Status:             SubscriptionStatusActive,
		ExpiresAt:          time.Now().Add(30 * 24 * time.Hour),
		DailyWindowStart:   daily,
		WeeklyWindowStart:  &weekly,
		MonthlyWindowStart: &monthly,
		DailyUsageUSD:      12,
		WeeklyUsageUSD:     23,
		MonthlyUsageUSD:    34,
	}
}

func TestSubscriptionQuotaResetRunNow_PreservesSelectedWindowsAndSkipsUnactivated(t *testing.T) {
	daily := time.Date(2026, 7, 20, 11, 12, 13, 0, time.Local)
	ready := quotaResetBatchSub(1, &daily)
	unactivated := quotaResetBatchSub(2, nil)
	svc, repo := newQuotaResetServiceForTest(ready, unactivated)

	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		IntervalHours:   5,
		GroupIDs:        []int64{7, 7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartPreserve,
	})
	require.NoError(t, err)

	settings, err := svc.RunNow(context.Background())

	require.NoError(t, err)
	require.Equal(t, "success", settings.State.Status)
	require.Equal(t, 2, settings.State.MatchedCount)
	require.Equal(t, 1, settings.State.ResetCount)
	require.Equal(t, 1, settings.State.SkippedCount)
	require.Zero(t, settings.State.FailedCount)
	require.Equal(t, []int64{7}, settings.Config.GroupIDs)
	require.Equal(t, 0.0, repo.subs[1].DailyUsageUSD)
	require.Equal(t, daily, *repo.subs[1].DailyWindowStart)
	require.Equal(t, 23.0, repo.subs[1].WeeklyUsageUSD)
	_, recorded := repo.resetStarts[2]
	require.False(t, recorded)
}

func TestSubscriptionQuotaResetRunNow_UsesOneCurrentTimestampAndContinuesAfterFailure(t *testing.T) {
	daily := time.Now().Add(-time.Hour)
	first := quotaResetBatchSub(1, &daily)
	failed := quotaResetBatchSub(2, &daily)
	last := quotaResetBatchSub(3, &daily)
	svc, repo := newQuotaResetServiceForTest(first, failed, last)
	repo.resetErr[2] = errors.New("write failed")

	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		IntervalHours:   5,
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
	})
	require.NoError(t, err)

	settings, err := svc.RunNow(context.Background())

	require.NoError(t, err)
	require.Equal(t, "partial_failed", settings.State.Status)
	require.Equal(t, 2, settings.State.ResetCount)
	require.Equal(t, 1, settings.State.FailedCount)
	require.Contains(t, settings.State.LastError, "subscription 2")
	require.NotNil(t, repo.resetStarts[1])
	require.NotNil(t, repo.resetStarts[3])
	require.Equal(t, *repo.resetStarts[1], *repo.resetStarts[3])
}

func TestSubscriptionQuotaResetConfig_RejectsNonSubscriptionGroup(t *testing.T) {
	groupRepo := &quotaResetGroupRepoStub{groups: map[int64]*Group{
		9: {ID: 9, SubscriptionType: SubscriptionTypeStandard},
	}}
	repo := &quotaResetUserSubRepoStub{subs: map[int64]*UserSubscription{}, resetErr: map[int64]error{}, resetStarts: map[int64]*time.Time{}}
	svc := NewSubscriptionQuotaResetService(NewSubscriptionService(groupRepo, repo, nil, nil, nil), repo, groupRepo, newQuotaResetSettingRepoStub())

	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		Enabled:         true,
		IntervalHours:   5,
		GroupIDs:        []int64{9},
		Daily:           true,
		WindowStartMode: QuotaWindowStartNaturalDay,
	})

	require.ErrorIs(t, err, ErrGroupNotSubscriptionType)
}

func TestSubscriptionQuotaResetRunNow_SkipsWhenAnotherInstanceHoldsLock(t *testing.T) {
	daily := time.Now().Add(-time.Hour)
	svc, repo := newQuotaResetServiceForTest(quotaResetBatchSub(1, &daily))
	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		IntervalHours:   5,
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
	})
	require.NoError(t, err)

	lock := &fakeLeaderLockCache{}
	acquired, err := lock.TryAcquireLeaderLock(context.Background(), subscriptionQuotaResetLeaderLockKey, "peer", time.Hour)
	require.NoError(t, err)
	require.True(t, acquired)
	svc.SetLeaderLock(lock, nil)

	_, err = svc.RunNow(context.Background())

	require.ErrorIs(t, err, ErrSubscriptionQuotaResetInProgress)
	require.Empty(t, repo.resetStarts)
}

func TestSubscriptionQuotaResetNextRun_UsesLatestRunTimestamp(t *testing.T) {
	updatedAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	finishedAt := updatedAt.Add(2 * time.Hour)
	manualFinishedAt := finishedAt.Add(3 * time.Hour)
	config := DefaultSubscriptionQuotaResetConfig()
	config.Enabled = true
	state := SubscriptionQuotaResetState{
		LastFinishedAt:          &manualFinishedAt,
		LastScheduledFinishedAt: &finishedAt,
	}

	next := subscriptionQuotaResetNextRun(config, state, updatedAt)

	require.NotNil(t, next)
	require.Equal(t, finishedAt.Add(5*time.Hour), *next)
	config.Enabled = false
	require.Nil(t, subscriptionQuotaResetNextRun(config, state, updatedAt))
}

func TestSubscriptionQuotaResetRunNowWithInput_DoesNotChangeSchedule(t *testing.T) {
	daily := time.Date(2026, 7, 20, 11, 12, 13, 0, time.Local)
	scheduledSub := quotaResetBatchSub(1, &daily)
	manualSub := quotaResetBatchSub(2, &daily)
	manualSub.GroupID = 8
	svc, repo := newQuotaResetServiceForTest(scheduledSub, manualSub)
	announcementRepo := &announcementRepoStub{}
	svc.SetAnnouncementService(NewAnnouncementService(announcementRepo, nil, nil, nil))
	scheduledConfig := SubscriptionQuotaResetConfig{
		Enabled:         true,
		IntervalHours:   5,
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
	}
	_, err := svc.UpdateConfig(context.Background(), scheduledConfig)
	require.NoError(t, err)
	before, err := svc.GetSettings(context.Background())
	require.NoError(t, err)
	require.NotNil(t, before.State.NextRunAt)

	settings, err := svc.RunNowWithInput(context.Background(), SubscriptionQuotaResetRunInput{
		GroupIDs:        []int64{8},
		Weekly:          true,
		WindowStartMode: QuotaWindowStartPreserve,
		RestartSchedule: false,
	})

	require.NoError(t, err)
	require.Equal(t, scheduledConfig, settings.Config)
	require.Equal(t, 12.0, repo.subs[1].DailyUsageUSD)
	require.Equal(t, 12.0, repo.subs[2].DailyUsageUSD)
	require.Zero(t, repo.subs[2].WeeklyUsageUSD)
	require.Equal(t, "每周配额已手动重置", announcementRepo.item.Title)
	require.True(t, announcementRepo.item.Targeting.Matches(0, map[int64]struct{}{8: {}}))
	require.False(t, announcementRepo.item.Targeting.Matches(0, map[int64]struct{}{7: {}}))
	require.Equal(t, subscriptionQuotaResetManualTTL, announcementRepo.item.EndsAt.Sub(*announcementRepo.item.StartsAt))
	after, err := svc.GetSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, before.State.NextRunAt, after.State.NextRunAt)
	require.Equal(t, scheduledConfig, after.Config)
}

func TestSubscriptionQuotaResetRunNowWithInput_RestartsScheduleWhenRequested(t *testing.T) {
	daily := time.Date(2026, 7, 20, 11, 12, 13, 0, time.Local)
	svc, _ := newQuotaResetServiceForTest(quotaResetBatchSub(1, &daily))
	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		Enabled:         true,
		IntervalHours:   5,
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
	})
	require.NoError(t, err)

	settings, err := svc.RunNowWithInput(context.Background(), SubscriptionQuotaResetRunInput{
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
		RestartSchedule: true,
	})

	require.NoError(t, err)
	require.Equal(t, 1, settings.State.ResetCount)
	require.NotNil(t, settings.State.LastFinishedAt)
	require.NotNil(t, settings.State.LastScheduledFinishedAt)
	require.Equal(t, *settings.State.LastFinishedAt, *settings.State.LastScheduledFinishedAt)
	require.NotNil(t, settings.State.NextRunAt)
	require.Equal(t, settings.State.LastScheduledFinishedAt.Add(5*time.Hour), *settings.State.NextRunAt)
}

func TestSubscriptionQuotaResetRunNowWithInput_DoesNotRestartScheduleWithoutReset(t *testing.T) {
	svc, _ := newQuotaResetServiceForTest()
	_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
		Enabled:         true,
		IntervalHours:   5,
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
	})
	require.NoError(t, err)
	before, err := svc.GetSettings(context.Background())
	require.NoError(t, err)

	settings, err := svc.RunNowWithInput(context.Background(), SubscriptionQuotaResetRunInput{
		GroupIDs:        []int64{7},
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
		RestartSchedule: true,
	})

	require.NoError(t, err)
	require.Zero(t, settings.State.ResetCount)
	require.Equal(t, before.State.NextRunAt, settings.State.NextRunAt)
}

func TestSubscriptionQuotaResetAnnouncements(t *testing.T) {
	daily := time.Date(2026, 7, 20, 11, 12, 13, 0, time.Local)

	t.Run("manual run publishes a scoped popup without groups or counts", func(t *testing.T) {
		svc, _ := newQuotaResetServiceForTest(quotaResetBatchSub(1, &daily))
		announcementRepo := &announcementRepoStub{}
		svc.SetAnnouncementService(NewAnnouncementService(announcementRepo, nil, nil, nil))
		_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
			IntervalHours:   5,
			GroupIDs:        []int64{7},
			Daily:           true,
			Weekly:          true,
			WindowStartMode: QuotaWindowStartCurrent,
		})
		require.NoError(t, err)

		settings, err := svc.RunNow(context.Background())

		require.NoError(t, err)
		require.Equal(t, "success", settings.State.Status)
		require.NotNil(t, announcementRepo.item)
		require.Equal(t, "每日配额、每周配额已手动重置", announcementRepo.item.Title)
		require.Equal(t, AnnouncementStatusActive, announcementRepo.item.Status)
		require.Equal(t, AnnouncementNotifyModePopup, announcementRepo.item.NotifyMode)
		require.Contains(t, announcementRepo.item.Content, "本次重置范围：每日配额、每周配额")
		require.Contains(t, announcementRepo.item.Content, "处理结果：重置成功")
		require.NotContains(t, announcementRepo.item.Content, "生效套餐组")
		require.NotContains(t, announcementRepo.item.Content, "个订阅")
		require.True(t, announcementRepo.item.Targeting.Matches(0, map[int64]struct{}{7: {}}))
		require.False(t, announcementRepo.item.Targeting.Matches(0, map[int64]struct{}{8: {}}))
		require.Equal(t, subscriptionQuotaResetManualTTL, announcementRepo.item.EndsAt.Sub(*announcementRepo.item.StartsAt))
	})

	t.Run("scheduled notice keeps the old interval wording and expiry", func(t *testing.T) {
		svc, _ := newQuotaResetServiceForTest(quotaResetBatchSub(1, &daily))
		announcementRepo := &announcementRepoStub{}
		svc.SetAnnouncementService(NewAnnouncementService(announcementRepo, nil, nil, nil))
		finishedAt := time.Date(2026, 7, 22, 13, 14, 15, 0, time.Local)
		config := SubscriptionQuotaResetConfig{
			Enabled:         true,
			IntervalHours:   5,
			GroupIDs:        []int64{7},
			Daily:           true,
			WindowStartMode: QuotaWindowStartCurrent,
		}

		err := svc.publishAnnouncement(context.Background(), config, SubscriptionQuotaResetState{Status: "partial_failed"}, finishedAt, false)

		require.NoError(t, err)
		require.Equal(t, "5小时配额重置部分失败", announcementRepo.item.Title)
		require.Contains(t, announcementRepo.item.Content, "本次重置范围：5小时配额（日限额）")
		require.Contains(t, announcementRepo.item.Content, "处理结果：部分失败")
		require.Equal(t, 5*time.Hour, announcementRepo.item.EndsAt.Sub(*announcementRepo.item.StartsAt))
	})

	t.Run("announcement failure is visible without failing an applied reset", func(t *testing.T) {
		svc, _ := newQuotaResetServiceForTest(quotaResetBatchSub(1, &daily))
		svc.SetAnnouncementService(NewAnnouncementService(&announcementRepoStub{createErr: errors.New("insert failed")}, nil, nil, nil))
		_, err := svc.UpdateConfig(context.Background(), SubscriptionQuotaResetConfig{
			IntervalHours:   5,
			GroupIDs:        []int64{7},
			Daily:           true,
			WindowStartMode: QuotaWindowStartCurrent,
		})
		require.NoError(t, err)

		settings, err := svc.RunNow(context.Background())

		require.NoError(t, err)
		require.Equal(t, "partial_failed", settings.State.Status)
		require.NotNil(t, settings.State.LastSuccessAt)
		require.Contains(t, settings.State.LastError, "publish quota reset announcement")
		require.Contains(t, settings.State.LastError, "insert failed")
	})
}
