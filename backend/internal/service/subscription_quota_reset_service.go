package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const (
	SettingKeySubscriptionQuotaResetConfig = "subscription_quota_reset_config"
	SettingKeySubscriptionQuotaResetState  = "subscription_quota_reset_state"

	subscriptionQuotaResetLeaderLockKey = "subscription:quota-reset:leader"
	subscriptionQuotaResetLeaderLockTTL = 6 * time.Hour
	subscriptionQuotaResetCheckInterval = time.Minute
	subscriptionQuotaResetPageSize      = 500
	subscriptionQuotaResetNoticeTimeout = 5 * time.Second
	subscriptionQuotaResetManualTTL     = 24 * time.Hour
)

var (
	ErrSubscriptionQuotaResetInvalidConfig = infraerrors.BadRequest("SUBSCRIPTION_QUOTA_RESET_INVALID_CONFIG", "invalid subscription quota reset configuration")
	ErrSubscriptionQuotaResetInProgress    = infraerrors.Conflict("SUBSCRIPTION_QUOTA_RESET_IN_PROGRESS", "subscription quota reset is already running")
)

type SubscriptionQuotaResetConfig struct {
	Enabled         bool                 `json:"enabled"`
	IntervalHours   int                  `json:"interval_hours"`
	GroupIDs        []int64              `json:"group_ids"`
	Daily           bool                 `json:"daily"`
	Weekly          bool                 `json:"weekly"`
	Monthly         bool                 `json:"monthly"`
	WindowStartMode QuotaWindowStartMode `json:"window_start_mode"`
}

type SubscriptionQuotaResetState struct {
	Status                  string     `json:"status"`
	LastStartedAt           *time.Time `json:"last_started_at"`
	LastFinishedAt          *time.Time `json:"last_finished_at"`
	LastScheduledFinishedAt *time.Time `json:"last_scheduled_finished_at"`
	LastSuccessAt           *time.Time `json:"last_success_at"`
	NextRunAt               *time.Time `json:"next_run_at"`
	MatchedCount            int        `json:"matched_count"`
	ResetCount              int        `json:"reset_count"`
	SkippedCount            int        `json:"skipped_count"`
	FailedCount             int        `json:"failed_count"`
	LastError               string     `json:"last_error"`
}

type SubscriptionQuotaResetRunInput struct {
	GroupIDs        []int64              `json:"group_ids"`
	Daily           bool                 `json:"daily"`
	Weekly          bool                 `json:"weekly"`
	Monthly         bool                 `json:"monthly"`
	WindowStartMode QuotaWindowStartMode `json:"window_start_mode"`
	RestartSchedule bool                 `json:"restart_schedule"`
}

type SubscriptionQuotaResetSettings struct {
	Config SubscriptionQuotaResetConfig `json:"config"`
	State  SubscriptionQuotaResetState  `json:"state"`
}

type SubscriptionQuotaResetService struct {
	subscriptionService *SubscriptionService
	userSubRepo         UserSubscriptionRepository
	groupRepo           GroupRepository
	settingRepo         SettingRepository
	announcementService *AnnouncementService

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string

	runMu     sync.Mutex
	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func (s *SubscriptionQuotaResetService) SetAnnouncementService(announcementService *AnnouncementService) {
	if s != nil {
		s.announcementService = announcementService
	}
}

func NewSubscriptionQuotaResetService(
	subscriptionService *SubscriptionService,
	userSubRepo UserSubscriptionRepository,
	groupRepo GroupRepository,
	settingRepo SettingRepository,
) *SubscriptionQuotaResetService {
	return &SubscriptionQuotaResetService{
		subscriptionService: subscriptionService,
		userSubRepo:         userSubRepo,
		groupRepo:           groupRepo,
		settingRepo:         settingRepo,
		instanceID:          uuid.NewString(),
		stopCh:              make(chan struct{}),
	}
}

func (s *SubscriptionQuotaResetService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *SubscriptionQuotaResetService) Start() {
	if s == nil || s.settingRepo == nil || s.userSubRepo == nil || s.subscriptionService == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(subscriptionQuotaResetCheckInterval)
			defer ticker.Stop()
			s.runIfDue()
			for {
				select {
				case <-ticker.C:
					s.runIfDue()
				case <-s.stopCh:
					return
				}
			}
		}()
	})
}

