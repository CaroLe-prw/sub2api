package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesWebSocket_GroupModelMappingWithoutAccountAlias(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:        `{"type":"response.create","model":"luna","stream":false}`,
				secondPayload:       `{"type":"response.create","stream":false}`,
				ingressMode:         mode,
				group:               &service.Group{ID: 4201, Platform: service.PlatformOpenAI, Status: service.StatusActive, ModelMapping: map[string]string{"luna": "gpt-5.6-terra"}},
				accountModelMapping: map[string]any{"gpt-5.6-terra": "gpt-5.6-terra"},
				// A conflicting channel rule and requested-model billing must lose to the group rule.
				channelMapping:     map[string]string{"luna": "gpt-5.6-sol"},
				billingModelSource: service.BillingModelSourceRequested,
			})
			require.Len(t, got.upstreamPayloads, 2)
			require.Len(t, got.logs, 2)
			for i, payload := range got.upstreamPayloads {
				require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(payload, "model").String())
				require.Equal(t, "luna", gjson.GetBytes(got.clientEvents[i], "response.model").String())
				require.Equal(t, "luna", got.logs[i].RequestedModel)
				require.Equal(t, "luna→gpt-5.6-terra", *got.logs[i].ModelMappingChain)
				require.InDelta(t, 16e-6, got.logs[i].TotalCost, 1e-12)
			}
		})
	}
}

type groupMappingHTTPUpstream struct {
	service.HTTPUpstream
	bodies [][]byte
}

func (u *groupMappingHTTPUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	payload := `{"id":"resp_mapped","object":"response","status":"completed","model":"gpt-5.6-terra","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
	if strings.Contains(r.URL.Path, "chat/completions") {
		payload = `{"id":"chat_mapped","object":"chat.completion","model":"gpt-5.6-terra","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	}
	if gjson.GetBytes(body, "stream").Bool() && !strings.Contains(r.URL.Path, "chat/completions") {
		payload = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func TestOpenAIHTTPGroupModelMappingWithoutAccountAlias(t *testing.T) {
	for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
		t.Run(endpoint, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			group := &service.Group{ID: 7, Hydrated: true, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowMessagesDispatch: true, ModelMapping: map[string]string{"luna": "gpt-5.6-terra"}}
			account := service.Account{ID: 9911, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://api.example.test", "model_mapping": map[string]any{"gpt-5.6-terra": "gpt-5.6-terra"}}, Extra: map[string]any{"openai_passthrough": true}}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			repo := &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}
			upstream := &groupMappingHTTPUpstream{}
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
			require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(upstream.bodies[0], "model").String())
		})
	}
}

func TestOpenAIResponsesWebSocket_CompositeGroupModelMappingPerTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModePassthrough, service.OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:        `{"type":"response.create","model":"luna","stream":false}`,
				secondPayload:       `{"type":"response.create","model":"sol","stream":false}`,
				ingressMode:         mode,
				group:               &service.Group{ID: 4201, Hydrated: true, Platform: service.PlatformComposite, Status: service.StatusActive, ModelMapping: map[string]string{"luna": "gpt-5.6-terra", "sol": "gpt-5.6-sol"}},
				accountModelMapping: map[string]any{"gpt-5.6-terra": "gpt-5.6-terra", "gpt-5.6-sol": "gpt-5.6-sol"},
			})
			require.Len(t, got.upstreamPayloads, 2)
			require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(got.upstreamPayloads[0], "model").String())
			require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(got.upstreamPayloads[1], "model").String())
			require.Equal(t, "luna", got.logs[0].RequestedModel)
			require.Equal(t, "sol", got.logs[1].RequestedModel)
			require.InDelta(t, 16e-6, got.logs[0].TotalCost, 1e-12)
			require.InDelta(t, 40e-6, got.logs[1].TotalCost, 1e-12)
		})
	}
}
