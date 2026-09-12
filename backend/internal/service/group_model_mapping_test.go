//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestNormalizeGroupModelMapping(t *testing.T) {
	got, err := NormalizeGroupModelMapping(PlatformOpenAI, map[string]string{" luna ": " terra "})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"luna": "terra"}, got)
	for name, mapping := range map[string]map[string]string{
		"empty source": {"": "terra"}, "empty target": {"luna": " "},
		"duplicate": {"luna": "terra", " luna ": "sol"},
		"control":   {"lu\nna": "terra"}, "wildcard": {"luna*": "terra"},
		"long": {"luna": strings.Repeat("a", 201)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizeGroupModelMapping(PlatformOpenAI, mapping)
			require.Error(t, err)
		})
	}
	for _, platform := range []string{
		PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformComposite,
	} {
		t.Run(platform, func(t *testing.T) {
			got, err := NormalizeGroupModelMapping(platform, map[string]string{" luna ": " terra "})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"luna": "terra"}, got)
		})
	}
}

func TestGroupModelMappingAllPlatforms(t *testing.T) {
	for _, platform := range []string{
		PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformComposite,
	} {
		t.Run(platform, func(t *testing.T) {
			group := &Group{ID: 7, Platform: platform, ModelMapping: map[string]string{"luna": "terra", "terra": "sol"}}
			ctx := context.WithValue(context.Background(), ctxkey.Group, group)
			base := ChannelMappingResult{ChannelID: 3, Mapped: true, MappedModel: "channel-target", BillingModelSource: BillingModelSourceRequested}
			got := applyGroupModelMapping(ctx, &group.ID, "luna", base)
			require.True(t, got.GroupMapped)
			require.True(t, got.Mapped)
			require.Equal(t, "terra", got.MappedModel, "use one group mapping before the channel rule")
			require.Equal(t, BillingModelSourceChannelMapped, got.BillingModelSource)
			require.Equal(t, int64(3), got.ChannelID)
			require.Equal(t, base, applyGroupModelMapping(ctx, &group.ID, "unknown", base))
		})
	}
}

func TestGroupModelMappingPrecedenceAndIsolation(t *testing.T) {
	group := &Group{ID: 7, Platform: PlatformOpenAI, ModelMapping: map[string]string{"luna": "terra", "terra": "sol"}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	base := ChannelMappingResult{ChannelID: 3, Mapped: true, MappedModel: "other", BillingModelSource: BillingModelSourceRequested}
	got := applyGroupModelMapping(ctx, &group.ID, "luna", base)
	require.Equal(t, "terra", got.MappedModel, "group mappings must not recurse")
	require.Equal(t, BillingModelSourceChannelMapped, got.BillingModelSource)
	require.Equal(t, int64(3), got.ChannelID)
	require.Equal(t, base, applyGroupModelMapping(ctx, &group.ID, "unknown", base))
	other := int64(8)
	require.Equal(t, base, applyGroupModelMapping(ctx, &other, "luna", base))
	require.Equal(t, base, applyGroupModelMapping(ctx, nil, "luna", base))
	require.Equal(t, base, applyGroupModelMapping(context.Background(), &group.ID, "luna", base))
	svc := &OpenAIGatewayService{}
	resolved, _ := svc.ResolveChannelMappingAndRestrict(ctx, &group.ID, "luna")
	require.Equal(t, "terra", resolved.MappedModel, "mapping must work without a channel")
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"terra": "provider-terra"}}}
	require.False(t, account.IsModelSupported("luna"))
	require.True(t, account.IsModelSupported(resolved.MappedModel))
	require.Equal(t, "provider-terra", resolveOpenAIForwardModel(account, resolved.MappedModel, ""))
	require.Equal(t, "terra", resolved.ToUsageFields("luna", "provider-terra").ChannelMappedModel)
}

