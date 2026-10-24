package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvoiceManualRoutesRequireAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	deny := func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }
	RegisterInvoiceRoutes(r.Group("/api/v1"), &handler.InvoiceHandler{}, middleware.JWTAuthMiddleware(deny), middleware.AdminAuthMiddleware(deny), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), nil, middleware.NewPanelRateLimiter(nil, nil))
	for _, tc := range []struct{ method, path string }{
		{"POST", "/invoices/quote"}, {"GET", "/invoices/INV/payment"}, {"POST", "/admin/invoices/INV/refund-fee"}, {"GET", "/invoices/config"}, {"POST", "/invoices"}, {"GET", "/invoices/INV/download"},
		{"PUT", "/admin/invoices/config"}, {"POST", "/admin/invoices/INV/action"}, {"POST", "/admin/invoices/INV/file"}, {"GET", "/admin/invoices/INV/download"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader("{}")))
		require.Equal(t, http.StatusUnauthorized, w.Code, tc.path)
	}
	for _, route := range r.Routes() {
		for _, removed := range []string{"/quota", "/recent", "/options", "/options/refresh"} {
			require.NotEqual(t, "/api/v1/admin/invoices"+removed, route.Path)
		}
	}
}
