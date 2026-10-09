package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountScheduler_StickyTTFTFallback(t *testing.T) {
	const stickyID, alternativeID int64 = 21151, 21152
	for _, mode := range []string{"session", "weighted_session", "weighted_previous", "subscription_priority", "compact"} {
		for _, tc := range []struct {
			name                string
			alternative         bool
			alternativeBusy     bool
			alternativePaused   bool
			overflowAlternative bool
			stickyBusy          bool
			wantID              int64
		}{
			{name: "only_account", wantID: stickyID},
			{name: "prefer_available_alternative", alternative: true, wantID: alternativeID},
			{name: "alternative_paused", alternative: true, alternativePaused: true, wantID: stickyID},
			{name: "alternative_busy", alternative: true, alternativeBusy: true, wantID: stickyID},
			{name: "only_account_busy_can_wait", stickyBusy: true, wantID: stickyID},
			{name: "alternative_outside_topk", alternative: true, alternativeBusy: true, overflowAlternative: true, wantID: alternativeID + 1},
			{name: "all_busy_keep_alternative_wait_plan", alternative: true, alternativeBusy: true, stickyBusy: true, wantID: alternativeID},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				groupID := int64(10151)
				accounts := []Account{{ID: stickyID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}}
				if tc.alternative {
					accounts = append(accounts, Account{ID: alternativeID, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: !tc.alternativePaused, Concurrency: 1, Priority: 100, GroupIDs: []int64{groupID}})
				}
				if tc.overflowAlternative {
					accounts = append(accounts, Account{ID: alternativeID + 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 200, GroupIDs: []int64{groupID}})
				}
				if mode == "subscription_priority" {
					accounts[0].Type = AccountTypeOAuth
					accounts[0].Credentials = map[string]any{"plan_type": "plus"}
				}
				if mode == "compact" {
					accounts[0].Extra = map[string]any{"openai_compact_supported": true}
				}
				cfg := &config.Config{}
				cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
				cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
				cfg.Gateway.OpenAIWS.LBTopK = 1
				cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"session-ttft-fallback": stickyID}}
				var acquiredIDs []int64
				svc := &OpenAIGatewayService{
					cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: cache,
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
						acquireResults: map[int64]bool{stickyID: !tc.stickyBusy, alternativeID: !tc.alternativeBusy},
						acquiredIDs:    &acquiredIDs,
					}),
				}
				stats := newOpenAIAccountRuntimeStats()
				ttft := 20000
				stats.reportTraffic(stickyID, "gpt-6-astra", true, &ttft)
				scheduler := newDefaultOpenAIAccountScheduler(svc, stats)
				req := OpenAIAccountScheduleRequest{GroupID: &groupID, SessionHash: "session-ttft-fallback", StickyAccountID: stickyID, RequestedModel: "gpt-6-astra"}
				if mode == "weighted_session" || mode == "weighted_previous" {
					req.StickyWeighted = true
				}
				req.SubscriptionPriority = mode == "subscription_priority"
				req.RequireCompact = mode == "compact"
				if mode == "weighted_previous" {
					req.SessionHash, req.StickyAccountID = "", 0
					req.StickyPreviousAccountID, req.PreviousResponseCanMove = stickyID, true
				}
				selection, decision, err := scheduler.Select(context.Background(), req)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, tc.wantID, selection.Account.ID)
				require.Equal(t, "ttft", decision.StickyEscapeReason)
				require.Equal(t, stickyID, cache.sessionBindings["session-ttft-fallback"])
				if tc.stickyBusy {
					require.False(t, selection.Acquired)
					require.NotNil(t, selection.WaitPlan)
					require.Equal(t, tc.wantID, selection.WaitPlan.AccountID)
				} else {
					require.True(t, selection.Acquired)
					require.Nil(t, selection.WaitPlan)
					t.Cleanup(selection.ReleaseFunc)
				}
				if tc.alternative && !tc.alternativePaused {
					require.NotEmpty(t, acquiredIDs)
					require.Equal(t, alternativeID, acquiredIDs[0], "try the alternative before the slow sticky account, even with TopK=1")
				}
				for _, candidate := range decision.Candidates {
					if candidate.AccountID == stickyID {
						require.Equal(t, "deprioritized", candidate.State)
						require.Equal(t, "ttft", candidate.Reason)
					}
				}
				if mode == "session" {
					store := NewOpenAISchedulerObservabilityStore()
					ctx := schedulerObservabilityTestContext("ttft-fallback", &Group{ID: groupID})
					store.RecordSelection(ctx, req, decision, selection, nil)
					traces := store.Snapshot(OpenAISchedulerObservabilityQuery{TimeRange: "1h", View: "requests", Page: 1, PageSize: 20}).Traces
					require.Len(t, traces, 1)
					require.Equal(t, "pending", traces[0].Status)
					require.NotContains(t, schedulerObservabilityAttemptKinds(traces[0].Attempts), "selection_failed")
					for _, candidate := range traces[0].Candidates {
						if candidate.AccountID == stickyID {
							wantState := "deprioritized"
							if tc.wantID == stickyID {
								wantState = "selected"
							}
							require.Equal(t, wantState, candidate.State)
						}
					}
				}
			})
		}
	}
}

func TestOpenAIAccountScheduler_StickyTTFTFallbackPreservesBlocks(t *testing.T) {
	for _, block := range []string{"disabled", "rate_limited", "auth_cooldown", "quota_exhausted", "excluded", "consecutive_errors", "error_rate"} {
		t.Run(block, func(t *testing.T) {
			groupID := int64(10152)
			account := Account{ID: 21153, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
			until := time.Now().Add(time.Hour)
			req := OpenAIAccountScheduleRequest{GroupID: &groupID, SessionHash: "blocked-ttft", StickyAccountID: account.ID, RequestedModel: "gpt-6-astra"}
			stats := newOpenAIAccountRuntimeStats()
			ttft := 20000
			stats.reportTraffic(account.ID, req.RequestedModel, true, &ttft)
			switch block {
			case "disabled":
				account.Schedulable = false
			case "rate_limited":
				account.RateLimitResetAt = &until
			case "auth_cooldown":
				account.TempUnschedulableUntil = &until
			case "quota_exhausted":
				account.Extra = map[string]any{"quota_limit": 10.0, "quota_used": 10.0}
			case "excluded":
				req.ExcludedIDs = map[int64]struct{}{account.ID: {}}
			case "consecutive_errors", "error_rate":
				for range 6 {
					stats.reportTraffic(account.ID, req.RequestedModel, false, nil)
				}
				if block == "error_rate" {
					stats.reportTraffic(account.ID, req.RequestedModel, true, nil)
				}
			}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
			cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
			cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
			var acquiredIDs []int64
			svc := &OpenAIGatewayService{
				cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
				cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{req.SessionHash: account.ID}},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs}),
			}
			selection, _, err := newDefaultOpenAIAccountScheduler(svc, stats).Select(context.Background(), req)
			require.ErrorIs(t, err, ErrNoAvailableAccounts)
			require.Nil(t, selection)
			require.Empty(t, acquiredIDs, "latency fallback must not attempt a blocked account")
		})
	}
}
