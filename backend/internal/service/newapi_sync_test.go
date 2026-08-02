package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type newAPITestEncryptor struct{}

func (newAPITestEncryptor) Encrypt(value string) (string, error) {
	return "encrypted:" + value, nil
}

func (newAPITestEncryptor) Decrypt(value string) (string, error) {
	plaintext, ok := strings.CutPrefix(value, "encrypted:")
	if !ok {
		return "", errors.New("invalid ciphertext")
	}
	return plaintext, nil
}

type newAPISyncTestRepo struct {
	*upstreamBillingProbeAccountRepo
	writeCalls  atomic.Int64
	rateChanges atomic.Int64
	writes      []*NewAPISyncWrite
}

func (r *newAPISyncTestRepo) UpdateNewAPISyncResult(
	_ context.Context,
	write *NewAPISyncWrite,
) (*NewAPISyncWriteResult, error) {
	r.writeCalls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[write.AccountID]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.Extra[NewAPISyncIdentityExtraKey] != write.ExpectedIdentity {
		return nil, ErrNewAPISyncIdentityChanged
	}
	if write.ExpectedAccountBaseURL != nil &&
		strings.TrimSpace(account.GetCredential("base_url")) != strings.TrimSpace(*write.ExpectedAccountBaseURL) {
		return nil, ErrNewAPISyncIdentityChanged
	}
	if newAPIAccountAPIKeyHash(account) != write.ExpectedAccountAPIKeyHash {
		return nil, ErrNewAPISyncIdentityChanged
	}
	r.writes = append(r.writes, write)
	oldRatio := account.BillingRateMultiplier()
	newRatio := oldRatio
	changed := false
	if write.Ratio != nil {
		newRatio = *write.Ratio
		changed = math.Abs(oldRatio-newRatio) > 1e-9
		account.RateMultiplier = float64Pointer(newRatio)
		if changed {
			r.rateChanges.Add(1)
		}
	}
	account.Extra[NewAPILastSyncAtExtraKey] = write.AttemptedAt
	account.Extra[NewAPILastSyncStatusExtraKey] = write.Status
	account.Extra[NewAPILastSyncErrorExtraKey] = write.Error
	if write.Status != NewAPISyncStatusFailed {
		account.Extra[NewAPIResolvedUserGroupExtraKey] = stringPointerValueForTest(write.UserGroup)
		account.Extra[NewAPIResolvedTokenGroupExtraKey] = stringPointerValueForTest(write.TokenGroup)
		account.Extra[NewAPIResolvedActualGroupExtraKey] = stringPointerValueForTest(write.ActualGroup)
		account.Extra[NewAPIRatioSourceExtraKey] = stringPointerValueForTest(write.RatioSource)
		account.Extra[NewAPICrossGroupRetryExtraKey] = write.CrossGroupRetry != nil && *write.CrossGroupRetry
		if write.SchedulingSnapshot != nil {
			account.Extra[UpstreamBillingProbeExtraKey] = write.SchedulingSnapshot
		}
		if write.BalanceSnapshot != nil {
			account.Extra[NewAPIBalanceSnapshotExtraKey] = write.BalanceSnapshot
		}
	}
	return &NewAPISyncWriteResult{Changed: changed, OldRatio: oldRatio, NewRatio: newRatio}, nil
}

func stringPointerValueForTest(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func newAPISyncTestService(
	t *testing.T,
	repo *newAPISyncTestRepo,
	factory func(*Account) (*NewAPIClient, error),
) *UpstreamBillingProbeService {
	t.Helper()
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		AllowInsecureHTTP: true,
		AllowPrivateHosts: true,
	}}}
	service := NewUpstreamBillingProbeService(repo, nil, nil)
	service.SetNewAPIDependencies(newAPITestEncryptor{}, cfg)
	service.newAPIClientFactory = factory
	return service
}

func newAPISyncTestAccount(id int64, ratio float64) *Account {
	baseURL := "https://newapi.example.test"
	accessToken := "encrypted:" + newAPITestAccessToken
	return &Account{
		ID:             id,
		Name:           fmt.Sprintf("newapi-%d", id),
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		Status:         StatusActive,
		RateMultiplier: float64Pointer(ratio),
		Credentials: map[string]any{
			"api_key": newAPITestAPIKey,
		},
		Extra: map[string]any{
			NewAPISyncEnabledExtraKey:     true,
			NewAPIBaseURLExtraKey:         baseURL,
			NewAPIUserAccessTokenExtraKey: accessToken,
			NewAPIUserIDExtraKey:          int64(42),
			NewAPISyncIdentityExtraKey:    newAPISyncIdentity(baseURL, 42, accessToken),
		},
	}
}

