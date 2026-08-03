package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"golang.org/x/sync/errgroup"
)

const (
	NewAPISyncEnabledExtraKey     = "newapi_sync_enabled"
	NewAPIBaseURLExtraKey         = "newapi_base_url"
	NewAPIUserAccessTokenExtraKey = "newapi_user_access_token"
	NewAPIUserIDExtraKey          = "newapi_user_id"
	// Legacy account-level fields are cleared when the configuration is saved.
	// NewAPI now reuses the account API key and the global probe interval.
	NewAPIAPIKeyExtraKey              = "newapi_api_key"
	NewAPISyncIntervalExtraKey        = "newapi_sync_interval"
	NewAPILastSyncAtExtraKey          = "newapi_last_sync_at"
	NewAPILastSyncStatusExtraKey      = "newapi_last_sync_status"
	NewAPILastSyncErrorExtraKey       = "newapi_last_sync_error"
	NewAPIBalanceSnapshotExtraKey     = "newapi_balance_snapshot"
	NewAPIResolvedUserGroupExtraKey   = "newapi_resolved_user_group"
	NewAPIResolvedTokenGroupExtraKey  = "newapi_resolved_token_group"
	NewAPIResolvedActualGroupExtraKey = "newapi_resolved_actual_group"
	NewAPIRatioSourceExtraKey         = "newapi_ratio_source"
	NewAPICrossGroupRetryExtraKey     = "newapi_cross_group_retry"

	NewAPISyncIdentityExtraKey = "newapi_sync_identity_hash"
	NewAPISecretMask           = "********"

	newAPIMaxPerCycle           = 20
	newAPISyncLeaderLockKey     = "upstream:newapi:ratio-sync:leader"
	newAPISyncAccountLockPrefix = "upstream:newapi:ratio-sync:account:"
)

const (
	NewAPISyncStatusNever  = "never"
	NewAPISyncStatusOK     = "ok"
	NewAPISyncStatusFailed = "failed"

	NewAPIRatioSourceConfiguredGroup = "configured_group"
)

var newAPISyncManagedExtraKeys = [...]string{
	NewAPISyncEnabledExtraKey,
	NewAPIBaseURLExtraKey,
	NewAPIUserAccessTokenExtraKey,
	NewAPIUserIDExtraKey,
	NewAPIAPIKeyExtraKey,
	NewAPISyncIntervalExtraKey,
	NewAPILastSyncAtExtraKey,
	NewAPILastSyncStatusExtraKey,
	NewAPILastSyncErrorExtraKey,
	NewAPIBalanceSnapshotExtraKey,
	NewAPIResolvedUserGroupExtraKey,
	NewAPIResolvedTokenGroupExtraKey,
	NewAPIResolvedActualGroupExtraKey,
	NewAPIRatioSourceExtraKey,
	NewAPICrossGroupRetryExtraKey,
	NewAPISyncIdentityExtraKey,
}

var (
	ErrNewAPISyncUnavailable = infraServiceUnavailable(
		"NEWAPI_SYNC_UNAVAILABLE", "NewAPI ratio synchronization is unavailable",
	)
	ErrNewAPISyncAccountInvalid = infraBadRequest(
		"NEWAPI_SYNC_ACCOUNT_INVALID", "account is not an OpenAI API key account",
	)
	ErrNewAPISyncBusy = infraConflict(
		"NEWAPI_SYNC_BUSY", "NewAPI ratio synchronization is already running for this account",
	)
	ErrNewAPISyncIdentityChanged = infraConflict(
		"NEWAPI_SYNC_IDENTITY_CHANGED", "NewAPI synchronization configuration changed; retry the operation",
	)
)

// Small wrappers keep this file independent from the internal errors package's
// concrete type while preserving the project's HTTP error mapping.
func infraServiceUnavailable(code, message string) error {
	return newAPIInfraError(503, code, message)
}

func infraBadRequest(code, message string) error {
	return newAPIInfraError(400, code, message)
}

func infraConflict(code, message string) error {
	return newAPIInfraError(409, code, message)
}

func newAPIInfraError(status int, code, message string) error {
	switch status {
	case 400:
		return infraerrors.BadRequest(code, message)
	case 409:
		return infraerrors.Conflict(code, message)
	default:
		return infraerrors.ServiceUnavailable(code, message)
	}
}

