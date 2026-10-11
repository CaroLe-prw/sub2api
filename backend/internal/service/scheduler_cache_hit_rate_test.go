package service

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func cacheTestSnapshot(accountID int64, model string, read, total, samples int64) OpenAISchedulerHealthSnapshot {
	at := time.Now().Add(-time.Minute)
	return OpenAISchedulerHealthSnapshot{AccountID: accountID, Model: model, SuccessCount: samples,
		CacheReadTokens: read, CacheEligibleTokens: total, CacheSampleCount: samples, LastCacheSampleAt: &at}
}

func TestSchedulerCacheHitRateEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		read, total, samples int64
		known                bool
	}{
		{"high", 9000, 10000, 10, true}, {"zero_hits", 0, 10000, 10, true},
		{"too_few_calls", 9000, 10000, 4, false}, {"too_few_tokens", 900, 1000, 10, false},
		{"missing_usage", 0, 0, 10, false}, {"invalid_usage", 11000, 10000, 10, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stats := newOpenAIAccountRuntimeStats()
			stats.replaceHistory([]OpenAISchedulerHealthSnapshot{cacheTestSnapshot(1, "GPT-5.6-SOL", tc.read, tc.total, tc.samples)})
			rate, samples, factor := stats.cacheHitRateForRequest(1, "gpt-5.6-sol", time.Now())
			require.Equal(t, tc.samples, samples)
			if tc.known {
				require.NotNil(t, rate)
				expected := float64(tc.read) / float64(tc.total)
				require.InDelta(t, expected, *rate, 1e-9)
				require.InDelta(t, 0.5+(expected-0.5)*float64(tc.samples)/float64(tc.samples+5), factor, 1e-9)
			} else {
				require.Nil(t, rate)
				require.Equal(t, 0.5, factor)
			}
			for _, model := range []string{"gpt-6-astra", ""} {
				rate, samples, factor = stats.cacheHitRateForRequest(1, model, time.Now())
				require.Nil(t, rate)
				require.Zero(t, samples)
				require.Equal(t, 0.5, factor)
			}
			rate, _, factor = stats.cacheHitRateForRequest(2, "gpt-5.6-sol", time.Now())
			require.Nil(t, rate)
			require.Equal(t, 0.5, factor)
		})
	}
}

func TestSchedulerCacheHitRateExpiresAndClears(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	snapshot := cacheTestSnapshot(1, "gpt-5.6-sol", 9000, 10000, 10)
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{snapshot})
	rate, _, _ := stats.cacheHitRateForRequest(1, snapshot.Model, time.Now().Add(10*time.Minute))
	require.Nil(t, rate, "failed background refreshes must not retain cache scores forever")
	old := time.Now().Add(-31 * time.Minute)
	snapshot.LastCacheSampleAt = &old
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{snapshot})
	rate, _, _ = stats.cacheHitRateForRequest(1, snapshot.Model, time.Now())
	require.Nil(t, rate)
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{cacheTestSnapshot(1, snapshot.Model, 9000, 10000, 10)})
	stats.replaceHistory(nil)
	rate, _, _ = stats.cacheHitRateForRequest(1, snapshot.Model, time.Now())
	require.Nil(t, rate)
	stats.reportProbe(1, snapshot.Model, true, nil)
	rate, _, _ = stats.cacheHitRateForRequest(1, snapshot.Model, time.Now())
	require.Nil(t, rate, "probes cannot create cache evidence")
}

func TestSchedulerCacheHitRateConcurrentRefresh(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				stats.replaceHistory([]OpenAISchedulerHealthSnapshot{cacheTestSnapshot(1, "model", 9000, 10000, 10)})
				stats.cacheHitRateForRequest(1, "model", time.Now())
			}
		}()
	}
	wg.Wait()
	rate, _, _ := stats.cacheHitRateForRequest(1, "model", time.Now())
	require.NotNil(t, rate)
	require.Equal(t, 0.9, *rate)
}

