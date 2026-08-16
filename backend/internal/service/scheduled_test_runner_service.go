package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

const (
	channelMonitorProbeModeFixedInt32 int32 = iota
	channelMonitorProbeModeAdaptiveInt32
)

const (
	adaptiveChannelProbeColdInterval          = 10 * time.Minute
	adaptiveChannelProbeStableInterval        = 60 * time.Minute
	adaptiveChannelProbeRecentTrafficWindow   = 30 * time.Minute
	adaptiveChannelProbeRecentTrafficDeferral = 60 * time.Minute
	adaptiveChannelProbeStableSuccesses       = 3
	adaptiveChannelProbeHistoryLimit          = 5
	adaptiveChannelProbeHistoryTTL            = 2 * time.Hour
)

var adaptiveChannelProbeFailureBackoff = [...]time.Duration{
	5 * time.Minute,
	10 * time.Minute,
	20 * time.Minute,
	40 * time.Minute,
	60 * time.Minute,
}

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc scheduledTestAccountTester
	rateLimitSvc   *RateLimitService
	cfg            *config.Config
	accounts       AccountRepository
	settings       channelMonitorAutoModelSettingStore
	probeReporter  channelMonitorProbeOutcomeReporter

	cron                      *cron.Cron
	startOnce                 sync.Once
	stopOnce                  sync.Once
	probeMode                 atomic.Int32
	fixedProbeIntervalMinutes atomic.Int64
	now                       func() time.Time
}

type scheduledTestAccountTester interface {
	RunTestBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
	RunChannelMonitorProbeBackground(ctx context.Context, accountID int64, modelID string) (*ScheduledTestResult, error)
}

type channelMonitorProbeOutcomeReporter interface {
	ReportChannelMonitorProbe(accountID int64, model string, success bool, firstTokenMs *int)
	RefreshOpenAISchedulerHealth(ctx context.Context)
	LatestSuccessfulRealTrafficAt(ctx context.Context, accountID int64) (*time.Time, error)
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		rateLimitSvc:   rateLimitSvc,
		cfg:            cfg,
	}
}

func (s *ScheduledTestRunnerService) SetChannelMonitorPoolDependencies(
	accounts AccountRepository,
	settings channelMonitorAutoModelSettingStore,
	reporter channelMonitorProbeOutcomeReporter,
) {
	s.accounts = accounts
	s.settings = settings
	s.probeReporter = reporter
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.reconcileChannelMonitorPlans(ctx); err != nil {
				logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] initial channel monitor reconciliation failed: %v", err)
			}
		}()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *ScheduledTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] cron stop timed out")
			}
		}
	})
}

func (s *ScheduledTestRunnerService) runScheduled() {
	// Delay 10s so execution lands at ~:10 of each minute instead of :00.
	time.Sleep(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	now := time.Now()
	if err := s.reconcileChannelMonitorPlans(ctx); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] channel monitor reconciliation failed: %v", err)
	}
	plans, err := s.planRepo.ListDue(ctx, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ListDue error: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	sem := make(chan struct{}, scheduledTestDefaultMaxWorkers)
	var wg sync.WaitGroup

	for _, plan := range plans {
		sem <- struct{}{}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) reconcileChannelMonitorPlans(ctx context.Context) error {
	if s == nil || s.planRepo == nil || s.accounts == nil {
		return nil
	}
	if s.probeReporter != nil {
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		s.probeReporter.RefreshOpenAISchedulerHealth(healthCtx)
		cancel()
	}
	policy, err := loadAutoModelPolicyFromStore(ctx, s.settings)
	if err != nil {
		return err
	}
	s.setChannelMonitorProbeSettings(policy.Mode, policy.FixedIntervalMinutes)
	if !policy.Enabled {
		return s.planRepo.ReconcileChannelMonitorPlans(ctx, nil)
	}

	platforms := []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok}
	accounts, err := s.accounts.ListAllWithFilters(ctx, "", "", "", "", 0, "")
	if err != nil {
		return fmt.Errorf("list account pool for active probes: %w", err)
	}

	desired := make([]*ScheduledTestPlan, 0)
	now := time.Now()
	for i := range accounts {
		if !channelMonitorPoolAccountEligible(&accounts[i], platforms) {
			continue
		}
		models := channelMonitorModelsForAccount(&accounts[i])
		models = filterAutoMonitorModels(models, policy.Whitelist)
		accountWhitelist := channelMonitorAccountModelWhitelist(&accounts[i])
		models = filterAutoMonitorModels(models, accountWhitelist)
		models = selectChannelMonitorProbeModels(&accounts[i], models, accountWhitelist)
		for _, model := range models {
			stagger := time.Duration((accounts[i].ID+int64(len(desired))*17)%240) * time.Second
			nextRun := now.Add(stagger)
			desired = append(desired, &ScheduledTestPlan{
				AccountID: accounts[i].ID, ModelID: model, CronExpression: "*/5 * * * *",
				Enabled: true, MaxResults: 288, AutoRecover: true, ManagedBy: ScheduledTestManagedBySchedulerProbe,
				NextRunAt: &nextRun,
			})
		}
	}
	return s.planRepo.ReconcileChannelMonitorPlans(ctx, desired)
}