type NewAPISyncConfig struct {
	Enabled             bool                   `json:"newapi_sync_enabled"`
	BaseURL             string                 `json:"newapi_base_url"`
	UserAccessToken     string                 `json:"newapi_user_access_token"`
	UserID              int64                  `json:"newapi_user_id"`
	LastSyncAt          *time.Time             `json:"newapi_last_sync_at,omitempty"`
	LastSyncStatus      string                 `json:"newapi_last_sync_status"`
	LastSyncError       string                 `json:"newapi_last_sync_error,omitempty"`
	ResolvedUserGroup   string                 `json:"newapi_resolved_user_group,omitempty"`
	ResolvedTokenGroup  string                 `json:"newapi_resolved_token_group,omitempty"`
	ResolvedActualGroup string                 `json:"newapi_resolved_actual_group,omitempty"`
	RatioSource         string                 `json:"newapi_ratio_source,omitempty"`
	CrossGroupRetry     bool                   `json:"newapi_cross_group_retry"`
	CurrentRatio        float64                `json:"current_ratio"`
	HasUserAccessToken  bool                   `json:"has_newapi_user_access_token"`
	HasAPIKey           bool                   `json:"has_newapi_api_key"`
	BalanceSyncEnabled  bool                   `json:"newapi_balance_sync_enabled"`
	BalanceSyncInterval int                    `json:"newapi_balance_sync_interval"`
	BalanceSnapshot     *NewAPIBalanceSnapshot `json:"newapi_balance_snapshot,omitempty"`
	BalanceStale        bool                   `json:"newapi_balance_stale"`
}

type NewAPISyncConfigUpdate struct {
	Enabled         bool   `json:"newapi_sync_enabled"`
	BaseURL         string `json:"newapi_base_url"`
	UserAccessToken string `json:"newapi_user_access_token"`
	UserID          int64  `json:"newapi_user_id"`
}

type NewAPISyncResult struct {
	AccountID          int64                         `json:"account_id"`
	Status             string                        `json:"status"`
	Changed            bool                          `json:"changed"`
	OldRatio           float64                       `json:"old_ratio"`
	NewRatio           *float64                      `json:"new_ratio,omitempty"`
	Resolution         *NewAPIResolution             `json:"resolution,omitempty"`
	BalanceSnapshot    *NewAPIBalanceSnapshot        `json:"balance_snapshot,omitempty"`
	SchedulingSnapshot *UpstreamBillingProbeSnapshot `json:"scheduling_snapshot,omitempty"`
	Error              string                        `json:"error,omitempty"`
}

type newAPISyncStoredConfig struct {
	Enabled         bool   `json:"newapi_sync_enabled"`
	BaseURL         string `json:"newapi_base_url"`
	UserAccessToken string `json:"newapi_user_access_token"`
	UserID          int64  `json:"newapi_user_id"`
	IdentityHash    string `json:"newapi_sync_identity_hash"`
}

type NewAPISyncWrite struct {
	AccountID                 int64
	ExpectedIdentity          string
	ExpectedAccountBaseURL    *string
	ExpectedAccountAPIKeyHash string
	AttemptedAt               time.Time
	Status                    string
	Error                     string
	UserGroup                 *string
	TokenGroup                *string
	ActualGroup               *string
	RatioSource               *string
	CrossGroupRetry           *bool
	Ratio                     *float64
	BalanceSnapshot           *NewAPIBalanceSnapshot
	SchedulingSnapshot        *UpstreamBillingProbeSnapshot
}

type NewAPISyncWriteResult struct {
	Changed  bool
	OldRatio float64
	NewRatio float64
}

type newAPISyncResultWriter interface {
	UpdateNewAPISyncResult(context.Context, *NewAPISyncWrite) (*NewAPISyncWriteResult, error)
}

func (s *UpstreamBillingProbeService) GetNewAPISyncConfig(ctx context.Context, accountID int64) (*NewAPISyncConfig, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrNewAPISyncUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !isNewAPISyncAccount(account) {
		return nil, ErrNewAPISyncAccountInvalid
	}
	config := newAPIPublicConfigFromAccount(account)
	if settings, settingsErr := s.getSettings(ctx); settingsErr == nil {
		config.BalanceSyncInterval = settings.IntervalMinutes
	}
	config.BalanceStale = newAPIBalanceIsStale(config, s.currentTime())
	return config, nil
}