func TestNewAPISyncConfigEncryptsAccessTokenMasksOutputAndPreservesEmptyValue(t *testing.T) {
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{
			1: {
				ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
				Credentials: map[string]any{
					"api_key":  newAPITestAPIKey,
					"base_url": "https://account-endpoint.example.test",
				},
				Extra: map[string]any{
					UpstreamBillingProbeEnabledExtraKey: true,
				},
			},
		},
	}}
	service := newAPISyncTestService(t, repo, nil)

	config, err := service.UpdateNewAPISyncConfig(t.Context(), 1, &NewAPISyncConfigUpdate{
		Enabled:         true,
		BaseURL:         "https://newapi.example.test///",
		UserAccessToken: newAPITestAccessToken,
		UserID:          42,
	})
	require.NoError(t, err)
	require.Equal(t, "https://newapi.example.test", config.BaseURL)
	require.Equal(t, NewAPISecretMask, config.UserAccessToken)

	stored := repo.accounts[1].Extra
	require.Equal(t, "encrypted:"+newAPITestAccessToken, stored[NewAPIUserAccessTokenExtraKey])
	require.NotEqual(t, newAPITestAccessToken, stored[NewAPIUserAccessTokenExtraKey])
	require.Nil(t, stored[NewAPIAPIKeyExtraKey])
	require.Nil(t, stored[NewAPISyncIntervalExtraKey])
	require.Equal(t, false, stored[UpstreamBillingProbeEnabledExtraKey])
	require.Equal(t, false, stored[UpstreamBillingRateSyncEnabledExtraKey])

	config, err = service.UpdateNewAPISyncConfig(t.Context(), 1, &NewAPISyncConfigUpdate{
		Enabled:         true,
		BaseURL:         "",
		UserAccessToken: "",
		UserID:          42,
	})
	require.NoError(t, err)
	require.Empty(t, config.BaseURL)
	require.Equal(t, "encrypted:"+newAPITestAccessToken, stored[NewAPIUserAccessTokenExtraKey])
}

func TestNewAPISyncConfigRejectsUnsafeBaseURL(t *testing.T) {
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{
			1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{}},
		},
	}}
	service := NewUpstreamBillingProbeService(repo, nil, nil)
	service.SetNewAPIDependencies(newAPITestEncryptor{}, &config.Config{
		Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}},
	})

	_, err := service.UpdateNewAPISyncConfig(t.Context(), 1, &NewAPISyncConfigUpdate{
		Enabled:         true,
		BaseURL:         "http://169.254.169.254/latest/meta-data",
		UserAccessToken: newAPITestAccessToken,
		UserID:          42,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "NewAPI base URL is not allowed")
}

func TestGenericAccountExtraUpdatePreservesManagedNewAPIValues(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	input := map[string]any{
		NewAPIUserAccessTokenExtraKey: NewAPISecretMask,
		NewAPIAPIKeyExtraKey:          "",
		NewAPISyncEnabledExtraKey:     false,
		"unrelated":                   true,
	}

	got := preserveNewAPISyncManagedExtra(account, input)
	require.Equal(t, account.Extra[NewAPIUserAccessTokenExtraKey], got[NewAPIUserAccessTokenExtraKey])
	require.Equal(t, account.Extra[NewAPIAPIKeyExtraKey], got[NewAPIAPIKeyExtraKey])
	require.Equal(t, true, got[NewAPISyncEnabledExtraKey])
	require.Equal(t, true, got["unrelated"])
}

func TestNewAPITestConnectionDoesNotModifyRatio(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	account.Extra[OpenAIUpstreamRateCalibrationExtraKey] = 2.0
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	result, err := service.TestNewAPIConnection(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.065, *result.NewRatio)
	require.Equal(t, 0.0325, *result.Resolution.Ratio)
	require.Equal(t, 0.4, account.BillingRateMultiplier())
	require.Zero(t, repo.writeCalls.Load())
}