func TestAdminGroupModelMappingLifecycle(t *testing.T) {
	for _, platform := range []string{
		PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformComposite,
	} {
		t.Run(platform, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{createID: 51}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "mapped", Platform: platform, RateMultiplier: 1, ModelMapping: map[string]string{" luna ": " terra "}})
			require.NoError(t, err)
			require.Equal(t, platform, group.Platform)
			require.Equal(t, map[string]string{"luna": "terra"}, group.ModelMapping)
			repo.getByID = group
			_, err = svc.UpdateGroup(context.Background(), group.ID, &UpdateGroupInput{Description: ptrString("updated")})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"luna": "terra"}, repo.updated.ModelMapping)
			cloned := cloneGroupModelMapping(group.ModelMapping)
			cloned["luna"] = "sol"
			require.Equal(t, "terra", group.ModelMapping["luna"])
			_, err = svc.UpdateGroup(context.Background(), group.ID, &UpdateGroupInput{ModelMapping: map[string]string{" luna ": " sol "}})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"luna": "sol"}, repo.updated.ModelMapping)
			_, err = svc.UpdateGroup(context.Background(), group.ID, &UpdateGroupInput{ModelMapping: map[string]string{}})
			require.NoError(t, err)
			require.Empty(t, repo.updated.ModelMapping)
		})
	}
}

func TestGroupModelMappingBillsTargetGroupPrice(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	lunaPrice, terraPrice := 1e-6, 9e-6
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1, ModelMapping: map[string]string{"luna": "terra"}, ModelPricing: []ChannelModelPricing{
		{Models: []string{"luna"}, BillingMode: BillingModeToken, InputPrice: &lunaPrice, OutputPrice: &lunaPrice},
		{Models: []string{"terra"}, BillingMode: BillingModeToken, InputPrice: &terraPrice, OutputPrice: &terraPrice},
	}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	mapping, _ := svc.ResolveChannelMappingAndRestrict(ctx, &group.ID, "luna")
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "group-model-mapping", Model: "terra", BillingModel: "provider-terra", UpstreamModel: "provider-terra", Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 20}},
		APIKey: &APIKey{ID: 10, GroupID: &group.ID, Group: group}, User: &User{ID: 20}, Account: &Account{ID: 30, Platform: PlatformOpenAI},
		ChannelUsageFields: mapping.ToUsageFields("luna", "provider-terra"),
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 120*terraPrice, usageRepo.lastLog.TotalCost, 1e-12)
	require.Equal(t, "luna", usageRepo.lastLog.RequestedModel)
	require.Equal(t, "luna→terra→provider-terra", *usageRepo.lastLog.ModelMappingChain)
}

func TestGroupModelMappingAuthCacheRoundTrip(t *testing.T) {
	svc := &APIKeyService{}
	group := &Group{ID: 7, Platform: PlatformOpenAI, ModelMapping: map[string]string{"luna": "terra"}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), &APIKey{ID: 1, GroupID: &group.ID, Group: group, User: &User{ID: 2}})
	require.NotNil(t, snapshot)
	require.Equal(t, group.ModelMapping, snapshot.Group.ModelMapping)
	restored := svc.snapshotToAPIKey("test", snapshot)
	require.Equal(t, group.ModelMapping, restored.Group.ModelMapping)
}

func TestGroupModelMappingDoesNotFallBackToSourcePrice(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	sourcePrice := 1e-6
	group := &Group{ID: 7, Platform: PlatformOpenAI, RateMultiplier: 1, ModelMapping: map[string]string{"luna": "unpriced-custom-target"}, ModelPricing: []ChannelModelPricing{{Models: []string{"luna"}, BillingMode: BillingModeToken, InputPrice: &sourcePrice, OutputPrice: &sourcePrice}}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	mapping, _ := svc.ResolveChannelMappingAndRestrict(ctx, &group.ID, "luna")
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "missing-target-price", Model: "luna", BillingModel: "luna", UpstreamModel: "unpriced-custom-target", Usage: OpenAIUsage{InputTokens: 100, OutputTokens: 20}},
		APIKey: &APIKey{ID: 10, GroupID: &group.ID, Group: group}, User: &User{ID: 20}, Account: &Account{ID: 30, Platform: PlatformOpenAI},
		ChannelUsageFields: mapping.ToUsageFields("luna", "unpriced-custom-target"),
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Zero(t, usageRepo.lastLog.TotalCost, "missing target pricing must use the existing missing-price path, never the source price")
}
