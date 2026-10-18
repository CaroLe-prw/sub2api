package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUserPreferredAccountLatencyFallback(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, samples := range []int{1, 3} {
			for _, tc := range []struct {
				name              string
				alternative       bool
				alternativeBusy   bool
				alternativePaused bool
				overflow          bool
				preferredBusy     bool
				wantID            int64
				wantWait          bool
			}{
				{name: "only_preferred", wantID: 1},
				{name: "healthy_alternative", alternative: true, wantID: 2},
				{name: "paused_alternative", alternative: true, alternativePaused: true, wantID: 1},
				{name: "busy_alternative", alternative: true, alternativeBusy: true, wantID: 1},
				{name: "exhaust_alternatives_before_fallback", alternative: true, alternativeBusy: true, overflow: true, wantID: 3},
				{name: "keep_wait_when_all_busy", alternative: true, alternativeBusy: true, preferredBusy: true, wantID: 2, wantWait: true},
			} {
				mode := map[bool]string{false: "legacy", true: "advanced"}[advanced]
				signal := map[int]string{1: "ttft", 3: "consecutive_slow"}[samples]
				t.Run(mode+"/"+signal+"/"+tc.name, func(t *testing.T) {
					resetOpenAIAdvancedSchedulerSettingCacheForTest()
					t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
					groupID := int64(10)
					accounts := []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}}
					if tc.alternative {
						accounts = append(accounts, Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: !tc.alternativePaused, Concurrency: 1, Priority: 10, GroupIDs: []int64{groupID}})
					}
					if tc.overflow {
						accounts = append(accounts, Account{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 100, GroupIDs: []int64{groupID}})
					}
					cfg := &config.Config{}
					cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
					cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
					cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
					cfg.Gateway.OpenAIWS.LBTopK = 1
					var acquiredIDs []int64
					cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:existing": 1}}
					svc := &OpenAIGatewayService{cfg: cfg, cache: cache,
						accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
						concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{
							acquireResults: map[int64]bool{1: !tc.preferredBusy, 2: !tc.alternativeBusy}, acquiredIDs: &acquiredIDs,
						}),
					}
					if advanced {
						svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "true")
					}
					scheduler := svc.ensureOpenAIAccountScheduler()
					ttft := 20000
					for range samples {
						scheduler.ReportResult(1, "gpt-5.1", true, &ttft)
					}
					ctx := preferredAccountTestContext(profitControlTestCtx(&Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 1}), &preferredAccountRepoStub{accountID: 1}, 7)
					selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "existing", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					require.NotNil(t, selection)
					require.Equal(t, tc.wantID, selection.Account.ID)
					require.Equal(t, !tc.wantWait, selection.Acquired)
					if selection.ReleaseFunc != nil {
						t.Cleanup(selection.ReleaseFunc)
					}
					if tc.wantID == 1 {
						require.Equal(t, userPreferredAccountLayer, decision.Layer)
						if tc.alternativeBusy {
							require.NotEmpty(t, acquiredIDs)
							require.Equal(t, int64(2), acquiredIDs[0], "try another group account before the slow preference")
						}
					} else if !tc.wantWait {
						require.NotContains(t, acquiredIDs, int64(1), "an available alternative must win before fallback")
					}
				})
			}
		}
	}
}

type preferredLatencyChangingRepo struct {
	schedulerGroupAwareOpenAIAccountRepo
	beforeList func()
}

func (r *preferredLatencyChangingRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	if r.beforeList != nil {
		change := r.beforeList
		r.beforeList = nil
		change()
	}
	return r.schedulerGroupAwareOpenAIAccountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
}

func TestUserPreferredAccountLatencyFallbackRechecksCurrentEligibility(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, block := range []string{"group_rate", "disabled", "errors", "removed_from_group"} {
			t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced]+"/"+block, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				groupID := int64(10)
				account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
				profitControlTestAccountWithRate(&account, 0.07)
				repo := &preferredLatencyChangingRepo{schedulerGroupAwareOpenAIAccountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}}}
				cfg := &config.Config{}
				cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
				cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
				var acquiredIDs []int64
				svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs})}
				if advanced {
					svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "true")
				}
				scheduler := svc.ensureOpenAIAccountScheduler()
				ttft := 20000
				scheduler.ReportResult(1, "gpt-5.1", true, &ttft)
				// The first preference attempt passes all hard gates and only
				// fails latency. A state change before fallback must be rechecked.
				changed := false
				repo.beforeList = func() {
					changed = true
					switch block {
					case "group_rate":
						profitControlTestAccountWithRate(&repo.accounts[0], 0.09)
					case "disabled":
						repo.accounts[0].Schedulable = false
					case "removed_from_group":
						repo.accounts[0].GroupIDs = []int64{99}
					case "errors":
						scheduler.ReportResult(1, "gpt-5.1", false, nil)
						scheduler.ReportResult(1, "gpt-5.1", false, nil)
					}
				}
				cap := 0.2
				ctx := preferredAccountTestContext(profitControlTestCtx(&Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.08, MaxAccountCostMultiplier: &cap}), &preferredAccountRepoStub{accountID: 1}, 7)
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "existing", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.True(t, changed)
				require.Error(t, err)
				require.Nil(t, selection)
				require.Empty(t, acquiredIDs)
			})
		}
	}
}

