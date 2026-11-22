//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestListCandyTestAccountsIncludesAPIKeysAndKeepsScope(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	group := mustCreateGroup(t, tx.Client(), &service.Group{Name: "candy-apikey-group", Platform: service.PlatformOpenAI})
	other := mustCreateGroup(t, tx.Client(), &service.Group{Name: "candy-apikey-other", Platform: service.PlatformOpenAI})
	var want []int64
	for _, tc := range []struct {
		name, platform, kind, status string
		blocked, deleted, otherGroup bool
		include                      bool
	}{
		{name: "oauth", platform: service.PlatformOpenAI, kind: service.AccountTypeOAuth, include: true},
		{name: "apikey", platform: service.PlatformOpenAI, kind: service.AccountTypeAPIKey, include: true},
		{name: "blocked-apikey", platform: service.PlatformOpenAI, kind: service.AccountTypeAPIKey, blocked: true, include: true},
		{name: "inactive-apikey", platform: service.PlatformOpenAI, kind: service.AccountTypeAPIKey, status: service.StatusError},
		{name: "deleted-apikey", platform: service.PlatformOpenAI, kind: service.AccountTypeAPIKey, deleted: true},
		{name: "other-group-apikey", platform: service.PlatformOpenAI, kind: service.AccountTypeAPIKey, otherGroup: true},
		{name: "anthropic-apikey", platform: service.PlatformAnthropic, kind: service.AccountTypeAPIKey},
		{name: "setup-token", platform: service.PlatformOpenAI, kind: service.AccountTypeSetupToken},
	} {
		account := mustCreateAccount(t, tx.Client(), &service.Account{
			Name: tc.name, Platform: tc.platform, Type: tc.kind, Status: tc.status,
			Extra: map[string]any{service.CandyTestBlockedExtraKey: tc.blocked},
		})
		groupID := group.ID
		if tc.otherGroup {
			groupID = other.ID
		}
		mustBindAccountToGroup(t, tx.Client(), account.ID, groupID, 1)
		if tc.deleted {
			_, err := tx.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, account.ID)
			require.NoError(t, err)
		}
		if tc.include {
			want = append(want, account.ID)
		}
	}
	accounts, err := repo.ListCandyTestAccounts(ctx, group.ID)
	require.NoError(t, err)
	var got []int64
	for _, account := range accounts {
		got = append(got, account.ID)
		require.Empty(t, account.Credentials, "periodic scan should not load account secrets")
	}
	require.Equal(t, want, got, "include active OpenAI OAuth/API Key accounts, including blocked accounts for retesting")
	require.Equal(t, true, accounts[2].Extra[service.CandyTestBlockedExtraKey])
}