func (s *UpstreamBillingProbeService) UpdateNewAPISyncConfig(
	ctx context.Context,
	accountID int64,
	update *NewAPISyncConfigUpdate,
) (*NewAPISyncConfig, error) {
	if s == nil || s.accountRepo == nil || s.encryptor == nil {
		return nil, ErrNewAPISyncUnavailable
	}
	if update == nil {
		return nil, infraBadRequest("NEWAPI_SYNC_CONFIG_INVALID", "configuration is required")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !isNewAPISyncAccount(account) {
		return nil, ErrNewAPISyncAccountInvalid
	}

	stored := newAPIStoredConfigFromAccount(account)
	normalizedBaseURL := strings.TrimSpace(update.BaseURL)
	if normalizedBaseURL != "" {
		normalizedBaseURL, err = s.validateNewAPIBaseURL(normalizedBaseURL)
		if err != nil {
			return nil, infraBadRequest("NEWAPI_BASE_URL_INVALID", "NewAPI base URL is not allowed")
		}
	} else if update.Enabled {
		if _, err = s.resolveNewAPIBaseURL(account, ""); err != nil {
			return nil, infraBadRequest("NEWAPI_BASE_URL_INVALID", "account endpoint URL is not allowed")
		}
	}
	encryptedAccessToken := stored.UserAccessToken
	if secretShouldReplace(update.UserAccessToken) {
		encryptedAccessToken, err = s.encryptor.Encrypt(strings.TrimSpace(update.UserAccessToken))
		if err != nil {
			return nil, ErrNewAPISyncUnavailable
		}
	}
	if update.Enabled && (update.UserID <= 0 || encryptedAccessToken == "") {
		return nil, infraBadRequest("NEWAPI_SYNC_CONFIG_INCOMPLETE", "enabled NewAPI synchronization requires UID and access token")
	}
	if update.UserID < 0 {
		return nil, infraBadRequest("NEWAPI_USER_ID_INVALID", "NewAPI UID must be greater than zero")
	}

	identity := newAPISyncIdentity(normalizedBaseURL, update.UserID, encryptedAccessToken)
	identityChanged := identity != stored.IdentityHash
	enabledChanged := update.Enabled != stored.Enabled
	updates := map[string]any{
		NewAPISyncEnabledExtraKey:     update.Enabled,
		NewAPIBaseURLExtraKey:         normalizedBaseURL,
		NewAPIUserAccessTokenExtraKey: encryptedAccessToken,
		NewAPIUserIDExtraKey:          update.UserID,
		NewAPIAPIKeyExtraKey:          nil,
		NewAPISyncIntervalExtraKey:    nil,
		NewAPISyncIdentityExtraKey:    identity,
	}
	if update.Enabled {
		updates[UpstreamBillingProbeEnabledExtraKey] = false
		updates[UpstreamBillingRateSyncEnabledExtraKey] = false
	}
	if identityChanged {
		updates[NewAPILastSyncAtExtraKey] = nil
		updates[NewAPILastSyncStatusExtraKey] = NewAPISyncStatusNever
		updates[NewAPILastSyncErrorExtraKey] = ""
		updates[NewAPIResolvedUserGroupExtraKey] = ""
		updates[NewAPIResolvedTokenGroupExtraKey] = ""
		updates[NewAPIResolvedActualGroupExtraKey] = ""
		updates[NewAPIRatioSourceExtraKey] = ""
		updates[NewAPICrossGroupRetryExtraKey] = false
		updates[NewAPIBalanceSnapshotExtraKey] = nil
	}
	if identityChanged || enabledChanged {
		updates[UpstreamBillingProbeExtraKey] = nil
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, updates); err != nil {
		return nil, err
	}
	updated, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	config := newAPIPublicConfigFromAccount(updated)
	if settings, settingsErr := s.getSettings(ctx); settingsErr == nil {
		config.BalanceSyncInterval = settings.IntervalMinutes
	}
	config.BalanceStale = newAPIBalanceIsStale(config, s.currentTime())
	return config, nil
}

func (s *UpstreamBillingProbeService) TestNewAPIConnection(ctx context.Context, accountID int64) (*NewAPISyncResult, error) {
	account, resolution, balance, err := s.resolveNewAPIAccountWithBalance(ctx, accountID)
	if err != nil {
		return nil, newAPISyncAPIError(err)
	}
	calibratedRatio, err := calibratedNewAPIAccountRatio(account, resolution)
	if err != nil {
		return nil, newAPISyncAPIError(err)
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	stampNewAPIBalanceSnapshot(balance, s.currentTime(), settings.IntervalMinutes)
	return &NewAPISyncResult{
		AccountID:       account.ID,
		Status:          newAPIResolutionStatus(resolution),
		OldRatio:        account.BillingRateMultiplier(),
		NewRatio:        &calibratedRatio,
		Resolution:      resolution,
		BalanceSnapshot: balance,
	}, nil
}

func (s *UpstreamBillingProbeService) SyncNewAPIAccount(ctx context.Context, accountID int64) (*NewAPISyncResult, error) {
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, err
	}
	result, err := s.syncNewAPIAccount(ctx, accountID, false, settings.IntervalMinutes)
	if err != nil {
		return result, newAPISyncAPIError(err)
	}
	return result, nil
}

func (s *UpstreamBillingProbeService) syncNewAPIAccount(
	ctx context.Context,
	accountID int64,
	requireEnabled bool,
	intervalMinutes int,
) (*NewAPISyncResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, ErrNewAPISyncUnavailable
	}
	key := strconv.FormatInt(accountID, 10)
	value, err, _ := s.newAPIGroup.Do(key, func() (any, error) {
		release, acquired, lockErr := s.tryAcquireLeaderLock(ctx, newAPISyncAccountLockPrefix+key)
		if lockErr != nil {
			return nil, ErrNewAPISyncUnavailable
		}
		if !acquired {
			if requireEnabled {
				return nil, nil
			}
			return nil, ErrNewAPISyncBusy
		}
		defer release()

		account, loadErr := s.accountRepo.GetByID(ctx, accountID)
		if loadErr != nil {
			return nil, loadErr
		}
		if !isNewAPISyncAccount(account) {
			return nil, ErrNewAPISyncAccountInvalid
		}
		if requireEnabled {
			if !account.IsActive() ||
				!newAPISyncEnabled(account) ||
				!newAPISyncDue(account, s.currentTime(), intervalMinutes) {
				return nil, nil
			}
		}
		return s.syncLoadedNewAPIAccount(ctx, account, intervalMinutes)
	})
	if err != nil || value == nil {
		return nil, err
	}
	result, ok := value.(*NewAPISyncResult)
	if !ok {
		return nil, ErrNewAPISyncUnavailable
	}
	return result, nil
}

