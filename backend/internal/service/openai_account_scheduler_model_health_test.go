package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountRuntimeStats_UnknownModelDoesNotInheritAccountHealth(t *testing.T) {
	for _, source := range []string{"traffic", "probe", "history", "unattributed"} {
		t.Run(source, func(t *testing.T) {
			stats := newOpenAIAccountRuntimeStats()
			slow := 60000
			switch source {
			case "traffic":
				stats.reportTraffic(1, "gpt-6-astra", false, &slow)
				stats.reportTraffic(1, "gpt-6-astra", false, &slow)
			case "probe":
				stats.reportProbe(1, "gpt-6-astra", false, &slow)
				stats.reportProbe(1, "gpt-6-astra", false, &slow)
			case "history":
				ttft := float64(slow)
				stats.replaceHistory([]OpenAISchedulerHealthSnapshot{{AccountID: 1, Model: "gpt-6-astra", SuccessCount: 1, FailureCount: 9, AvgTTFTMs: &ttft}})
			case "unattributed":
				stats.report(1, false, &slow)
				stats.report(1, false, &slow)
			}
			for _, model := range []string{"gpt-5.6-sol", ""} {
				errorRate, ttft, measured := stats.snapshotForRequest(1, model)
				require.Zero(t, errorRate)
				require.Zero(t, ttft)
				require.False(t, measured)
				require.Empty(t, stats.healthGateReasonForRequest(1, model))
			}
		})
	}
}

func TestOpenAIAccountScheduler_StickyHealthIsModelScoped(t *testing.T) {
	for _, mode := range []string{"session", "weighted_previous", "weighted_fallback", "load_balance"} {
		for _, tc := range []struct {
			name         string
			requested    string
			effective    string
			credentials  map[string]any
			passthrough  bool
			compact      bool
			measured     bool
			otherFailure bool
		}{
			{name: "fast_model", requested: "gpt-5.6-sol", effective: "gpt-5.6-sol", measured: true},
			{name: "unknown_model", requested: "gpt-5.6-sol", effective: "gpt-5.6-sol"},
			{name: "other_model_errors", requested: "gpt-5.6-sol", effective: "gpt-5.6-sol", otherFailure: true},
			{name: "mapped_model", requested: "public-model", effective: "gpt-5.6-sol", measured: true,
				credentials: map[string]any{"model_mapping": map[string]any{"public-model": "gpt-5.6-sol"}}},
			{name: "compact_mapping", requested: "gpt-5.6-sol", effective: "gpt-5.4", measured: true, compact: true,
				credentials: map[string]any{"compact_model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.4"}}},
			{name: "passthrough_ignores_account_mapping", requested: "gpt-5.6-sol", effective: "gpt-5.6-sol", measured: true, passthrough: true,
				credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-6-astra"}}},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				groupID := int64(10161)
				sticky := Account{ID: 21651, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}, Credentials: tc.credentials,
					Extra: map[string]any{"openai_passthrough": tc.passthrough, "openai_compact_supported": true}}
				alternative := Account{ID: 21652, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 100, GroupIDs: []int64{groupID}}
				cfg := &config.Config{}
				cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
				cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
				cfg.Gateway.OpenAIWS.LBTopK = 1
				cfg.Gateway.OpenAIWS.SchedulerScoreWeights.Priority = 10
				stats := newOpenAIAccountRuntimeStats()
				slow, fast := 60000, 100
				stats.reportTraffic(sticky.ID, "gpt-6-astra", !tc.otherFailure, &slow)
				if tc.otherFailure {
					stats.reportTraffic(sticky.ID, "gpt-6-astra", false, &slow)
				}
				if tc.requested != tc.effective {
					// Old samples for the public alias/ordinary model must not be
					// blended with the model this request will actually send.
					stats.reportTraffic(sticky.ID, tc.requested, true, &slow)
				}
				if tc.measured {
					stats.reportTraffic(sticky.ID, tc.effective, true, &fast)
				}
				svc := &OpenAIGatewayService{cfg: cfg,
					accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{sticky, alternative}},
					cache:              &schedulerTestGatewayCache{},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
				}
				scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: stats}
				req := OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: tc.requested, RequireCompact: tc.compact,
					SessionHash: "model-scoped", StickyAccountID: sticky.ID}
				if mode != "session" {
					req.SessionHash, req.StickyAccountID = "", 0
				}
				if mode == "weighted_previous" || mode == "weighted_fallback" {
					req.StickyWeighted, req.PreviousResponseCanMove = true, true
					req.StickyPreviousAccountID = sticky.ID
				}
				var selection *AccountSelectionResult
				var decision OpenAIAccountScheduleDecision
				var err error
				if mode == "weighted_fallback" {
					selection, err = scheduler.tryFallbackToWeightedSticky(context.Background(), req)
				} else {
					selection, decision, err = scheduler.Select(context.Background(), req)
				}
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, sticky.ID, selection.Account.ID)
				require.Empty(t, decision.StickyEscapeReason)
				t.Cleanup(selection.ReleaseFunc)

				plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{&sticky}, nil)
				require.Len(t, plan.candidates, 1)
				require.Equal(t, tc.measured, plan.candidates[0].hasTTFT)
				if tc.measured {
					require.Equal(t, float64(fast), plan.candidates[0].ttft)
				}
			})
		}
	}
}

func TestOpenAIAccountScheduler_PrefersFastAccountForEachModel(t *testing.T) {
	groupID := int64(10162)
	accounts := []Account{
		{ID: 21661, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
		{ID: 21662, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
	}
	stats := newOpenAIAccountRuntimeStats()
	fast, slow := 100, 60000
	stats.reportTraffic(accounts[0].ID, "gpt-5.6-sol", true, &fast)
	stats.reportTraffic(accounts[0].ID, "gpt-6-astra", true, &slow)
	stats.reportTraffic(accounts[1].ID, "gpt-5.6-sol", true, &slow)
	stats.reportTraffic(accounts[1].ID, "gpt-6-astra", true, &fast)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.LBTopK = 1
	cfg.Gateway.OpenAIWS.SchedulerScoreWeights.TTFT = 1
	svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}
	scheduler := newDefaultOpenAIAccountScheduler(svc, stats)
	for model, want := range map[string]int64{"gpt-5.6-sol": accounts[0].ID, "gpt-6-astra": accounts[1].ID} {
		selection, _, err := scheduler.Select(context.Background(), OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: model})
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, want, selection.Account.ID)
		selection.ReleaseFunc()
	}
}
