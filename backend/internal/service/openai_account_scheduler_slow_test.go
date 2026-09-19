package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func slowSchedulerFixture(t *testing.T) (*defaultOpenAIAccountScheduler, context.Context, OpenAIAccountScheduleRequest, []*Account) {
	t.Helper()
	now := time.Now()
	cheap := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.01, now, time.Hour)
	fast := upstreamCostTestAccount(2, UpstreamBillingProbeStatusOK, 1, now, time.Hour)
	for _, a := range []*Account{cheap, fast} {
		a.Status, a.Schedulable, a.Concurrency = StatusActive, true, 1
	}
	policy, ok := resolveGroupOpenAISchedulerPreset(GroupOpenAISchedulerProfileCost)
	require.True(t, ok)
	ctx := context.WithValue(context.Background(), openAIGroupSchedulerPolicyContextKey{}, openAIGroupSchedulerPolicy{
		profile: GroupOpenAISchedulerProfileCost, config: policy,
	})
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: 15,
	}}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*cheap, *fast}}}
	s := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
	req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.6-sol", UseUpstreamTokenCost: true, SessionHash: "slow-cost", observation: &openAIAccountScheduleObservation{}}
	return s, ctx, req, []*Account{cheap, fast}
}

func TestSlowFirstOutputCostPoolPrefersHealthyAfterThreeRealCalls(t *testing.T) {
	s, ctx, req, accounts := slowSchedulerFixture(t)
	slow, fast := 57130, 1000
	s.ReportResult(2, req.RequestedModel, true, &fast)
	for i := 0; i < 3; i++ {
		s.ReportResult(1, req.RequestedModel, true, &slow)
	}
	for i := 0; i < 30; i++ {
		req.SessionHash = fmt.Sprint("session-", i)
		plan := s.buildOpenAIAccountLoadPlan(ctx, req, accounts, nil)
		require.Len(t, plan.selectionOrder, 2, "slow account remains a capacity fallback")
		require.Equal(t, int64(2), plan.selectionOrder[0].account.ID, "health must take precedence over cheap cost before TopK")
	}
}

func TestSlowFirstOutputStickyAllSlowStillServes(t *testing.T) {
	s, ctx, req, accounts := slowSchedulerFixture(t)
	s.service.cache = &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:slow-cost": 1}}
	req.StickyWeighted = true
	req.StickyAccountID = 1
	slow := 40000
	for _, a := range accounts {
		for i := 0; i < 3; i++ {
			s.ReportResult(a.ID, req.RequestedModel, true, &slow)
		}
	}
	selection, _, err := s.Select(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, selection)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSlowFirstOutputTemporaryAccountCannotKeepWinningCostPool(t *testing.T) {
	s, ctx, req, accounts := slowSchedulerFixture(t)
	third := upstreamCostTestAccount(3, UpstreamBillingProbeStatusOK, 10, time.Now(), time.Hour)
	third.Status, third.Schedulable, third.Concurrency = StatusActive, true, 1
	accounts = append(accounts, third)
	s.service.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*accounts[0], *accounts[1], *third}}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:slow-cost": 1}}
	s.service.cache = cache
	req.StickyWeighted, req.StickyAccountID = true, 1
	policy, _ := openAIGroupSchedulerPolicyFromContext(ctx)
	policy.config.TopK = 1
	ctx = context.WithValue(ctx, openAIGroupSchedulerPolicyContextKey{}, policy)
	slow := 40000
	for i := 0; i < 3; i++ {
		s.ReportResult(1, req.RequestedModel, true, &slow)
	}
	for i := 0; i < 3; i++ {
		selection, _, err := s.Select(ctx, req)
		require.NoError(t, err)
		require.EqualValues(t, 2, selection.Account.ID)
		s.ReportResult(2, req.RequestedModel, true, &slow)
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
	}
	selection, _, err := s.Select(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 3, selection.Account.ID, "third slow completion must demote the temporary account too")
	require.EqualValues(t, 1, cache.sessionBindings["openai:slow-cost"], "reproduce preserved original binding")
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSlowFirstOutputPreviousResponseMovesOnlyWhenReplayable(t *testing.T) {
	for _, canMove := range []bool{false, true} {
		t.Run(fmt.Sprint("canMove=", canMove), func(t *testing.T) {
			s, ctx, req, accounts := slowSchedulerFixture(t)
			accounts[0].Extra["openai_apikey_responses_websockets_v2_enabled"] = true
			s.service.cache = &schedulerTestGatewayCache{}
			cfg := &s.service.cfg.Gateway.OpenAIWS
			cfg.Enabled, cfg.APIKeyEnabled, cfg.ResponsesWebsocketsV2 = true, true, true
			cfg.StickyResponseIDTTLSeconds = 3600
			req.PreviousResponseID, req.PreviousResponseCanMove = "resp_slow", canMove
			require.NoError(t, s.service.getOpenAIWSStateStore().BindResponseAccount(ctx, 0, req.PreviousResponseID, 1, time.Hour))
			slow := 40000
			for i := 0; i < 3; i++ {
				s.ReportResult(1, req.RequestedModel, true, &slow)
			}
			selection, _, err := s.Select(ctx, req)
			require.NoError(t, err)
			if canMove {
				require.EqualValues(t, 2, selection.Account.ID)
			} else {
				require.EqualValues(t, 1, selection.Account.ID)
			}
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestSlowFirstOutputCountsOnlyThreeRecentMeasuredRealCalls(t *testing.T) {
	s, _, req, accounts := slowSchedulerFixture(t)
	slow, fast := 20000, 15000
	for i := 0; i < 2; i++ {
		s.ReportResult(1, req.RequestedModel, true, &slow)
	}
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded)
	// Synthetic probes and requests without usable first-token measurements
	// cannot create a three-user-request streak.
	s.stats.reportProbe(1, req.RequestedModel, true, &slow)
	s.ReportResult(1, req.RequestedModel, true, nil)
	s.ReportResult(1, req.RequestedModel, false, &slow)
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded)
	s.ReportResult(1, req.RequestedModel, true, &slow)
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded)
	other := req
	other.RequestedModel = "gpt-other"
	degraded, _ = s.slowAccountStatus(accounts[0], other)
	require.False(t, degraded, "slow model must not degrade other models")
	s.ReportResult(1, req.RequestedModel, true, &fast)
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded, "a call at the threshold breaks the streak")
	for i := 0; i < 2; i++ {
		s.ReportResult(1, req.RequestedModel, true, &slow)
	}
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded, "must require three new consecutive slow measurements")
}