func (s *UpstreamBillingProbeService) syncLoadedNewAPIAccount(
	ctx context.Context,
	account *Account,
	intervalMinutes int,
) (*NewAPISyncResult, error) {
	resolution, balance, err := s.resolveLoadedNewAPIAccountWithBalance(ctx, account)
	if err != nil {
		return s.persistNewAPISyncFailure(ctx, account, err)
	}
	calibratedRatio, err := calibratedNewAPIAccountRatio(account, resolution)
	if err != nil {
		return s.persistNewAPISyncFailure(ctx, account, err)
	}

	status := newAPIResolutionStatus(resolution)
	userGroup := resolution.UserGroup
	tokenGroup := resolution.TokenGroup
	actualGroup := resolution.ActualGroup
	ratioSource := resolution.RatioSource
	crossGroupRetry := resolution.CrossGroupRetry
	stored := newAPIStoredConfigFromAccount(account)
	previousSync := newAPIPublicConfigFromAccount(account)
	previousSchedulingSnapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	attemptedAt := s.currentTime().UTC()
	stampNewAPIBalanceSnapshot(balance, attemptedAt, intervalMinutes)
	schedulingSnapshot := newAPISchedulingSnapshot(resolution, balance, attemptedAt, intervalMinutes)
	balanceValue, balanceThreshold, notifyBalanceLow := applyUpstreamBalanceAlertSnapshot(
		account,
		previousSchedulingSnapshot,
		schedulingSnapshot,
		attemptedAt,
	)
	write := &NewAPISyncWrite{
		AccountID:                 account.ID,
		ExpectedIdentity:          stored.IdentityHash,
		ExpectedAccountBaseURL:    expectedNewAPIAccountBaseURL(account, stored),
		ExpectedAccountAPIKeyHash: newAPIAccountAPIKeyHash(account),
		AttemptedAt:               attemptedAt,
		Status:                    status,
		Error:                     "",
		UserGroup:                 &userGroup,
		TokenGroup:                &tokenGroup,
		ActualGroup:               &actualGroup,
		RatioSource:               &ratioSource,
		CrossGroupRetry:           &crossGroupRetry,
		Ratio:                     &calibratedRatio,
		BalanceSnapshot:           balance,
		SchedulingSnapshot:        schedulingSnapshot,
	}
	writeResult, err := s.writeNewAPISyncResult(ctx, write)
	if err != nil {
		return nil, err
	}
	result := &NewAPISyncResult{
		AccountID:          account.ID,
		Status:             status,
		Changed:            writeResult.Changed,
		OldRatio:           writeResult.OldRatio,
		NewRatio:           &calibratedRatio,
		Resolution:         resolution,
		BalanceSnapshot:    balance,
		SchedulingSnapshot: schedulingSnapshot,
	}
	if writeResult.Changed && resolution.Ratio != nil {
		slog.Info("newapi_ratio_sync_updated",
			"account_id", account.ID,
			"old_ratio", writeResult.OldRatio,
			"new_ratio", writeResult.NewRatio,
			"upstream_ratio", *resolution.Ratio,
			"calibration", openAIUpstreamRateCalibration(account),
			"user_group", resolution.UserGroup,
			"token_group", resolution.TokenGroup,
			"actual_group", resolution.ActualGroup,
			"ratio_source", resolution.RatioSource,
		)
		if s.opsService != nil && previousSync.LastSyncAt != nil {
			s.opsService.notifyUpstreamRateChange(account, writeResult.OldRatio, writeResult.NewRatio, "NewAPI ratio sync")
		}
	}
	if notifyBalanceLow && s.opsService != nil {
		s.opsService.notifyUpstreamBalanceLow(account, balanceValue, balanceThreshold)
	}
	if newAPIBalanceChanged(previousSync.BalanceSnapshot, balance) {
		slog.Info("newapi_balance_sync_updated",
			"account_id", account.ID,
			"account_remaining_quota", balance.Account.RemainingQuota,
			"token_remaining_quota", balance.Token.RemainingQuota,
			"token_unlimited_quota", balance.Token.UnlimitedQuota,
		)
	}
	for _, warning := range balance.Warnings {
		slog.Warn("newapi_balance_sync_warning", "account_id", account.ID, "warning", warning)
	}
	return result, nil
}

