package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	newAPIRequestTimeout = 10 * time.Second
	newAPIMaxBodyBytes   = 256 * 1024
	newAPIMaxSafeInteger = int64(1<<53 - 1)

	newAPITokenStatusEnabled = 1
)

type newAPIHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// NewAPIConnection contains the credentials needed for one discovery run.
// Callers must never log this value or errors from lower-level transports.
type NewAPIConnection struct {
	BaseURL         string
	UserAccessToken string
	UserID          int64
	APIKey          string
}

type NewAPIResolution struct {
	UserGroup       string   `json:"user_group"`
	TokenGroup      string   `json:"token_group"`
	ActualGroup     string   `json:"actual_group"`
	CrossGroupRetry bool     `json:"cross_group_retry"`
	Ratio           *float64 `json:"ratio,omitempty"`
	RatioSource     string   `json:"ratio_source,omitempty"`
}

type NewAPIBalanceAccount struct {
	UserID         int64  `json:"user_id"`
	Group          string `json:"group"`
	RemainingQuota int64  `json:"remaining_quota"`
	UsedQuota      int64  `json:"used_quota"`
	TotalQuota     int64  `json:"total_quota"`
}

type NewAPIBalanceToken struct {
	Name           string `json:"name"`
	RemainingQuota int64  `json:"remaining_quota"`
	UsedQuota      int64  `json:"used_quota"`
	TotalQuota     int64  `json:"total_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	ExpiresAt      int64  `json:"expires_at"`
}

type NewAPIQuotaDisplay struct {
	DisplayType  string  `json:"display_type"`
	Symbol       string  `json:"symbol,omitempty"`
	QuotaPerUnit float64 `json:"quota_per_unit"`
	ExchangeRate float64 `json:"exchange_rate"`
}

type NewAPIBalanceSnapshot struct {
	Account          NewAPIBalanceAccount `json:"account"`
	Token            NewAPIBalanceToken   `json:"token"`
	QuotaDisplay     *NewAPIQuotaDisplay  `json:"quota_display,omitempty"`
	TokenAvailable   bool                 `json:"token_available"`
	AccountAvailable bool                 `json:"account_available"`
	OverallAvailable bool                 `json:"overall_available"`
	Warnings         []string             `json:"warnings,omitempty"`
	SyncedAt         time.Time            `json:"synced_at"`
	FreshUntil       time.Time            `json:"fresh_until"`
}

// NewAPIClient implements the NewAPI calls needed by ratio and balance
// synchronization. Same-origin redirects are allowed for endpoint slash
// compatibility; cross-origin redirects are stopped before credentials leave
// the configured origin.
type NewAPIClient struct {
	doer         newAPIHTTPDoer
	requestLimit int64
	timeout      time.Duration
}

func NewNewAPIClient(doer newAPIHTTPDoer) *NewAPIClient {
	if client, ok := doer.(*http.Client); ok && client != nil {
		clone := *client
		previousCheckRedirect := clone.CheckRedirect
		clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 || len(via) == 0 || !sameHTTPOrigin(via[0].URL, req.URL) {
				return http.ErrUseLastResponse
			}
			if previousCheckRedirect != nil {
				return previousCheckRedirect(req, via)
			}
			return nil
		}
		doer = &clone
	}
	return &NewAPIClient{
		doer:         doer,
		requestLimit: newAPIMaxBodyBytes,
		timeout:      newAPIRequestTimeout,
	}
}

type newAPIEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type newAPIUser struct {
	ID        int64           `json:"id"`
	Group     string          `json:"group"`
	Quota     json.RawMessage `json:"quota"`
	UsedQuota json.RawMessage `json:"used_quota"`
}

type newAPITokenUsageEnvelope struct {
	Code    bool                  `json:"code"`
	Message string                `json:"message"`
	Data    *newAPITokenUsageData `json:"data"`
}

type newAPITokenUsageData struct {
	Name           string          `json:"name"`
	TotalGranted   json.RawMessage `json:"total_granted"`
	TotalUsed      json.RawMessage `json:"total_used"`
	TotalAvailable json.RawMessage `json:"total_available"`
	UnlimitedQuota bool            `json:"unlimited_quota"`
	ExpiresAt      json.RawMessage `json:"expires_at"`
}

type newAPITokenSearchPage struct {
	Total int           `json:"total"`
	Items []newAPIToken `json:"items"`
}

type newAPIToken struct {
	UserID          int64  `json:"user_id"`
	Status          int    `json:"status"`
	Group           string `json:"group"`
	CrossGroupRetry bool   `json:"cross_group_retry"`
}

type newAPIStatusData struct {
	DisplayInCurrency          *bool           `json:"display_in_currency"`
	QuotaDisplayType           string          `json:"quota_display_type"`
	QuotaPerUnit               json.RawMessage `json:"quota_per_unit"`
	USDExchangeRate            json.RawMessage `json:"usd_exchange_rate"`
	CustomCurrencySymbol       string          `json:"custom_currency_symbol"`
	CustomCurrencyExchangeRate json.RawMessage `json:"custom_currency_exchange_rate"`
}

type newAPIGroup struct {
	Ratio json.RawMessage `json:"ratio"`
}

func (c *NewAPIClient) Resolve(ctx context.Context, connection NewAPIConnection) (*NewAPIResolution, error) {
	if c == nil || c.doer == nil {
		return nil, newAPIClientError("client_unavailable")
	}
	if strings.TrimSpace(connection.BaseURL) == "" ||
		strings.TrimSpace(connection.UserAccessToken) == "" ||
		connection.UserID <= 0 ||
		strings.TrimSpace(connection.APIKey) == "" {
		return nil, newAPIClientError("configuration_incomplete")
	}

	user, err := c.getUser(ctx, connection)
	if err != nil {
		return nil, err
	}
	if user.ID != connection.UserID {
		return nil, newAPIClientError("user_id_mismatch")
	}
	return c.resolveWithUser(ctx, connection, user)
}

func (c *NewAPIClient) ResolveWithBalance(
	ctx context.Context,
	connection NewAPIConnection,
) (*NewAPIResolution, *NewAPIBalanceSnapshot, error) {
	if c == nil || c.doer == nil {
		return nil, nil, newAPIClientError("client_unavailable")
	}
	if strings.TrimSpace(connection.BaseURL) == "" ||
		strings.TrimSpace(connection.UserAccessToken) == "" ||
		connection.UserID <= 0 ||
		strings.TrimSpace(connection.APIKey) == "" {
		return nil, nil, newAPIClientError("configuration_incomplete")
	}

	user, err := c.getUser(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	if user.ID != connection.UserID {
		return nil, nil, newAPIClientError("user_id_mismatch")
	}
	quotaDisplay := c.getQuotaDisplay(ctx, connection.BaseURL)
	accountBalance, err := newAPIAccountBalance(user)
	if err != nil {
		return nil, nil, err
	}
	tokenBalance, warnings, err := c.getTokenUsage(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	resolution, err := c.resolveWithUser(ctx, connection, user)
	if err != nil {
		return nil, nil, err
	}
	tokenAvailable := tokenBalance.UnlimitedQuota || tokenBalance.RemainingQuota > 0
	accountAvailable := accountBalance.RemainingQuota > 0
	return resolution, &NewAPIBalanceSnapshot{
		Account:          *accountBalance,
		Token:            *tokenBalance,
		QuotaDisplay:     quotaDisplay,
		TokenAvailable:   tokenAvailable,
		AccountAvailable: accountAvailable,
		OverallAvailable: tokenAvailable && accountAvailable,
		Warnings:         warnings,
	}, nil
}

func (c *NewAPIClient) getQuotaDisplay(ctx context.Context, baseURL string) *NewAPIQuotaDisplay {
	status, body, err := c.get(ctx, baseURL, "/api/status", nil, "", 0)
	if err != nil || status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil
	}
	var data newAPIStatusData
	if err := decodeNewAPIEnvelope(body, &data); err != nil {
		return nil
	}
	return normalizeNewAPIQuotaDisplay(&data)
}

func normalizeNewAPIQuotaDisplay(data *newAPIStatusData) *NewAPIQuotaDisplay {
	if data == nil {
		return nil
	}
	quotaPerUnit, err := parseNewAPIPositiveNumber(data.QuotaPerUnit)
	if err != nil {
		return nil
	}

	displayType := strings.ToUpper(strings.TrimSpace(data.QuotaDisplayType))
	if displayType == "" {
		if data.DisplayInCurrency != nil && !*data.DisplayInCurrency {
			displayType = "TOKENS"
		} else {
			displayType = "USD"
		}
	}

	display := &NewAPIQuotaDisplay{
		DisplayType:  displayType,
		QuotaPerUnit: quotaPerUnit,
		ExchangeRate: 1,
	}
	switch displayType {
	case "USD":
		display.Symbol = "$"
	case "CNY":
		exchangeRate, parseErr := parseNewAPIPositiveNumber(data.USDExchangeRate)
		if parseErr != nil {
			return nil
		}
		display.Symbol = "¥"
		display.ExchangeRate = exchangeRate
	case "CUSTOM":
		exchangeRate, parseErr := parseNewAPIPositiveNumber(data.CustomCurrencyExchangeRate)
		if parseErr != nil || strings.TrimSpace(data.CustomCurrencySymbol) == "" {
			return nil
		}
		display.Symbol = strings.TrimSpace(data.CustomCurrencySymbol)
		display.ExchangeRate = exchangeRate
	case "TOKENS":
	default:
		return nil
	}
	return display
}

func (c *NewAPIClient) resolveWithUser(
	ctx context.Context,
	connection NewAPIConnection,
	user *newAPIUser,
) (*NewAPIResolution, error) {
	token, err := c.searchToken(ctx, connection)
	if err != nil {
		return nil, err
	}
	if token.UserID != connection.UserID {
		return nil, newAPIClientError("token_user_mismatch")
	}
	if token.Status != newAPITokenStatusEnabled {
		return nil, newAPIClientError("token_not_enabled")
	}

	groups, err := c.getGroups(ctx, connection)
	if err != nil {
		return nil, err
	}

	result := &NewAPIResolution{
		UserGroup:       strings.TrimSpace(user.Group),
		TokenGroup:      strings.TrimSpace(token.Group),
		CrossGroupRetry: token.CrossGroupRetry,
	}
	usingGroup := result.TokenGroup
	if usingGroup == "" {
		usingGroup = result.UserGroup
	}
	if strings.EqualFold(result.TokenGroup, "auto") {
		return nil, newAPIClientError("auto_group_unsupported")
	}
	if usingGroup == "" {
		return nil, newAPIClientError("effective_group_empty")
	}
	group, ok := groups[usingGroup]
	if !ok {
		return nil, newAPIClientError("effective_group_not_found")
	}
	ratio, err := parsePositiveRatio(group.Ratio)
	if err != nil {
		return nil, newAPIClientError("effective_ratio_invalid")
	}
	result.Ratio = float64Pointer(ratio)
	result.RatioSource = NewAPIRatioSourceConfiguredGroup
	result.ActualGroup = usingGroup
	return result, nil
}

func (c *NewAPIClient) getTokenUsage(
	ctx context.Context,
	connection NewAPIConnection,
) (*NewAPIBalanceToken, []string, error) {
	status, body, err := c.get(
		ctx,
		connection.BaseURL,
		"/api/usage/token/",
		nil,
		connection.APIKey,
		0,
	)
	if err != nil {
		return nil, nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, nil, newAPIHTTPError("token_usage", status)
	}
	var envelope newAPITokenUsageEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Code || envelope.Data == nil {
		return nil, nil, newAPIClientError("token_usage_invalid_response")
	}
	remaining, err := parseNewAPIQuotaWithNegative(
		envelope.Data.TotalAvailable,
		envelope.Data.UnlimitedQuota,
	)
	if err != nil {
		return nil, nil, newAPIClientError("token_remaining_quota_invalid")
	}
	used, err := parseNewAPIQuota(envelope.Data.TotalUsed)
	if err != nil {
		return nil, nil, newAPIClientError("token_used_quota_invalid")
	}
	total, err := parseNewAPIQuotaWithNegative(
		envelope.Data.TotalGranted,
		envelope.Data.UnlimitedQuota,
	)
	if err != nil {
		return nil, nil, newAPIClientError("token_total_quota_invalid")
	}
	expiresAt, err := parseNewAPIQuota(envelope.Data.ExpiresAt)
	if err != nil {
		return nil, nil, newAPIClientError("token_expires_at_invalid")
	}
	warnings := make([]string, 0, 1)
	if remaining > newAPIMaxSafeInteger-used || total != remaining+used {
		warnings = append(warnings, "newapi_token_quota_mismatch")
	}
	return &NewAPIBalanceToken{
		Name:           strings.TrimSpace(envelope.Data.Name),
		RemainingQuota: remaining,
		UsedQuota:      used,
		TotalQuota:     total,
		UnlimitedQuota: envelope.Data.UnlimitedQuota,
		ExpiresAt:      expiresAt,
	}, warnings, nil
}

func newAPIAccountBalance(user *newAPIUser) (*NewAPIBalanceAccount, error) {
	if user == nil {
		return nil, newAPIClientError("user_self_invalid_response")
	}
	// NewAPI permits an account to overdraw, so the remaining account quota can
	// legitimately be negative. Keep the safe-integer bound because snapshots
	// are serialized to JavaScript numbers in the admin UI.
	remaining, err := parseNewAPIQuotaWithNegative(user.Quota, true)
	if err != nil {
		return nil, newAPIClientError("account_remaining_quota_invalid")
	}
	used, err := parseNewAPIQuota(user.UsedQuota)
	if err != nil {
		return nil, newAPIClientError("account_used_quota_invalid")
	}
	if remaining > newAPIMaxSafeInteger-used {
		return nil, newAPIClientError("account_total_quota_invalid")
	}
	return &NewAPIBalanceAccount{
		UserID:         user.ID,
		Group:          strings.TrimSpace(user.Group),
		RemainingQuota: remaining,
		UsedQuota:      used,
		TotalQuota:     remaining + used,
	}, nil
}

func (c *NewAPIClient) getUser(ctx context.Context, connection NewAPIConnection) (*newAPIUser, error) {
	status, body, err := c.get(ctx, connection.BaseURL, "/api/user/self", nil, connection.UserAccessToken, 0)
	if err != nil {
		return nil, err
	}
	if newAPIRequiresUserHeader(status, body) {
		status, body, err = c.get(ctx, connection.BaseURL, "/api/user/self", nil, connection.UserAccessToken, connection.UserID)
		if err != nil {
			return nil, err
		}
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, newAPIHTTPError("user_self", status)
	}
	var user newAPIUser
	if err := decodeNewAPIEnvelope(body, &user); err != nil {
		return nil, newAPIClientError("user_self_invalid_response")
	}
	return &user, nil
}

func (c *NewAPIClient) searchToken(ctx context.Context, connection NewAPIConnection) (*newAPIToken, error) {
	query := url.Values{}
	query.Set("token", connection.APIKey)
	query.Set("p", "1")
	query.Set("size", "10")
	status, body, err := c.get(
		ctx,
		connection.BaseURL,
		"/api/token/search",
		query,
		connection.UserAccessToken,
		connection.UserID,
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, newAPIHTTPError("token_search", status)
	}
	var page newAPITokenSearchPage
	if err := decodeNewAPIEnvelope(body, &page); err != nil {
		return nil, newAPIClientError("token_search_invalid_response")
	}
	if page.Total != 1 || len(page.Items) != 1 {
		return nil, newAPIClientError("token_search_not_unique")
	}
	return &page.Items[0], nil
}

func (c *NewAPIClient) getGroups(ctx context.Context, connection NewAPIConnection) (map[string]newAPIGroup, error) {
	status, body, err := c.get(
		ctx,
		connection.BaseURL,
		"/api/user/self/groups",
		nil,
		connection.UserAccessToken,
		connection.UserID,
	)
	if err != nil {
		return nil, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, newAPIHTTPError("user_groups", status)
	}
	groups := make(map[string]newAPIGroup)
	if err := decodeNewAPIEnvelope(body, &groups); err != nil {
		return nil, newAPIClientError("user_groups_invalid_response")
	}
	return groups, nil
}

func (c *NewAPIClient) get(
	ctx context.Context,
	baseURL string,
	path string,
	query url.Values,
	bearerToken string,
	userID int64,
) (int, []byte, error) {
	limit := c.requestLimit
	if limit <= 0 {
		limit = newAPIMaxBodyBytes
	}
	requestURL := strings.TrimRight(baseURL, "/") + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = newAPIRequestTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, newAPIClientError("request_build_failed")
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if userID > 0 {
		req.Header.Set("New-Api-User", strconv.FormatInt(userID, 10))
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(requestCtx.Err(), context.DeadlineExceeded) {
			return 0, nil, newAPIClientError("request_timeout")
		}
		return 0, nil, newAPIClientError("request_failed")
	}
	if resp == nil || resp.Body == nil {
		return 0, nil, newAPIClientError("empty_response")
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, nil, newAPIClientError("response_read_failed")
	}
	if int64(len(body)) > limit {
		return 0, nil, newAPIClientError("response_too_large")
	}
	return resp.StatusCode, body, nil
}

func decodeNewAPIEnvelope(body []byte, target any) error {
	var envelope newAPIEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if !envelope.Success || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("newapi request was not successful")
	}
	return json.Unmarshal(envelope.Data, target)
}

func newAPIRequiresUserHeader(status int, body []byte) bool {
	const marker = "new-api-user header not provided"
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return false
	}
	if strings.Contains(strings.ToLower(string(body)), marker) {
		return true
	}
	var envelope newAPIEnvelope
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	return strings.Contains(strings.ToLower(envelope.Message), marker)
}

func parsePositiveRatio(raw json.RawMessage) (float64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || strings.HasPrefix(trimmed, `"`) {
		return 0, errors.New("ratio must be a JSON number")
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, errors.New("ratio must be finite and greater than zero")
	}
	return value, nil
}

