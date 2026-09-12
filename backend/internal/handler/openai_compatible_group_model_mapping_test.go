//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type compatibleGroupMappingUpstream struct {
	service.HTTPUpstream
	bodies [][]byte
}

func (u *compatibleGroupMappingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	model := gjson.GetBytes(body, "model").String()
	payload := `{"id":"resp_mapped","object":"response","status":"completed","model":"` + model + `","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
	contentType := "application/json"
	switch {
	case strings.Contains(req.URL.Path, "chat/completions"):
		payload = `{"id":"chat_mapped","object":"chat.completion","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
		if gjson.GetBytes(body, "stream").Bool() {
			contentType = "text/event-stream"
			payload = "data: {\"id\":\"chat_mapped\",\"object\":\"chat.completion.chunk\",\"model\":\"" + model + "\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
		}
	case strings.Contains(req.URL.Path, "messages"):
		payload = `{"id":"msg_mapped","type":"message","role":"assistant","model":"` + model + `","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
	default:
		if gjson.GetBytes(body, "stream").Bool() {
			contentType = "text/event-stream"
			payload = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\n"
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func (u *compatibleGroupMappingUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestOpenAICompatibleGroupModelMappingWithoutAccountAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []struct{ platform, model string }{
		{service.PlatformGrok, "grok-4.3"},
		{service.PlatformKimi, "kimi-k2-thinking"},
		{service.PlatformZhipu, "glm-5.2"},
		{service.PlatformDeepseek, "deepseek-v4-pro"},
		{service.PlatformMiniMax, "MiniMax-M3"},
	} {
		for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
			t.Run(provider.platform+endpoint, func(t *testing.T) {
				group := &service.Group{ID: 821, Hydrated: true, Platform: provider.platform, Status: service.StatusActive, ModelMapping: map[string]string{"luna": provider.model, provider.model: "unavailable-next-hop"}}
				account := service.Account{ID: 822, Platform: provider.platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"api_key": "test", "base_url": "https://api.example.test", "model_mapping": map[string]any{provider.model: provider.model}}, Extra: map[string]any{"openai_passthrough": true}}
				cfg := &config.Config{RunMode: config.RunModeSimple}
				cfg.Default.RateMultiplier = 1
				repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
				upstream := &compatibleGroupMappingUpstream{}
				cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(cache.Stop)
				gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
				h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), cache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"model":"luna","input":"hello","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":false}`))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 2, Status: service.StatusActive}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})
				switch endpoint {
				case "/v1/responses":
					h.Responses(c)
				case "/v1/chat/completions":
					h.ChatCompletions(c)
				case "/v1/messages":
					h.Messages(c)
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Len(t, upstream.bodies, 1)
				require.Equal(t, provider.model, gjson.GetBytes(upstream.bodies[0], "model").String())
			})
		}
	}
}
