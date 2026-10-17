package service

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSchedulerTTFTRelativeDifferenceAndExploration(t *testing.T) {
	for _, test := range []struct {
		name       string
		slowerMS   float64
		minFastPct float64
		maxFastPct float64
	}{
		{name: "one_percent_difference", slowerMS: 1010, minFastPct: 0.47, maxFastPct: 0.54},
		{name: "ten_times_slower", slowerMS: 10000, minFastPct: 0.78, maxFastPct: 0.98},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidates := []openAIAccountCandidateScore{
				{account: &Account{ID: 1}},
				{account: &Account{ID: 2}},
			}
			candidates[0].setTTFT(schedulerTTFTSnapshot{ms: 1000, confidence: 1, source: "traffic"})
			candidates[1].setTTFT(schedulerTTFTSnapshot{ms: test.slowerMS, confidence: 1, source: "traffic"})
			factors := openAITTFTFactors(candidates)
			require.Greater(t, factors[0], factors[1])
			if test.slowerMS == 1010 {
				require.Less(t, factors[0]-factors[1], 0.01, "a 1%% improvement must not become a full-scale advantage")
			} else {
				require.Greater(t, factors[0]-factors[1], 0.5)
			}
			for i := range candidates {
				candidates[i].score = 3 + 2.5*factors[i]
			}
			fastFirst := 0
			const draws = 10000
			for i := 0; i < draws; i++ {
				order := buildOpenAIWeightedSelectionOrderWithTTFTBias(candidates, OpenAIAccountScheduleRequest{
					SessionHash: fmt.Sprintf("relative-ttft-%d", i), RequestedModel: "gpt-5.1",
				}, 2.5)
				if order[0].account.ID == 1 {
					fastFirst++
				}
			}
			fraction := float64(fastFirst) / draws
			require.Greater(t, fraction, test.minFastPct)
			require.Less(t, fraction, test.maxFastPct, "slower accounts must retain exploration opportunities")
		})
	}
}

func TestSchedulerTTFTWindowDecayAndExpiry(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	var window schedulerTTFTWindow
	for i := 0; i < 20; i++ {
		window.record(1000, now)
	}
	fresh := window.snapshot(now)
	aged := window.snapshot(now.Add(5 * time.Minute))
	require.Equal(t, int64(20), aged.samples)
	require.InDelta(t, fresh.effectiveSamples/2, aged.effectiveSamples, 1e-9)
	require.Equal(t, now, aged.lastSampleAt)
	require.Equal(t, float64(1000), aged.mean)
	require.Less(t, aged.snapshot(now.Add(5*time.Minute), "traffic").confidence, fresh.snapshot(now, "traffic").confidence/2)
	require.Empty(t, window.snapshot(now.Add(30*time.Minute)).snapshot(now.Add(30*time.Minute), "traffic").source)
	require.Empty(t, fresh.snapshot(now.Add(30*time.Minute), "traffic").source, "a cached snapshot must expire without a refresh")

	// Refreshing a snapshot must not refresh the underlying measurement age.
	direct := fresh.snapshot(now.Add(10*time.Minute), "traffic")
	refreshed := window.snapshot(now.Add(10*time.Minute)).snapshot(now.Add(10*time.Minute), "traffic")
	require.InDelta(t, direct.confidence, refreshed.confidence, 1e-9)
	require.Equal(t, now, refreshed.lastSampleAt)

	window.record(4000, now.Add(5*time.Minute))
	mixed := window.snapshot(now.Add(5 * time.Minute))
	require.Equal(t, int64(21), mixed.samples)
	require.InDelta(t, 14000.0/11, mixed.mean, 1e-9, "new samples must outweigh old samples after decay")
}

