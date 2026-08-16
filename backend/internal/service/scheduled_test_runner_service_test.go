//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledTestAccountTesterStub struct {
	ordinaryCalls int
	probeCalls    int
}

func (s *scheduledTestAccountTesterStub) RunTestBackground(_ context.Context, _ int64, _ string) (*ScheduledTestResult, error) {
	s.ordinaryCalls++
	return scheduledTestRunnerResult(), nil
}

func (s *scheduledTestAccountTesterStub) RunChannelMonitorProbeBackground(_ context.Context, _ int64, _ string) (*ScheduledTestResult, error) {
	s.probeCalls++
	return scheduledTestRunnerResult(), nil
}

func scheduledTestRunnerResult() *ScheduledTestResult {
	now := time.Now()
	return &ScheduledTestResult{Status: "failed", StartedAt: now, FinishedAt: now}
}

type scheduledTestRunnerPlanRepoStub struct {
	ScheduledTestPlanRepository
	updated *ScheduledTestPlan
}

func (s *scheduledTestRunnerPlanRepoStub) UpdateAfterRun(_ context.Context, _ int64, _ time.Time, _ time.Time) error {
	return nil
}

func (s *scheduledTestRunnerPlanRepoStub) Update(_ context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	copy := *plan
	s.updated = &copy
	return &copy, nil
}

type scheduledTestRunnerResultRepoStub struct {
	ScheduledTestResultRepository
}

func (s *scheduledTestRunnerResultRepoStub) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	return result, nil
}

func (s *scheduledTestRunnerResultRepoStub) PruneOldResults(_ context.Context, _ int64, _ int) error {
	return nil
}

func TestScheduledTestRunnerUsesProbeEntryPointOnlyForManagedPlans(t *testing.T) {
	tester := &scheduledTestAccountTesterStub{}
	planRepo := &scheduledTestRunnerPlanRepoStub{}
	runner := &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   NewScheduledTestService(planRepo, &scheduledTestRunnerResultRepoStub{}),
		accountTestSvc: tester,
	}

	runner.runOnePlan(context.Background(), &ScheduledTestPlan{ID: 1, AccountID: 42, CronExpression: "* * * * *"})
	require.Equal(t, 1, tester.ordinaryCalls)
	require.Zero(t, tester.probeCalls)

	runner.runOnePlan(context.Background(), &ScheduledTestPlan{ID: 2, AccountID: 42, CronExpression: "* * * * *", ManagedBy: ScheduledTestManagedBySchedulerProbe})
	require.Equal(t, 1, tester.ordinaryCalls)
	require.Equal(t, 1, tester.probeCalls)
}

func TestAdaptiveChannelProbeKeepsColdAccountsWarmUntilThreeSuccesses(t *testing.T) {
	runner := &ScheduledTestRunnerService{}
	runner.setChannelMonitorProbeSettings(ChannelMonitorProbeModeAdaptive, defaultChannelMonitorProbeFixedIntervalMinutes)
	plan := &ScheduledTestPlan{ManagedBy: ScheduledTestManagedBySchedulerProbe, CronExpression: "*/5 * * * *"}
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)

	oneSuccess := []*ScheduledTestResult{{Status: "success"}}
	successNext, err := runner.nextRunAfterPlan(plan, oneSuccess[0], oneSuccess, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(10*time.Minute), successNext)

	threeSuccesses := []*ScheduledTestResult{{Status: "success"}, {Status: "success"}, {Status: "success"}}
	successNext, err = runner.nextRunAfterPlan(plan, threeSuccesses[0], threeSuccesses, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(60*time.Minute), successNext)
}

func TestAdaptiveChannelProbeBacksFailuresOffToOneHour(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	for failures, want := range []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, 60 * time.Minute, 60 * time.Minute} {
		history := make([]*ScheduledTestResult, failures+1)
		for i := range history {
			history[i] = &ScheduledTestResult{Status: "failed"}
		}
		require.Equal(t, now.Add(want), nextAdaptiveChannelProbeRun(history[0], history, now))
	}
}

