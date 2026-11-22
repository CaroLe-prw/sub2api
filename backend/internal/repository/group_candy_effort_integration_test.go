//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupCandyEffortPersistenceAndScheduledProjection(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	accounts := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	group := &service.Group{
		Name: "candy-effort-config", Platform: service.PlatformOpenAI,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
		CandyTestModel: "gpt-6.2-sol", CandyTestReasoningEffort: "high", CandyTestIntervalSeconds: 60,
	}
	require.NoError(t, repo.Create(ctx, group))
	created, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	require.Equal(t, "high", created.CandyTestReasoningEffort)
	var accountIDs []int64
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		account := mustCreateAccount(t, tx.Client(), &service.Account{Name: "effort-" + kind, Platform: service.PlatformOpenAI, Type: kind})
		mustBindAccountToGroup(t, tx.Client(), account.ID, group.ID, 1)
		accountIDs = append(accountIDs, account.ID)
	}
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max", ""} {
		group.CandyTestReasoningEffort = effort
		require.NoError(t, repo.Update(ctx, group))
		got, err := repo.GetByID(ctx, group.ID)
		require.NoError(t, err)
		require.Equal(t, effort, got.CandyTestReasoningEffort)
		scheduled, err := repo.ListCandyTestGroups(ctx)
		require.NoError(t, err)
		found := false
		for _, candidate := range scheduled {
			if candidate.ID == group.ID {
				found = true
				require.Equal(t, effort, candidate.CandyTestReasoningEffort)
				require.Equal(t, "gpt-6.2-sol", candidate.CandyTestModel)
				require.Equal(t, 60, candidate.CandyTestIntervalSeconds)
			}
		}
		require.True(t, found, "scheduled group projection must retain effort")
		for _, id := range accountIDs {
			loaded, err := accounts.GetByID(ctx, id)
			require.NoError(t, err)
			require.Len(t, loaded.Groups, 1)
			require.Equal(t, effort, loaded.Groups[0].CandyTestReasoningEffort, "manual and scheduled probes reload groups via account repository")
		}
	}
}

func TestGroupCandyEffortMigrationDefaultsToMedium(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	// Bypass the ORM so this tests the migration's SQL default, not an Ent default.
	rows, err := tx.QueryContext(ctx, `INSERT INTO groups (name, platform) VALUES ('legacy-effort-default', 'openai') RETURNING candy_test_reasoning_effort`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	require.True(t, rows.Next())
	var effort string
	require.NoError(t, rows.Scan(&effort))
	require.Equal(t, "medium", effort)
	require.NoError(t, rows.Err())
}