func TestSlowFirstOutputUsesActualMappedModel(t *testing.T) {
	s, _, req, accounts := slowSchedulerFixture(t)
	accounts[0].Credentials = map[string]any{
		"model_mapping":         map[string]any{req.RequestedModel: "actual-model"},
		"compact_model_mapping": map[string]any{req.RequestedModel: "compact-model"},
	}
	slow := 40000
	for i := 0; i < 3; i++ {
		s.ReportResult(1, "actual-model", true, &slow)
	}
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded)
	req.RequireCompact = true
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded, "compact mapping is a different upstream model")
	for i := 0; i < 3; i++ {
		s.ReportResult(1, "compact-model", true, &slow)
	}
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded)
}

func TestSlowFirstOutputFallbackAndStickySelection(t *testing.T) {
	for _, sticky := range []bool{false, true} {
		t.Run(fmt.Sprint("sticky=", sticky), func(t *testing.T) {
			s, ctx, req, accounts := slowSchedulerFixture(t)
			req.StickyWeighted = true
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
			s.service.cache = cache
			if sticky {
				cache.sessionBindings["openai:slow-cost"] = 1
				req.StickyAccountID = 1
			}
			slow := 40000
			for i := 0; i < 3; i++ {
				s.ReportResult(1, req.RequestedModel, true, &slow)
			}
			selection, decision, err := s.Select(ctx, req)
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			if sticky {
				require.Equal(t, openAISlowFirstOutputReason, decision.StickyEscapeReason)
			}
			// Both healthy and unhealthy capacity remain available: an unavailable
			// healthy account must not turn a soft demotion into an outage.
			s.service.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: true, 2: false}})
			plan := s.buildOpenAIAccountLoadPlan(ctx, req, accounts, nil)
			selection, _, err = s.tryAcquireOpenAISelectionOrder(ctx, req, plan.selectionOrder)
			require.NoError(t, err)
			require.Equal(t, int64(1), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestSlowFirstOutputRecoveryProbeIsBoundedAndRestores(t *testing.T) {
	s, ctx, req, accounts := slowSchedulerFixture(t)
	health := &s.stats.loadOrCreateModel(1, req.RequestedModel).slowFirstOutput
	now := time.Now()
	for i := 0; i < 3; i++ {
		health.record(40000, now.Add(-2*time.Minute+time.Duration(i)*time.Second))
	}
	plan := s.buildOpenAIAccountLoadPlan(ctx, req, accounts, nil)
	require.True(t, plan.selectionOrder[0].slowRecoveryProbe)
	selection, _, err := s.tryAcquireOpenAISelectionOrder(ctx, req, plan.selectionOrder)
	require.NoError(t, err)
	require.Equal(t, int64(1), selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
	plan = s.buildOpenAIAccountLoadPlan(ctx, req, accounts, nil)
	require.Equal(t, int64(2), plan.selectionOrder[0].account.ID, "only one recovery trial per minute")
	fast := 1000
	s.ReportResult(1, req.RequestedModel, true, &fast)
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded)
}

func TestSlowFirstOutputProbeClaimIsAtomic(t *testing.T) {
	var health openAISlowFirstOutputHealth
	now := time.Now()
	for i := 0; i < 3; i++ {
		health.record(40000, now.Add(-2*time.Minute))
	}
	var successes atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if health.claimProbe(15*time.Second, now) {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
}

func TestSlowFirstOutputHistoryRecoveryAndExpiration(t *testing.T) {
	s, _, req, accounts := slowSchedulerFixture(t)
	now := time.Now()
	ms := int64(40000)
	var events []ChannelMonitorUserTrafficEvent
	for i := 0; i < 3; i++ {
		events = append(events, ChannelMonitorUserTrafficEvent{AccountID: 1, Model: req.RequestedModel, Status: "success", TTFTMs: &ms, CreatedAt: now.Add(-time.Duration(3-i) * time.Minute)})
	}
	s.stats.restoreSlowFirstOutputHistory(events)
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded, "usage logs restore the last three individual measurements")
	health := s.slowAccountHealth(accounts[0], req)
	health.recover(1000, 15*time.Second, now)
	s.stats.restoreSlowFirstOutputHistory(events)
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded, "old database samples must not undo a recovery probe")
	for i := 0; i < 3; i++ {
		health.record(40000, now.Add(time.Duration(i)*time.Second))
	}
	degraded, _ = health.status(15*time.Second, now.Add(31*time.Minute))
	require.False(t, degraded, "old samples cannot penalize an idle model forever")
}

func TestSlowFirstOutputGroupOnlyFeedbackAndProbeRecovery(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	s, _, req, accounts := slowSchedulerFixture(t)
	slow := 40000
	for i := 0; i < 3; i++ {
		s.service.ReportOpenAIAccountScheduleResult(accounts[0], req.RequestedModel, true, &slow)
	}
	s.stats = s.service.openaiAccountStats
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded, "feedback is independent of the global scheduler enable switch")
	fast := 1000
	s.service.ReportChannelMonitorProbe(1, req.RequestedModel, true, &fast)
	degraded, _ = s.slowAccountStatus(accounts[0], req)
	require.False(t, degraded)
}

