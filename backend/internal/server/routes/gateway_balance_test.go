package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newBalanceCompatibilityRouteTestRouter() (*gin.Engine, *int) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authCalls := 0
	groupID := int64(42)
	apiKey := &service.APIKey{
		ID:        100,
		UserID:    7,
		Key:       "cc-switch-balance-route-test-key",
		Status:    service.StatusActive,
		Quota:     100,
		QuotaUsed: 30,
		User:      &service.User{ID: 7, Status: service.StatusActive, Balance: 200},
		GroupID:   &groupID,
		Group: &service.Group{
			ID:               groupID,
			Status:           service.StatusActive,
			Hydrated:         true,
			Platform:         service.PlatformOpenAI,
			SubscriptionType: service.SubscriptionTypeStandard,
		},
	}
	auth := servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		authCalls++
		if c.GetHeader("Authorization") != "Bearer "+apiKey.Key {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid API key"})
			return
		}
		c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: apiKey.UserID})
		c.Next()
	})
	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: &handler.OpenAIGatewayHandler{},
			AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
		},
		auth,
		nil, nil, nil, nil, nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024}},
	)
	return router, &authCalls
}

func TestGatewayRoutesBalanceCompatibilityPathsAreRegistered(t *testing.T) {
	router, _ := newBalanceCompatibilityRouteTestRouter()
	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{"/user/balance", "/v1/user/balance"} {
		require.True(t, registered[http.MethodGet+" "+path], "GET %s should be registered", path)
	}
}

func TestGatewayRoutesBalanceCompatibilityUsesAPIKeyAuth(t *testing.T) {
	for _, path := range []string{"/user/balance", "/v1/user/balance"} {
		t.Run(path, func(t *testing.T) {
			for _, tc := range []struct {
				name          string
				authorization string
				status        int
			}{
				{name: "missing credentials", status: http.StatusUnauthorized},
				{name: "invalid key", authorization: "Bearer invalid-key", status: http.StatusUnauthorized},
				{name: "API key without browser session", authorization: "Bearer cc-switch-balance-route-test-key", status: http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					router, authCalls := newBalanceCompatibilityRouteTestRouter()
					req := httptest.NewRequest(http.MethodGet, path, nil)
					req.Header.Set("Authorization", tc.authorization)
					req.Header.Set("User-Agent", "cc-switch/1.0")
					w := httptest.NewRecorder()

					router.ServeHTTP(w, req)

					require.Equal(t, 1, *authCalls, "balance aliases must pass through API-key authentication")
					require.Equal(t, tc.status, w.Code, w.Body.String())
					require.Contains(t, w.Header().Get("Content-Type"), "application/json")
					var body map[string]any
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					if tc.status != http.StatusOK {
						require.NotContains(t, body, "balance", "unauthenticated requests must not disclose balance")
						return
					}
					// CC Switch's generic extractor reads these fields at the top level.
					require.Equal(t, true, body["is_active"])
					require.Equal(t, float64(70), body["balance"])
					require.Equal(t, "USD", body["unit"])
				})
			}
		})
	}
}
