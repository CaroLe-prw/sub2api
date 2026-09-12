//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestCompositeGroupModelMappingSelectsTargetOwningAccount(t *testing.T) {
	group := &Group{ID: 77, Platform: PlatformComposite, Status: StatusActive, Hydrated: true, ModelMapping: map[string]string{"luna": "terra", "terra": "sol"}}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	resolver := NewCompositeRouteResolver(nil)
	resolver.SetModelOwnershipResolver(func(_ context.Context, groupID int64, model string) (CompositeModelOwnership, error) {
		require.Equal(t, group.ID, groupID)
		require.Equal(t, "terra", model, "ownership must be looked up after exactly one group mapping")
		return CompositeModelOwnership{Matched: true, TargetPlatform: PlatformAnthropic}, nil
	})
	decision, err := resolver.Resolve(ctx, group.ID, "luna", CompositeRouteEndpointResponses)
	require.NoError(t, err)
	require.Equal(t, CompositeRouteSourceAccount, decision.Source)
	ctx = WithCompositeRouteDecision(ctx, decision)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 1, AccountGroups: []AccountGroup{{GroupID: group.ID}}},
			{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Priority: 2, AccountGroups: []AccountGroup{{GroupID: group.ID}}, Credentials: map[string]any{"model_mapping": map[string]any{"terra": "claude-opus-4-5"}}},
		},
		accountsByID: map[int64]*Account{},
	}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	svc := &GatewayService{accountRepo: repo, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{group.ID: group}}, cfg: testConfig()}
	// The load-aware scheduler checks support directly using middleware context;
	// the legacy scheduler reconstructs a decision before doing the same check.
	require.True(t, svc.isModelSupportedByAccountWithContext(ctx, &repo.accounts[1], "terra"))
	require.False(t, svc.isModelSupportedByAccountWithContext(ctx, &repo.accounts[0], "terra"))
	account, err := svc.SelectAccountForModelWithExclusions(ctx, &group.ID, "", "terra", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), account.ID)
	reconstructed, ok, err := svc.resolveCompositeRouteDecision(ctx, group, "terra", CompositeRouteEndpointAny)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "luna", reconstructed.PublicModel)
	require.Equal(t, "terra", reconstructed.UpstreamModel)
	otherGroup := *group
	otherGroup.ID++
	mismatchedCtx := context.WithValue(ctx, ctxkey.Group, &otherGroup)
	require.False(t, svc.isModelSupportedByAccountWithContext(mismatchedCtx, &repo.accounts[1], "terra"), "a different group's mapping must not satisfy the route ownership guard")
}
