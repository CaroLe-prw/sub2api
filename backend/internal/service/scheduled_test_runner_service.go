package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc *AccountTestService
	rateLimitSvc   *RateLimitService
	cfg            *config.Config
	accounts       AccountRepository
	settings       channelMonitorAutoModelSettingStore
	probeReporter  channelMonitorProbeOutcomeReporter

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
}

type channelMonitorProbeOutcomeReporter interface {
	ReportChannelMonitorProbe(accountID int64, model string, success bool)
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
	policy, err := loadAutoModelPolicyFromStore(ctx, s.settings)
	if err != nil {
		return err
	}
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
		// Monitoring must retain unhealthy and manually unschedulable accounts;
		// otherwise the probe that detects a failure would remove its own plan.
		// Explicitly disabled/expired accounts remain outside the active probes.
		if !channelMonitorPoolAccountEligible(&accounts[i], platforms) {
			continue
		}
		models := channelMonitorModelsForAccount(&accounts[i])
		models = filterAutoMonitorModels(models, policy.Whitelist)
		for _, model := range models {
			stagger := time.Duration((accounts[i].ID+int64(len(desired))*17)%240) * time.Second
			nextRun := now.Add(stagger)
			desired = append(desired, &ScheduledTestPlan{
				AccountID: accounts[i].ID, ModelID: model, CronExpression: "*/5 * * * *",
				Enabled: true, MaxResults: 288, AutoRecover: true, ManagedBy: ScheduledTestManagedByChannelMonitor,
				NextRunAt: &nextRun,
			})
		}
	}
	return s.planRepo.ReconcileChannelMonitorPlans(ctx, desired)
}

func channelMonitorPoolAccountEligible(account *Account, platforms []string) bool {
	if account == nil || (account.Status != StatusActive && account.Status != StatusError) {
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
	result, err := s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		return
	}

	if err := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, err)
	}
	if plan.ManagedBy == ScheduledTestManagedByChannelMonitor && s.probeReporter != nil {
		s.probeReporter.ReportChannelMonitorProbe(plan.AccountID, plan.ModelID, result.Status == "success")
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		return
	}

	if err := s.planRepo.UpdateAfterRun(ctx, plan.ID, time.Now(), nextRun); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d UpdateAfterRun error: %v", plan.ID, err)
	}
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
