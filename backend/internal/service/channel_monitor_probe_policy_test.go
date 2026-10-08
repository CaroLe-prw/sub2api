//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorProbeModelsChooseLowestPriceForGPT6Candidates(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{
		"gpt-5.6-sol": "gpt-5.6-sol",
		"gpt-6-astra": "gpt-6-astra",
		"gpt-6-sol":   "gpt-6-sol",
		"gpt-6.1-sol": "gpt-6.1-sol",
	}}}

	selected := selectChannelMonitorProbeModels(account, channelMonitorModelsForAccount(account), nil, NewBillingService(&config.Config{}, nil))

	// Input, output, and cache-write rates tie; GPT-6.1 Sol has cheaper reads.
	require.Equal(t, []string{"gpt-6.1-sol"}, selected)
}

func TestChannelMonitorProbeModelsIncludeCachePrices(t *testing.T) {
	billing := NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, `{
		"cheap-text-expensive-read": {
			"input_cost_per_token": 1e-6, "output_cost_per_token": 1e-6,
			"cache_read_input_token_cost": 10e-6, "cache_creation_input_token_cost": 0
		},
		"cheap-text-expensive-write": {
			"input_cost_per_token": 1e-6, "output_cost_per_token": 1e-6,
			"cache_read_input_token_cost": 0, "cache_creation_input_token_cost": 10e-6
		},
		"lowest-combined": {
			"input_cost_per_token": 2e-6, "output_cost_per_token": 2e-6,
			"cache_read_input_token_cost": 0.1e-6, "cache_creation_input_token_cost": 0.1e-6
		}
	}`))
	for _, model := range []string{"cheap-text-expensive-read", "cheap-text-expensive-write"} {
		t.Run(model, func(t *testing.T) {
			require.Equal(t, []string{"lowest-combined"}, selectChannelMonitorProbeModels(
				&Account{Platform: PlatformOpenAI}, []string{model, "lowest-combined"}, nil, billing,
			))
		})
	}
}

func TestChannelMonitorProbeModelsCompareCatalogPrices(t *testing.T) {
	billing := NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, `{
		"custom-cheap": {"input_cost_per_token": 2e-6, "output_cost_per_token": 3e-6},
		"custom-mini": {"input_cost_per_token": 1e-6, "output_cost_per_token": 20e-6},
		"custom-free": {"input_cost_per_token": 0, "output_cost_per_token": 0},
		"custom-equal": {"input_cost_per_token": 2e-6, "output_cost_per_token": 3e-6},
		"media-only": {"output_cost_per_image": 0.01}
	}`))
	tests := []struct {
		name       string
		candidates []string
		whitelist  []string
		mapping    map[string]any
		want       []string
	}{
		{
			name:       "default model does not outrank a cheaper model",
			candidates: []string{"gpt-5.4", "gpt-5.4-mini"}, want: []string{"gpt-5.4-mini"},
		},
		{
			name:       "input and output prices both count regardless of name",
			candidates: []string{"custom-mini", "custom-cheap"}, want: []string{"custom-cheap"},
		},
		{
			name:       "unknown and guessed family prices cannot outrank known prices",
			candidates: []string{"unknown-mini", "claude-madeup-haiku", "media-only", "gpt-6-astra"}, want: []string{"gpt-6-astra"},
		},
		{
			name:       "explicit zero price is free",
			candidates: []string{"custom-free", "custom-cheap"}, want: []string{"custom-free"},
		},
		{
			name:       "equal prices have stable model name ordering",
			candidates: []string{"custom-equal", "custom-cheap"}, want: []string{"custom-cheap"},
		},
		{
			name:       "explicit account models remain individually probed",
			candidates: []string{"gpt-6-astra", "gpt-6-sol"}, whitelist: []string{"gpt-6-astra", "gpt-6-sol"},
			want: []string{"gpt-6-astra", "gpt-6-sol"},
		},
		{
			name:       "wildcards still select the cheapest candidate",
			candidates: []string{"gpt-6-astra", "gpt-6-sol"}, whitelist: []string{"gpt-6*"}, want: []string{"gpt-6-sol"},
		},
		{
			name:       "mapped upstream price overrides the public alias price",
			candidates: []string{"gpt-6-astra", "gpt-6-sol"},
			mapping:    map[string]any{"gpt-6-astra": "gpt-6-sol", "gpt-6-sol": "gpt-6-astra"}, want: []string{"gpt-6-astra"},
		},
		{
			name:       "media model aliases are excluded from automatic text probes",
			candidates: []string{"custom-free", "gpt-6-astra"},
			mapping:    map[string]any{"custom-free": "gpt-image-2", "gpt-6-astra": "gpt-6-astra"}, want: []string{"gpt-6-astra"},
		},
		{
			name:       "all unknown prices retain the previous fallback",
			candidates: []string{"unknown-sol", "unknown-mini"}, want: []string{"unknown-mini"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI}
			if tt.mapping != nil {
				account.Credentials = map[string]any{"model_mapping": tt.mapping}
			}
			require.Equal(t, tt.want, selectChannelMonitorProbeModels(account, tt.candidates, tt.whitelist, billing))
		})
	}
}

