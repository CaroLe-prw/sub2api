package handler

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/tidwall/gjson"
)

const (
	defaultOpenAIFailoverSoftBudget              = 45 * time.Second
	defaultOpenAIFailoverHardBudget              = 75 * time.Second
	defaultOpenAIHighEffortFailoverSoftBudget    = 60 * time.Second
	defaultOpenAIHighEffortFailoverHardBudget    = 120 * time.Second
	defaultOpenAIMaxFirstOutputTimeoutSwitches   = 2
	openAIFailoverClientDeadlineReserve          = 5 * time.Second
	openAIFailoverMinimumRetryAdmissionRemaining = 5 * time.Second
)

type openAIFailoverBudget struct {
	startedAt                     time.Time
	softDeadline                  time.Time
	hardDeadline                  time.Time
	hardDeadlineReason            string
	maxSwitches                   int
	maxFirstOutputSwitches        int
	firstOutputTimeoutSwitches    int
	softBudgetFinalAttemptGranted bool
	now                           func() time.Time
}

type openAIFailoverBudgetDecision struct {
	AllowNext           bool
	Reason              string
	ElapsedMs           int64
	BudgetMs            int64
	SwitchCount         int
	SwitchLimit         int
	RemainingCandidates int
}

func newOpenAIFailoverBudget(
	ctx context.Context,
	cfg *config.Config,
	body []byte,
	startedAt time.Time,
	maxSwitches int,
) *openAIFailoverBudget {
	softBudget := defaultOpenAIFailoverSoftBudget
	hardBudget := defaultOpenAIFailoverHardBudget
	maxFirstOutputSwitches := defaultOpenAIMaxFirstOutputTimeoutSwitches
	highEffort := isOpenAIHighEffortRequest(body)
	if highEffort {
		softBudget = defaultOpenAIHighEffortFailoverSoftBudget
		hardBudget = defaultOpenAIHighEffortFailoverHardBudget
	}
	if cfg != nil {
		failoverCfg := cfg.Gateway.OpenAIFailover
		if highEffort {
			softBudget = positiveSecondsOrDefault(failoverCfg.HighEffortSoftBudgetSeconds, softBudget)
			hardBudget = positiveSecondsOrDefault(failoverCfg.HighEffortHardBudgetSeconds, hardBudget)
		} else {
			softBudget = positiveSecondsOrDefault(failoverCfg.SoftBudgetSeconds, softBudget)
			hardBudget = positiveSecondsOrDefault(failoverCfg.HardBudgetSeconds, hardBudget)
		}
		if failoverCfg.MaxFirstOutputSwitches > 0 {
			maxFirstOutputSwitches = failoverCfg.MaxFirstOutputSwitches
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	if maxSwitches <= 0 {
		maxSwitches = 5
	}
	budget := &openAIFailoverBudget{
		startedAt:              startedAt,
		softDeadline:           startedAt.Add(softBudget),
		hardDeadline:           startedAt.Add(hardBudget),
		hardDeadlineReason:     "hard_retry_budget",
		maxSwitches:            maxSwitches,
		maxFirstOutputSwitches: maxFirstOutputSwitches,
		now:                    time.Now,
	}
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			clientRetryDeadline := deadline.Add(-openAIFailoverClientDeadlineReserve)
			if clientRetryDeadline.Before(budget.hardDeadline) {
				budget.hardDeadline = clientRetryDeadline
				budget.hardDeadlineReason = "client_deadline"
			}
		}
	}
	if budget.softDeadline.After(budget.hardDeadline) {
		budget.softDeadline = budget.hardDeadline
	}
	return budget
}

func (b *openAIFailoverBudget) evaluateNextAccount(
	ctx context.Context,
	failoverErr *service.UpstreamFailoverError,
	switchCount int,
	remainingCandidates int,
) openAIFailoverBudgetDecision {
	return b.evaluateRetry(ctx, failoverErr, switchCount, remainingCandidates, true)
}