func (s *SubscriptionQuotaResetService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func DefaultSubscriptionQuotaResetConfig() SubscriptionQuotaResetConfig {
	return SubscriptionQuotaResetConfig{
		IntervalHours:   5,
		Daily:           true,
		WindowStartMode: QuotaWindowStartCurrent,
		GroupIDs:        []int64{},
	}
}

func (s *SubscriptionQuotaResetService) GetSettings(ctx context.Context) (*SubscriptionQuotaResetSettings, error) {
	config, updatedAt, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.loadState(ctx)
	if err != nil {
		return nil, err
	}
	state.NextRunAt = subscriptionQuotaResetNextRun(config, state, updatedAt)
	return &SubscriptionQuotaResetSettings{Config: config, State: state}, nil
}

func (s *SubscriptionQuotaResetService) UpdateConfig(ctx context.Context, config SubscriptionQuotaResetConfig) (*SubscriptionQuotaResetSettings, error) {
	if err := s.validateConfig(ctx, &config, false); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal subscription quota reset config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySubscriptionQuotaResetConfig, string(payload)); err != nil {
		return nil, fmt.Errorf("save subscription quota reset config: %w", err)
	}
	return s.GetSettings(ctx)
}

func (s *SubscriptionQuotaResetService) RunNow(ctx context.Context) (*SubscriptionQuotaResetSettings, error) {
	return s.run(ctx, false, nil)
}

func (s *SubscriptionQuotaResetService) RunNowWithInput(ctx context.Context, input SubscriptionQuotaResetRunInput) (*SubscriptionQuotaResetSettings, error) {
	return s.run(ctx, false, &input)
}

func (s *SubscriptionQuotaResetService) runIfDue() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if _, err := s.run(ctx, true, nil); err != nil && !errors.Is(err, ErrSubscriptionQuotaResetInProgress) {
		log.Printf("[SubscriptionQuotaReset] Scheduled run failed: %v", err)
	}
}

func (s *SubscriptionQuotaResetService) run(ctx context.Context, dueOnly bool, input *SubscriptionQuotaResetRunInput) (*SubscriptionQuotaResetSettings, error) {
	if !s.runMu.TryLock() {
		return nil, ErrSubscriptionQuotaResetInProgress
	}
	defer s.runMu.Unlock()

	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, subscriptionQuotaResetLeaderLockKey, s.instanceID, subscriptionQuotaResetLeaderLockTTL)
	if !ok {
		return nil, ErrSubscriptionQuotaResetInProgress
	}
	defer release()

	config, updatedAt, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	responseConfig := config
	if input != nil {
		config.GroupIDs = append([]int64(nil), input.GroupIDs...)
		config.Daily = input.Daily
		config.Weekly = input.Weekly
		config.Monthly = input.Monthly
		config.WindowStartMode = input.WindowStartMode
	}
	state, err := s.loadState(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if dueOnly {
		nextRunAt := subscriptionQuotaResetNextRun(config, state, updatedAt)
		if !config.Enabled || nextRunAt == nil || now.Before(*nextRunAt) {
			state.NextRunAt = nextRunAt
			return &SubscriptionQuotaResetSettings{Config: config, State: state}, nil
		}
	}
	if err := s.validateConfig(ctx, &config, true); err != nil {
		if dueOnly {
			failedAt := now
			state.Status = "failed"
			state.LastStartedAt = &failedAt
			state.LastFinishedAt = &failedAt
			state.LastScheduledFinishedAt = &failedAt
			state.LastError = err.Error()
			if saveErr := s.saveState(ctx, state); saveErr != nil {
				return nil, errors.Join(err, saveErr)
			}
		}
		return nil, err
	}

	previousSuccess := state.LastSuccessAt
	lastScheduledFinishedAt := state.LastScheduledFinishedAt
	if lastScheduledFinishedAt == nil {
		anchor := updatedAt
		if state.LastFinishedAt != nil {
			anchor = *state.LastFinishedAt
		}
		if !anchor.IsZero() {
			lastScheduledFinishedAt = &anchor
		}
	}
	state = SubscriptionQuotaResetState{
		Status:                  "running",
		LastStartedAt:           &now,
		LastScheduledFinishedAt: lastScheduledFinishedAt,
		LastSuccessAt:           previousSuccess,
	}
	if err := s.saveState(ctx, state); err != nil {
		return nil, err
	}

	subs, err := s.listTargets(ctx, config.GroupIDs)
	if err != nil {
		return s.finishFailedRun(ctx, responseConfig, state, fmt.Errorf("list target subscriptions: %w", err), dueOnly)
	}
	state.MatchedCount = len(subs)
	errorsSeen := make([]error, 0)
	for i := range subs {
		sub := &subs[i]
		if !quotaResetWindowsActivated(sub, config.Daily, config.Weekly, config.Monthly) {
			state.SkippedCount++
			continue
		}
		if _, resetErr := s.subscriptionService.adminResetQuotaAt(
			ctx,
			sub.ID,
			config.Daily,
			config.Weekly,
			config.Monthly,
			config.WindowStartMode,
			now,
		); resetErr != nil {
			state.FailedCount++
			errorsSeen = append(errorsSeen, fmt.Errorf("subscription %d: %w", sub.ID, resetErr))
			continue
		}
		state.ResetCount++
	}

	finishedAt := time.Now()
	state.LastFinishedAt = &finishedAt
	if dueOnly || (input != nil && input.RestartSchedule && state.ResetCount > 0) {
		state.LastScheduledFinishedAt = &finishedAt
	}
	switch {
	case state.FailedCount == 0:
		state.Status = "success"
		state.LastSuccessAt = &finishedAt
	case state.ResetCount > 0 || state.SkippedCount > 0:
		state.Status = "partial_failed"
		state.LastError = subscriptionQuotaResetErrorSummary(errorsSeen)
	default:
		state.Status = "failed"
		state.LastError = subscriptionQuotaResetErrorSummary(errorsSeen)
	}
	if err := s.saveState(ctx, state); err != nil {
		return nil, err
	}
	if state.ResetCount > 0 && s.announcementService != nil {
		noticeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), subscriptionQuotaResetNoticeTimeout)
		noticeErr := s.publishAnnouncement(noticeCtx, config, state, finishedAt, !dueOnly)
		if noticeErr != nil {
			log.Printf("[SubscriptionQuotaReset] Publish announcement failed: %v", noticeErr)
			if state.Status == "success" {
				state.Status = "partial_failed"
			}
			if state.LastError == "" {
				state.LastError = noticeErr.Error()
			} else {
				state.LastError += "\n" + noticeErr.Error()
			}
			if saveErr := s.saveState(noticeCtx, state); saveErr != nil {
				log.Printf("[SubscriptionQuotaReset] Save announcement failure state failed: %v", saveErr)
			}
		}
		cancel()
	}
	state.NextRunAt = subscriptionQuotaResetNextRun(config, state, updatedAt)
	return &SubscriptionQuotaResetSettings{Config: responseConfig, State: state}, nil
}

