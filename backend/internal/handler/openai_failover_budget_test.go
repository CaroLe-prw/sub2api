package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIFailoverBudgetSoftLimitAllowsOneFinalAttempt(t *testing.T) {
	startedAt := time.Now()
	budget := newOpenAIFailoverBudget(context.Background(), nil, []byte(`{"reasoning":{"effort":"medium"}}`), startedAt, 5)

	budget.now = func() time.Time { return startedAt.Add(10 * time.Second) }
	decision := budget.evaluateNextAccount(context.Background(), nil, 0, 7)
	require.True(t, decision.AllowNext)
	require.Equal(t, "within_retry_budget", decision.Reason)
	require.Equal(t, 1, decision.SwitchCount)

	budget.now = func() time.Time { return startedAt.Add(46 * time.Second) }
	decision = budget.evaluateNextAccount(context.Background(), nil, 1, 6)
	require.True(t, decision.AllowNext)
	require.Equal(t, "soft_retry_budget_final_attempt", decision.Reason)

	budget.now = func() time.Time { return startedAt.Add(47 * time.Second) }
	decision = budget.evaluateNextAccount(context.Background(), nil, 2, 5)
	require.False(t, decision.AllowNext)
	require.Equal(t, "soft_retry_budget_exhausted", decision.Reason)
}

func TestOpenAIFailoverBudgetStopsBeforeHardDeadlineWithoutRetryWindow(t *testing.T) {
	startedAt := time.Now()
	budget := newOpenAIFailoverBudget(context.Background(), nil, nil, startedAt, 5)
	budget.now = func() time.Time { return startedAt.Add(71 * time.Second) }

	decision := budget.evaluateNextAccount(context.Background(), nil, 2, 4)

	require.False(t, decision.AllowNext)
	require.Equal(t, "hard_retry_budget", decision.Reason)
	require.Equal(t, int64(75_000), decision.BudgetMs)
}

func TestOpenAIFailoverBudgetAllowsTwoFirstOutputTimeoutSwitches(t *testing.T) {
	startedAt := time.Now()
	budget := newOpenAIFailoverBudget(context.Background(), nil, nil, startedAt, 5)
	budget.now = func() time.Time { return startedAt.Add(time.Second) }
	failoverErr := &service.UpstreamFailoverError{Reason: service.GatewayFailureReason("first_output_timeout")}

	first := budget.evaluateNextAccount(context.Background(), failoverErr, 0, 7)
	second := budget.evaluateNextAccount(context.Background(), failoverErr, 1, 6)
	third := budget.evaluateNextAccount(context.Background(), failoverErr, 2, 5)

	require.True(t, first.AllowNext)
	require.True(t, second.AllowNext)
	require.False(t, third.AllowNext)
	require.Equal(t, "first_output_timeout_switch_limit", third.Reason)
}

func TestOpenAIFailoverBudgetHonorsSwitchLimit(t *testing.T) {
	startedAt := time.Now()
	budget := newOpenAIFailoverBudget(context.Background(), nil, nil, startedAt, 5)
	budget.now = func() time.Time { return startedAt.Add(time.Second) }

	decision := budget.evaluateNextAccount(context.Background(), nil, 5, 2)

	require.False(t, decision.AllowNext)
	require.Equal(t, "max_account_switches", decision.Reason)
	require.Equal(t, 5, decision.SwitchLimit)
}

func TestOpenAIFailoverBudgetSameAccountRetryUsesBudgetWithoutCountingSwitch(t *testing.T) {
	startedAt := time.Now()
	budget := newOpenAIFailoverBudget(context.Background(), nil, nil, startedAt, 5)
	budget.now = func() time.Time { return startedAt.Add(46 * time.Second) }

	finalAttempt := budget.evaluateSameAccountRetry(context.Background(), 2, 4)
	rejectedNextSwitch := budget.evaluateNextAccount(context.Background(), nil, 2, 4)

	require.True(t, finalAttempt.AllowNext)
	require.Equal(t, "soft_retry_budget_final_attempt", finalAttempt.Reason)
	require.Equal(t, 2, finalAttempt.SwitchCount)
	require.False(t, rejectedNextSwitch.AllowNext)
	require.Equal(t, "soft_retry_budget_exhausted", rejectedNextSwitch.Reason)
}

func TestOpenAIFailoverBudgetUsesHighEffortAndClientDeadline(t *testing.T) {
	startedAt := time.Now()
	clientCtx, cancel := context.WithDeadline(context.Background(), startedAt.Add(40*time.Second))
	defer cancel()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIFailover.HighEffortSoftBudgetSeconds = 70
	cfg.Gateway.OpenAIFailover.HighEffortHardBudgetSeconds = 150
	budget := newOpenAIFailoverBudget(clientCtx, cfg, []byte(`{"reasoning_effort":"xhigh"}`), startedAt, 5)
	budget.now = func() time.Time { return startedAt.Add(31 * time.Second) }

	decision := budget.evaluateNextAccount(clientCtx, nil, 1, 4)

	require.False(t, decision.AllowNext)
	require.Equal(t, "client_deadline", decision.Reason)
	require.InDelta(t, 35_000, decision.BudgetMs, 20)
}

func TestIsOpenAIHighEffortRequest(t *testing.T) {
	require.True(t, isOpenAIHighEffortRequest([]byte(`{"reasoning":{"effort":"high"}}`)))
	require.True(t, isOpenAIHighEffortRequest([]byte(`{"reasoningEffort":"MAX"}`)))
	require.False(t, isOpenAIHighEffortRequest([]byte(`{"reasoning_effort":"medium"}`)))
}
