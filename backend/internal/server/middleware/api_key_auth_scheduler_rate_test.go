//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSchedulerUsesCurrentPersonalRate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(mode, func(t *testing.T) {
			group := &service.Group{ID: 42, Name: "priced", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true, RateMultiplier: 0.08, SubscriptionType: service.SubscriptionTypeStandard}
			user := &service.User{ID: 7, Status: service.StatusActive, Role: service.RoleUser, Balance: 10}
			key := &service.APIKey{ID: 100, UserID: user.ID, Key: "personal-rate-test", Status: service.StatusActive, User: user, Group: group, GroupID: &group.ID}
			cfg := &config.Config{RunMode: mode}
			oldRate, newRate := 0.08, 0.07
			rateRepo := &authTestUserGroupRateRepo{rate: &oldRate}
			gateway := service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, rateRepo, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			require.Equal(t, 0.08, gateway.ResolveUserGroupRateMultiplier(context.Background(), user.ID, group.ID, group.RateMultiplier))
			// Admin changes invalidate auth snapshots, but not the gateway's independent TTL cache.
			rateRepo.rate = &newRate
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { clone := *key; return &clone, nil }}
			auth := service.NewAPIKeyService(repo, nil, nil, nil, rateRepo, nil, cfg)
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(auth, nil, cfg)))
			router.GET("/t", func(c *gin.Context) {
				actualKey, ok := GetAPIKeyFromContext(c)
				require.True(t, ok)
				require.Equal(t, newRate, actualKey.GroupRateMultiplier)
				ctx, _ := gateway.WithOpenAIRequestPricingContext(c.Request.Context(), actualKey.GroupID)
				for _, rate := range []float64{0.069, 0.07, 0.075, 0.08} {
					vetoed, _ := service.OpenAIProfitControlVeto(ctx, &service.Account{ID: 1, RateMultiplier: &rate})
					require.Equal(t, rate > newRate, vetoed, "account rate %v must be compared with personal 0.07, not group 0.08", rate)
				}
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, "/t", nil)
			request.Header.Set("x-api-key", key.Key)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

func TestGoogleAPIKeyAuthPublishesSchedulerIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(mode, func(t *testing.T) {
			group := &service.Group{ID: 42, Name: "gemini", Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true, RateMultiplier: 0.08}
			user := &service.User{ID: 7, Email: "rate-test@example.com", Status: service.StatusActive, Role: service.RoleUser, Balance: 10}
			key := &service.APIKey{ID: 100, Name: "rate-test", UserID: user.ID, Key: "google-rate-test", Status: service.StatusActive, User: user, Group: group, GroupID: &group.ID}
			cfg := &config.Config{RunMode: mode}
			rate := 0.07
			repo := fakeAPIKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { clone := *key; return &clone, nil }}
			auth := service.NewAPIKeyService(repo, nil, nil, nil, &authTestUserGroupRateRepo{rate: &rate}, nil, cfg)
			router := gin.New()
			router.Use(APIKeyAuthGoogle(auth, cfg))
			router.GET("/v1beta/models", func(c *gin.Context) {
				require.Equal(t, user.ID, c.Request.Context().Value(ctxkey.UserID))
				require.Equal(t, key.ID, c.Request.Context().Value(ctxkey.APIKeyID))
				require.Equal(t, key.Name, c.Request.Context().Value(ctxkey.APIKeyName))
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
			request.Header.Set("x-goog-api-key", key.Key)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}