func TestAdaptiveChannelProbeIgnoresExpiredHistory(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	history := []*ScheduledTestResult{
		{Status: "success", FinishedAt: now},
		{Status: "success", FinishedAt: now.Add(-adaptiveChannelProbeHistoryTTL - time.Second)},
		{Status: "success", FinishedAt: now.Add(-adaptiveChannelProbeHistoryTTL - 2*time.Second)},
	}

	require.Equal(t, now.Add(10*time.Minute), nextAdaptiveChannelProbeRun(history[0], history, now))
}

func TestFixedChannelProbeKeepsCronSchedule(t *testing.T) {
	runner := &ScheduledTestRunnerService{}
	runner.setChannelMonitorProbeSettings(ChannelMonitorProbeModeFixed, defaultChannelMonitorProbeFixedIntervalMinutes)
	plan := &ScheduledTestPlan{ManagedBy: ScheduledTestManagedBySchedulerProbe, CronExpression: "*/5 * * * *"}
	now := time.Date(2026, time.August, 16, 12, 2, 0, 0, time.UTC)

	next, err := runner.nextRunAfterPlan(plan, &ScheduledTestResult{Status: "success"}, nil, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(3*time.Minute), next)
}

func TestFixedChannelProbeUsesCustomInterval(t *testing.T) {
	runner := &ScheduledTestRunnerService{}
	runner.setChannelMonitorProbeSettings(ChannelMonitorProbeModeFixed, 15)
	plan := &ScheduledTestPlan{ManagedBy: ScheduledTestManagedBySchedulerProbe, CronExpression: "*/5 * * * *"}
	now := time.Date(2026, time.August, 16, 12, 2, 0, 0, time.UTC)

	next, err := runner.nextRunAfterPlan(plan, &ScheduledTestResult{Status: "success"}, nil, now)
	require.NoError(t, err)
	require.Equal(t, now.Add(15*time.Minute), next)
}

type scheduledTestProbeReporterStub struct {
	latest *time.Time
}

func (s *scheduledTestProbeReporterStub) ReportChannelMonitorProbe(int64, string, bool, *int) {}

func (s *scheduledTestProbeReporterStub) RefreshOpenAISchedulerHealth(context.Context) {}

func (s *scheduledTestProbeReporterStub) LatestSuccessfulRealTrafficAt(context.Context, int64) (*time.Time, error) {
	return s.latest, nil
}

func TestAdaptiveChannelProbeSkipsAfterRecentRealTraffic(t *testing.T) {
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	latest := now.Add(-10 * time.Minute)
	tester := &scheduledTestAccountTesterStub{}
	planRepo := &scheduledTestRunnerPlanRepoStub{}
	runner := &ScheduledTestRunnerService{
		planRepo:       planRepo,
		accountTestSvc: tester,
		probeReporter:  &scheduledTestProbeReporterStub{latest: &latest},
		now:            func() time.Time { return now },
	}
	runner.setChannelMonitorProbeSettings(ChannelMonitorProbeModeAdaptive, defaultChannelMonitorProbeFixedIntervalMinutes)

	runner.runOnePlan(context.Background(), &ScheduledTestPlan{
		ID: 1, AccountID: 42, ModelID: "gpt-5.4", CronExpression: "*/5 * * * *",
		Enabled: true, MaxResults: 288, AutoRecover: true, ManagedBy: ScheduledTestManagedBySchedulerProbe,
	})

	require.Zero(t, tester.probeCalls)
	require.NotNil(t, planRepo.updated)
	require.NotNil(t, planRepo.updated.NextRunAt)
	require.Equal(t, now.Add(60*time.Minute), *planRepo.updated.NextRunAt)
}

