package service

import (
	"context"
	"math"
	"sort"
	"time"
)

// gatewayGroupSchedulerPolicyContextKey keeps the resolved group policy on the
// request. The persisted/API field names remain openai_scheduler_* for backward
// compatibility, but the policy is also consumed by the multi-platform gateway.
type gatewayGroupSchedulerPolicyContextKey struct{}

type gatewayGroupSchedulerPolicyContextValue struct {
	groupID int64
	policy  openAIGroupSchedulerPolicy
}

func gatewayGroupSchedulerPolicyFromContext(ctx context.Context) (openAIGroupSchedulerPolicy, bool) {
	if ctx == nil {
		return openAIGroupSchedulerPolicy{}, false
	}
	value, ok := ctx.Value(gatewayGroupSchedulerPolicyContextKey{}).(gatewayGroupSchedulerPolicyContextValue)
	policy := value.policy
	return policy, ok &&
		policy.profile != GroupOpenAISchedulerProfileInherit &&
		policy.config.TopK > 0
}

func (s *GatewayService) withGatewayGroupSchedulerPolicyContext(
	ctx context.Context,
	groupID *int64,
	group *Group,
) context.Context {
	if value, ok := ctx.Value(gatewayGroupSchedulerPolicyContextKey{}).(gatewayGroupSchedulerPolicyContextValue); ok {
		if groupID != nil && value.groupID == *groupID {
			return ctx
		}
	}
	if group == nil && groupID != nil && *groupID > 0 && s.groupRepo != nil {
		group, _ = s.resolveGroupByID(ctx, *groupID)
	}
	if group == nil {
		return ctx
	}

	// Reuse the existing preset/template resolver so OpenAI and the generic
	// gateway read exactly the same administrator-defined policy templates.
	resolver := &OpenAIGatewayService{
		cfg:              s.cfg,
		rateLimitService: s.rateLimitService,
	}
	policy, ok := resolver.resolveOpenAIGroupSchedulerPolicy(ctx, group)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, gatewayGroupSchedulerPolicyContextKey{}, gatewayGroupSchedulerPolicyContextValue{
		groupID: group.ID,
		policy:  policy,
	})
}

func gatewayGroupSchedulerUsesWeightedSticky(ctx context.Context) bool {
	policy, ok := gatewayGroupSchedulerPolicyFromContext(ctx)
	return ok && policy.config.StickyWeightedEnabled
}

// gatewaySchedulingCostFactors normalizes the durable account billing
// multiplier for every token platform. Lower upstream cost receives a higher
// factor. Equal-cost pools remain neutral so cost weight becomes an exact no-op.
func gatewaySchedulingCostFactors(accounts []*Account) map[int64]float64 {
	factors := make(map[int64]float64, len(accounts))
	if len(accounts) == 0 {
		return factors
	}

	minRate, maxRate := math.Inf(1), math.Inf(-1)
	rates := make(map[int64]float64, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		rate := account.BillingRateMultiplier()
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			continue
		}
		rates[account.ID] = rate
		if rate < minRate {
			minRate = rate
		}
		if rate > maxRate {
			maxRate = rate
		}
	}

	for _, account := range accounts {
		if account != nil {
			factors[account.ID] = openAIUpstreamCostNeutralFactor
		}
	}
	if len(rates) < 2 || !(maxRate > minRate) {
		return factors
	}
	for accountID, rate := range rates {
		factors[accountID] = 1 - clamp01((rate-minRate)/(maxRate-minRate))
	}
	return factors
}