func TestSchedulerTTFTWindowKeepsOutOfOrderSamplesInCurrentBucket(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	latest := now.Add(20 * time.Second)
	var ordered, reversed schedulerTTFTWindow
	ordered.record(1000, now)
	ordered.record(2000, latest)
	reversed.record(2000, latest)
	reversed.record(1000, now)
	expected := ordered.snapshot(latest)
	actual := reversed.snapshotWithClock(func() time.Time { return latest })
	require.Equal(t, int64(2), actual.samples, "a newer completion must not hide earlier samples in the same minute")
	require.Equal(t, latest, actual.lastSampleAt)
	require.Equal(t, latest, actual.observedAt)
	require.InDelta(t, expected.mean, actual.mean, 1e-9)
	require.InDelta(t, expected.effectiveSamples, actual.effectiveSamples, 1e-9)

	// A completion arriving after its bucket has been reused must not erase
	// fresh measurements or resurrect expired evidence.
	reversed.record(3000, now.Add(30*time.Minute))
	reversed.record(1000, latest)
	rolled := reversed.snapshot(now.Add(30 * time.Minute))
	require.Equal(t, int64(1), rolled.samples)
	require.Equal(t, 3000.0, rolled.mean)
}

func TestSchedulerTTFTConfidenceShrinksSparseAndStaleEvidence(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	snapshot := func(count int64, sampledAt time.Time) schedulerTTFTSnapshot {
		var window schedulerTTFTWindow
		for i := int64(0); i < count; i++ {
			window.record(1000, sampledAt)
		}
		return window.snapshot(now).snapshot(now, "traffic")
	}
	sparse := snapshot(1, now)
	fresh := snapshot(100, now)
	stale := snapshot(100, now.Add(-15*time.Minute))
	require.Less(t, sparse.confidence, fresh.confidence)
	require.Less(t, stale.confidence, fresh.confidence)
	require.Less(t, stale.confidence, 0.125, "high historic volume must not preserve confidence indefinitely")
	for _, weak := range []schedulerTTFTSnapshot{sparse, stale} {
		candidates := []openAIAccountCandidateScore{{}, {}, {}}
		candidates[0].setTTFT(weak)
		candidates[1].setTTFT(schedulerTTFTSnapshot{ms: 10000, confidence: 1, source: "traffic"})
		factors := openAITTFTFactors(candidates)
		require.InDelta(t, 0.5, factors[0], 0.1)
		require.Equal(t, 0.5, factors[2], "unknown latency is neutral")
		multipliers := openAITTFTSelectionMultipliers(candidates, 2.5)
		require.Equal(t, 1.0, multipliers[2], "unknown accounts retain an unbiased exploration weight")
	}
	candidates := []openAIAccountCandidateScore{{}, {}}
	candidates[0].setTTFT(fresh)
	require.Equal(t, []float64{0.5, 0.5}, openAITTFTFactors(candidates), "one measured account is insufficient to establish relative speed")

	baseline := []openAIAccountCandidateScore{{}, {}, {}}
	baseline[0].setTTFT(schedulerTTFTSnapshot{ms: 1000, confidence: 1, source: "traffic"})
	baseline[1].setTTFT(schedulerTTFTSnapshot{ms: 1010, confidence: 1, source: "traffic"})
	before := openAITTFTFactors(baseline)
	baseline[2].setTTFT(schedulerTTFTSnapshot{ms: 1000000, confidence: 0.000001, source: "traffic"})
	after := openAITTFTFactors(baseline)
	require.InDelta(t, before[0], after[0], 0.00001, "stale outliers must not distort healthy peers")
	require.InDelta(t, before[1], after[1], 0.00001)
}

func TestSchedulerTTFTReportsRequireSuccessfulPositiveMeasurements(t *testing.T) {
	now := time.Now().Add(-time.Minute).Truncate(time.Second)
	for _, probe := range []bool{false, true} {
		t.Run(fmt.Sprintf("probe_%v", probe), func(t *testing.T) {
			stats := newOpenAIAccountRuntimeStats()
			stat := stats.loadOrCreateModel(1, "gpt-5.1")
			window := &stat.trafficTTFT
			report := stats.reportTraffic
			if probe {
				window = &stat.probeTTFT
				report = stats.reportProbe
			}
			window.record(1200, now)
			report(1, "gpt-5.1", true, nil)
			report(1, "gpt-5.1", true, intPtrForTest(0))
			report(1, "gpt-5.1", true, intPtrForTest(-1))
			report(1, "gpt-5.1", false, intPtrForTest(600))
			evidence := stats.ttftForRequest(1, "gpt-5.1", time.Now())
			require.Equal(t, int64(1), evidence.samples)
			require.Equal(t, now, evidence.lastSampleAt)
			require.InDelta(t, 1200.0, evidence.ms, 1e-9)
			report(1, "gpt-5.1", true, intPtrForTest(800))
			evidence = stats.ttftForRequest(1, "gpt-5.1", time.Now())
			require.Equal(t, int64(2), evidence.samples)
			require.True(t, evidence.lastSampleAt.After(now))
		})
	}
	var missing *openAIAccountRuntimeStats
	require.Empty(t, missing.ttftForRequest(1, "gpt-5.1", now).source)
}

