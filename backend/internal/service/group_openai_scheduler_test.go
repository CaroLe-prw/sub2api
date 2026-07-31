package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResolveGroupOpenAISchedulerPresets(t *testing.T) {
	tests := []struct {
		profile      string
		topK         int
		upstreamCost float64
		ttft         float64
	}{
		{profile: GroupOpenAISchedulerProfileSLA, topK: 2, upstreamCost: 0, ttft: 2.5},
		{profile: GroupOpenAISchedulerProfileBalanced, topK: 3, upstreamCost: 1.5, ttft: 1},
		{profile: GroupOpenAISchedulerProfileCost, topK: 2, upstreamCost: 8, ttft: 0.3},
	}
	for _, test := range tests {
		t.Run(test.profile, func(t *testing.T) {
			resolved, ok := resolveGroupOpenAISchedulerPreset(test.profile)
			require.True(t, ok)
			require.Equal(t, test.topK, resolved.TopK)
			require.Equal(t, test.upstreamCost, resolved.UpstreamCost)
			require.Equal(t, test.ttft, resolved.TTFT)
			require.True(t, resolved.StickyWeightedEnabled)
			require.False(t, resolved.SubscriptionPriorityEnabled)
		})
	}

	_, ok := resolveGroupOpenAISchedulerPreset(GroupOpenAISchedulerProfileInherit)
	require.False(t, ok)
}

func TestValidateGroupOpenAISchedulerCustomPolicy(t *testing.T) {
	valid := DefaultGroupOpenAISchedulerConfig()
	require.NoError(t, ValidateGroupOpenAISchedulerPolicy(GroupOpenAISchedulerProfileCustom, valid))
	require.NoError(t, ValidateGroupOpenAISchedulerPolicy(GroupOpenAISchedulerProfileInherit, GroupOpenAISchedulerConfig{}))

	invalidTopK := valid
	invalidTopK.TopK = groupSchedulerTestPointer(0)
	require.ErrorContains(t, ValidateGroupOpenAISchedulerPolicy(GroupOpenAISchedulerProfileCustom, invalidTopK), "top_k")

	allZero := GroupOpenAISchedulerConfig{
		TopK:          groupSchedulerTestPointer(1),
		Priority:      groupSchedulerTestPointer(0.0),
		Load:          groupSchedulerTestPointer(0.0),
		Queue:         groupSchedulerTestPointer(0.0),
		ErrorRate:     groupSchedulerTestPointer(0.0),
		TTFT:          groupSchedulerTestPointer(0.0),
		Reset:         groupSchedulerTestPointer(0.0),
		QuotaHeadroom: groupSchedulerTestPointer(0.0),
		UpstreamCost:  groupSchedulerTestPointer(0.0),
	}
	require.ErrorContains(t, ValidateGroupOpenAISchedulerPolicy(GroupOpenAISchedulerProfileCustom, allZero), "must not all be zero")

	invalidWeight := valid
	invalidWeight.Load = groupSchedulerTestPointer(math.Inf(1))
	require.ErrorContains(t, ValidateGroupOpenAISchedulerPolicy(GroupOpenAISchedulerProfileCustom, invalidWeight), "finite")

	require.ErrorContains(t, ValidateGroupOpenAISchedulerPolicy("unknown", valid), "openai_scheduler_profile")
}

func TestGroupSchedulerPolicyEnablesAdvancedSchedulerAndCostScoring(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()

	now := time.Now()
	cheap := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.03, now.Add(-time.Minute), 30*time.Minute)
	expensive := upstreamCostTestAccount(2, UpstreamBillingProbeStatusOK, 0.8, now.Add(-time.Minute), 30*time.Minute)
	for _, account := range []*Account{cheap, expensive} {
		account.Status = StatusActive
		account.Schedulable = true
		account.Concurrency = 1
	}

	groupID := int64(41)
	for _, account := range []*Account{cheap, expensive} {
		account.AccountGroups = []AccountGroup{{AccountID: account.ID, GroupID: groupID}}
	}
	custom := GroupOpenAISchedulerConfig{
		TopK:          groupSchedulerTestPointer(1),
		UpstreamCost:  groupSchedulerTestPointer(8.0),
		SessionSticky: groupSchedulerTestPointer(0.0),
	}
	groupRepo := &upstreamCostGroupRepo{group: &Group{
		ID:                     groupID,
		RateMultiplier:         1,
		OpenAISchedulerProfile: GroupOpenAISchedulerProfileCustom,
		OpenAISchedulerConfig:  custom,
	}}
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{cheap, expensive},
		accountsByID: map[int64]*Account{
			cheap.ID:     cheap,
			expensive.ID: expensive,
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*cheap, *expensive}},
		cfg:         &config.Config{},
		schedulerSnapshot: NewSchedulerSnapshotService(
			snapshotCache,
			nil,
			nil,
			groupRepo,
			nil,
		),
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}

	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-test",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, cheap.ID, selection.Account.ID)
	require.Equal(t, 1, decision.TopK)
	selection.ReleaseFunc()
}