func (s *UpstreamBillingProbeService) persistNewAPISyncFailure(
	ctx context.Context,
	account *Account,
	syncErr error,
) (*NewAPISyncResult, error) {
	safeError := safeNewAPISyncError(syncErr)
	stored := newAPIStoredConfigFromAccount(account)
	writeResult, writeErr := s.writeNewAPISyncResult(ctx, &NewAPISyncWrite{
		AccountID:                 account.ID,
		ExpectedIdentity:          stored.IdentityHash,
		ExpectedAccountBaseURL:    expectedNewAPIAccountBaseURL(account, stored),
		ExpectedAccountAPIKeyHash: newAPIAccountAPIKeyHash(account),
		AttemptedAt:               s.currentTime().UTC(),
		Status:                    NewAPISyncStatusFailed,
		Error:                     safeError,
	})
	if writeErr != nil {
		return nil, writeErr
	}
	return &NewAPISyncResult{
		AccountID: account.ID,
		Status:    NewAPISyncStatusFailed,
		Changed:   writeResult.Changed,
		OldRatio:  writeResult.OldRatio,
		Error:     safeError,
	}, syncErr
}

func (s *UpstreamBillingProbeService) resolveNewAPIAccountWithBalance(
	ctx context.Context,
	accountID int64,
) (*Account, *NewAPIResolution, *NewAPIBalanceSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return nil, nil, nil, ErrNewAPISyncUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isNewAPISyncAccount(account) {
		return nil, nil, nil, ErrNewAPISyncAccountInvalid
	}
	resolution, balance, err := s.resolveLoadedNewAPIAccountWithBalance(ctx, account)
	return account, resolution, balance, err
}

func (s *UpstreamBillingProbeService) resolveLoadedNewAPIAccountWithBalance(
	ctx context.Context,
	account *Account,
) (*NewAPIResolution, *NewAPIBalanceSnapshot, error) {
	client, connection, err := s.newAPIConnectionForAccount(account)
	if err != nil {
		return nil, nil, err
	}
	return client.ResolveWithBalance(ctx, connection)
}

func (s *UpstreamBillingProbeService) newAPIConnectionForAccount(
	account *Account,
) (*NewAPIClient, NewAPIConnection, error) {
	if s.encryptor == nil {
		return nil, NewAPIConnection{}, ErrNewAPISyncUnavailable
	}
	stored := newAPIStoredConfigFromAccount(account)
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if stored.UserID <= 0 || stored.UserAccessToken == "" || apiKey == "" {
		return nil, NewAPIConnection{}, newAPIClientError("configuration_incomplete")
	}
	baseURL, err := s.resolveNewAPIBaseURL(account, stored.BaseURL)
	if err != nil {
		return nil, NewAPIConnection{}, newAPIClientError("base_url_invalid")
	}
	accessToken, err := s.encryptor.Decrypt(stored.UserAccessToken)
	if err != nil {
		return nil, NewAPIConnection{}, newAPIClientError("credential_decrypt_failed")
	}
	client, err := s.newAPIClientForAccount(account)
	if err != nil {
		return nil, NewAPIConnection{}, newAPIClientError("client_unavailable")
	}
	return client, NewAPIConnection{
		BaseURL:         baseURL,
		UserAccessToken: accessToken,
		UserID:          stored.UserID,
		APIKey:          apiKey,
	}, nil
}

func (s *UpstreamBillingProbeService) newAPIClientForAccount(account *Account) (*NewAPIClient, error) {
	if s != nil && s.newAPIClientFactory != nil {
		return s.newAPIClientFactory(account)
	}
	proxyURL := ""
	if account != nil && account.ProxyID != nil {
		if account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
			return nil, errors.New("proxy is unavailable")
		}
		proxyURL = account.Proxy.URL()
	}
	allowPrivate := s.cfg != nil && s.cfg.Security.URLAllowlist.AllowPrivateHosts
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               newAPIRequestTimeout,
		ResponseHeaderTimeout: 8 * time.Second,
		ValidateResolvedIP:    !allowPrivate,
		AllowPrivateHosts:     allowPrivate,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       8,
	})
	if err != nil {
		return nil, err
	}
	return NewNewAPIClient(client), nil
}

