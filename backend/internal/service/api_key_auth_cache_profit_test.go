package service

// 投影漏列回归（service 半程）：认证快照 build → L2 JSON 序列化
// → 反序列化 → 还原 apiKey.Group → 请求 ctx → 利润门解析，全链路保真。
// repository 半程（真实 GetByKeyForAuth 投影）见
// internal/repository/api_key_repo_profit_projection_integration_test.go。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func profitAuthTestAPIKey() *APIKey {
	groupID := int64(50)
	maxAccountCostMultiplier := 0.04
	return &APIKey{
		ID:                     82,
		UserID:                 40,
		GroupID:                &groupID,
		Name:                   "profit-auth-roundtrip",
		Status:                 StatusActive,
		MaxGroupRateMultiplier: 0.08,
		User: &User{
			ID:          40,
			Email:       "profit@test.local",
			Status:      StatusActive,
			Concurrency: 5,
		},
		Group: &Group{
			ID:                       groupID,
			Name:                     "VIP-roundtrip",
			Platform:                 PlatformOpenAI,
			Status:                   StatusActive,
			Hydrated:                 true,
			RequireOAuthOnly:         true,
			RateMultiplier:           0.06,
			SubscriptionType:         SubscriptionTypeStandard,
			PeakRateEnabled:          false,
			ProfitControlEnabled:     true,
			ProfitMinMargin:          0.2,
			ProfitSafetyBuffer:       0.05,
			MaxAccountCostMultiplier: &maxAccountCostMultiplier,
		},
	}
}

// 快照构建 → L2 JSON 往返 → 还原 → 装门：利润字段必须全程保真，阈值与
// 计费同源（0.06 × (1−0.25) = 0.045）。
func TestAPIKeyAuthSnapshotProfitControlRoundtrip(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.Equal(t, apiKeyAuthSnapshotVersion, snapshot.Version)
	require.Equal(t, 23, snapshot.Version, "v23 起认证快照同时携带分组长上下文、模型定价、search/audio/video 计费字段，并淘汰可能含旧用户专属倍率的快照")

	// 模拟 L2 缓存的完整 JSON 往返（与 apiKeyCache.SetAuthCache/GetAuthCache 同构）。
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	var restored APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &restored))

	materialized, used, err := svc.applyAuthCacheEntry(apiKey.Key, &restored)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.Group)
	require.True(t, materialized.Group.Hydrated)
	require.True(t, materialized.Group.RequireOAuthOnly)
	require.True(t, materialized.Group.ProfitControlEnabled)
	require.InDelta(t, 0.2, materialized.Group.ProfitMinMargin, 1e-12)
	require.InDelta(t, 0.05, materialized.Group.ProfitSafetyBuffer, 1e-12)
	require.InDelta(t, 0.06, materialized.Group.RateMultiplier, 1e-12)
	require.InDelta(t, 0.06, materialized.GroupRateMultiplier, 1e-12)
	require.InDelta(t, 0.08, materialized.MaxGroupRateMultiplier, 1e-12)
	require.NotNil(t, materialized.Group.MaxAccountCostMultiplier)
	require.InDelta(t, 0.04, *materialized.Group.MaxAccountCostMultiplier, 1e-12)

	// 中间件语义：materialized.Group 进请求 ctx → 门必须按快照配置装上。
	ctx := context.WithValue(context.Background(), ctxkey.Group, materialized.Group)
	gwSvc := &OpenAIGatewayService{}
	gate := gwSvc.resolveOpenAIProfitControlGate(ctx, materialized.GroupID)
	require.NotNil(t, gate, "还原后的认证分组必须能装门（投影漏列时本断言最先失败）")
	require.InDelta(t, 0.04, gate.threshold, 1e-12, "绝对上限应与利润门取更严格值")

	ctx = gwSvc.withGroupOAuthOnlyFilter(ctx, materialized.GroupID)
	require.False(t, accountAllowedByGroupOAuthOnlyFilter(ctx, &Account{Type: AccountTypeAPIKey}), "认证快照还原后的分组必须拒绝 API-key 账号")
}

// 旧版本快照必须被淘汰回源，不得复用。
func TestAPIKeyAuthSnapshotOldVersionEvicted(t *testing.T) {
	svc := &APIKeyService{}
	snapshot := svc.snapshotFromAPIKey(context.Background(), profitAuthTestAPIKey())
	require.NotNil(t, snapshot)
	snapshot.Version = apiKeyAuthSnapshotVersion - 1

	materialized, used, err := svc.applyAuthCacheEntry("sk-old", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.False(t, used, "版本不匹配的缓存条目必须淘汰并回源重建")
	require.Nil(t, materialized)
}