func TestUserPreferredAccountLatencyFallbackPreservesBindingAndProfitGate(t *testing.T) {
	groupID := int64(10)
	account := Account{ID: 1, Name: "preferred", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
	profitControlTestAccountWithRate(&account, 0.07)
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"keep-binding": 999}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache, accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}}}
	stats := newOpenAIAccountRuntimeStats()
	ttft := 20000
	stats.reportTraffic(account.ID, "gpt-5.1", true, &ttft)
	cap := 0.2
	ctx := preferredAccountTestContext(profitControlTestCtx(&Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.08, MaxAccountCostMultiplier: &cap}), &preferredAccountRepoStub{accountID: account.ID}, 7)
	excluded := map[int64]struct{}{88: {}}
	req := OpenAIAccountScheduleRequest{GroupID: &groupID, Platform: PlatformOpenAI, SessionHash: "keep-binding", StickyAccountID: 999, PreserveStickyBinding: true, RequestedModel: "gpt-5.1", ExcludedIDs: excluded}
	selection, decision, err := newDefaultOpenAIAccountScheduler(svc, stats).Select(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, selection)
	t.Cleanup(selection.ReleaseFunc)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, int64(999), cache.sessionBindings["keep-binding"])
	require.Empty(t, cache.deletedSessions)
	require.Equal(t, map[int64]struct{}{88: {}}, excluded, "caller failover exclusions must remain unchanged")
	require.NotNil(t, selection.profitGate)
	require.InDelta(t, 0.08, selection.profitGate.threshold, 1e-9, "the strict group ceiling must travel with the selected account")
	require.Equal(t, userPreferredAccountLayer, decision.Layer)
	require.Equal(t, "ttft", decision.StickyEscapeReason)
	require.NotEmpty(t, decision.Candidates)
	require.Equal(t, account.ID, decision.Candidates[len(decision.Candidates)-1].AccountID)
	require.Equal(t, "selected", decision.Candidates[len(decision.Candidates)-1].State)
}

func TestUserPreferredAccountLatencyFallbackDoesNotOverrideContinuation(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			groupID := int64(10)
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{groupID}},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{groupID}},
			}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: &schedulerTestGatewayCache{}, accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}}}
			if advanced {
				svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "true")
			}
			ttft := 20000
			svc.ensureOpenAIAccountScheduler().ReportResult(1, "gpt-5.1", true, &ttft)
			ctx := preferredAccountTestContext(context.Background(), &preferredAccountRepoStub{accountID: 1}, 7)
			require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_keep_owner", 2, time.Hour))
			selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "resp_keep_owner", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
			t.Cleanup(selection.ReleaseFunc)
		})
	}
}

func TestUserPreferredAccountLatencyFallbackPreservesBindingAfterStickyEscape(t *testing.T) {
	groupID := int64(10)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}},
	}
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:keep-binding": 2}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, cache: cache,
		accountRepo:        schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: true, 2: false}}),
	}
	stats := newOpenAIAccountRuntimeStats()
	ttft := 20000
	for _, account := range accounts {
		stats.reportTraffic(account.ID, "gpt-5.1", true, &ttft)
	}
	// No profit gate: this exercises eager binding and the preservation flag
	// installed after the ordinary sticky account is also latency-escaped.
	ctx := preferredAccountTestContext(context.Background(), &preferredAccountRepoStub{accountID: 1}, 7)
	selection, _, err := newDefaultOpenAIAccountScheduler(svc, stats).Select(ctx, OpenAIAccountScheduleRequest{
		GroupID: &groupID, Platform: PlatformOpenAI, SessionHash: "keep-binding", StickyAccountID: 2, RequestedModel: "gpt-5.1",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), selection.Account.ID)
	t.Cleanup(selection.ReleaseFunc)
	require.Equal(t, map[string]int64{"openai:keep-binding": 2}, cache.sessionBindings)
	require.Empty(t, cache.deletedSessions)
}

func TestUserPreferredAccountLatencyFallbackPreservesHardBlocks(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, block := range []string{"disabled", "rate_limited", "auth_cooldown", "quota", "excluded", "errors", "model", "group_rate", "group_membership"} {
			t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced]+"/"+block, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				groupID := int64(10)
				account := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
				profitControlTestAccountWithRate(&account, 0.07)
				until := time.Now().Add(time.Hour)
				var excluded map[int64]struct{}
				switch block {
				case "disabled":
					account.Schedulable = false
				case "rate_limited":
					account.RateLimitResetAt = &until
				case "auth_cooldown":
					account.TempUnschedulableUntil = &until
				case "quota":
					account.Extra = map[string]any{"quota_limit": 1.0, "quota_used": 1.0}
				case "excluded":
					excluded = map[int64]struct{}{1: {}}
				case "model":
					account.Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
				case "group_rate":
					profitControlTestAccountWithRate(&account, 0.09)
				case "group_membership":
					account.GroupIDs = []int64{99}
				}
				cfg := &config.Config{}
				cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
				cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
				cfg.Gateway.OpenAIScheduler.StickyEscapeErrorRate = 0.5
				var acquiredIDs []int64
				svc := &OpenAIGatewayService{cfg: cfg, cache: &schedulerTestGatewayCache{},
					accountRepo:        schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquiredIDs}),
				}
				if advanced {
					svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "true")
				}
				scheduler := svc.ensureOpenAIAccountScheduler()
				ttft := 20000
				scheduler.ReportResult(1, "gpt-5.1", true, &ttft)
				if block == "errors" {
					scheduler.ReportResult(1, "gpt-5.1", false, nil)
					scheduler.ReportResult(1, "gpt-5.1", false, nil)
				}
				cap := 0.2
				ctx := preferredAccountTestContext(profitControlTestCtx(&Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.08, MaxAccountCostMultiplier: &cap}), &preferredAccountRepoStub{accountID: 1}, 7)
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "existing", "gpt-5.1", excluded, OpenAIUpstreamTransportAny, false)
				require.Error(t, err)
				require.Nil(t, selection)
				require.Empty(t, acquiredIDs, "latency fallback must never bypass a hard block")
			})
		}
	}
}