func TestNewAPISyncBlankBaseURLUsesAccountEndpoint(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	accountEndpoint := "https://account-endpoint.example.test"
	account.Credentials = map[string]any{
		"base_url": accountEndpoint,
		"api_key":  newAPITestAPIKey,
	}
	account.Extra[NewAPIBaseURLExtraKey] = ""
	account.Extra[NewAPISyncIdentityExtraKey] = newAPISyncIdentity(
		"",
		42,
		account.Extra[NewAPIUserAccessTokenExtraKey].(string),
	)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	result, err := service.SyncNewAPIAccount(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.NewRatio)
	require.NotEmpty(t, doer.requests)
	for _, request := range doer.requests {
		require.Equal(t, "account-endpoint.example.test", request.URL.Host)
	}
	require.Equal(t, accountEndpoint, *repo.writes[0].ExpectedAccountBaseURL)
	require.Equal(t, newAPIAccountAPIKeyHash(account), repo.writes[0].ExpectedAccountAPIKeyHash)
}

func TestRefreshSchedulingCostUsesNewAPISnapshotAndCalibration(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.0325)
	account.Extra[OpenAIUpstreamRateCalibrationExtraKey] = 2.0
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.RefreshSchedulingCost(t.Context(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result.NewAPISync)
	require.NotNil(t, result.Snapshot)
	require.Equal(t, "newapi", result.Snapshot.Data["source"])
	require.Equal(t, "VIP", result.Snapshot.Data["newapi_group"])
	require.Equal(t, 0.0325, result.Snapshot.Data["resolved_rate_multiplier"])
	require.InDelta(t, 16.0, result.Snapshot.Data["balance"], 1e-12)
	require.Equal(t, "wallet", result.Snapshot.Data["balance_kind"])
	require.Equal(t, now.Add(30*time.Minute), result.Snapshot.NextProbeAt)
	require.Equal(t, now.Add(time.Hour), *result.Snapshot.FreshUntil)
	require.Same(t, result.Snapshot, account.Extra[UpstreamBillingProbeExtraKey])
	require.Equal(t, 0.065, *result.NewAPISync.NewRatio)
	require.Equal(t, 0.065, account.BillingRateMultiplier())

	schedulingRate, ok := openAISchedulingRate(account, now, 1)
	require.True(t, ok)
	require.InDelta(t, 0.065, schedulingRate, 1e-12)
}

func TestNewAPISyncAppliesSharedUpstreamBalanceAlert(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.0325)
	account.Extra[UpstreamBalanceAlertEnabledExtraKey] = true
	account.Extra[UpstreamBalanceAlertThresholdExtraKey] = 20.0
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	first, err := service.SyncNewAPIAccount(t.Context(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, first.SchedulingSnapshot.BalanceAlert)
	require.True(t, first.SchedulingSnapshot.BalanceAlert.Active)
	require.Equal(t, 20.0, first.SchedulingSnapshot.BalanceAlert.Threshold)
	require.Equal(t, &now, first.SchedulingSnapshot.BalanceAlert.TriggeredAt)

	service.now = func() time.Time { return now.Add(time.Hour) }
	second, err := service.SyncNewAPIAccount(t.Context(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, second.SchedulingSnapshot.BalanceAlert)
	require.True(t, second.SchedulingSnapshot.BalanceAlert.Active)
	require.Equal(t, &now, second.SchedulingSnapshot.BalanceAlert.TriggeredAt)
}

func TestNewAPISyncZeroCalibrationStoresZeroAccountCost(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	account.Extra[OpenAIUpstreamRateCalibrationExtraKey] = 0.0
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	result, err := service.SyncNewAPIAccount(t.Context(), account.ID)

	require.NoError(t, err)
	require.Equal(t, 0.0, *result.NewRatio)
	require.Equal(t, 0.0, account.BillingRateMultiplier())
	require.Equal(t, 0.0325, *result.Resolution.Ratio)
}

func TestNewAPIDisablingSyncClearsPreviousSchedulingSnapshot(t *testing.T) {
	account := newAPISyncTestAccount(1, 1)
	account.Extra[UpstreamBillingProbeExtraKey] = &UpstreamBillingProbeSnapshot{
		Status:        UpstreamBillingProbeStatusOK,
		LastAttemptAt: time.Now(),
		NextProbeAt:   time.Now().Add(time.Hour),
	}
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	service := newAPISyncTestService(t, repo, nil)

	_, err := service.UpdateNewAPISyncConfig(t.Context(), account.ID, &NewAPISyncConfigUpdate{
		Enabled:         false,
		BaseURL:         "https://newapi.example.test",
		UserAccessToken: NewAPISecretMask,
		UserID:          42,
	})

	require.NoError(t, err)
	require.Nil(t, account.Extra[UpstreamBillingProbeExtraKey])
}

func TestNewAPISyncAutoGroupFailureDoesNotOverwriteRatio(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "auto", false, "0.04")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	result, err := service.SyncNewAPIAccount(t.Context(), 1)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 0.4, account.BillingRateMultiplier())
	require.Equal(t, NewAPISyncStatusFailed, account.Extra[NewAPILastSyncStatusExtraKey])
	require.Equal(t, "newapi_auto_group_unsupported", account.Extra[NewAPILastSyncErrorExtraKey])
}