func TestChannelMonitorProbeModelsRejectInvalidPrices(t *testing.T) {
	for _, price := range []*LiteLLMModelPricing{
		{InputCostPerToken: -1, OutputCostPerToken: 2},
		{InputCostPerToken: 2, OutputCostPerToken: -1},
		{InputCostPerToken: math.NaN()},
		{OutputCostPerToken: math.Inf(1)},
		{InputCostPerToken: 2, CacheReadInputTokenCost: -1},
		{InputCostPerToken: 2, CacheCreationInputTokenCost: -1},
		{CacheReadInputTokenCost: math.NaN()},
		{CacheCreationInputTokenCost: math.NaN()},
		{CacheReadInputTokenCost: math.Inf(1)},
		{CacheCreationInputTokenCost: math.Inf(1)},
	} {
		billing := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"invalid-mini": price}})
		require.Equal(t, []string{"gpt-6-astra"}, selectChannelMonitorProbeModels(
			&Account{Platform: PlatformOpenAI}, []string{"invalid-mini", "gpt-6-astra"}, nil, billing,
		))
	}
}

func TestChannelMonitorProbeModelsComparePricesWithinEachProtocolFamily(t *testing.T) {
	billing := NewBillingService(&config.Config{}, newStubPricingServiceFromJSON(t, `{
		"claude-custom-mini": {"input_cost_per_token": 9e-6, "output_cost_per_token": 9e-6},
		"claude-custom": {"input_cost_per_token": 2e-6, "output_cost_per_token": 2e-6},
		"gemini-custom-flash": {"input_cost_per_token": 3e-6, "output_cost_per_token": 3e-6},
		"gemini-custom-pro": {"input_cost_per_token": 1e-6, "output_cost_per_token": 1e-6}
	}`))
	account := &Account{Platform: PlatformAntigravity, Credentials: map[string]any{"model_mapping": map[string]any{
		"claude-custom-mini": "claude-custom-mini", "claude-custom": "claude-custom",
		"gemini-custom-flash": "gemini-custom-flash", "gemini-custom-pro": "gemini-custom-pro",
	}}}

	require.Equal(t, []string{"claude-custom", "gemini-custom-pro"}, selectChannelMonitorProbeModels(
		account, channelMonitorModelsForAccount(account), nil, billing,
	))
}

type probePricingAccountRepo struct {
	AccountRepository
	account Account
}

func (r *probePricingAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return &r.account, nil
}

func (r *probePricingAccountRepo) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	return []Account{r.account}, nil
}

type probePricingPlanRepo struct {
	ScheduledTestPlanRepository
	plans []*ScheduledTestPlan
}

func (r *probePricingPlanRepo) ReconcileChannelMonitorPlans(_ context.Context, plans []*ScheduledTestPlan) error {
	r.plans = plans
	return nil
}

func TestChannelMonitorProbePricingPreviewMatchesReconciliationAndReload(t *testing.T) {
	ctx := context.Background()
	catalog := newStubPricingServiceFromJSON(t, `{
		"custom-first": {"input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6},
		"custom-second": {"input_cost_per_token": 2e-6, "output_cost_per_token": 3e-6}
	}`)
	billing := NewBillingService(&config.Config{}, catalog)
	accounts := &probePricingAccountRepo{account: Account{
		ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"alias-first": "custom-first", "alias-second": "custom-second", "alias-excluded": "glm-4.5-flash",
		}},
	}}
	settings := &autoMonitorSettingStoreStub{values: map[string]string{
		SettingKeySchedulerProbesWhitelist: `["alias-first", "alias-second"]`,
	}}
	monitor := NewChannelMonitorService(nil, nil)
	monitor.SetAutoModelDependencies(nil, settings)
	monitor.SetChannelMonitorPoolAccountRepository(accounts)
	monitor.SetProbeModelPricing(billing)
	plans := &probePricingPlanRepo{}
	runner := &ScheduledTestRunnerService{planRepo: plans}
	runner.SetChannelMonitorPoolDependencies(accounts, settings, nil)
	runner.SetProbeModelPricing(billing)
	check := func(want ...string) {
		t.Helper()
		policy, err := monitor.GetAccountModelPolicy(ctx, 42)
		require.NoError(t, err)
		require.Equal(t, want, policy.EffectiveModels)
		require.NoError(t, runner.reconcileChannelMonitorPlans(ctx))
		actual := make([]string, 0, len(plans.plans))
		for _, plan := range plans.plans {
			actual = append(actual, plan.ModelID)
		}
		require.Equal(t, want, actual)
	}
	check("alias-first")

	// Replacing catalog entries uses the same path as a pricing hot reload.
	catalog.mu.Lock()
	catalog.pricingData["custom-first"] = &LiteLLMModelPricing{
		InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, CacheReadInputTokenCost: 10e-6, CacheCreationInputTokenCost: 20e-6,
	}
	catalog.mu.Unlock()
	check("alias-second")

	accounts.account.Extra = map[string]any{SchedulerProbeAccountModelWhitelistExtraKey: []string{"alias-first"}}
	check("alias-first")
	accounts.account.Extra = map[string]any{SchedulerProbeAccountModelWhitelistExtraKey: []string{"alias-first", "alias-second"}}
	check("alias-first", "alias-second")
	accounts.account.Extra = map[string]any{SchedulerProbeAccountModelWhitelistExtraKey: []string{"alias-*"}}
	check("alias-second")
}
