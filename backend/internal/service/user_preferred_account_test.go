package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type preferredAccountRepoStub struct {
	UserPreferredAccountRepository
	accountID int64
	err       error
	calls     int
}

func (r *preferredAccountRepoStub) Get(_ context.Context, userID, groupID int64) (int64, error) {
	r.calls++
	if userID != 7 || groupID != 10 {
		return 0, nil
	}
	return r.accountID, r.err
}

func preferredAccountTestContext(ctx context.Context, repo *preferredAccountRepoStub, userID int64) context.Context {
	auth := &APIKeyService{preferredAccounts: NewUserPreferredAccountService(repo)}
	return auth.ContextWithUserAccountPreferences(ctx, userID)
}

func TestUserPreferredAccountLookupScopeAndRefresh(t *testing.T) {
	repo := &preferredAccountRepoStub{accountID: 1}
	ctx := preferredAccountTestContext(context.Background(), repo, 7)
	groupID := int64(10)
	require.Equal(t, int64(1), preferredAccountForRequest(ctx, &groupID, nil))
	require.Equal(t, int64(1), preferredAccountForRequest(ctx, &groupID, nil))
	require.Equal(t, 1, repo.calls, "retries reuse the lookup")
	require.Zero(t, preferredAccountForRequest(ctx, &groupID, map[int64]struct{}{1: {}}))
	otherGroup := int64(11)
	require.Zero(t, preferredAccountForRequest(ctx, &otherGroup, nil))
	require.Zero(t, preferredAccountForRequest(preferredAccountTestContext(context.Background(), repo, 8), &groupID, nil))
	require.Zero(t, preferredAccountForRequest(ctx, nil, nil))
	repo.accountID = 2
	lookup, ok := ctx.Value(userPreferredAccountContextKey{}).(*preferredAccountLookup)
	require.True(t, ok)
	lookup.entries[groupID] = preferredAccountLookupEntry{accountID: 1, expires: time.Now().Add(-time.Second)}
	require.Equal(t, int64(2), preferredAccountForRequest(ctx, &groupID, nil), "long-lived connections refresh configuration")
	repo.err = errors.New("database unavailable")
	require.Zero(t, preferredAccountForRequest(preferredAccountTestContext(context.Background(), repo, 7), &groupID, nil))
}

func TestOpenAIUserPreferredAccountScheduling(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, scenario := range []string{"preferred", "other_user", "disabled", "cooldown", "excluded", "busy", "removed_from_group", "unsupported_model", "unhealthy", "cost_limit", "cost_limit_no_fallback", "cost_cap_above_group", "cost_equal"} {
			t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced]+"/"+scenario, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				groupID := int64(10)
				accounts := []Account{
					{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 100, Concurrency: 1, GroupIDs: []int64{groupID}},
					{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 0, Concurrency: 1, GroupIDs: []int64{groupID}},
				}
				want := int64(2)
				userID := int64(7)
				var excluded map[int64]struct{}
				acquire := map[int64]bool{1: true, 2: true}
				ctx := context.Background()
				switch scenario {
				case "preferred":
					want = 1
				case "other_user":
					userID = 8
				case "disabled":
					accounts[0].Status = StatusDisabled
				case "cooldown":
					until := time.Now().Add(time.Hour)
					accounts[0].TempUnschedulableUntil = &until
				case "excluded":
					excluded = map[int64]struct{}{1: {}}
				case "busy":
					acquire[1] = false
				case "removed_from_group":
					accounts[0].GroupIDs = []int64{99}
				case "unsupported_model":
					accounts[0].Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
				case "cost_limit", "cost_limit_no_fallback", "cost_cap_above_group", "cost_equal":
					ctx = profitControlTestCtx(&Group{ID: groupID, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, RateMultiplier: 0.08})
					profitControlTestAccountWithRate(&accounts[0], 0.09)
					profitControlTestAccountWithRate(&accounts[1], 0.07)
				}
				if scenario == "cost_cap_above_group" {
					g, ok := ctx.Value(ctxkey.Group).(*Group)
					require.True(t, ok)
					cap := 0.2
					g.MaxAccountCostMultiplier = &cap
				}
				if scenario == "cost_equal" {
					profitControlTestAccountWithRate(&accounts[0], 0.08)
					want = 1
				}
				if scenario == "cost_limit_no_fallback" {
					accounts[1].Status = StatusDisabled
				}
				ctx = preferredAccountTestContext(ctx, &preferredAccountRepoStub{accountID: 1}, userID)
				svc := &OpenAIGatewayService{
					accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
					cfg:         &config.Config{}, cache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:existing": 2}},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: acquire}),
				}
				if advanced {
					svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true", "true")
				}
				if scenario == "unhealthy" {
					svc.ensureOpenAIAccountScheduler().ReportResult(1, "gpt-5.1", false, nil)
					svc.ensureOpenAIAccountScheduler().ReportResult(1, "gpt-5.1", false, nil)
				}
				selection, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "existing", "gpt-5.1", excluded, OpenAIUpstreamTransportAny, false)
				if scenario == "cost_limit_no_fallback" {
					require.Error(t, err)
					require.Nil(t, selection)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.True(t, selection.Acquired)
				require.Equal(t, want, selection.Account.ID)
				if want == 1 {
					require.Equal(t, userPreferredAccountLayer, decision.Layer)
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			})
		}
	}
}

func TestUserPreferredAccountDoesNotOverridePinnedRetry(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	groupID := int64(10)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{groupID}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{groupID}},
	}
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cfg: &config.Config{}, rateLimitService: newOpenAIAdvancedSchedulerRateLimitService("true")}
	ctx := preferredAccountTestContext(context.Background(), &preferredAccountRepoStub{accountID: 1}, 7)
	selection, _, err := svc.SelectPinnedAccountWithSchedulerForCapability(ctx, 2, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, "", false, false, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), selection.Account.ID)
	selection.ReleaseFunc()
}
