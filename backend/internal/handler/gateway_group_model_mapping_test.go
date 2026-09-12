//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gatewayGroupMappingUpstream struct {
	platform string
	model    string
	bodies   [][]byte
	paths    []string
}

func (u *gatewayGroupMappingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	u.paths = append(u.paths, req.URL.Path)
	contentType := "application/json"
	payload := `{"id":"msg_group_mapping","type":"message","role":"assistant","model":"` + u.model + `","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1}}`
	if strings.HasSuffix(req.URL.Path, "count_tokens") || strings.HasSuffix(req.URL.Path, ":countTokens") {
		payload = `{"input_tokens":2,"totalTokens":2}`
	} else if u.platform == service.PlatformGemini || u.platform == service.PlatformAntigravity {
		payload = `{"responseId":"resp_group_mapping","modelVersion":"` + u.model + `","candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`
		if u.platform == service.PlatformAntigravity {
			payload = `{"response":` + payload + `}`
		}
		if strings.Contains(req.URL.Path, "streamGenerateContent") {
			contentType = "text/event-stream"
			payload = "data: " + payload + "\n\n"
		}
	} else if gjson.GetBytes(body, "stream").Bool() {
		contentType = "text/event-stream"
		payload = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_group_mapping\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + u.model + "\",\"content\":[],\"usage\":{\"input_tokens\":2}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func (u *gatewayGroupMappingUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type gatewayGroupMappingGroupRepo struct {
	*fakeGroupRepo
	groups map[int64]*service.Group
}

func (r *gatewayGroupMappingGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if group := r.groups[id]; group != nil {
		return group, nil
	}
	return nil, service.ErrGroupNotFound
}

func (r *gatewayGroupMappingGroupRepo) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	return r.GetByID(ctx, id)
}

type gatewayGroupMappingSchedulerCache struct{ *fakeSchedulerCache }

func (s *gatewayGroupMappingSchedulerCache) GetSnapshot(_ context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	var accounts []*service.Account
	for _, account := range s.accounts {
		for _, link := range account.AccountGroups {
			if link.GroupID == bucket.GroupID {
				accounts = append(accounts, account)
				break
			}
		}
	}
	return accounts, true, nil
}

func newGatewayGroupMappingHandler(t *testing.T, group *service.Group, account *service.Account, upstream service.HTTPUpstream) (*GatewayHandler, chan *service.UsageLog) {
	t.Helper()
	return newGatewayGroupMappingHandlerForGroups(t, []*service.Group{group}, []*service.Account{account}, upstream)
}

func newGatewayGroupMappingHandlerForGroups(t *testing.T, groups []*service.Group, accounts []*service.Account, upstream service.HTTPUpstream) (*GatewayHandler, chan *service.UsageLog) {
	t.Helper()
	snapshot := service.NewSchedulerSnapshotService(&gatewayGroupMappingSchedulerCache{&fakeSchedulerCache{accounts: accounts}}, nil, nil, nil, nil)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	logs := make(chan *service.UsageLog, 4)
	groupRepo := &gatewayGroupMappingGroupRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: map[int64]*service.Group{}}
	for _, group := range groups {
		groupRepo.groups[group.ID] = group
	}
	gateway := service.NewGatewayService(
		nil, groupRepo, &openAIWSUsageHandlerUsageLogRepoStub{created: logs}, nil, nil, nil, nil, nil, cfg,
		snapshot, nil, service.NewBillingService(cfg, nil), nil, cache, nil, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	antigravity := service.NewAntigravityGatewayService(nil, nil, snapshot, service.NewAntigravityTokenProvider(nil, nil, nil), nil, upstream, service.NewSettingService(&contentModerationHandlerSettingRepo{}, cfg), nil)
	return &GatewayHandler{
		gatewayService: gateway, billingCacheService: cache,
		concurrencyHelper:         NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatClaude, 0),
		geminiCompatService:       service.NewGeminiMessagesCompatService(nil, groupRepo, nil, snapshot, nil, nil, upstream, antigravity, cfg),
		antigravityGatewayService: antigravity,
		maxAccountSwitches:        1, maxAccountSwitchesGemini: 1, cfg: cfg,
	}, logs
}

func TestGatewayGroupModelMappingWithoutAccountAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformGemini, service.PlatformAntigravity} {
		endpoints := []string{"/v1/messages", "/v1/chat/completions"}
		if platform != service.PlatformGemini {
			endpoints = append(endpoints, "/v1/responses")
		}
		if platform == service.PlatformAnthropic {
			endpoints = append(endpoints, "/v1/messages/count_tokens")
		}
		if platform == service.PlatformGemini {
			endpoints = append(endpoints, "/v1beta/models/luna:generateContent", "/v1beta/models/luna:countTokens")
		}
		for _, endpoint := range endpoints {
			t.Run(platform+endpoint, func(t *testing.T) {
				target := "claude-sonnet-4-5"
				if platform == service.PlatformGemini {
					target = "gemini-2.5-pro"
				}
				// The target is also a source to prove each request maps only once.
				group := &service.Group{ID: 731, Hydrated: true, Platform: platform, Status: service.StatusActive, ModelMapping: map[string]string{"luna": target, target: "unavailable-next-hop"}}
				account := &service.Account{ID: 732, Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials:   map[string]any{"api_key": "test", "base_url": "https://api.example.test", "model_mapping": map[string]any{target: target}},
					Extra:         map[string]any{"anthropic_passthrough": true},
					AccountGroups: []service.AccountGroup{{AccountID: 732, GroupID: 731}},
				}
				if platform == service.PlatformAntigravity {
					account.Type = service.AccountTypeOAuth
					account.Credentials["access_token"] = "test"
					account.Credentials["project_id"] = "test-project"
					account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
				}
				upstream := &gatewayGroupMappingUpstream{platform: platform, model: target}
				h, logs := newGatewayGroupMappingHandler(t, group, account, upstream)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				body := `{"model":"luna","input":"hello","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":false}`
				if strings.HasPrefix(endpoint, "/v1beta/") {
					body = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
					c.Params = gin.Params{{Key: "modelAction", Value: strings.TrimPrefix(endpoint, "/v1beta/models/")}}
				}
				c.Request = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 2, GroupID: &group.ID, Group: group, User: &service.User{ID: 2, Status: service.StatusActive}})
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})
				switch endpoint {
				case "/v1/messages":
					h.Messages(c)
				case "/v1/chat/completions":
					h.ChatCompletions(c)
				case "/v1/responses":
					h.Responses(c)
				case "/v1/messages/count_tokens":
					h.CountTokens(c)
				default:
					h.GeminiV1BetaModels(c)
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Len(t, upstream.bodies, 1)
				if platform == service.PlatformGemini {
					require.Contains(t, upstream.paths[0], "/models/"+target+":")
				} else {
					require.Equal(t, target, gjson.GetBytes(upstream.bodies[0], "model").String())
				}
				if !strings.Contains(endpoint, "count") {
					require.Len(t, logs, 1)
					usage := <-logs
					require.Equal(t, "luna", usage.RequestedModel)
					require.NotNil(t, usage.ModelMappingChain)
					require.Equal(t, "luna→"+target, *usage.ModelMappingChain)
				}
			})
		}
	}
}
