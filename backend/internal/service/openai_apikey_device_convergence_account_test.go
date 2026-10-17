package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIAPIKeyDeviceConvergenceRequiresExplicitBooleanOptIn(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform string
		typ      string
		extra    map[string]any
		want     bool
	}{
		{name: "default off", platform: PlatformOpenAI, typ: AccountTypeAPIKey},
		{name: "explicitly enabled", platform: PlatformOpenAI, typ: AccountTypeAPIKey, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true}, want: true},
		{name: "explicitly disabled", platform: PlatformOpenAI, typ: AccountTypeAPIKey, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: false}},
		{name: "string true is not enabled", platform: PlatformOpenAI, typ: AccountTypeAPIKey, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: "true"}},
		{name: "numeric truthy is not enabled", platform: PlatformOpenAI, typ: AccountTypeAPIKey, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: 1}},
		{name: "other platform", platform: PlatformAnthropic, typ: AccountTypeAPIKey, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true}},
		{name: "oauth ignores apikey switch", platform: PlatformOpenAI, typ: AccountTypeOAuth, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true}},
		{name: "setup token ignores apikey switch", platform: PlatformOpenAI, typ: AccountTypeSetupToken, extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: tt.platform, Type: tt.typ, Extra: tt.extra}
			require.Equal(t, tt.want, account.IsOpenAIAPIKeyDeviceConvergenceEnabled())
			wantMode := codexFingerprintOff
			if tt.want {
				wantMode = codexFingerprintDevice
			}
			require.Equal(t, wantMode, account.GetCodexFingerprintMode())
			prepared := prepareCodexFingerprintExtraForCreate(tt.platform, tt.typ, tt.extra)
			_, hasSeed := codexFingerprintSeed(prepared)
			require.Equal(t, tt.want, hasSeed)
		})
	}
	var account *Account
	require.False(t, account.IsOpenAIAPIKeyDeviceConvergenceEnabled())
}

func TestOpenAIAPIKeyDeviceConvergenceIgnoresOAuthModes(t *testing.T) {
	for _, mode := range []string{"off", "device", "session", "full"} {
		for _, enabled := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/disabled", true: "/enabled"}[enabled], func(t *testing.T) {
				extra := map[string]any{
					codexFingerprintModeExtraKey:                 mode,
					openAIAPIKeyDeviceConvergenceEnabledExtraKey: enabled,
				}
				account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: extra}
				want := codexFingerprintOff
				if enabled {
					want = codexFingerprintDevice
				}
				require.Equal(t, want, account.GetCodexFingerprintMode())
				prepared := prepareCodexFingerprintExtraForCreate(account.Platform, account.Type, extra)
				_, hasSeed := codexFingerprintSeed(prepared)
				require.Equal(t, enabled, hasSeed)
			})
		}
	}
}

func TestAdminAPIKeyDeviceConvergenceSeedLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := &upstreamBillingProbeAccountRepo{accounts: make(map[int64]*Account)}
	svc := &adminServiceImpl{accountRepo: repo}
	create := func(name string) *Account {
		t.Helper()
		created, err := svc.CreateAccount(ctx, &CreateAccountInput{
			Name:                 name,
			Platform:             PlatformOpenAI,
			Type:                 AccountTypeAPIKey,
			SkipDefaultGroupBind: true,
			Extra: map[string]any{
				openAIAPIKeyDeviceConvergenceEnabledExtraKey: true,
				codexFingerprintSeedExtraKey:                 userSuppliedCodexFingerprintSeed,
			},
		})
		require.NoError(t, err)
		return created
	}
	first, second := create("device-a"), create("device-b")
	seed := requireValidCodexFingerprintSeed(t, first.Extra)
	require.NotEqual(t, userSuppliedCodexFingerprintSeed, seed)
	require.NotEqual(t, seed, requireValidCodexFingerprintSeed(t, second.Extra))
	for _, enabled := range []bool{false, true} {
		updated, err := svc.UpdateAccount(ctx, first.ID, &UpdateAccountInput{
			Extra: map[string]any{
				openAIAPIKeyDeviceConvergenceEnabledExtraKey: enabled,
				codexFingerprintSeedExtraKey:                 userSuppliedCodexFingerprintSeed,
			},
		})
		require.NoError(t, err)
		require.Equal(t, seed, requireValidCodexFingerprintSeed(t, updated.Extra), "disabling/re-enabling and supplied seeds must not rotate the identity")
		require.Equal(t, enabled, updated.IsOpenAIAPIKeyDeviceConvergenceEnabled())
	}
}

func TestAccountServiceAPIKeyDeviceConvergenceEnablesExistingAccount(t *testing.T) {
	ctx := context.Background()
	repo := &upstreamBillingProbeAccountRepo{accounts: make(map[int64]*Account)}
	svc := NewAccountService(repo, nil)
	created, err := svc.Create(ctx, CreateAccountRequest{
		Name: "default-off", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Extra: map[string]any{codexFingerprintModeExtraKey: "full", codexFingerprintSeedExtraKey: userSuppliedCodexFingerprintSeed},
	})
	require.NoError(t, err)
	require.NotContains(t, created.Extra, codexFingerprintSeedExtraKey)
	updated, err := svc.Update(ctx, created.ID, UpdateAccountRequest{
		Extra: &map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true, codexFingerprintSeedExtraKey: userSuppliedCodexFingerprintSeed},
	})
	require.NoError(t, err)
	require.NotEqual(t, userSuppliedCodexFingerprintSeed, requireValidCodexFingerprintSeed(t, updated.Extra))
	require.Equal(t, codexFingerprintDevice, updated.GetCodexFingerprintMode())
}

func TestDuplicateAPIKeyDeviceConvergenceAccountGetsIndependentSeed(t *testing.T) {
	ctx := context.Background()
	repo := &codexSeedDuplicateRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: make(map[int64]*Account)}}
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}
	source := &Account{
		Name: "source", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Extra: map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true, codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
	}
	require.NoError(t, repo.Create(ctx, source))
	duplicate, err := svc.DuplicateAccount(ctx, source.ID, "admin:1", "")
	require.NoError(t, err)
	require.True(t, duplicate.IsOpenAIAPIKeyDeviceConvergenceEnabled())
	require.NotEqual(t, testCodexFingerprintSeed, requireValidCodexFingerprintSeed(t, duplicate.Extra))
}

func TestAPIKeyDeviceConvergenceBulkEnableLeavesSeedCreationToRepository(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{401, 402},
		Extra:      map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: true, codexFingerprintSeedExtraKey: userSuppliedCodexFingerprintSeed},
	})
	require.NoError(t, err)
	require.Empty(t, repo.updates)
	require.Len(t, repo.bulkUpdates, 1)
	require.True(t, repo.bulkUpdates[0].EnsureCodexFingerprintSeed)
	require.NotContains(t, repo.bulkUpdates[0].Extra, codexFingerprintSeedExtraKey)
	for _, value := range []any{false, "true", 1, nil} {
		require.False(t, ShouldEnsureCodexFingerprintSeedForExtraUpdates(map[string]any{openAIAPIKeyDeviceConvergenceEnabledExtraKey: value}))
	}
}