func channelMonitorPoolAccountEligible(account *Account, platforms []string) bool {
	if account == nil || account.Status != StatusActive || !account.Schedulable || account.Type == AccountTypeOAuth {
		return false
	}
	for _, platform := range platforms {
		if account.Platform == platform {
			return true
		}
	}
	return false
}

func channelMonitorModelsForAccount(account *Account) []string {
	if account == nil {
		return []string{}
	}
	models := make([]string, 0)
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		models = append(models, defaultModelsListCandidateIDs(account.Platform)...)
	} else {
		for requested := range mapping {
			requested = strings.TrimSpace(requested)
			if requested != "" && !strings.Contains(requested, "*") {
				models = append(models, requested)
			}
		}
	}
	models = normalizeModels(models)
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i]) < strings.ToLower(models[j]) })
	return models
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	checkedAt := s.currentTime()
	if s.shouldDeferProbeForRecentTraffic(ctx, plan, checkedAt) {
		return
	}

	var (
		result *ScheduledTestResult
		err    error
	)
	if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe {
		result, err = s.accountTestSvc.RunChannelMonitorProbeBackground(ctx, plan.AccountID, plan.ModelID)
	} else {
		result, err = s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	}
	if err != nil {
		now := s.currentTime()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe {
			failedResult := &ScheduledTestResult{
				Status:       "failed",
				ErrorMessage: err.Error(),
				StartedAt:    checkedAt,
				FinishedAt:   now,
			}
			if s.scheduledSvc != nil {
				if saveErr := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, failedResult); saveErr != nil {
					logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d save execution error result failed: %v", plan.ID, saveErr)
				}
			}
			if s.probeReporter != nil {
				s.probeReporter.ReportChannelMonitorProbe(plan.AccountID, plan.ModelID, false, nil)
			}
			if s.channelMonitorProbeMode() == channelMonitorProbeModeAdaptiveInt32 {
				history := s.recentProbeResults(ctx, plan.ID, failedResult)
				nextRun, nextErr := s.nextRunAfterPlan(plan, failedResult, history, now)
				if nextErr == nil {
					if updateErr := s.planRepo.UpdateAfterRun(ctx, plan.ID, now, nextRun); updateErr != nil {
						logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d adaptive failure reschedule error: %v", plan.ID, updateErr)
					}
				}
			} else if interval := s.fixedProbeInterval(); interval != defaultChannelMonitorProbeFixedIntervalMinutes*time.Minute {
				if updateErr := s.planRepo.UpdateAfterRun(ctx, plan.ID, now, now.Add(interval)); updateErr != nil {
					logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d fixed failure reschedule error: %v", plan.ID, updateErr)
				}
			}
		}
		return
	}

	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, err)
	}
	if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe && s.probeReporter != nil {
		var firstTokenMs *int
		if result.TTFTMs != nil {
			value := int(*result.TTFTMs)
			firstTokenMs = &value
		}
		s.probeReporter.ReportChannelMonitorProbe(plan.AccountID, plan.ModelID, result.Status == "success", firstTokenMs)
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	now := s.currentTime()
	var history []*ScheduledTestResult
	if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe && s.channelMonitorProbeMode() == channelMonitorProbeModeAdaptiveInt32 {
		history = s.recentProbeResults(ctx, plan.ID, result)
	}
	nextRun, err := s.nextRunAfterPlan(plan, result, history, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		return
	}

	if err := s.planRepo.UpdateAfterRun(ctx, plan.ID, now, nextRun); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d UpdateAfterRun error: %v", plan.ID, err)
	}
}

