package service

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAPISyncUsesConfiguredUSDConversionWhenUpstreamHidesRules(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}}
	base := newAPIBalanceHandler(t,
		`{"success":true,"data":{"id":42,"group":"Basic","quota":1661242,"used_quota":838758}}`,
		validNewAPITokenBalanceBody(),
	)
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/status" {
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"system_name":"NewAPI"}}`), nil
		}
		return base(req)
	}}
	svc := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) { return NewNewAPIClient(doer), nil })
	var update NewAPISyncConfigUpdate
	require.NoError(t, json.Unmarshal([]byte(`{"newapi_sync_enabled":true,"newapi_base_url":"https://newapi.example.test","newapi_user_id":42,"newapi_quota_per_usd":500000}`), &update))
	_, err := svc.UpdateNewAPISyncConfig(t.Context(), 1, &update)
	require.NoError(t, err)
	result, err := svc.SyncNewAPIAccount(t.Context(), 1)
	require.NoError(t, err)
	require.NotNil(t, result.BalanceSnapshot.QuotaDisplay)
	require.Equal(t, "USD", result.BalanceSnapshot.QuotaDisplay.DisplayType)
	require.Equal(t, float64(500000), result.BalanceSnapshot.QuotaDisplay.QuotaPerUnit)
	require.Equal(t, "manual", result.BalanceSnapshot.QuotaDisplay.Source)
	require.InDelta(t, 3.322484, result.SchedulingSnapshot.Data["balance"], 1e-9)
	require.Equal(t, int64(1661242), result.SchedulingSnapshot.Data["balance_quota"])
	config, err := svc.GetNewAPISyncConfig(t.Context(), 1)
	require.NoError(t, err)
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"newapi_quota_per_usd":500000`)
}

func TestNewAPISyncConversionConfigPreservesOmittedAndInvalidatesChangedRules(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	oldIdentity := account.Extra[NewAPISyncIdentityExtraKey].(string)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}}
	svc := newAPISyncTestService(t, repo, nil)
	update := &NewAPISyncConfigUpdate{
		Enabled: true, BaseURL: "https://newapi.example.test", UserID: "42", QuotaPerUSD: json.RawMessage(`500000`),
	}
	config, err := svc.UpdateNewAPISyncConfig(t.Context(), 1, update)
	require.NoError(t, err)
	require.Equal(t, float64(500000), *config.QuotaPerUSD)
	newIdentity := account.Extra[NewAPISyncIdentityExtraKey].(string)
	require.NotEqual(t, oldIdentity, newIdentity)
	require.Nil(t, account.Extra[NewAPIBalanceSnapshotExtraKey])
	require.Nil(t, account.Extra[UpstreamBillingProbeExtraKey])
	_, err = repo.UpdateNewAPISyncResult(t.Context(), &NewAPISyncWrite{AccountID: 1, ExpectedIdentity: oldIdentity})
	require.ErrorIs(t, err, ErrNewAPISyncIdentityChanged, "an in-flight result using the old rule must not overwrite new amounts")

	// Old clients do not know this field; an unrelated save must preserve it.
	update.QuotaPerUSD = nil
	account.Extra[UpstreamBillingProbeExtraKey] = "preserved snapshot"
	config, err = svc.UpdateNewAPISyncConfig(t.Context(), 1, update)
	require.NoError(t, err)
	require.Equal(t, float64(500000), *config.QuotaPerUSD)
	require.Equal(t, newIdentity, account.Extra[NewAPISyncIdentityExtraKey])
	require.Equal(t, "preserved snapshot", account.Extra[UpstreamBillingProbeExtraKey])
	copyConfig := duplicateAccountNewAPIConfig(account.Extra)
	require.Equal(t, newIdentity, copyConfig[NewAPISyncIdentityExtraKey])
	require.Equal(t, float64(500000), *newAPIStoredConfigFromAccount(&Account{Extra: copyConfig}).QuotaPerUSD)

	update.QuotaPerUSD = json.RawMessage(`null`)
	config, err = svc.UpdateNewAPISyncConfig(t.Context(), 1, update)
	require.NoError(t, err)
	require.Nil(t, config.QuotaPerUSD)
	require.Equal(t, oldIdentity, account.Extra[NewAPISyncIdentityExtraKey], "automatic mode retains the legacy identity format")
	require.Nil(t, account.Extra[UpstreamBillingProbeExtraKey])
}

func TestNewAPISyncConversionConfigRejectsInvalidNumbers(t *testing.T) {
	for _, raw := range []string{`0`, `-1`, `"500000"`, `9007199254740992`, `1e309`, `true`, `{}`, `[]`} {
		t.Run(raw, func(t *testing.T) {
			account := newAPISyncTestAccount(1, 0.4)
			oldIdentity := account.Extra[NewAPISyncIdentityExtraKey]
			repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}}
			svc := newAPISyncTestService(t, repo, nil)
			_, err := svc.UpdateNewAPISyncConfig(t.Context(), 1, &NewAPISyncConfigUpdate{
				Enabled: true, BaseURL: "https://newapi.example.test", UserID: "42", QuotaPerUSD: json.RawMessage(raw),
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "NEWAPI_QUOTA_PER_USD_INVALID")
			require.Equal(t, oldIdentity, account.Extra[NewAPISyncIdentityExtraKey])
		})
	}
}

func TestNewAPISyncConversionFallbackDoesNotOverrideUpstream(t *testing.T) {
	for _, displayType := range []string{"USD", "TOKENS"} {
		t.Run(displayType, func(t *testing.T) {
			account := newAPISyncTestAccount(1, 0.4)
			account.Extra[NewAPIQuotaPerUSDExtraKey] = 1000
			repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}}
			base := newAPIBalanceHandler(t, validNewAPIUserBalanceBody(), validNewAPITokenBalanceBody())
			doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/status" {
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"`+displayType+`"}}`), nil
				}
				return base(req)
			}}
			svc := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) { return NewNewAPIClient(doer), nil })
			result, err := svc.TestNewAPIConnection(t.Context(), 1)
			require.NoError(t, err)
			require.Equal(t, float64(500000), result.BalanceSnapshot.QuotaDisplay.QuotaPerUnit)
			require.Equal(t, displayType, result.BalanceSnapshot.QuotaDisplay.DisplayType)
			require.Empty(t, result.BalanceSnapshot.QuotaDisplay.Source)
		})
	}
}
