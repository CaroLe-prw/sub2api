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

func TestBuildGatewayGroupSelectionOrderUsesSharedProbeHealth(t *testing.T) {
	groupID := int64(16)
	unhealthy := &Account{ID: 1, Platform: PlatformAnthropic, Priority: 1}
	healthy := &Account{ID: 2, Platform: PlatformAnthropic, Priority: 1}
	stats := newOpenAIAccountRuntimeStats()
	stats.reportProbe(unhealthy.ID, "claude-sonnet-4-6", false, nil)
	stats.reportProbe(healthy.ID, "claude-sonnet-4-6", true, nil)
	ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{
		TopK:      1,
		ErrorRate: 10,
	})
	ctx = withGatewaySchedulerHealthStats(ctx, stats)

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: unhealthy, loadInfo: &AccountLoadInfo{AccountID: unhealthy.ID}},
			{account: healthy, loadInfo: &AccountLoadInfo{AccountID: healthy.ID}},
		},
		0,
		"",
		"claude-sonnet-4-6",
	)

	require.True(t, ok)
	require.Equal(t, healthy.ID, order[0].account.ID)
}

func TestBuildGatewayGroupSelectionOrderCapturesObservableScores(t *testing.T) {
	groupID := int64(10)
	first := &Account{ID: 1, Name: "first", Platform: PlatformGemini, Priority: 1}
	second := &Account{ID: 2, Name: "second", Platform: PlatformGemini, Priority: 2}
	ctx := gatewaySchedulerTestPolicy(resolvedGroupOpenAISchedulerConfig{
		TopK:     2,
		Priority: 1,
		Load:     1,
	})
	ctx = withGatewayScheduleObservation(ctx, 0)

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: second, loadInfo: &AccountLoadInfo{AccountID: second.ID, LoadRate: 80}},
			{account: first, loadInfo: &AccountLoadInfo{AccountID: first.ID, LoadRate: 10}},
		},
		0,
		"session",
		"gemini-2.5-pro",
	)

	require.True(t, ok)
	require.Len(t, order, 2)
	observation := gatewayScheduleObservationFromContext(ctx)
	require.NotNil(t, observation)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, observation.decision.Layer)
	require.Len(t, observation.decision.Candidates, 2)
	require.Equal(t, first.ID, observation.decision.Candidates[0].AccountID)
	require.Greater(t, observation.decision.Candidates[0].TotalScore, observation.decision.Candidates[1].TotalScore)
}

func TestBuildGatewayGroupSelectionOrderCapturesLegacyCandidates(t *testing.T) {
	groupID := int64(15)
	first := &Account{ID: 310, Name: "supeai-kiro", Platform: PlatformAnthropic, Priority: 1}
	second := &Account{ID: 311, Name: "kiro-backup", Platform: PlatformAnthropic, Priority: 2}
	ctx := withGatewayScheduleObservation(context.Background(), 0)

	order, ok := buildGatewayGroupSelectionOrder(
		ctx,
		&groupID,
		[]accountWithLoad{
			{account: first, loadInfo: &AccountLoadInfo{AccountID: first.ID, LoadRate: 10}},
			{account: second, loadInfo: &AccountLoadInfo{AccountID: second.ID, LoadRate: 20}},
		},
		0,
		"session",
		"claude-opus-4-6",
	)

	require.False(t, ok, "未启用高级评分时必须保持原调度顺序")
	require.Nil(t, order)
	observation := gatewayScheduleObservationFromContext(ctx)
	require.NotNil(t, observation)
	require.Equal(t, 2, observation.decision.CandidateCount)
	require.Len(t, observation.decision.Candidates, 2, "传统调度也应展示全部可用候选，而不是只补最终账号")
	require.Equal(t, first.ID, observation.decision.Candidates[0].AccountID)
	require.Equal(t, second.ID, observation.decision.Candidates[1].AccountID)
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
