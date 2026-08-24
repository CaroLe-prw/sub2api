package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIStickyCredentialFailoverCache struct {
	service.GatewayCache
	mu        sync.Mutex
	accountID int64
}

func (c *openAIStickyCredentialFailoverCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountID, nil
}

func (c *openAIStickyCredentialFailoverCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	// HTTP response ownership shares the GatewayCache interface but uses an
	// independent keyspace; it must not overwrite the sticky-session probe.
	if strings.HasPrefix(sessionHash, "openai:http-response-owner:") {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accountID = accountID
	return nil
}

func (c *openAIStickyCredentialFailoverCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *openAIStickyCredentialFailoverCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *openAIStickyCredentialFailoverCache) currentAccountID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountID
}

type openAICredentialSwitchUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
}

func (u *openAICredentialSwitchUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_credential_fallback","object":"response","status":"completed","model":"gpt-5.6-sol","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}, nil
}

func (u *openAICredentialSwitchUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func TestOpenAIResponses_StickyCredentialFailureSwitchesToNextAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(4214)
	accounts := []service.Account{
		{
			ID: 319, Name: "sticky-missing-credential", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"base_url": "https://api.example.test"},
			Extra:       map[string]any{"openai_passthrough": true},
		},
		{
			ID: 320, Name: "healthy-fallback", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-fallback", "base_url": "https://api.example.test"},
			Extra:       map[string]any{"openai_passthrough": true},
		},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Gateway.MaxAccountSwitches = 1

	accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
	stickyCache := &openAIStickyCredentialFailoverCache{accountID: 319}
	upstream := &openAICredentialSwitchUpstream{}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheSvc.Stop)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, stickyCache, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCacheSvc, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	h := NewOpenAIGatewayHandler(
		gatewaySvc, service.NewConcurrencyService(nil), billingCacheSvc,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg,
	)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(
		`{"model":"gpt-5.6-sol","input":"hello","stream":false,"prompt_cache_key":"sticky-credential-failover"}`,
	))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: 1814, GroupID: &groupID,
		User:  &service.User{ID: 1714, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1714, Concurrency: 0})

	h.Responses(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{320}, upstream.calls(), "missing credential must fail before opening upstream transport")
	require.Equal(t, int64(320), stickyCache.currentAccountID(), "successful fallback must replace the failed sticky binding")
	require.Contains(t, rec.Body.String(), "resp_credential_fallback")
}
