//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupCandyCandidatesPersistenceAndReload(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	accounts := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	models := "gpt-6.2-sol\ngpt-6.1-sol\n" + strings.Repeat("x", 90)
	require.Greater(t, len(models), 100, "exercise the expanded database and ORM limit")
	group := &service.Group{
		Name: "candy-candidate-config", Platform: service.PlatformOpenAI,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
		CandyTestModel: models, CandyTestReasoningEffort: "high", CandyTestIntervalSeconds: 60,
	}
	require.NoError(t, repo.Create(ctx, group))
	loaded, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, models, loaded.CandyTestModel)
	group.CandyTestModel = "gpt-6.1-sol\ngpt-6.2-sol\n" + strings.Repeat("x", 90)
	require.NoError(t, repo.Update(ctx, group))
	loaded, err = repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, group.CandyTestModel, loaded.CandyTestModel)
	groups, err := repo.ListCandyTestGroups(ctx)
	require.NoError(t, err)
	found := false
	for _, candidate := range groups {
		if candidate.ID == group.ID {
			found = true
			require.Equal(t, group.CandyTestModel, candidate.CandyTestModel)
		}
	}
	require.True(t, found)
	account := mustCreateAccount(t, tx.Client(), &service.Account{
		Name: "candy-candidates", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
	})
	mustBindAccountToGroup(t, tx.Client(), account.ID, group.ID, 1)
	loadedAccount, err := accounts.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Len(t, loadedAccount.Groups, 1)
	require.Equal(t, group.CandyTestModel, loadedAccount.Groups[0].CandyTestModel)
}
