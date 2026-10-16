//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGatewayUserPreferredAccountScheduling(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, scenario := range []string{"preferred", "other_user", "disabled", "cooldown", "excluded", "unhealthy", "cost_limit", "cost_limit_no_fallback", "cost_cap_above_group", "cost_equal"} {
			t.Run(map[bool]string{false: "legacy", true: "load_batch"}[batch]+"/"+scenario, func(t *testing.T) {
				groupID := int64(10)
				accounts := []Account{
					{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 5, Priority: 100, AccountGroups: []AccountGroup{{GroupID: groupID}}, GroupIDs: []int64{groupID}},
					{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 5, Priority: 0, AccountGroups: []AccountGroup{{GroupID: groupID}}, GroupIDs: []int64{groupID}},
				}
				group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, RateMultiplier: 1}
				userID := int64(7)
				want := int64(2)
				var excluded map[int64]struct{}
				stats := newOpenAIAccountRuntimeStats()
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
				case "unhealthy":
					stats.reportTraffic(1, "claude-sonnet-4-6", false, nil)
					stats.reportTraffic(1, "claude-sonnet-4-6", false, nil)
				case "cost_limit", "cost_limit_no_fallback", "cost_cap_above_group", "cost_equal":
					group.RateMultiplier = 0.08
					profitControlTestAccountWithRate(&accounts[0], 0.09)
					profitControlTestAccountWithRate(&accounts[1], 0.07)
				}
				if scenario == "cost_cap_above_group" {
					cap := 0.2
					group.MaxAccountCostMultiplier = &cap
				}
				if scenario == "cost_equal" {
					profitControlTestAccountWithRate(&accounts[0], 0.08)
					want = 1
				}
				if scenario == "cost_limit_no_fallback" {
					accounts[1].Status = StatusDisabled
				}
				repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{1: &accounts[0], 2: &accounts[1]}}
				cfg := testConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = batch
				svc := &GatewayService{
					accountRepo: repo, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{groupID: group}}, cfg: cfg,
					cache:              &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"existing": 2}},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
				}
				ctx := context.WithValue(context.Background(), ctxkey.Group, group)
				ctx, _ = WithGatewayTokenRequestPricing(ctx)
				ctx = withGatewaySchedulerHealthStats(ctx, stats)
				ctx = preferredAccountTestContext(ctx, &preferredAccountRepoStub{accountID: 1}, userID)
				selection, err := svc.SelectAccountWithLoadAwareness(ctx, &groupID, "existing", "claude-sonnet-4-6", excluded, "", userID)
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
					require.Equal(t, userPreferredAccountLayer, selection.SchedulerDecision.Layer)
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			})
		}
	}
}