func (s *UpstreamBillingProbeService) validateNewAPIBaseURL(raw string) (string, error) {
	allowInsecure := false
	allowPrivate := false
	var allowedHosts []string
	requireAllowlist := false
	if s != nil && s.cfg != nil {
		allowInsecure = s.cfg.Security.URLAllowlist.AllowInsecureHTTP
		allowPrivate = s.cfg.Security.URLAllowlist.AllowPrivateHosts
		if s.cfg.Security.URLAllowlist.Enabled {
			allowedHosts = s.cfg.Security.URLAllowlist.UpstreamHosts
			requireAllowlist = true
		}
	}
	normalized, err := urlvalidator.ValidateHTTPURL(raw, allowInsecure, urlvalidator.ValidationOptions{
		AllowedHosts:     allowedHosts,
		RequireAllowlist: requireAllowlist,
		AllowPrivate:     allowPrivate,
	})
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("base URL must not contain credentials, query, or fragment")
	}
	return strings.TrimRight(normalized, "/"), nil
}

func (s *UpstreamBillingProbeService) resolveNewAPIBaseURL(account *Account, configured string) (string, error) {
	raw := strings.TrimSpace(configured)
	if raw == "" && account != nil {
		raw = strings.TrimSpace(account.GetOpenAIBaseURL())
	}
	if raw == "" {
		return "", errors.New("NewAPI base URL is unavailable")
	}
	return s.validateNewAPIBaseURL(raw)
}

func (s *UpstreamBillingProbeService) writeNewAPISyncResult(
	ctx context.Context,
	write *NewAPISyncWrite,
) (*NewAPISyncWriteResult, error) {
	writer, ok := s.accountRepo.(newAPISyncResultWriter)
	if !ok {
		return nil, ErrNewAPISyncUnavailable
	}
	return writer.UpdateNewAPISyncResult(ctx, write)
}

func (s *UpstreamBillingProbeService) RunNewAPIDue(ctx context.Context) error {
	if s == nil || s.accountRepo == nil {
		return nil
	}
	settings, err := s.getSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	s.newAPICycleMu.Lock()
	defer s.newAPICycleMu.Unlock()

	release, acquired, err := s.tryAcquireLeaderLock(ctx, newAPISyncLeaderLockKey)
	if err != nil {
		return ErrNewAPISyncUnavailable
	}
	if !acquired {
		return nil
	}
	defer release()

	accounts, err := s.accountRepo.FindByExtraField(ctx, NewAPISyncEnabledExtraKey, true)
	if err != nil {
		return err
	}
	now := s.currentTime()
	due := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if account.IsActive() && isNewAPISyncAccount(&account) &&
			newAPISyncDue(&account, now, settings.IntervalMinutes) {
			due = append(due, account)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		left := newAPIPublicConfigFromAccount(&due[i]).LastSyncAt
		right := newAPIPublicConfigFromAccount(&due[j]).LastSyncAt
		if left == nil && right == nil {
			return due[i].ID < due[j].ID
		}
		if left == nil {
			return true
		}
		if right == nil {
			return false
		}
		return left.Before(*right)
	})
	if len(due) > newAPIMaxPerCycle {
		due = due[:newAPIMaxPerCycle]
	}
	var group errgroup.Group
	for i := range due {
		accountID := due[i].ID
		group.Go(func() error {
			if _, syncErr := s.syncNewAPIAccount(ctx, accountID, true, settings.IntervalMinutes); syncErr != nil {
				slog.Warn("newapi_ratio_sync_failed",
					"account_id", accountID,
					"error", safeNewAPISyncError(syncErr),
				)
			}
			return nil
		})
	}
	return group.Wait()
}

func newAPIPublicConfigFromAccount(account *Account) *NewAPISyncConfig {
	config := &NewAPISyncConfig{
		LastSyncStatus: NewAPISyncStatusNever,
	}
	if account == nil {
		return config
	}
	config.CurrentRatio = account.BillingRateMultiplier()
	config.HasAPIKey = strings.TrimSpace(account.GetCredential("api_key")) != ""
	if account.Extra != nil {
		raw, err := json.Marshal(account.Extra)
		if err == nil {
			_ = json.Unmarshal(raw, config)
		}
	}
	stored := newAPIStoredConfigFromAccount(account)
	config.BalanceSyncEnabled = stored.Enabled
	config.HasUserAccessToken = stored.UserAccessToken != ""
	if config.HasUserAccessToken {
		config.UserAccessToken = NewAPISecretMask
	} else {
		config.UserAccessToken = ""
	}
	switch config.LastSyncStatus {
	case NewAPISyncStatusNever, NewAPISyncStatusOK, NewAPISyncStatusFailed:
	default:
		config.LastSyncStatus = NewAPISyncStatusNever
	}
	if config.RatioSource != NewAPIRatioSourceConfiguredGroup {
		config.RatioSource = ""
	}
	return config
}

