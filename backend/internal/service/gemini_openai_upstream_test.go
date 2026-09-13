package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func geminiOpenAITestAccount() *Account {
	return &Account{ID: 42, Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 2,
		Credentials: map[string]any{"api_protocol": "chat_completions", "api_key": "upstream-test-key", "base_url": "https://upstream.example.com/v1", "model_mapping": map[string]any{"gemini-public": "vendor/gemini"}}}
}

func geminiOpenAITestService(body string, status int) (*GeminiMessagesCompatService, *geminiCompatHTTPUpstreamStub) {
	stub := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"upstream-request"}}, Body: io.NopCloser(strings.NewReader(body))}}
	return &GeminiMessagesCompatService{httpUpstream: stub, cfg: &config.Config{}}, stub
}

func TestGeminiOpenAIForwardNative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			upstream := `{"model":"vendor/gemini","choices":[{"index":0,"message":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":10}}}`
			action := "generateContent"
			if stream {
				action = "streamGenerateContent"
				upstream = "data: " + strings.Replace(upstream, `"message"`, `"delta"`, 1) + "\n\ndata: [DONE]\n\n"
			}
			svc, stub := geminiOpenAITestService(upstream, http.StatusOK)
			body := []byte(`{"contents":[{"role":"user","parts":[{"text":"Hi"}]}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:"+action, bytes.NewReader(body))
			result, err := svc.ForwardNative(c.Request.Context(), c, geminiOpenAITestAccount(), "gemini-public", action, stream, body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), `"text":"Hello"`)
			require.Equal(t, "gemini-public", result.Model)
			require.Equal(t, "vendor/gemini", result.UpstreamModel)
			require.Equal(t, "vendor/gemini", result.UpstreamResponseModel)
			require.Equal(t, 80, result.Usage.InputTokens)
			require.Equal(t, 20, result.Usage.CacheReadInputTokens)
			require.Equal(t, 30, result.Usage.OutputTokens)
			require.Equal(t, "upstream-request", rec.Header().Get("x-request-id"))
			require.Equal(t, 1, stub.calls)
			require.Equal(t, "https://upstream.example.com/v1/chat/completions", stub.lastReq.URL.String())
			require.Equal(t, "Bearer upstream-test-key", stub.lastReq.Header.Get("Authorization"))
			require.Empty(t, stub.lastReq.Header.Get("x-goog-api-key"))
			raw, err := io.ReadAll(stub.lastReq.Body)
			require.NoError(t, err)
			var request map[string]any
			require.NoError(t, json.Unmarshal(raw, &request))
			require.Equal(t, "vendor/gemini", request["model"])
			require.Equal(t, stream, request["stream"])
		})
	}
}

func TestGeminiOpenAIUnsupportedRequestDoesNotContactUpstream(t *testing.T) {
	svc, stub := geminiOpenAITestService("", http.StatusOK)
	body := []byte(`{"contents":[{"parts":[{"text":"Hi"}]}],"tools":[{"googleSearch":{}}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:generateContent", bytes.NewReader(body))
	_, err := svc.ForwardNative(c.Request.Context(), c, geminiOpenAITestAccount(), "gemini-public", "generateContent", false, body)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "googleSearch")
	require.Zero(t, stub.calls)
}

func TestGeminiOpenAITokenCountIsLocal(t *testing.T) {
	svc, stub := geminiOpenAITestService("", http.StatusOK)
	body := []byte(`{"generateContentRequest":{"contents":[{"parts":[{"text":"你好世界"}]}]}}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:countTokens", bytes.NewReader(body))
	result, err := svc.ForwardNative(c.Request.Context(), c, geminiOpenAITestAccount(), "gemini-public", "countTokens", false, body)
	require.NoError(t, err)
	require.JSONEq(t, `{"totalTokens":4}`, rec.Body.String())
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.Zero(t, stub.calls)
}

func TestGeminiOpenAINativeErrorAndFailover(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		svc, _ := geminiOpenAITestService(`{"error":{"message":"upstream rejected request","type":"invalid_request_error"}}`, status)
		body := []byte(`{"contents":[{"parts":[{"text":"Hi"}]}]}`)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-public:generateContent", bytes.NewReader(body))
		_, err := svc.ForwardNative(c.Request.Context(), c, geminiOpenAITestAccount(), "gemini-public", "generateContent", false, body)
		require.Error(t, err)
		if status == http.StatusUnauthorized {
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, status, failover.StatusCode)
			require.False(t, c.Writer.Written())
		} else {
			require.Equal(t, status, rec.Code)
			require.JSONEq(t, `{"error":{"code":400,"message":"upstream rejected request","status":"INVALID_ARGUMENT"}}`, rec.Body.String())
		}
	}
}

func TestGeminiOpenAIModelDiscoveryAndConnectionRequest(t *testing.T) {
	account := geminiOpenAITestAccount()
	for _, path := range []string{"/v1beta/models", "/v1beta/models/gemini-public"} {
		upstream := `{"data":[{"id":"vendor/gemini"}]}`
		if path != "/v1beta/models" {
			upstream = `{"id":"vendor/gemini"}`
		}
		svc, stub := geminiOpenAITestService(upstream, http.StatusOK)
		result, err := svc.ForwardAIStudioGET(context.Background(), account, path)
		require.NoError(t, err)
		require.Contains(t, string(result.Body), `"name":"models/vendor/gemini"`)
		require.Equal(t, "Bearer upstream-test-key", stub.lastReq.Header.Get("Authorization"))
		require.Empty(t, stub.lastReq.Header.Get("x-goog-api-key"))
		want := "https://upstream.example.com/v1/models"
		if path != "/v1beta/models" {
			want += "/vendor/gemini"
		}
		require.Equal(t, want, stub.lastReq.URL.String())
	}
	svc := &AccountTestService{cfg: &config.Config{}}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example.com/v1/models", req.URL.String())
	req, err = svc.buildGeminiAPIKeyRequest(context.Background(), account, "vendor/gemini", createGeminiTestPayload("gemini-public", "hello"))
	require.NoError(t, err)
	require.Equal(t, "https://upstream.example.com/v1/chat/completions", req.URL.String())
	require.Equal(t, "Bearer upstream-test-key", req.Header.Get("Authorization"))
}

func TestGeminiOpenAIProtocolDoesNotChangeNativeAccounts(t *testing.T) {
	for _, account := range []*Account{nil, {Platform: PlatformGemini, Type: AccountTypeAPIKey}, {Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"api_protocol": "chat_completions"}}, {Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_protocol": "chat_completions"}}} {
		require.False(t, account.GeminiUsesOpenAI())
	}
}

type geminiOpenAIRateLimitRepo struct {
	AccountRepository
	resetAt time.Time
}

func (r *geminiOpenAIRateLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.resetAt = resetAt
	return nil
}
func TestGeminiOpenAIUsesUpstreamRateLimitWindow(t *testing.T) {
	for _, tc := range []struct {
		header string
		wait   time.Duration
	}{{"15", 15 * time.Second}, {"", time.Minute}, {"-10", time.Minute}} {
		repo := &geminiOpenAIRateLimitRepo{}
		svc := &GeminiMessagesCompatService{accountRepo: repo}
		before := time.Now()
		svc.handleGeminiUpstreamError(context.Background(), geminiOpenAITestAccount(), http.StatusTooManyRequests, http.Header{"Retry-After": {tc.header}}, []byte(`{"error":{"message":"rate limit"}}`))
		require.WithinDuration(t, before.Add(tc.wait), repo.resetAt, time.Second)
	}
}

func TestGeminiOpenAIHasNoAIStudioQuota(t *testing.T) {
	account := geminiOpenAITestAccount()
	account.Credentials["tier_id"] = "aistudio_free"
	svc := &GeminiQuotaService{}
	_, limited := svc.QuotaForAccount(context.Background(), account)
	require.False(t, limited)
	require.Empty(t, geminiQuotaTierKeyForAccount(account))
	account.Credentials["api_protocol"] = "gemini"
	require.NotEmpty(t, geminiQuotaTierKeyForAccount(account))
}