func (s *SubscriptionQuotaResetService) publishAnnouncement(
	ctx context.Context,
	config SubscriptionQuotaResetConfig,
	state SubscriptionQuotaResetState,
	finishedAt time.Time,
	manual bool,
) error {
	title, content := buildSubscriptionQuotaResetAnnouncement(config, state.Status, finishedAt, manual)
	endsAt := finishedAt.Add(subscriptionQuotaResetManualTTL)
	if !manual {
		endsAt = finishedAt.Add(time.Duration(config.IntervalHours) * time.Hour)
	}
	_, err := s.announcementService.Create(ctx, &CreateAnnouncementInput{
		Title:      title,
		Content:    content,
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		Targeting: AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{{AllOf: []AnnouncementCondition{{
			Type:     AnnouncementConditionTypeSubscription,
			Operator: AnnouncementOperatorIn,
			GroupIDs: append([]int64(nil), config.GroupIDs...),
		}}}}},
		StartsAt: &finishedAt,
		EndsAt:   &endsAt,
	})
	if err != nil {
		return fmt.Errorf("publish quota reset announcement: %w", err)
	}
	return nil
}

func buildSubscriptionQuotaResetAnnouncement(
	config SubscriptionQuotaResetConfig,
	status string,
	finishedAt time.Time,
	manual bool,
) (string, string) {
	titleScope := subscriptionQuotaResetAnnouncementScope(config, manual, true)
	bodyScope := subscriptionQuotaResetAnnouncementScope(config, manual, false)
	statusText, resultText := "已重置", "重置成功"
	titleSuffix := "已重置"
	headingSuffix := "重置通知"
	if manual {
		titleSuffix = "已手动重置"
		headingSuffix = "手动重置通知"
	}
	switch status {
	case "partial_failed":
		statusText, resultText = "部分失败", "部分失败"
		if manual {
			titleSuffix = "手动重置部分失败"
		} else {
			titleSuffix = "重置部分失败"
		}
	case "failed":
		statusText, resultText = "重置失败", "重置失败"
		if manual {
			titleSuffix = "手动重置失败"
		} else {
			titleSuffix = "重置失败"
		}
	}

	lines := []string{
		fmt.Sprintf("### %s%s", bodyScope, headingSuffix),
		"",
		fmt.Sprintf("- 当前状态：%s", statusText),
		fmt.Sprintf("- 本次重置范围：%s", bodyScope),
	}
	if manual {
		lines = append(lines, fmt.Sprintf("- 重置时间：%s", formatSubscriptionQuotaResetAnnouncementTime(finishedAt)))
	} else {
		nextRunAt := finishedAt.Add(time.Duration(config.IntervalHours) * time.Hour)
		lines = append(lines,
			fmt.Sprintf("- 上次重置：%s", formatSubscriptionQuotaResetAnnouncementTime(finishedAt)),
			fmt.Sprintf("- 下次预计重置：%s", formatSubscriptionQuotaResetAnnouncementTime(nextRunAt)),
		)
	}
	lines = append(lines, fmt.Sprintf("- 处理结果：%s", resultText))
	return titleScope + titleSuffix, strings.Join(lines, "\n")
}

