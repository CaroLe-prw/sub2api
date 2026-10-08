package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPaymentVerificationAdminRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPaymentVerificationRoutes(r.Group("/api/v1"), &handler.PaymentVerificationHandler{},
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }),
		middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), nil)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/config"}, {"PUT", "/config"}, {"POST", "/scan"}, {"GET", "/alerts"}, {"POST", "/telegram/connect"}, {"POST", "/alerts/1/resolve"},
	} {
		req := httptest.NewRequest(tc.method, "/api/v1/admin/payment/verification"+tc.path, strings.NewReader("{}"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code, tc.path)
	}
	// The Telegram route is public but bounded; authorization is done by the
	// verification service using the webhook secret, actor and message binding.
	req := httptest.NewRequest("POST", "/api/v1/payment/verification/telegram", strings.NewReader(strings.Repeat("x", 64*1024+1)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}
