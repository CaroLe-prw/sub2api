//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise real forwarding and selection: capacity failures must not be written
// to the client while another candidate remains within the request's budget.
func TestOpenAIOAuth429FailoverUsesRequestBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name              string
		endpoint          string
		maxSwitches       int
		healthyID         int64
		wantStatus        int
		wantAccounts      []int64
		retryAfter        string
		hardBudgetSeconds int
	}{
		{"responses reaches fourth account", "responses", 5, 9404, http.StatusOK, []int64{9401, 9401, 9402, 9402, 9403, 9403, 9404}, "1", 0},
		{"messages reaches fourth account", "messages", 5, 9404, http.StatusOK, []int64{9401, 9401, 9402, 9402, 9403, 9403, 9404}, "1", 0},
		{"chat completions reaches fourth account", "chat/completions", 5, 9404, http.StatusOK, []int64{9401, 9401, 9402, 9402, 9403, 9403, 9404}, "1", 0},
		{"capacity without retry header reaches fourth account", "responses", 5, 9404, http.StatusOK, []int64{9401, 9401, 9402, 9402, 9403, 9403, 9404}, "", 0},
		{"hard time budget stops before another attempt", "responses", 5, 9404, http.StatusTooManyRequests, []int64{9401}, "1", 1},
		{"configured switch limit remains enforced", "responses", 1, 9404, http.StatusTooManyRequests, []int64{9401, 9401, 9402, 9402}, "1", 0},
		{"all candidates exhausted returns an error", "responses", 5, 0, http.StatusTooManyRequests, []int64{9401, 9401, 9402, 9402, 9403, 9403, 9404, 9404}, "1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := make([]service.Account, 0, 4)
			for i := 0; i < 4; i++ {
				accounts = append(accounts, service.Account{
					ID: int64(9401 + i), Name: fmt.Sprintf("oauth-%d", i), Platform: service.PlatformOpenAI,
					Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
					Priority: 1, CreatedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
					Credentials: map[string]any{"access_token": "test-oauth-token"},
				})
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxAccountSwitches = tc.maxSwitches
			cfg.Gateway.OpenAIFailover.HardBudgetSeconds = tc.hardBudgetSeconds
			repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
			upstream := &openAIOAuthCapacityFailoverUpstream{healthyID: tc.healthyID, retryAfter: tc.retryAfter}
			cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(cache.Stop)
			gateway := service.NewOpenAIGatewayService(
				repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), nil, cache, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
			)
			h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), cache,
				service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			groupID := int64(9400)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+tc.endpoint,
				strings.NewReader(`{"model":"gpt-5.4","input":"hello","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":true}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
				ID: 9405, GroupID: &groupID, User: &service.User{ID: 9406, Status: service.StatusActive},
				Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowMessagesDispatch: true},
			})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 9406})
			switch tc.endpoint {
			case "responses":
				h.Responses(c)
			case "messages":
				h.Messages(c)
			case "chat/completions":
				h.ChatCompletions(c)
			}
			require.Equal(t, tc.wantAccounts, upstream.accountIDs, rec.Body.String())
			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantStatus == http.StatusOK {
				require.Contains(t, rec.Body.String(), "answer from second account")
				require.NotContains(t, rec.Body.String(), "rate_limit")
				require.NotContains(t, rec.Body.String(), "Codex is at capacity")
			} else {
				require.Contains(t, rec.Body.String(), "rate_limit_error")
			}
		})
	}
}

type openAIOAuthCapacityFailoverUpstream struct {
	service.HTTPUpstream
	healthyID  int64
	retryAfter string
	accountIDs []int64
}

func (u *openAIOAuthCapacityFailoverUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	u.accountIDs = append(u.accountIDs, accountID)
	if accountID == u.healthyID {
		return preContentSSEResponse(io.NopCloser(strings.NewReader(preContentHandlerSuccess))), nil
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	// Most cases use Retry-After to keep the real retries fast. The no-header
	// case replays the reported response including its default eight-second wait.
	if u.retryAfter != "" {
		headers.Set("Retry-After", u.retryAfter)
	}
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"turn_rate_limit_exceeded","message":"Codex is at capacity, please try again later","type":"rate_limit_error"}}`)),
	}, nil
}
