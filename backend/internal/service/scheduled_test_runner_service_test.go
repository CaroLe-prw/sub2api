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
}

func (s *scheduledTestRunnerPlanRepoStub) UpdateAfterRun(_ context.Context, _ int64, _ time.Time, _ time.Time) error {
	return nil
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
