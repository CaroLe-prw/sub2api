package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProfitControlSingleExpensiveAccountCannotBypassViaStickyOrTTFTFallback(t *testing.T) {
	for _, advanced := range []string{"true", "false"} {
		for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			for _, control := range []string{"default", "profit", "cost_cap"} {
				for _, sticky := range []bool{false, true} {
					name := advanced + "/" + accountType + "/" + control
					if sticky {
						name += "/sticky_slow"
					}
					t.Run(name, func(t *testing.T) {
						resetOpenAIAdvancedSchedulerSettingCacheForTest()
						t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
						group := profitControlTestGroup(54, 0, 0)
						group.RateMultiplier = 0.08
						if control != "profit" {
							group.ProfitControlEnabled = false
						}
						if control == "cost_cap" {
							group.MaxAccountCostMultiplier = &group.RateMultiplier
						}
						cost := 0.09
						account := Account{ID: 713, Platform: PlatformOpenAI, Type: accountType, Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{group.ID}, RateMultiplier: &cost}
						var acquisitions []int64
						cfg := &config.Config{}
						cfg.Gateway.OpenAIScheduler.StickyEscapeEnabled = true
						cfg.Gateway.OpenAIScheduler.StickyEscapeTTFTMs = 8000
						svc := &OpenAIGatewayService{
							cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
							cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:profit-single": account.ID}},
							rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(advanced),
							concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquisitions}),
							openaiAccountStats: newOpenAIAccountRuntimeStats(),
						}
						slow := 20000
						svc.openaiAccountStats.reportTraffic(account.ID, "gpt-6-astra", true, &slow)
						session := ""
						if sticky {
							session = "profit-single"
						}
						ctx := profitControlTestCtx(group)
						selection, _, err := svc.SelectAccountWithScheduler(ctx, &group.ID, "", session, "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
						require.ErrorIs(t, err, ErrNoAvailableAccounts)
						require.Nil(t, selection)
						require.Empty(t, acquisitions)
						gate := svc.resolveOpenAIProfitControlGate(ctx, &group.ID)
						require.NotNil(t, gate)
						require.InDelta(t, 0.08, gate.threshold, 1e-12)
					})
				}
			}
		}
	}
}

func TestGroupSaleRateIsDefaultCostCeiling(t *testing.T) {
	group := profitControlTestGroup(54, 0, 0)
	group.ProfitControlEnabled = false
	group.RateMultiplier = 0.08
	svc := &OpenAIGatewayService{}
	ctx, _ := svc.WithOpenAIRequestPricingContext(profitControlTestCtx(group), &group.ID)
	gate := svc.resolveOpenAIProfitControlGate(ctx, &group.ID)
	require.NotNil(t, gate)
	require.InDelta(t, 0.08, gate.threshold, 1e-12)
	for _, rate := range []float64{0.07, 0.08, 0.09} {
		blocked, _ := OpenAIProfitControlVeto(ctx, &Account{RateMultiplier: &rate})
		require.Equal(t, rate > 0.08, blocked)
	}
}

func TestDefaultCostCeilingAdminScoreUsesGroupRate(t *testing.T) {
	group := profitControlTestGroup(54, 0, 0)
	group.ProfitControlEnabled = false
	group.RateMultiplier = 0.08
	rates := []float64{0.07, 0.08, 0.09}
	accounts := make([]*Account, 0, 4)
	for i := range rates {
		accounts = append(accounts, &Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, RateMultiplier: &rates[i]})
	}
	accounts = append(accounts, &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	svc := NewRateLimitService(nil, nil, &config.Config{}, nil, nil)
	scores := svc.BuildOpenAIAccountSchedulerScoreSnapshotForGroup(context.Background(), accounts, nil, &group.ID, group)
	require.Contains(t, scores, int64(1))
	require.Contains(t, scores, int64(2))
	require.NotContains(t, scores, int64(3))
	require.Contains(t, scores, int64(4), "default guard preserves legacy unknown-rate behavior")
}