// buildGatewayGroupSelectionOrder applies the active group policy to generic
// gateway candidates. Platform-common signals are scored here; OpenAI-only
// runtime signals (TTFT/error/quota snapshots/previous_response) stay on the
// specialized OpenAI scheduler and are neutral for other platforms.
func buildGatewayGroupSelectionOrder(
	ctx context.Context,
	groupID *int64,
	candidates []accountWithLoad,
	stickyAccountID int64,
	sessionHash string,
	requestedModel string,
) ([]accountWithLoad, bool) {
	policy, ok := gatewayGroupSchedulerPolicyFromContext(ctx)
	if !ok || len(candidates) == 0 {
		return nil, false
	}

	scored := make([]openAIAccountCandidateScore, 0, len(candidates))
	byID := make(map[int64]accountWithLoad, len(candidates))
	accounts := make([]*Account, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.account == nil {
			continue
		}
		loadInfo := candidate.loadInfo
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: candidate.account.ID}
		}
		item := accountWithLoad{account: candidate.account, loadInfo: loadInfo}
		byID[candidate.account.ID] = item
		accounts = append(accounts, candidate.account)
		scored = append(scored, openAIAccountCandidateScore{
			account:  candidate.account,
			loadInfo: loadInfo,
			priority: openAIAccountSchedulingPriority(candidate.account, groupID),
		})
	}
	if len(scored) == 0 {
		return nil, false
	}

	minPriority, maxPriority := scored[0].priority, scored[0].priority
	maxWaiting := 1
	for i := range scored {
		if scored[i].priority < minPriority {
			minPriority = scored[i].priority
		}
		if scored[i].priority > maxPriority {
			maxPriority = scored[i].priority
		}
		if scored[i].loadInfo.WaitingCount > maxWaiting {
			maxWaiting = scored[i].loadInfo.WaitingCount
		}
	}

	now := time.Now()
	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	if policy.config.Reset > 0 {
		for _, candidate := range scored {
			end := candidate.account.SessionWindowEnd
			if end == nil || !now.Before(*end) {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	costFactors := gatewaySchedulingCostFactors(accounts)
	weights := groupOpenAISchedulerWeights(policy.config)
	for i := range scored {
		candidate := &scored[i]
		priorityFactor := 1.0
		if maxPriority > minPriority {
			priorityFactor = 1 - float64(candidate.priority-minPriority)/float64(maxPriority-minPriority)
		}
		loadFactor := 1 - clamp01(float64(candidate.loadInfo.LoadRate)/100)
		queueFactor := 1 - clamp01(float64(candidate.loadInfo.WaitingCount)/float64(maxWaiting))

		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end := candidate.account.SessionWindowEnd; end != nil && now.Before(*end) {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01(
						(end.Sub(now).Seconds()-minResetRemaining)/
							(maxResetRemaining-minResetRemaining),
					)
				} else {
					resetFactor = 1
				}
			}
		}

		costFactor := openAIUpstreamCostNeutralFactor
		if factor, exists := costFactors[candidate.account.ID]; exists {
			costFactor = factor
		}
		// Error rate, TTFT and quota headroom are deliberately neutral here:
		// the generic gateway does not currently collect equivalent snapshots.
		candidate.score =
			weights.Priority*priorityFactor +
				weights.Load*loadFactor +
				weights.Queue*queueFactor +
				weights.ErrorRate*1 +
				weights.TTFT*0.5 +
				weights.Reset*resetFactor +
				weights.QuotaHeadroom*0.5 +
				weights.UpstreamCost*(costFactor-openAIUpstreamCostNeutralFactor)
		if policy.config.StickyWeightedEnabled &&
			stickyAccountID > 0 &&
			candidate.account.ID == stickyAccountID {
			candidate.score += weights.SessionSticky
		}
	}

	topK := policy.config.TopK
	if topK > len(scored) {
		topK = len(scored)
	}
	if topK <= 0 {
		topK = 1
	}
	top := selectTopKOpenAICandidates(scored, topK)
	primary := buildOpenAIWeightedSelectionOrder(top, OpenAIAccountScheduleRequest{
		GroupID:         groupID,
		SessionHash:     sessionHash,
		RequestedModel:  requestedModel,
		StickyAccountID: stickyAccountID,
	})

	selected := make(map[int64]struct{}, len(primary))
	orderedScores := make([]openAIAccountCandidateScore, 0, len(scored))
	for _, candidate := range primary {
		orderedScores = append(orderedScores, candidate)
		selected[candidate.account.ID] = struct{}{}
	}
	overflow := make([]openAIAccountCandidateScore, 0, len(scored)-len(primary))
	for _, candidate := range scored {
		if _, exists := selected[candidate.account.ID]; !exists {
			overflow = append(overflow, candidate)
		}
	}
	sort.SliceStable(overflow, func(i, j int) bool {
		return isOpenAIAccountCandidateBetter(overflow[i], overflow[j])
	})
	orderedScores = append(orderedScores, overflow...)

	order := make([]accountWithLoad, 0, len(orderedScores))
	for _, candidate := range orderedScores {
		order = append(order, byID[candidate.account.ID])
	}
	return order, true
}

func buildGatewayGroupAccountOrder(
	ctx context.Context,
	groupID *int64,
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
	stickyAccountID int64,
	sessionHash string,
	requestedModel string,
) ([]*Account, bool) {
	candidates := make([]accountWithLoad, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadInfo := loadMap[account.ID]
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
		}
		candidates = append(candidates, accountWithLoad{account: account, loadInfo: loadInfo})
	}
	ordered, ok := buildGatewayGroupSelectionOrder(
		ctx,
		groupID,
		candidates,
		stickyAccountID,
		sessionHash,
		requestedModel,
	)
	if !ok {
		return nil, false
	}
	result := make([]*Account, 0, len(ordered))
	for _, candidate := range ordered {
		result = append(result, candidate.account)
	}
	return result, true
}