func TestSchedulerTTFTSourcePrecedenceAndModelIsolation(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	stats := newOpenAIAccountRuntimeStats()
	stat := stats.loadOrCreateModel(1, "gpt-5.1")
	stat.probeTTFT.record(500, now)
	probe := stats.ttftForRequest(1, "gpt-5.1", now)
	require.Equal(t, "probe", probe.source)
	stat.trafficTTFT.record(1000, now)
	traffic := stats.ttftForRequest(1, "gpt-5.1", now)
	require.Equal(t, "traffic", traffic.source)
	require.InDelta(t, 1000.0, traffic.ms, 1e-9)
	require.InDelta(t, traffic.confidence*0.25, probe.confidence, 1e-9)
	require.Empty(t, stats.ttftForRequest(1, "gpt-5.2", now).source)
	require.Empty(t, stats.ttftForRequest(2, "gpt-5.1", now).source)
	require.Empty(t, stats.ttftForRequest(1, "", now).source)
	stat.probeTTFT.record(700, now.Add(30*time.Minute))
	require.Equal(t, "probe", stats.ttftForRequest(1, "gpt-5.1", now.Add(30*time.Minute)).source, "fresh probes take over after traffic expires")
}

func TestSchedulerTTFTHistoryDoesNotDoubleCountLiveTraffic(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	stats := newOpenAIAccountRuntimeStats()
	stat := stats.loadOrCreateModel(1, "gpt-5.1")
	for i := 0; i < 5; i++ {
		stat.trafficTTFT.record(1000, now)
	}
	mean := 1500.0
	history := OpenAISchedulerHealthSnapshot{
		AccountID: 1, Model: "gpt-5.1", SuccessCount: 20,
		TTFTSampleCount: 20, TTFTEffectiveSamples: 20,
		WeightedTTFTMs: &mean, LastTTFTSampleAt: &now, TTFTObservedAt: now,
	}
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{history})
	evidence := stats.ttftForRequest(1, "gpt-5.1", now)
	require.Equal(t, int64(5), evidence.samples, "equal timestamps prefer live evidence rather than adding overlapping counts")
	require.InDelta(t, 1000.0, evidence.ms, 1e-9)

	later := now.Add(time.Second)
	history.LastTTFTSampleAt = &later
	history.TTFTObservedAt = later
	stats.replaceHistory([]OpenAISchedulerHealthSnapshot{history})
	evidence = stats.ttftForRequest(1, "gpt-5.1", later)
	require.Equal(t, int64(20), evidence.samples)
	require.InDelta(t, 1500.0, evidence.ms, 1e-9)
	require.Equal(t, "traffic", evidence.source)
	require.Equal(t, later, evidence.lastSampleAt)
	require.Empty(t, stats.ttftForRequest(1, "gpt-5.2", later).source)

	stats.replaceHistory(nil)
	evidence = stats.ttftForRequest(1, "gpt-5.1", later)
	require.Equal(t, int64(5), evidence.samples, "removed history must not survive its generation")
	require.InDelta(t, 1000.0, evidence.ms, 1e-9)
}

func TestSchedulerTTFTWindowConcurrentRecordAndSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	var window schedulerTTFTWindow
	var wg sync.WaitGroup
	const workers, samples = 8, 1000
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < samples; i++ {
				window.record(1000, now)
				window.snapshot(now)
			}
		}()
	}
	wg.Wait()
	evidence := window.snapshot(now)
	require.Equal(t, int64(workers*samples), evidence.samples)
	require.Equal(t, float64(workers*samples), evidence.effectiveSamples)
	require.Equal(t, 1000.0, evidence.mean)
}