func (b *openAIFailoverBudget) evaluateSameAccountRetry(
	ctx context.Context,
	switchCount int,
	remainingCandidates int,
) openAIFailoverBudgetDecision {
	return b.evaluateRetry(ctx, nil, switchCount, remainingCandidates, false)
}

func (b *openAIFailoverBudget) evaluateRetry(
	ctx context.Context,
	failoverErr *service.UpstreamFailoverError,
	switchCount int,
	remainingCandidates int,
	nextAccount bool,
) openAIFailoverBudgetDecision {
	now := time.Now()
	if b != nil && b.now != nil {
		now = b.now()
	}
	decision := openAIFailoverBudgetDecision{
		Reason:              "within_retry_budget",
		SwitchCount:         switchCount,
		RemainingCandidates: remainingCandidates,
	}
	if b == nil {
		decision.AllowNext = true
		if nextAccount {
			decision.SwitchCount = switchCount + 1
		}
		return decision
	}
	decision.ElapsedMs = max(now.Sub(b.startedAt).Milliseconds(), 0)
	decision.BudgetMs = max(b.hardDeadline.Sub(b.startedAt).Milliseconds(), 0)
	decision.SwitchLimit = b.maxSwitches
	if ctx != nil && ctx.Err() != nil {
		decision.Reason = "client_deadline"
		return decision
	}
	if !now.Before(b.hardDeadline) || b.hardDeadline.Sub(now) < openAIFailoverMinimumRetryAdmissionRemaining {
		decision.Reason = b.hardDeadlineReason
		return decision
	}
	if nextAccount && switchCount >= b.maxSwitches {
		decision.Reason = "max_account_switches"
		return decision
	}
	firstOutputTimeout := nextAccount && failoverErr != nil && string(failoverErr.Reason) == "first_output_timeout"
	if firstOutputTimeout && b.firstOutputTimeoutSwitches >= b.maxFirstOutputSwitches {
		decision.Reason = "first_output_timeout_switch_limit"
		return decision
	}
	if !now.Before(b.softDeadline) {
		if b.softBudgetFinalAttemptGranted {
			decision.Reason = "soft_retry_budget_exhausted"
			return decision
		}
		b.softBudgetFinalAttemptGranted = true
		decision.Reason = "soft_retry_budget_final_attempt"
	}
	if firstOutputTimeout {
		b.firstOutputTimeoutSwitches++
	}
	decision.AllowNext = true
	if nextAccount {
		decision.SwitchCount = switchCount + 1
	}
	return decision
}

func positiveSecondsOrDefault(value int, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func isOpenAIHighEffortRequest(body []byte) bool {
	effort := strings.TrimSpace(gjson.GetBytes(body, "reasoning.effort").String())
	if effort == "" {
		effort = strings.TrimSpace(gjson.GetBytes(body, "reasoning_effort").String())
	}
	if effort == "" {
		effort = strings.TrimSpace(gjson.GetBytes(body, "reasoningEffort").String())
	}
	switch strings.ToLower(effort) {
	case "high", "xhigh", "x-high", "max", "ultra", "ultracode":
		return true
	default:
		return false
	}
}

func openAIFailoverRemainingCandidates(candidateCount int) int {
	if candidateCount <= 0 {
		return -1
	}
	return max(candidateCount-1, 0)
}

func (h *OpenAIGatewayHandler) recordOpenAIFailoverBudgetDecision(ctx context.Context, decision openAIFailoverBudgetDecision) {
	if h == nil || h.gatewayService == nil {
		return
	}
	h.gatewayService.RecordOpenAISchedulerObservabilityRetryDecision(ctx, service.OpenAISchedulerObservabilityRetryDecision{
		Continue:            decision.AllowNext,
		Reason:              decision.Reason,
		ElapsedMs:           decision.ElapsedMs,
		BudgetMs:            decision.BudgetMs,
		SwitchCount:         decision.SwitchCount,
		SwitchLimit:         decision.SwitchLimit,
		RemainingCandidates: decision.RemainingCandidates,
	})
}