func parseNewAPIPositiveNumber(raw json.RawMessage) (float64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, errors.New("number is missing")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var numeric string
		if err := json.Unmarshal(raw, &numeric); err != nil {
			return 0, errors.New("number is not numeric")
		}
		trimmed = strings.TrimSpace(numeric)
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, errors.New("number must be finite and greater than zero")
	}
	return value, nil
}

func parseNewAPIQuota(raw json.RawMessage) (int64, error) {
	return parseNewAPIQuotaWithNegative(raw, false)
}

func parseNewAPIQuotaWithNegative(raw json.RawMessage, allowNegative bool) (int64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, errors.New("quota is missing")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var numeric string
		if err := json.Unmarshal(raw, &numeric); err != nil {
			return 0, errors.New("quota is not numeric")
		}
		trimmed = strings.TrimSpace(numeric)
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	minimum := float64(0)
	if allowNegative {
		minimum = -float64(newAPIMaxSafeInteger)
	}
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) ||
		value < minimum || value > float64(newAPIMaxSafeInteger) || math.Trunc(value) != value {
		return 0, errors.New("quota is not a safe integer")
	}
	return int64(value), nil
}

func sameHTTPOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}

func float64Pointer(value float64) *float64 {
	return &value
}

type newAPISafeError struct {
	code string
}

func (e *newAPISafeError) Error() string {
	if e == nil {
		return "newapi_sync_failed"
	}
	return e.code
}

func newAPIClientError(code string) error {
	return &newAPISafeError{code: "newapi_" + code}
}

func newAPIHTTPError(endpoint string, status int) error {
	return newAPIClientError(fmt.Sprintf("%s_http_%d", endpoint, status))
}
