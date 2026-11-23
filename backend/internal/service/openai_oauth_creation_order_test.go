//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthSamePriorityPrefersEarlierCreation(t *testing.T) {
	for _, mode := range []string{"legacy_lru", "legacy_load", "advanced"} {
		t.Run(mode, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			groupID := int64(41000)
			now := time.Now()
			previous := now.Add(-time.Hour)
			accounts := []Account{
				{ID: 41001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
					Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID},
					CreatedAt: now.Add(-time.Hour), LastUsedAt: &previous},
				{ID: 41002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
					Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID},
					CreatedAt: now.Add(-48 * time.Hour), LastUsedAt: &now},
			}
			svc := newLegacySchedulerDecisionTestService(accounts, mode != "legacy_lru", schedulerTestConcurrencyCache{
				loadMap: map[int64]*AccountLoadInfo{
					41001: {AccountID: 41001, LoadRate: 0},
					41002: {AccountID: 41002, LoadRate: 50},
				},
			})
			svc.cfg.Gateway.OpenAIWS.LBTopK = 1
			var result *AccountSelectionResult
			var err error
			if mode == "advanced" {
				result, _, err = newDefaultOpenAIAccountScheduler(svc, nil).Select(context.Background(), OpenAIAccountScheduleRequest{
					GroupID: &groupID, RequestedModel: "gpt-6.1-sol", RequiredTransport: OpenAIUpstreamTransportAny,
				})
			} else {
				result, err = svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-6.1-sol", nil)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			defer releaseLegacySchedulerDecisionSelection(result)
			require.Equal(t, int64(41002), result.Account.ID, "same-priority OAuth accounts must use creation time, not last use or load score")
		})
	}
}

func TestOpenAIOAuthCreationOrderBusyAccountFallsBack(t *testing.T) {
	for _, mode := range []string{"legacy_load", "advanced"} {
		for _, knownFull := range []bool{false, true} {
			name := "acquire_full"
			if knownFull {
				name = "known_full"
			}
			t.Run(mode+"/"+name, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				groupID := int64(41000)
				now := time.Now()
				accounts := []Account{
					{ID: 41001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
						Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID}, CreatedAt: now},
					{ID: 41002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
						Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID}, CreatedAt: now.Add(-time.Hour)},
				}
				olderLoad := &AccountLoadInfo{AccountID: 41002}
				if knownFull {
					olderLoad.LoadRate = 100
					olderLoad.CurrentConcurrency = 10
				}
				svc := newLegacySchedulerDecisionTestService(accounts, true, schedulerTestConcurrencyCache{
					loadMap:        map[int64]*AccountLoadInfo{41002: olderLoad},
					acquireResults: map[int64]bool{41002: false},
				})
				svc.cfg.Gateway.OpenAIWS.LBTopK = 1
				var result *AccountSelectionResult
				var err error
				if mode == "advanced" {
					result, _, err = newDefaultOpenAIAccountScheduler(svc, nil).Select(context.Background(), OpenAIAccountScheduleRequest{
						GroupID: &groupID, RequestedModel: "gpt-6.1-sol", RequiredTransport: OpenAIUpstreamTransportAny,
					})
				} else {
					result, err = svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-6.1-sol", nil)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				defer releaseLegacySchedulerDecisionSelection(result)
				require.Equal(t, int64(41001), result.Account.ID, "a busy older account must not hide available newer peers outside Top-K")
				require.True(t, result.Acquired)
			})
		}
	}
}