func subscriptionQuotaResetAnnouncementScope(config SubscriptionQuotaResetConfig, manual, forTitle bool) string {
	labels := make([]string, 0, 3)
	if config.Daily {
		label := "每日配额"
		if !manual {
			label = fmt.Sprintf("%d小时配额", config.IntervalHours)
			if !forTitle {
				label += "（日限额）"
			}
		}
		labels = append(labels, label)
	}
	if config.Weekly {
		labels = append(labels, "每周配额")
	}
	if config.Monthly {
		labels = append(labels, "每月配额")
	}
	return strings.Join(labels, "、")
}

func formatSubscriptionQuotaResetAnnouncementTime(value time.Time) string {
	return fmt.Sprintf("%s (%s)", value.In(time.Local).Format("2006-01-02 15:04:05"), time.Local.String())
}

func (s *SubscriptionQuotaResetService) finishFailedRun(
	ctx context.Context,
	config SubscriptionQuotaResetConfig,
	state SubscriptionQuotaResetState,
	runErr error,
	scheduled bool,
) (*SubscriptionQuotaResetSettings, error) {
	finishedAt := time.Now()
	state.Status = "failed"
	state.LastFinishedAt = &finishedAt
	if scheduled {
		state.LastScheduledFinishedAt = &finishedAt
	}
	state.LastError = runErr.Error()
	if err := s.saveState(ctx, state); err != nil {
		return nil, errors.Join(runErr, err)
	}
	return &SubscriptionQuotaResetSettings{Config: config, State: state}, runErr
}

func (s *SubscriptionQuotaResetService) listTargets(ctx context.Context, groupIDs []int64) ([]UserSubscription, error) {
	byID := make(map[int64]UserSubscription)
	for _, groupID := range groupIDs {
		for page := 1; ; page++ {
			subs, result, err := s.userSubRepo.List(
				ctx,
				pagination.PaginationParams{Page: page, PageSize: subscriptionQuotaResetPageSize},
				nil,
				&groupID,
				SubscriptionStatusActive,
				"",
				"created_at",
				"asc",
			)
			if err != nil {
				return nil, err
			}
			for i := range subs {
				byID[subs[i].ID] = subs[i]
			}
			if len(subs) == 0 || result == nil || page >= result.Pages {
				break
			}
		}
	}
	ids := make([]int64, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]UserSubscription, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, nil
}

func (s *SubscriptionQuotaResetService) loadConfig(ctx context.Context) (SubscriptionQuotaResetConfig, time.Time, error) {
	config := DefaultSubscriptionQuotaResetConfig()
	setting, err := s.settingRepo.Get(ctx, SettingKeySubscriptionQuotaResetConfig)
	if errors.Is(err, ErrSettingNotFound) {
		return config, time.Time{}, nil
	}
	if err != nil {
		return config, time.Time{}, fmt.Errorf("load subscription quota reset config: %w", err)
	}
	if err := json.Unmarshal([]byte(setting.Value), &config); err != nil {
		return config, setting.UpdatedAt, fmt.Errorf("decode subscription quota reset config: %w", err)
	}
	if err := normalizeSubscriptionQuotaResetConfig(&config); err != nil {
		return config, setting.UpdatedAt, err
	}
	return config, setting.UpdatedAt, nil
}