func stampNewAPIBalanceSnapshot(snapshot *NewAPIBalanceSnapshot, now time.Time, intervalMinutes int) {
	if snapshot == nil {
		return
	}
	settings := UpstreamBillingProbeSettings{IntervalMinutes: intervalMinutes}
	normalizeUpstreamBillingProbeSettings(&settings)
	now = now.UTC()
	snapshot.SyncedAt = now
	snapshot.FreshUntil = now.Add(2 * time.Duration(settings.IntervalMinutes) * time.Minute)
}

func newAPIBalanceIsStale(config *NewAPISyncConfig, now time.Time) bool {
	if config == nil || config.BalanceSnapshot == nil || config.BalanceSnapshot.SyncedAt.IsZero() {
		return true
	}
	return config.LastSyncStatus == NewAPISyncStatusFailed ||
		config.BalanceSnapshot.FreshUntil.IsZero() ||
		!now.Before(config.BalanceSnapshot.FreshUntil)
}

func newAPIBalanceChanged(left, right *NewAPIBalanceSnapshot) bool {
	if right == nil {
		return false
	}
	if left == nil {
		return true
	}
	return left.Account != right.Account || left.Token != right.Token
}

func newAPIStoredConfigFromAccount(account *Account) newAPISyncStoredConfig {
	stored := newAPISyncStoredConfig{}
	if account == nil || account.Extra == nil {
		return stored
	}
	raw, err := json.Marshal(account.Extra)
	if err == nil {
		_ = json.Unmarshal(raw, &stored)
	}
	return stored
}

func newAPISyncIdentity(baseURL string, userID int64, encryptedAccessToken string) string {
	value := strings.Join([]string{
		strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		strconv.FormatInt(userID, 10),
		encryptedAccessToken,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func secretShouldReplace(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed != NewAPISecretMask
}

func isNewAPISyncAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey
}

func newAPISyncEnabled(account *Account) bool {
	return newAPIStoredConfigFromAccount(account).Enabled
}

// NewAPISyncEnabled is used by persistence code while holding the account row
// lock, so ownership is rechecked against current state rather than a stale
// service-layer object.
func NewAPISyncEnabled(account *Account) bool {
	return newAPISyncEnabled(account)
}

func expectedNewAPIAccountBaseURL(account *Account, stored newAPISyncStoredConfig) *string {
	if account == nil || strings.TrimSpace(stored.BaseURL) != "" {
		return nil
	}
	value := strings.TrimSpace(account.GetCredential("base_url"))
	return &value
}

func newAPIAccountAPIKeyHash(account *Account) string {
	if account == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(account.GetCredential("api_key"))))
	return hex.EncodeToString(sum[:])
}

// preserveNewAPISyncManagedExtra keeps the generic account update path from
// replacing encrypted credentials with DTO masks or mutating synchronization
// state. These fields are owned exclusively by the dedicated admin endpoints.
func preserveNewAPISyncManagedExtra(account *Account, extra map[string]any) map[string]any {
	if account == nil {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any)
	}
	for _, key := range newAPISyncManagedExtraKeys {
		value, exists := account.Extra[key]
		if exists {
			extra[key] = value
		} else {
			delete(extra, key)
		}
	}
	return extra
}

// PreserveNewAPISyncManagedExtra exposes the same merge rule to the
// repository's row-locked update path, where it protects against a concurrent
// dedicated NewAPI configuration save being overwritten by a stale edit form.
func PreserveNewAPISyncManagedExtra(account *Account, extra map[string]any) map[string]any {
	return preserveNewAPISyncManagedExtra(account, extra)
}

func newAPISyncDue(account *Account, now time.Time, intervalMinutes int) bool {
	if !newAPISyncEnabled(account) {
		return false
	}
	config := newAPIPublicConfigFromAccount(account)
	if config.LastSyncAt == nil {
		return true
	}
	if intervalMinutes < upstreamBillingProbeMinIntervalMinutes {
		intervalMinutes = upstreamBillingProbeMinIntervalMinutes
	}
	if intervalMinutes > upstreamBillingProbeMaxIntervalMinutes {
		intervalMinutes = upstreamBillingProbeMaxIntervalMinutes
	}
	return !now.Before(config.LastSyncAt.Add(time.Duration(intervalMinutes) * time.Minute))
}