func TestNewAPISyncFailurePreservesRatioAndStoresOnlySafeError(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{handle: func(*http.Request) (*http.Response, error) {
		return nil, errors.New("leaked " + newAPITestAccessToken + " " + newAPITestAPIKey)
	}}
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	_, err := service.SyncNewAPIAccount(t.Context(), 1)
	require.Error(t, err)
	require.Equal(t, 0.4, account.BillingRateMultiplier())
	require.Equal(t, "newapi_request_failed", account.Extra[NewAPILastSyncErrorExtraKey])
	require.NotContains(t, account.Extra[NewAPILastSyncErrorExtraKey], newAPITestAccessToken)
	require.NotContains(t, account.Extra[NewAPILastSyncErrorExtraKey], newAPITestAPIKey)
}

func TestConcurrentNewAPISyncProducesOnlyOneRateUpdate(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	const callers = 12
	var group sync.WaitGroup
	group.Add(callers)
	errs := make(chan error, callers)
	for range callers {
		go func() {
			defer group.Done()
			_, err := service.SyncNewAPIAccount(t.Context(), 1)
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Positive(t, repo.writeCalls.Load())
	require.Equal(t, int64(1), repo.rateChanges.Load())
	require.Equal(t, 0.0325, account.BillingRateMultiplier())
	require.Zero(t, len(doer.requests)%5)
}

func TestNewAPIPeriodicFailuresDoNotStopOtherAccounts(t *testing.T) {
	first := newAPISyncTestAccount(1, 0.4)
	second := newAPISyncTestAccount(2, 0.5)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: first, 2: second},
	}}
	service := newAPISyncTestService(t, repo, func(account *Account) (*NewAPIClient, error) {
		if account.ID == 1 {
			return NewNewAPIClient(&newAPITestDoer{handle: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("remote failure")
			}}), nil
		}
		doer := &newAPITestDoer{}
		doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
		return NewNewAPIClient(doer), nil
	})

	require.NoError(t, service.RunNewAPIDue(t.Context()))
	require.Equal(t, 0.4, first.BillingRateMultiplier())
	require.Equal(t, NewAPISyncStatusFailed, first.Extra[NewAPILastSyncStatusExtraKey])
	require.Equal(t, 0.0325, second.BillingRateMultiplier())
	require.Equal(t, NewAPISyncStatusOK, second.Extra[NewAPILastSyncStatusExtraKey])
}

func TestNewAPIPeriodicSyncUsesGlobalProbeSettings(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	account.Extra[NewAPILastSyncAtExtraKey] = now.Add(-45 * time.Minute)

	require.False(t, newAPISyncDue(account, now, 60))
	require.True(t, newAPISyncDue(account, now, 30))

	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})
	settingsRepo := &upstreamBillingProbeSettingRepo{values: map[string]string{
		SettingKeyUpstreamBillingProbeSettings: `{"enabled":false,"interval_minutes":30}`,
	}}
	service.settingService = NewSettingService(settingsRepo, &config.Config{})
	service.now = func() time.Time { return now }

	require.NoError(t, service.RunNewAPIDue(t.Context()))
	require.Empty(t, doer.requests)
	require.Zero(t, repo.writeCalls.Load())

	settingsRepo.values[SettingKeyUpstreamBillingProbeSettings] = `{"enabled":true,"interval_minutes":60}`
	require.NoError(t, service.RunNewAPIDue(t.Context()))
	require.Empty(t, doer.requests)
	require.Zero(t, repo.writeCalls.Load())

	settingsRepo.values[SettingKeyUpstreamBillingProbeSettings] = `{"enabled":true,"interval_minutes":30}`
	require.NoError(t, service.RunNewAPIDue(t.Context()))
	require.NotEmpty(t, doer.requests)
	require.Equal(t, int64(1), repo.writeCalls.Load())
}