func (s *SubscriptionQuotaResetService) loadState(ctx context.Context) (SubscriptionQuotaResetState, error) {
	state := SubscriptionQuotaResetState{Status: "idle"}
	setting, err := s.settingRepo.Get(ctx, SettingKeySubscriptionQuotaResetState)
	if errors.Is(err, ErrSettingNotFound) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("load subscription quota reset state: %w", err)
	}
	if err := json.Unmarshal([]byte(setting.Value), &state); err != nil {
		return state, fmt.Errorf("decode subscription quota reset state: %w", err)
	}
	if state.Status == "" {
		state.Status = "idle"
	}
	state.NextRunAt = nil
	return state, nil
}

func (s *SubscriptionQuotaResetService) saveState(ctx context.Context, state SubscriptionQuotaResetState) error {
	state.NextRunAt = nil
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal subscription quota reset state: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySubscriptionQuotaResetState, string(payload)); err != nil {
		return fmt.Errorf("save subscription quota reset state: %w", err)
	}
	return nil
}

func (s *SubscriptionQuotaResetService) validateConfig(ctx context.Context, config *SubscriptionQuotaResetConfig, requireGroups bool) error {
	if err := normalizeSubscriptionQuotaResetConfig(config); err != nil {
		return err
	}
	if (config.Enabled || requireGroups) && len(config.GroupIDs) == 0 {
		return ErrSubscriptionQuotaResetInvalidConfig.WithMetadata(map[string]string{"field": "group_ids"})
	}
	for _, groupID := range config.GroupIDs {
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			return fmt.Errorf("validate subscription group %d: %w", groupID, err)
		}
		if !group.IsSubscriptionType() {
			return ErrGroupNotSubscriptionType.WithMetadata(map[string]string{"group_id": fmt.Sprintf("%d", groupID)})
		}
	}
	return nil
}

func normalizeSubscriptionQuotaResetConfig(config *SubscriptionQuotaResetConfig) error {
	if config == nil {
		return ErrSubscriptionQuotaResetInvalidConfig
	}
	if config.IntervalHours < 1 || config.IntervalHours > 8760 {
		return ErrSubscriptionQuotaResetInvalidConfig.WithMetadata(map[string]string{"field": "interval_hours"})
	}
	if !config.Daily && !config.Weekly && !config.Monthly {
		return ErrInvalidInput
	}
	switch config.WindowStartMode {
	case QuotaWindowStartCurrent, QuotaWindowStartNaturalDay, QuotaWindowStartPreserve:
	default:
		return ErrSubscriptionQuotaResetInvalidConfig.WithMetadata(map[string]string{"field": "window_start_mode"})
	}
	seen := make(map[int64]struct{}, len(config.GroupIDs))
	groupIDs := make([]int64, 0, len(config.GroupIDs))
	for _, id := range config.GroupIDs {
		if id <= 0 {
			return ErrSubscriptionQuotaResetInvalidConfig.WithMetadata(map[string]string{"field": "group_ids"})
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		groupIDs = append(groupIDs, id)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	config.GroupIDs = groupIDs
	return nil
}

func subscriptionQuotaResetNextRun(config SubscriptionQuotaResetConfig, state SubscriptionQuotaResetState, configUpdatedAt time.Time) *time.Time {
	if !config.Enabled || config.IntervalHours <= 0 || configUpdatedAt.IsZero() {
		return nil
	}
	anchor := configUpdatedAt
	candidate := state.LastScheduledFinishedAt
	if candidate == nil {
		candidate = state.LastFinishedAt
	}
	if candidate != nil && candidate.After(anchor) {
		anchor = *candidate
	}
	next := anchor.Add(time.Duration(config.IntervalHours) * time.Hour)
	return &next
}

func quotaResetWindowsActivated(sub *UserSubscription, daily, weekly, monthly bool) bool {
	if sub == nil {
		return false
	}
	return (!daily || sub.DailyWindowStart != nil) &&
		(!weekly || sub.WeeklyWindowStart != nil) &&
		(!monthly || sub.MonthlyWindowStart != nil)
}

func subscriptionQuotaResetErrorSummary(runErrors []error) string {
	const maxDetailedErrors = 20
	if len(runErrors) == 0 {
		return ""
	}
	if len(runErrors) <= maxDetailedErrors {
		return errors.Join(runErrors...).Error()
	}
	detailed := append([]error(nil), runErrors[:maxDetailedErrors]...)
	detailed = append(detailed, fmt.Errorf("%d additional failures", len(runErrors)-maxDetailedErrors))
	return errors.Join(detailed...).Error()
}