func TestSlowFirstOutputObservabilityDistinguishesEscapeReasons(t *testing.T) {
	for reason, want := range map[string]string{
		"consecutive_errors":        "sticky_escaped_consecutive_errors",
		"ttft":                      "sticky_escaped_ttft",
		"error_rate":                "sticky_escaped_error_rate",
		openAISlowFirstOutputReason: "sticky_escaped_slow_first_output",
	} {
		require.Equal(t, want, schedulerObservabilitySummary(OpenAIAccountScheduleDecision{StickyEscapeReason: reason}))
	}
	candidates := []OpenAISchedulerObservabilityCandidate{{AccountID: 1, State: "deprioritized", Reason: openAISlowFirstOutputReason}, {AccountID: 2, State: "eligible"}}
	markSchedulerCandidateStates(candidates, []OpenAISchedulerObservabilityAccount{{ID: 2}}, 2)
	require.Equal(t, "deprioritized", candidates[0].State)
	require.Equal(t, "selected", candidates[1].State)
}

type slowFirstOutputHistoryRepo struct {
	UsageLogRepository
	events []ChannelMonitorUserTrafficEvent
	calls  int
}

func (r *slowFirstOutputHistoryRepo) GetOpenAISchedulerHealthSnapshots(context.Context, time.Time) ([]OpenAISchedulerHealthSnapshot, error) {
	return nil, nil
}
func (r *slowFirstOutputHistoryRepo) GetSchedulerFirstOutputEvents(context.Context, time.Time) ([]ChannelMonitorUserTrafficEvent, error) {
	r.calls++
	return r.events, nil
}

func TestSlowFirstOutputRefreshRestoresUserLogsAndThrottles(t *testing.T) {
	s, _, req, accounts := slowSchedulerFixture(t)
	ms := int64(40000)
	repo := &slowFirstOutputHistoryRepo{}
	for i := 0; i < 3; i++ {
		repo.events = append(repo.events, ChannelMonitorUserTrafficEvent{AccountID: 1, Model: req.RequestedModel, Status: "success", TTFTMs: &ms, CreatedAt: time.Now().Add(-time.Duration(3-i) * time.Second)})
	}
	s.service.usageLogRepo = repo
	s.service.RefreshOpenAISchedulerHealth(context.Background())
	s.service.RefreshOpenAISchedulerHealth(context.Background())
	require.Equal(t, 1, repo.calls)
	s.stats = s.service.openaiAccountStats
	degraded, _ := s.slowAccountStatus(accounts[0], req)
	require.True(t, degraded)
}
