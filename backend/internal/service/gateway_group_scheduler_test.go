package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func gatewaySchedulerFloat64Ptr(value float64) *float64 {
	return &value
}

func gatewaySchedulerIntPtr(value int) *int {
	return &value
}

func gatewaySchedulerTestPolicy(config resolvedGroupOpenAISchedulerConfig) context.Context {
	return context.WithValue(
		context.Background(),
		gatewayGroupSchedulerPolicyContextKey{},
		gatewayGroupSchedulerPolicyContextValue{
			groupID: 1,
			policy: openAIGroupSchedulerPolicy{
				profile: GroupOpenAISchedulerProfileCustom,
				config:  config,
			},
		},
	)
}

func TestGatewaySchedulingCostFactorsSupportEveryPlatform(t *testing.T) {
	cheapRate := 0.2
	expensiveRate := 1.4
	cheap := &Account{ID: 1, Platform: PlatformAnthropic, RateMultiplier: &cheapRate}
	expensive := &Account{ID: 2, Platform: PlatformGemini, RateMultiplier: &expensiveRate}

	factors := gatewaySchedulingCostFactors([]*Account{cheap, expensive})

	require.Equal(t, 1.0, factors[cheap.ID])
	require.Equal(t, 0.0, factors[expensive.ID])
}

func TestBuildGatewayGroupSelectionOrderHonorsCostPolicy(t *testing.T) {
	groupID := int64(9)
	cheapRate := 0.2
	expensiveRate := 1.4
	cheap := &Account{ID: 1, Platform: PlatformAnthropic, Priority: 100, RateMultiplier: &cheapRate}
	expensive := &Account{ID: 2, Platform: PlatformAnthropic, Priority: 1, RateMultiplier: &expensiveRate}
	ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{
		TopK:         1,
		UpstreamCost: 10,
	})

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: expensive, loadInfo: &AccountLoadInfo{AccountID: expensive.ID}},
			{account: cheap, loadInfo: &AccountLoadInfo{AccountID: cheap.ID}},
		},
		0,
		"",
		"claude-sonnet",
	)

	require.True(t, ok)
	require.Len(t, order, 2)
	require.Equal(t, cheap.ID, order[0].account.ID)
}

func TestBuildGatewayGroupSelectionOrderHonorsLoadPolicy(t *testing.T) {
	groupID := int64(10)
	busy := &Account{ID: 1, Platform: PlatformGrok, Priority: 1}
	idle := &Account{ID: 2, Platform: PlatformGrok, Priority: 100}
	ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{
		TopK: 1,
		Load: 10,
	})

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: busy, loadInfo: &AccountLoadInfo{AccountID: busy.ID, LoadRate: 90}},
			{account: idle, loadInfo: &AccountLoadInfo{AccountID: idle.ID, LoadRate: 5}},
		},
		0,
		"",
		"grok-4",
	)

	require.True(t, ok)
	require.Equal(t, idle.ID, order[0].account.ID)
}

func TestGatewayGroupSchedulerWeightedStickyIsAWeightNotHardHit(t *testing.T) {
	groupID := int64(11)
	sticky := &Account{ID: 1, Platform: PlatformGemini, Priority: 1}
	healthy := &Account{ID: 2, Platform: PlatformGemini, Priority: 1}
	ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{
		TopK:                  1,
		Load:                  10,
		SessionSticky:         1,
		StickyWeightedEnabled: true,
	})

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: sticky, loadInfo: &AccountLoadInfo{AccountID: sticky.ID, LoadRate: 99}},
			{account: healthy, loadInfo: &AccountLoadInfo{AccountID: healthy.ID, LoadRate: 0}},
		},
		sticky.ID,
		"session",
		"gemini-2.5-pro",
	)

	require.True(t, ok)
	require.True(t, gatewayGroupSchedulerUsesWeightedSticky(ctx))
	require.Equal(t, healthy.ID, order[0].account.ID)
}

func TestGatewayGroupSchedulerKeepsLegacyFieldCompatibility(t *testing.T) {
	groupID := int64(12)
	group := &Group{
		ID:                     groupID,
		Platform:               PlatformAntigravity,
		OpenAISchedulerProfile: GroupOpenAISchedulerProfileCustom,
		OpenAISchedulerConfig: GroupOpenAISchedulerConfig{
			TopK:          gatewaySchedulerIntPtr(1),
			Priority:      gatewaySchedulerFloat64Ptr(0),
			Load:          gatewaySchedulerFloat64Ptr(0),
			Queue:         gatewaySchedulerFloat64Ptr(0),
			ErrorRate:     gatewaySchedulerFloat64Ptr(0),
			TTFT:          gatewaySchedulerFloat64Ptr(0),
			Reset:         gatewaySchedulerFloat64Ptr(0),
			QuotaHeadroom: gatewaySchedulerFloat64Ptr(0),
			UpstreamCost:  gatewaySchedulerFloat64Ptr(10),
		},
	}
	svc := &GatewayService{}

	ctx := svc.withGatewayGroupSchedulerPolicyContext(context.Background(), &groupID, group)
	policy, ok := gatewayGroupSchedulerPolicyFromContext(ctx)

	require.True(t, ok)
	require.Equal(t, 1, policy.config.TopK)
	require.Equal(t, 10.0, policy.config.UpstreamCost)
}