func TestInheritedGroupSchedulerDoesNotEnableAdvancedScheduler(t *testing.T) {
	groupID := int64(42)
	groupRepo := &upstreamCostGroupRepo{group: &Group{
		ID:                     groupID,
		OpenAISchedulerProfile: GroupOpenAISchedulerProfileInherit,
	}}
	svc := &OpenAIGatewayService{
		schedulerSnapshot: NewSchedulerSnapshotService(nil, nil, nil, groupRepo, nil),
	}
	ctx := svc.withOpenAIGroupSchedulerPolicyContext(context.Background(), &groupID)
	require.False(t, hasOpenAIGroupSchedulerPolicy(ctx))
	require.Nil(t, svc.getOpenAIAccountScheduler(ctx))
}

func TestGroupSchedulerPolicyIsUsedByAdminScoreSnapshot(t *testing.T) {
	now := time.Now()
	cheap := upstreamCostTestAccount(1, UpstreamBillingProbeStatusOK, 0.03, now.Add(-time.Minute), 30*time.Minute)
	expensive := upstreamCostTestAccount(2, UpstreamBillingProbeStatusOK, 0.8, now.Add(-time.Minute), 30*time.Minute)
	groupID := int64(43)
	group := &Group{
		ID:                     groupID,
		OpenAISchedulerProfile: GroupOpenAISchedulerProfileCost,
	}
	rateLimit := &RateLimitService{cfg: &config.Config{}}

	scores := rateLimit.BuildOpenAIAccountSchedulerScoreSnapshotForGroup(
		context.Background(),
		[]*Account{cheap, expensive},
		map[int64]*AccountLoadInfo{
			cheap.ID:     {AccountID: cheap.ID},
			expensive.ID: {AccountID: expensive.ID},
		},
		&groupID,
		group,
	)

	require.Greater(t, scores[cheap.ID].BaseScore, scores[expensive.ID].BaseScore)
	require.True(t, scores[cheap.ID].StickyWeightedEnabled)
}

func TestCustomGroupSchedulerInheritsUnsetSystemWeights(t *testing.T) {
	topK := 4
	upstreamCost := 2.5
	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				OpenAIWS: config.GatewayOpenAIWSConfig{
					LBTopK: topK,
					SchedulerScoreWeights: config.GatewayOpenAIWSSchedulerScoreWeights{
						Priority:         1,
						Load:             1,
						Queue:            0.7,
						ErrorRate:        0.8,
						TTFT:             0.5,
						UpstreamCost:     0,
						PreviousResponse: 5,
						SessionSticky:    3,
					},
				},
			},
		},
	}
	group := &Group{
		OpenAISchedulerProfile: GroupOpenAISchedulerProfileCustom,
		OpenAISchedulerConfig: GroupOpenAISchedulerConfig{
			UpstreamCost:          &upstreamCost,
			StickyWeightedEnabled: true,
		},
	}

	policy, ok := svc.resolveOpenAIGroupSchedulerPolicy(context.Background(), group)
	require.True(t, ok)
	require.Equal(t, topK, policy.config.TopK)
	require.Equal(t, 0.7, policy.config.Queue)
	require.Equal(t, 5.0, policy.config.PreviousResponse)
	require.Equal(t, upstreamCost, policy.config.UpstreamCost)
}

func groupSchedulerTestPointer[T any](value T) *T {
	return &value
}
