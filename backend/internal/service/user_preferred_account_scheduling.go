package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// A user's preference cannot raise the group's effective selling-rate ceiling,
// even if an independent account-cost cap is configured above that rate.
// Carry the stricter gate with the result for the post-acquisition recheck.
func withUserPreferredAccountRateCeiling(ctx context.Context, groupID *int64) context.Context {
	if groupID == nil {
		return ctx
	}
	group := gatewayTokenRequestBillingGroupFromContext(ctx)
	if group == nil {
		group, _ = ctx.Value(ctxkey.Group).(*Group)
	}
	if group == nil || !IsGroupContextValid(group) {
		return ctx
	}
	rate, ok := authenticatedUserGroupRate(ctx, group.ID)
	if !ok {
		rate = group.RateMultiplier
	}
	pricingAt, ok := openAIPricingAtFromContext(ctx)
	if !ok {
		pricingAt, ok = gatewayTokenRequestPricingAtFromContext(ctx)
	}
	if !ok {
		pricingAt = time.Now()
	}
	threshold := clampProfitControlThreshold(rate * group.PeakMultiplierAt(pricingAt))
	gate := &openAIProfitControlGate{groupID: *groupID, platform: group.Platform, threshold: threshold, pricingAt: pricingAt, allowUnknownRate: true}
	if existing, ok := ctx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate); ok && existing != nil && existing.groupID == *groupID {
		copy := *existing
		gate = &copy
		if threshold < gate.threshold {
			gate.threshold = threshold
		}
	}
	return context.WithValue(ctx, openAIProfitControlGateCtxKey{}, gate)
}

// Preference is below hard continuation/owner affinity and above ordinary
// sticky sessions. Reuse the complete sticky admission path, including the
// fresh DB check, cost guard, transport/capability and concurrency checks.
func (s *defaultOpenAIAccountScheduler) selectUserPreferredAccount(ctx context.Context, req OpenAIAccountScheduleRequest) *AccountSelectionResult {
	id := preferredAccountForRequest(ctx, req.GroupID, req.ExcludedIDs)
	if id == 0 || req.PinnedAccountID > 0 {
		return nil
	}
	ctx = withUserPreferredAccountRateCeiling(ctx, req.GroupID)
	preferredReq := req
	preferredReq.preferredAccount = true
	preferredReq.StickyAccountID = id
	preferredReq.PreserveStickyBinding = true
	selection, _, err := s.selectBySessionHash(ctx, preferredReq)
	if err != nil || selection == nil || !selection.Acquired {
		return nil
	}
	if req.SessionHash != "" && !req.PreserveStickyBinding {
		_ = s.service.bindOpenAIStickySessionDuringSelection(ctx, req.GroupID, req.SessionHash, id)
	}
	return selection
}

func (s *GatewayService) preferredAccountProbeContext(ctx context.Context, groupID *int64, model string, excluded map[int64]struct{}) (context.Context, int64) {
	id := preferredAccountForRequest(ctx, groupID, excluded)
	if id == 0 {
		return ctx, 0
	}
	ctx = withUserPreferredAccountRateCeiling(ctx, groupID)
	account, err := s.getSchedulableAccount(ctx, id)
	if err != nil || account == nil || !account.IsSchedulable() {
		return ctx, 0
	}
	stats := gatewaySchedulerHealthStatsFromContext(ctx)
	if stats.healthGateReasonForRequest(id, accountSchedulingUpstreamModel(ctx, account, model)) != "" {
		return ctx, 0
	}
	return context.WithValue(ctx, preferredAccountProbeContextKey{}, id), id
}

func (s *GatewayService) selectUserPreferredAccount(ctx context.Context, groupID *int64, sessionHash, model string, excluded map[int64]struct{}) *AccountSelectionResult {
	probeCtx, id := s.preferredAccountProbeContext(ctx, groupID, model, excluded)
	if id == 0 {
		return nil
	}
	// The probe uses the normal selector with a single eligible account. An
	// empty session avoids overwriting an existing binding if admission fails.
	selection, err := s.SelectAccountWithLoadAwareness(probeCtx, groupID, "", model, excluded, "", 0)
	if err != nil || selection == nil {
		return nil
	}
	if !selection.Acquired || selection.Account == nil || selection.Account.ID != id {
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return nil
	}
	if !s.checkAndRegisterSession(ctx, selection.Account, sessionHash) {
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return nil
	}
	if sessionHash != "" {
		_ = s.bindGatewayStickySessionDuringSelection(ctx, groupID, sessionHash, id)
	}
	if selection.SchedulerDecision != nil {
		selection.SchedulerDecision.Layer = userPreferredAccountLayer
	}
	return selection
}