func TestOpenAIOAuthCreationOrderPreservesSchedulingPolicies(t *testing.T) {
	for _, mode := range []string{"legacy_lru", "legacy_load", "advanced"} {
		for _, policy := range []string{
			"api_keys", "mixed_account_types", "different_priorities", "disabled", "excluded",
			"unknown_creation", "equal_creation", "session_sticky", "previous_response",
			"compact_supported", "load_lookup_failure", "all_busy", "group_priority",
		} {
			if policy == "group_priority" && mode != "advanced" {
				continue // Group-specific score priorities are an advanced policy.
			}
			t.Run(mode+"/"+policy, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
				groupID := int64(41000)
				now := time.Now()
				previous := now.Add(-time.Hour)
				accounts := []Account{
					{ID: 41001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
						Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID},
						CreatedAt: now.Add(-time.Hour), LastUsedAt: &previous},
					{ID: 41002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
						Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID},
						CreatedAt: now.Add(-48 * time.Hour), LastUsedAt: &now},
				}
				cache := schedulerTestConcurrencyCache{loadMap: map[int64]*AccountLoadInfo{
					41001: {AccountID: 41001, LoadRate: 0},
					41002: {AccountID: 41002, LoadRate: 50},
				}}
				req := OpenAIAccountScheduleRequest{GroupID: &groupID, RequestedModel: "gpt-6.1-sol", RequiredTransport: OpenAIUpstreamTransportAny}
				wantID := int64(41001)
				switch policy {
				case "api_keys":
					accounts[0].Type, accounts[1].Type = AccountTypeAPIKey, AccountTypeAPIKey
				case "mixed_account_types":
					accounts[0].Type = AccountTypeAPIKey
				case "different_priorities":
					accounts[1].Priority = 10
				case "disabled":
					accounts[1].Schedulable = false
				case "excluded":
					req.ExcludedIDs = map[int64]struct{}{41002: {}}
				case "unknown_creation":
					accounts[1].CreatedAt = time.Time{}
				case "equal_creation":
					accounts[1].CreatedAt = accounts[0].CreatedAt
				case "session_sticky":
					req.SessionHash = "oauth-creation-sticky"
				case "previous_response":
					req.PreviousResponseID = "resp_oauth_creation"
					accounts[0].Extra = map[string]any{"openai_oauth_responses_websockets_v2_enabled": true}
				case "compact_supported":
					req.RequireCompact = true
					accounts[0].Extra = map[string]any{"openai_compact_mode": OpenAICompactModeForceOn}
				case "load_lookup_failure":
					cache.loadBatchErr = errors.New("load lookup unavailable")
					wantID = 41002
				case "all_busy":
					cache.acquireResults = map[int64]bool{41001: false, 41002: false}
					wantID = 41002
				case "group_priority":
					accounts[1].Priority = 100
					for i := range accounts {
						accounts[i].AccountGroups = []AccountGroup{{AccountID: accounts[i].ID, GroupID: groupID, Priority: 1}}
					}
					wantID = 41002
				}
				svc := newLegacySchedulerDecisionTestService(accounts, mode != "legacy_lru", cache)
				svc.cfg.Gateway.OpenAIWS.LBTopK = 1
				ctx := context.Background()
				if req.SessionHash != "" {
					require.NoError(t, svc.setStickySessionAccountID(ctx, &groupID, req.SessionHash, 41001, time.Hour))
				}
				if req.PreviousResponseID != "" {
					require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, req.PreviousResponseID, 41001, time.Hour))
				}
				var result *AccountSelectionResult
				var err error
				if mode == "advanced" {
					result, _, err = newDefaultOpenAIAccountScheduler(svc, nil).Select(ctx, req)
				} else {
					result, _, err = svc.SelectAccountWithScheduler(ctx, req.GroupID, req.PreviousResponseID, req.SessionHash,
						req.RequestedModel, req.ExcludedIDs, req.RequiredTransport, req.RequireCompact)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				defer releaseLegacySchedulerDecisionSelection(result)
				require.Equal(t, wantID, result.Account.ID)
				if policy == "all_busy" {
					require.False(t, result.Acquired)
					require.NotNil(t, result.WaitPlan)
				} else {
					require.True(t, result.Acquired)
				}
			})
		}
	}
}

func TestOpenAIOAuthCreationOrderSurvivesWeightedDraw(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	groupID := int64(41000)
	now := time.Now()
	accounts := []Account{
		{ID: 41001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
			Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID}, CreatedAt: now},
		{ID: 41002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
			Schedulable: true, Concurrency: 10, Priority: 1, GroupIDs: []int64{groupID}, CreatedAt: now.Add(-time.Hour)},
	}
	svc := newLegacySchedulerDecisionTestService(accounts, true, schedulerTestConcurrencyCache{})
	svc.cfg.Gateway.OpenAIWS.LBTopK = 2
	scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
	for i := 0; i < 64; i++ {
		result, _, err := scheduler.Select(context.Background(), OpenAIAccountScheduleRequest{
			GroupID: &groupID, RequestedModel: "gpt-6.1-sol", RequiredTransport: OpenAIUpstreamTransportAny,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		releaseLegacySchedulerDecisionSelection(result)
		require.True(t, result.Acquired)
		require.Equal(t, int64(41002), result.Account.ID, "weighted randomness must not undo creation order")
	}
}