func TestOpenAIAccountSchedulerCacheHitRatePlacementAndSticky(t *testing.T) {
	groupID := int64(1)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
	}
	stats := newOpenAIAccountRuntimeStats()
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{
		cacheTestSnapshot(1, "gpt-5.6-sol", 9000, 10000, 10), cacheTestSnapshot(2, "gpt-5.6-sol", 2000, 10000, 10),
		cacheTestSnapshot(1, "gpt-6-astra", 1000, 10000, 10), cacheTestSnapshot(2, "gpt-6-astra", 8000, 10000, 10),
	})
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.CacheHitRate = 1
	svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
	scheduler := newDefaultOpenAIAccountScheduler(svc, stats)
	for _, tc := range []struct {
		name, model string
		sticky      int64
		excluded    map[int64]struct{}
		want        int64
	}{
		{name: "new_session", model: "gpt-5.6-sol", want: 1},
		{name: "other_model", model: "gpt-6-astra", want: 2},
		{name: "healthy_sticky", model: "gpt-5.6-sol", sticky: 2, want: 2},
		{name: "failover", model: "gpt-5.6-sol", excluded: map[int64]struct{}{1: {}}, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: tc.model, ExcludedIDs: tc.excluded}
			if tc.sticky > 0 {
				req.StickyAccountID = tc.sticky
				req.SessionHash = "existing"
			}
			selected, decision, err := scheduler.Select(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, tc.want, selected.Account.ID)
			if tc.sticky > 0 {
				require.True(t, decision.StickySessionHit)
			}
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
	// A high cache rate cannot bypass the restored group-rate ceiling.
	rateCheap, rateExpensive := 0.07, 0.09
	accounts[0].RateMultiplier = &rateExpensive
	accounts[1].RateMultiplier = &rateCheap
	svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
	group := profitControlTestGroup(groupID, 0, 0)
	group.ProfitControlEnabled = false
	group.RateMultiplier = 0.08
	ctx := svc.withOpenAIProfitControlGate(profitControlTestCtx(group), &groupID)
	selected, _, err := scheduler.Select(ctx, OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: "gpt-5.6-sol"})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.Account.ID)
	if selected.ReleaseFunc != nil {
		selected.ReleaseFunc()
	}
}

func TestGatewaySchedulerCacheHitRateAndDisable(t *testing.T) {
	high := &Account{ID: 1, Platform: PlatformAnthropic, Priority: 100, Credentials: map[string]any{"model_mapping": map[string]any{"public-model": "claude-sonnet"}}}
	low := &Account{ID: 2, Platform: PlatformAnthropic, Priority: 1, Credentials: high.Credentials}
	stats := newOpenAIAccountRuntimeStats()
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{cacheTestSnapshot(1, "claude-sonnet", 9000, 10000, 10), cacheTestSnapshot(2, "claude-sonnet", 1000, 10000, 10)})
	for _, tc := range []struct {
		weight float64
		want   int64
	}{{10, 1}, {0, 2}} {
		ctx := withGatewaySchedulerHealthStats(gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{TopK: 1, Priority: 1, CacheHitRate: tc.weight}), stats)
		order, ok := buildGatewayGroupSelectionOrder(ctx, nil, []accountWithLoad{{account: low, loadInfo: &AccountLoadInfo{}}, {account: high, loadInfo: &AccountLoadInfo{}}}, 0, "", "public-model")
		require.True(t, ok)
		require.Equal(t, tc.want, order[0].account.ID)
	}
}

func TestSchedulerCacheHitRateTemplateCompatibility(t *testing.T) {
	defaults := DefaultOpenAISchedulerTemplates()
	raw, err := json.Marshal(defaults)
	require.NoError(t, err)
	var legacy map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &legacy))
	for _, template := range legacy {
		delete(template, "cache_hit_rate")
	}
	raw, err = json.Marshal(legacy)
	require.NoError(t, err)
	require.Equal(t, defaults, ParseOpenAISchedulerTemplates(string(raw)))
	legacy["cost"]["cache_hit_rate"] = 0
	raw, err = json.Marshal(legacy)
	require.NoError(t, err)
	require.Zero(t, ParseOpenAISchedulerTemplates(string(raw)).Cost.CacheHitRate)
	custom := DefaultGroupOpenAISchedulerConfig()
	for _, invalid := range []float64{-1, math.NaN(), math.Inf(1)} {
		custom.CacheHitRate = &invalid
		require.Error(t, ValidateGroupOpenAISchedulerPolicy("custom", custom))
	}
	weights := applyOpenAIAdvancedSchedulerWeightOverrides(GatewayOpenAIWSSchedulerScoreWeightsView{CacheHitRate: 1}, map[string]float64{"cache_hit_rate": 0})
	require.Zero(t, weights.CacheHitRate)
}

func TestSchedulerCachePlacementPreservesWeightedAffinity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sticky openAIAccountCandidateScore
		want   float64
	}{
		{name: "healthy", sticky: openAIAccountCandidateScore{}, want: 0},
		{name: "excluded", sticky: openAIAccountCandidateScore{excluded: true}, want: 2},
		{name: "slow", sticky: openAIAccountCandidateScore{slowDeprioritized: true}, want: 2},
		{name: "failed", sticky: openAIAccountCandidateScore{errorRate: 0.8}, want: 2},
		{name: "full", sticky: openAIAccountCandidateScore{loadInfo: &AccountLoadInfo{LoadRate: 100}}, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.sticky.account = &Account{ID: 7}
			require.Equal(t, tc.want, schedulerCachePlacementWeight(2, []openAIAccountCandidateScore{tc.sticky}, 0, 7))
		})
	}
}
