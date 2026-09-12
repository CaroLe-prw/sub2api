//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayGroupModelMappingPinsTargetPriceAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			for _, target := range []string{"terra", "claude-opus-4-5", "unpriced-custom-target"} {
				t.Run(target, func(t *testing.T) {
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					svc := newGatewayRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					svc.resolver = NewModelPricingResolver(nil, svc.billingService)
					sourcePrice, targetPrice, upstreamPrice := 1e-6, 9e-6, 2e-6
					group := &Group{ID: 7, Platform: platform, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
						{Models: []string{"luna"}, BillingMode: BillingModeToken, InputPrice: &sourcePrice, OutputPrice: &sourcePrice},
						{Models: []string{"terra"}, BillingMode: BillingModeToken, InputPrice: &targetPrice, OutputPrice: &targetPrice},
						{Models: []string{"provider-terra"}, BillingMode: BillingModeToken, InputPrice: &upstreamPrice, OutputPrice: &upstreamPrice},
					}}
					mapping := ChannelMappingResult{GroupMapped: true, Mapped: true, MappedModel: target, BillingModelSource: BillingModelSourceResponse}
					err := svc.RecordUsage(context.Background(), &RecordUsageInput{
						Result: &ForwardResult{
							RequestID: "gateway-group-target", Model: target, UpstreamModel: "provider-terra", UpstreamResponseModel: "luna",
							Usage: ClaudeUsage{InputTokens: 100, OutputTokens: 20},
						},
						APIKey: &APIKey{ID: 10, GroupID: &group.ID, Group: group}, User: &User{ID: 20}, Account: &Account{ID: 30, Platform: platform},
						ChannelUsageFields: mapping.ToUsageFields("luna", "provider-terra"),
					})
					require.NoError(t, err)
					require.NotNil(t, usageRepo.lastLog)
					var expected float64
					switch target {
					case "terra":
						expected = 120 * targetPrice
					case "claude-opus-4-5":
						cost, priceErr := svc.billingService.CalculateCost(target, UsageTokens{InputTokens: 100, OutputTokens: 20}, 1)
						require.NoError(t, priceErr)
						expected = cost.TotalCost
					}
					require.InDelta(t, expected, usageRepo.lastLog.TotalCost, 1e-12, "the explicit target must override source, upstream alias, response model, and composite fallbacks")
					require.Equal(t, "luna", usageRepo.lastLog.RequestedModel)
					require.Equal(t, "provider-terra", *usageRepo.lastLog.UpstreamModel)
					require.Equal(t, "luna→"+target+"→provider-terra", *usageRepo.lastLog.ModelMappingChain)
				})
			}
		})
	}
}

func TestOpenAIGatewayGroupModelMappingPinsTargetPriceAcrossPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			sourcePrice, targetPrice, upstreamPrice := 1e-6, 9e-6, 2e-6
			group := &Group{ID: 7, Platform: platform, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{
				{Models: []string{"luna"}, BillingMode: BillingModeToken, InputPrice: &sourcePrice, OutputPrice: &sourcePrice},
				{Models: []string{"terra"}, BillingMode: BillingModeToken, InputPrice: &targetPrice, OutputPrice: &targetPrice},
				{Models: []string{"provider-terra"}, BillingMode: BillingModeToken, InputPrice: &upstreamPrice, OutputPrice: &upstreamPrice},
			}}
			mapping := ChannelMappingResult{GroupMapped: true, Mapped: true, MappedModel: "terra", BillingModelSource: BillingModelSourceResponse}
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{
					RequestID: "openai-group-target", Model: "terra", BillingModel: "provider-terra", UpstreamModel: "provider-terra", UpstreamResponseModel: "luna",
					Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 20},
				},
				APIKey: &APIKey{ID: 10, GroupID: &group.ID, Group: group}, User: &User{ID: 20}, Account: &Account{ID: 30, Platform: platform},
				ChannelUsageFields: mapping.ToUsageFields("luna", "provider-terra"),
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			require.InDelta(t, 120*targetPrice, usageRepo.lastLog.TotalCost, 1e-12)
			require.Equal(t, "luna", usageRepo.lastLog.RequestedModel)
			require.Equal(t, "provider-terra", *usageRepo.lastLog.UpstreamModel)
			require.Equal(t, "luna→terra→provider-terra", *usageRepo.lastLog.ModelMappingChain)
		})
	}
}

func TestCNGroupModelMappingKeepsExplicitBuiltinTargetPrice(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			group := &Group{ID: 7, Platform: platform, RateMultiplier: 1}
			target := "claude-opus-4-5"
			err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
				Result: &OpenAIForwardResult{RequestID: "cn-explicit-target", Model: target, UpstreamModel: "provider-target", Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 20}},
				APIKey: &APIKey{ID: 10, GroupID: &group.ID, Group: group}, User: &User{ID: 20}, Account: &Account{ID: 30, Platform: platform},
				ChannelUsageFields: ChannelUsageFields{GroupMapped: true, OriginalModel: "luna", ChannelMappedModel: target},
			})
			require.NoError(t, err)
			require.NotNil(t, usageRepo.lastLog)
			expected, err := svc.billingService.CalculateCost(target, UsageTokens{InputTokens: 100, OutputTokens: 20}, 1)
			require.NoError(t, err)
			require.InDelta(t, expected.TotalCost, usageRepo.lastLog.TotalCost, 1e-12)
		})
	}
}