func newAPIResolutionStatus(resolution *NewAPIResolution) string {
	if resolution == nil {
		return NewAPISyncStatusFailed
	}
	return NewAPISyncStatusOK
}

func safeNewAPISyncError(err error) string {
	if err == nil {
		return ""
	}
	var safe *newAPISafeError
	if errors.As(err, &safe) {
		return safe.Error()
	}
	switch {
	case errors.Is(err, ErrNewAPISyncUnavailable):
		return "newapi_sync_unavailable"
	case errors.Is(err, ErrNewAPISyncIdentityChanged):
		return "newapi_identity_changed"
	case errors.Is(err, ErrNewAPISyncBusy):
		return "newapi_sync_busy"
	default:
		return "newapi_sync_failed"
	}
}

func safeSchedulingCostRefreshError(err error) string {
	if err == nil {
		return ""
	}
	var applicationError *infraerrors.ApplicationError
	if errors.As(err, &applicationError) {
		if applicationError.Reason != "" {
			return applicationError.Reason
		}
		return "scheduling_cost_refresh_failed"
	}
	var safe *newAPISafeError
	if errors.As(err, &safe) {
		return safe.Error()
	}
	return safeProbeError(err)
}

func newAPISchedulingSnapshot(
	resolution *NewAPIResolution,
	balance *NewAPIBalanceSnapshot,
	now time.Time,
	intervalMinutes int,
) *UpstreamBillingProbeSnapshot {
	if resolution == nil || resolution.Ratio == nil {
		return nil
	}
	ratio := *resolution.Ratio
	if ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return nil
	}
	settings := UpstreamBillingProbeSettings{IntervalMinutes: intervalMinutes}
	normalizeUpstreamBillingProbeSettings(&settings)
	interval := time.Duration(settings.IntervalMinutes) * time.Minute
	now = now.UTC()
	data := map[string]any{
		"object":                    "newapi.group_ratio",
		"schema_version":            1,
		"source":                    "newapi",
		"billing_scope":             "token",
		"group_rate_multiplier":     ratio,
		"resolved_rate_multiplier":  ratio,
		"peak_rate_enabled":         false,
		"effective_rate_multiplier": ratio,
		"observed_at":               now.Format(time.RFC3339Nano),
		"newapi_group":              resolution.ActualGroup,
	}
	if accountBalance, ok := newAPIAccountBalanceUSD(balance); ok {
		data["balance"] = accountBalance
		data["balance_kind"] = "wallet"
	}
	return &UpstreamBillingProbeSnapshot{
		Status:        UpstreamBillingProbeStatusOK,
		Data:          data,
		ReceivedAt:    probeTimePtr(now),
		FreshUntil:    probeTimePtr(now.Add(2 * interval)),
		LastAttemptAt: now,
		NextProbeAt:   now.Add(interval),
		HTTPStatus:    http.StatusOK,
	}
}

func newAPIAccountBalanceUSD(balance *NewAPIBalanceSnapshot) (float64, bool) {
	if balance == nil || balance.QuotaDisplay == nil || balance.Account.RemainingQuota < 0 {
		return 0, false
	}
	quotaPerUnit := balance.QuotaDisplay.QuotaPerUnit
	if quotaPerUnit <= 0 || math.IsNaN(quotaPerUnit) || math.IsInf(quotaPerUnit, 0) {
		return 0, false
	}
	value := float64(balance.Account.RemainingQuota) / quotaPerUnit
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func calibratedNewAPIAccountRatio(account *Account, resolution *NewAPIResolution) (float64, error) {
	if resolution == nil || resolution.Ratio == nil {
		return 0, newAPIClientError("calibrated_ratio_invalid")
	}
	rawRatio := *resolution.Ratio
	if rawRatio <= 0 || math.IsNaN(rawRatio) || math.IsInf(rawRatio, 0) {
		return 0, newAPIClientError("calibrated_ratio_invalid")
	}
	ratio := rawRatio * openAIUpstreamRateCalibration(account)
	if ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0, newAPIClientError("calibrated_ratio_invalid")
	}
	return ratio, nil
}

func newAPISyncAPIError(err error) error {
	if err == nil {
		return nil
	}
	var applicationError *infraerrors.ApplicationError
	if errors.As(err, &applicationError) {
		return err
	}
	var safe *newAPISafeError
	if errors.As(err, &safe) {
		reason := safe.Error()
		if strings.Contains(reason, "_http_") {
			reason = "newapi_http_error"
		}
		return infraerrors.New(
			http.StatusBadGateway,
			reason,
			"NewAPI connection or ratio resolution failed",
		).WithCause(err)
	}
	return err
}
