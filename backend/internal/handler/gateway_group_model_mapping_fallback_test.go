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

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gatewayGroupMappingFallbackUpstream struct {
	firstBody []byte
	fallback  gatewayGroupMappingUpstream
}

func (u *gatewayGroupMappingFallbackUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if accountID == 931 {
		var err error
		u.firstBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"prompt is too long"}}`))}, nil
	}
	return u.fallback.Do(req, proxy, accountID, concurrency)
}

func (u *gatewayGroupMappingFallbackUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestGatewayGroupModelMappingFallbackUsesFallbackGroupRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, composite := range []bool{false, true} {
		name := "native source group"
		if composite {
			name = "composite source with rewritten request"
		}
		t.Run(name, func(t *testing.T) {
			fallback := &service.Group{ID: 922, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, ModelMapping: map[string]string{"luna": "claude-opus-4-6"}}
			group := &service.Group{ID: 921, Hydrated: true, Platform: service.PlatformAntigravity, Status: service.StatusActive,
				FallbackGroupIDOnInvalidRequest: &fallback.ID, ModelMapping: map[string]string{"luna": "claude-sonnet-4-5"}}
			if composite {
				group.Platform = service.PlatformComposite
			}
			first := &service.Account{ID: 931, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials:   map[string]any{"access_token": "test", "project_id": "test-project", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"}},
				AccountGroups: []service.AccountGroup{{AccountID: 931, GroupID: group.ID}},
			}
			second := &service.Account{ID: 932, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://api.example.test", "model_mapping": map[string]any{"claude-opus-4-6": "claude-opus-4-6"}}, Extra: map[string]any{"anthropic_passthrough": true},
				AccountGroups: []service.AccountGroup{{AccountID: 932, GroupID: fallback.ID}},
			}
			upstream := &gatewayGroupMappingFallbackUpstream{fallback: gatewayGroupMappingUpstream{platform: service.PlatformAnthropic, model: "claude-opus-4-6"}}
			h, logs := newGatewayGroupMappingHandlerForGroups(t, []*service.Group{group, fallback}, []*service.Account{first, second}, upstream)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			requestModel := "luna"
			if composite {
				requestModel = "claude-sonnet-4-5"
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"`+requestModel+`","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"stream":false}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
			if composite {
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, GroupID: group.ID, PublicModel: "luna", UpstreamModel: "claude-sonnet-4-5", TargetPlatform: service.PlatformAntigravity, Source: service.CompositeRouteSourceDetector}))
			}
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, UserID: 2, GroupID: &group.ID, Group: group, User: &service.User{ID: 2, Status: service.StatusActive}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})

			h.Messages(c)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, "claude-sonnet-4-5", gjson.GetBytes(upstream.firstBody, "model").String())
			require.Len(t, upstream.fallback.bodies, 1)
			require.Equal(t, "claude-opus-4-6", gjson.GetBytes(upstream.fallback.bodies[0], "model").String())
			require.Len(t, logs, 1)
			usage := <-logs
			require.Equal(t, &fallback.ID, usage.GroupID)
			require.Equal(t, "luna", usage.RequestedModel)
			require.Equal(t, "luna→claude-opus-4-6", *usage.ModelMappingChain)
			require.Equal(t, service.PlatformAnthropic, service.QuotaPlatform(c.Request.Context(), &service.APIKey{Group: fallback}))
		})
	}
}