func (s *ScheduledTestRunnerService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *ScheduledTestRunnerService) shouldDeferProbeForRecentTraffic(ctx context.Context, plan *ScheduledTestPlan, now time.Time) bool {
	if s == nil || plan == nil || plan.ManagedBy != ScheduledTestManagedBySchedulerProbe ||
		s.channelMonitorProbeMode() != channelMonitorProbeModeAdaptiveInt32 || s.probeReporter == nil || s.planRepo == nil {
		return false
	}
	latest, err := s.probeReporter.LatestSuccessfulRealTrafficAt(ctx, plan.AccountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d recent real traffic lookup error: %v", plan.ID, err)
		return false
	}
	if latest == nil || latest.IsZero() || now.Before(*latest) || now.Sub(*latest) > adaptiveChannelProbeRecentTrafficWindow {
		return false
	}

	nextRun := now.Add(adaptiveChannelProbeRecentTrafficDeferral)
	deferred := *plan
	deferred.NextRunAt = &nextRun
	if _, err := s.planRepo.Update(ctx, &deferred); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d defer after real traffic error: %v", plan.ID, err)
		return false
	}
	return true
}

func (s *ScheduledTestRunnerService) recentProbeResults(ctx context.Context, planID int64, current *ScheduledTestResult) []*ScheduledTestResult {
	history := make([]*ScheduledTestResult, 0, adaptiveChannelProbeHistoryLimit+1)
	if s != nil && s.scheduledSvc != nil {
		persisted, err := s.scheduledSvc.ListResults(ctx, planID, adaptiveChannelProbeHistoryLimit)
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d load recent probe results error: %v", planID, err)
		} else {
			history = append(history, persisted...)
		}
	}
	if current == nil {
		return history
	}
	if current.ID > 0 {
		for _, item := range history {
			if item != nil && item.ID == current.ID {
				return history
			}
		}
	}
	return append([]*ScheduledTestResult{current}, history...)
}

func (s *ScheduledTestRunnerService) setChannelMonitorProbeSettings(mode string, fixedIntervalMinutes int) {
	if strings.EqualFold(strings.TrimSpace(mode), ChannelMonitorProbeModeAdaptive) {
		s.probeMode.Store(channelMonitorProbeModeAdaptiveInt32)
	} else {
		s.probeMode.Store(channelMonitorProbeModeFixedInt32)
	}
	if fixedIntervalMinutes <= 0 {
		fixedIntervalMinutes = defaultChannelMonitorProbeFixedIntervalMinutes
	}
	s.fixedProbeIntervalMinutes.Store(int64(fixedIntervalMinutes))
}

func (s *ScheduledTestRunnerService) channelMonitorProbeMode() int32 {
	if s == nil {
		return channelMonitorProbeModeFixedInt32
	}
	return s.probeMode.Load()
}

func (s *ScheduledTestRunnerService) fixedProbeInterval() time.Duration {
	if s == nil {
		return defaultChannelMonitorProbeFixedIntervalMinutes * time.Minute
	}
	minutes := s.fixedProbeIntervalMinutes.Load()
	if minutes <= 0 {
		minutes = defaultChannelMonitorProbeFixedIntervalMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func (s *ScheduledTestRunnerService) nextRunAfterPlan(plan *ScheduledTestPlan, result *ScheduledTestResult, history []*ScheduledTestResult, now time.Time) (time.Time, error) {
	if plan == nil {
		return time.Time{}, fmt.Errorf("scheduled test plan is nil")
	}
	if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe && s.channelMonitorProbeMode() == channelMonitorProbeModeAdaptiveInt32 {
		return nextAdaptiveChannelProbeRun(result, history, now), nil
	}
	if plan.ManagedBy == ScheduledTestManagedBySchedulerProbe {
		interval := s.fixedProbeInterval()
		if interval != defaultChannelMonitorProbeFixedIntervalMinutes*time.Minute {
			return now.Add(interval), nil
		}
	}
	return computeNextRun(plan.CronExpression, now)
}

func nextAdaptiveChannelProbeRun(result *ScheduledTestResult, history []*ScheduledTestResult, now time.Time) time.Time {
	status := "failed"
	if result != nil {
		status = result.Status
	}
	consecutive := 0
	for _, item := range history {
		if item == nil || item.Status != status {
			break
		}
		observedAt := item.FinishedAt
		if observedAt.IsZero() {
			observedAt = item.CreatedAt
		}
		if !observedAt.IsZero() && (now.Before(observedAt) || now.Sub(observedAt) > adaptiveChannelProbeHistoryTTL) {
			break
		}
		consecutive++
	}
	if consecutive == 0 {
		consecutive = 1
	}
	if status == "success" {
		if consecutive >= adaptiveChannelProbeStableSuccesses {
			return now.Add(adaptiveChannelProbeStableInterval)
		}
		return now.Add(adaptiveChannelProbeColdInterval)
	}
	index := consecutive - 1
	if index >= len(adaptiveChannelProbeFailureBackoff) {
		index = len(adaptiveChannelProbeFailureBackoff) - 1
	}
	return now.Add(adaptiveChannelProbeFailureBackoff[index])
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s.rateLimitSvc == nil {
		return
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}