func TestChannelMonitorPoolAccountEligibleRequiresEnabledNonOAuthAccount(t *testing.T) {
	platforms := []string{PlatformOpenAI, PlatformAnthropic}
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "active api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}, want: true},
		{name: "oauth excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}, want: false},
		{name: "unschedulable excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: false}, want: false},
		{name: "error excluded", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError, Schedulable: true}, want: false},
		{name: "disabled is excluded", account: &Account{Platform: PlatformOpenAI, Status: StatusDisabled}, want: false},
		{name: "expired is excluded", account: &Account{Platform: PlatformOpenAI, Status: StatusExpired}, want: false},
		{name: "unsupported platform", account: &Account{Platform: "custom", Status: StatusActive}, want: false},
		{name: "nil account", account: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelMonitorPoolAccountEligible(tt.account, platforms); got != tt.want {
				t.Fatalf("channelMonitorPoolAccountEligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChannelMonitorAccountWhitelistNarrowsGlobalPolicy(t *testing.T) {
	account := &Account{Extra: map[string]any{
		ChannelMonitorAccountModelWhitelistExtraKey: []any{"gpt-5.6-sol", "gpt-5.6-sol"},
	}}

	channelWhitelist := channelMonitorAccountModelWhitelist(account)
	models := filterAutoMonitorModels([]string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-4.1"}, []string{"gpt-5.*"})
	models = filterAutoMonitorModels(models, channelWhitelist)

	require.Equal(t, []string{"gpt-5.6-sol"}, channelWhitelist)
	require.Equal(t, []string{"gpt-5.6-sol"}, models)
}

func TestChannelMonitorProbeModelsChooseOneRepresentativePerProvider(t *testing.T) {
	tests := []struct {
		name       string
		account    *Account
		candidates []string
		want       []string
	}{
		{
			name: "openai prefers configured default text model",
			account: &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{
				"gpt-5.4": "gpt-5.4", "gpt-5.4-mini": "gpt-5.4-mini", "gpt-image-1": "gpt-image-1",
			}}},
			candidates: []string{"gpt-5.4-mini", "gpt-image-1", "gpt-5.4"},
			want:       []string{"gpt-5.4"},
		},
		{
			name: "claude keeps one text representative",
			account: &Account{Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{
				"claude-opus-4-6": "claude-opus-4-6", "claude-haiku-4-5": "claude-haiku-4-5",
			}}},
			candidates: []string{"claude-opus-4-6", "claude-haiku-4-5"},
			want:       []string{"claude-haiku-4-5"},
		},
		{
			name: "gemini excludes image and prefers flash",
			account: &Account{Platform: PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{
				"gemini-2.5-pro": "gemini-2.5-pro", "gemini-2.5-flash": "gemini-2.5-flash", "gemini-2.5-flash-image": "gemini-2.5-flash-image",
			}}},
			candidates: []string{"gemini-2.5-pro", "gemini-2.5-flash-image", "gemini-2.5-flash"},
			want:       []string{"gemini-2.5-flash"},
		},
		{
			name: "grok excludes imagine models",
			account: &Account{Platform: PlatformGrok, Credentials: map[string]any{"model_mapping": map[string]any{
				"grok-4.5": "grok-4.5", "grok-imagine-video": "grok-imagine-video",
			}}},
			candidates: []string{"grok-imagine-video", "grok-4.5"},
			want:       []string{"grok-4.5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, selectChannelMonitorProbeModels(tt.account, tt.candidates, nil))
		})
	}
}

func TestChannelMonitorProbeModelsPreserveExplicitAccountModels(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{
		"gpt-5.4": "gpt-5.4", "gpt-5.4-mini": "gpt-5.4-mini", "gpt-image-1": "gpt-image-1",
	}}}

	selected := selectChannelMonitorProbeModels(
		account,
		[]string{"gpt-5.4", "gpt-5.4-mini", "gpt-image-1"},
		[]string{"gpt-5.4", "gpt-image-1"},
	)

	require.Equal(t, []string{"gpt-5.4", "gpt-image-1"}, selected)
}

func TestChannelMonitorProbeModelsChooseOnePerAntigravityProtocolFamily(t *testing.T) {
	account := &Account{Platform: PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{
		"claude-sonnet": "claude-sonnet-4-5-20250929",
		"claude-opus":   "claude-opus-4-6",
		"gemini-flash":  "gemini-2.0-flash",
		"gemini-pro":    "gemini-2.5-pro",
	}}}

	selected := selectChannelMonitorProbeModels(account, []string{
		"claude-sonnet", "claude-opus", "gemini-flash", "gemini-pro",
	}, nil)

	require.Equal(t, []string{"claude-sonnet", "gemini-flash"}, selected)
}
