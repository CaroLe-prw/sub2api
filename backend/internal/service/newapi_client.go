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

// NewAPIClient implements only the NewAPI calls needed by ratio synchronization.
// It deliberately disables redirects so credentials cannot cross origins.
type NewAPIClient struct {
	doer         newAPIHTTPDoer
	requestLimit int64
	timeout      time.Duration
}

func NewNewAPIClient(doer newAPIHTTPDoer) *NewAPIClient {
	if client, ok := doer.(*http.Client); ok && client != nil {
		clone := *client
		clone.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
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
	ID    int64  `json:"id"`
	Group string `json:"group"`
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
	req.Header.Set("Authorization", "Bearer "+bearerToken)
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
	if strings.Contains(strings.ToLower(string(body)), marker) {
		return true
	}
	var envelope newAPIEnvelope
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	return (status == http.StatusUnauthorized || !envelope.Success) &&
		strings.Contains(strings.ToLower(envelope.Message), marker)
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