func TestSchedulerTTFTInvalidEvidenceIsUnknown(t *testing.T) {
	now := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	valid := schedulerTTFTEvidence{mean: 1000, samples: 10, effectiveSamples: 10, lastSampleAt: now, observedAt: now}
	for _, modify := range []func(*schedulerTTFTEvidence){
		func(e *schedulerTTFTEvidence) { e.mean = math.NaN() },
		func(e *schedulerTTFTEvidence) { e.mean = math.Inf(1) },
		func(e *schedulerTTFTEvidence) { e.effectiveSamples = math.NaN() },
		func(e *schedulerTTFTEvidence) { e.samples = 0 },
		func(e *schedulerTTFTEvidence) { e.lastSampleAt = time.Time{} },
		func(e *schedulerTTFTEvidence) { e.observedAt = now.Add(-time.Second) },
		func(e *schedulerTTFTEvidence) { e.observedAt = now.Add(time.Second) },
	} {
		invalid := valid
		modify(&invalid)
		result := invalid.snapshot(now, "traffic")
		require.Empty(t, result.source)
		require.Nil(t, result.observedMS())
		require.Nil(t, result.observedAt())
	}
}

func TestSchedulerTTFTOpenAIAndGatewayExposeCandidateEvidence(t *testing.T) {
	for _, generic := range []bool{false, true} {
		t.Run(fmt.Sprintf("generic_%v", generic), func(t *testing.T) {
			now := time.Now().Add(-time.Second)
			stats := newOpenAIAccountRuntimeStats()
			model, platform := "gpt-5.1", PlatformOpenAI
			if generic {
				model, platform = "gemini-2.5-pro", PlatformGemini
			}
			accounts := []*Account{
				{ID: 1, Name: "traffic", Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"team-alias": model}}},
				{ID: 2, Name: "probe", Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"team-alias": model}}},
				{ID: 3, Name: "unknown", Platform: platform, Type: AccountTypeAPIKey},
			}
			stats.loadOrCreateModel(1, model).trafficTTFT.record(1000, now)
			stats.loadOrCreateModel(2, model).probeTTFT.record(1010, now)
			var observed []OpenAISchedulerObservabilityCandidate
			if generic {
				ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{TopK: 3, TTFT: 2.5})
				ctx = withGatewaySchedulerHealthStats(ctx, stats)
				ctx = withGatewayScheduleObservation(ctx, 0)
				_, ok := buildGatewayGroupSelectionOrder(ctx, nil, []accountWithLoad{
					{account: accounts[0]}, {account: accounts[1]}, {account: accounts[2]},
				}, 0, "ttft-evidence", "team-alias")
				require.True(t, ok)
				observed = gatewayScheduleObservationFromContext(ctx).decision.Candidates
			} else {
				cfg := &config.Config{}
				cfg.Gateway.OpenAIWS.LBTopK = 3
				cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 2.5
				scheduler := &defaultOpenAIAccountScheduler{stats: stats, service: &OpenAIGatewayService{cfg: cfg}}
				observation := &openAIAccountScheduleObservation{}
				plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{
					RequestedModel: "team-alias", observation: observation,
				}, accounts, nil)
				require.Len(t, plan.selectionOrder, 3)
				observed = observation.candidates
			}
			require.Len(t, observed, 3)
			byID := make(map[int64]OpenAISchedulerObservabilityCandidate)
			for _, candidate := range observed {
				byID[candidate.AccountID] = candidate
			}
			for id, source := range map[int64]string{1: "traffic", 2: "probe"} {
				candidate := byID[id]
				require.Equal(t, source, candidate.TTFTSource)
				require.Equal(t, int64(1), candidate.TTFTSampleCount)
				require.NotNil(t, candidate.TTFTMs)
				require.InDelta(t, 1000+float64(id-1)*10, *candidate.TTFTMs, 1e-9)
				require.NotNil(t, candidate.TTFTLastSampleAt)
				require.True(t, now.Equal(*candidate.TTFTLastSampleAt))
				require.Positive(t, candidate.TTFTConfidence)
			}
			require.Greater(t, byID[1].TTFTConfidence, byID[2].TTFTConfidence)
			require.Less(t, math.Abs(byID[1].BaseScore-byID[2].BaseScore), 0.01)
			require.Empty(t, byID[3].TTFTSource)
			require.Nil(t, byID[3].TTFTMs)
			require.Nil(t, byID[3].TTFTLastSampleAt)
		})
	}
}
