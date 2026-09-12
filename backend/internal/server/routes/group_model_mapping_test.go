package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompositeGroupMappingBeforePlatformResolution(t *testing.T) {
	for _, tc := range []struct {
		path, body, target, provider, platform string
		gemini                                 bool
	}{
		{"/v1/chat/completions", `{"model":"luna","messages":[]}`, "priced-terra", "deepseek-v4-pro", service.PlatformDeepseek, false},
		{"/v1/messages", `{"model":"luna","messages":[]}`, "priced-terra", "claude-sonnet-4-6", service.PlatformAnthropic, false},
		{"/v1/live", `{"session":{"model":"luna"}}`, "priced-terra", "gpt-5.6-terra", service.PlatformOpenAI, false},
		{"/v1beta/models/luna:generateContent", `{"contents":[]}`, "priced-terra", "gemini-2.5-pro", service.PlatformGemini, true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			group := &service.Group{ID: 7, Hydrated: true, Platform: service.PlatformComposite, ModelMapping: map[string]string{"luna": tc.target, tc.target: "must-not-recurse"}}
			resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: 7, PublicModel: tc.target, MatchType: service.CompositeRouteMatchExact, TargetPlatform: tc.platform, UpstreamModel: tc.provider, Endpoint: service.CompositeRouteEndpointAny, Enabled: true}}})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &group.ID, Group: group})
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Next()
			})
			route := tc.path
			if tc.gemini {
				router.Use(compositeGeminiTargetPlatformMiddleware(resolver))
				route = "/v1beta/models/*modelAction"
			} else {
				router.Use(compositeTargetPlatformMiddleware(resolver))
			}
			router.POST(route, func(c *gin.Context) {
				platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
				require.True(t, ok)
				require.Equal(t, tc.platform, platform)
				original, _ := service.RequestedPublicModelFromContext(c.Request.Context())
				require.Equal(t, "luna", original)
				upstream, _ := service.ResolvedUpstreamModelFromContext(c.Request.Context())
				require.Equal(t, tc.provider, upstream)
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				if !tc.gemini {
					require.Equal(t, tc.provider, requestmodel.FromBodyForRoute(c.FullPath(), "application/json", body))
				}
				// Handler receives rewritten body, but billing must retain the group target.
				svc := &service.OpenAIGatewayService{}
				mapping, _ := svc.ResolveChannelMappingAndRestrict(c.Request.Context(), &group.ID, tc.provider)
				require.True(t, mapping.GroupMapped)
				require.Equal(t, tc.provider, mapping.MappedModel)
				fields := mapping.ToUsageFields(original, tc.provider)
				require.Equal(t, tc.target, fields.ChannelMappedModel)
				require.Equal(t, "luna→"+tc.target+"→"+tc.provider, fields.ModelMappingChain)
				c.Status(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}
