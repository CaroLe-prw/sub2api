package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func RegisterInvoiceRoutes(v1 *gin.RouterGroup, h *handler.InvoiceHandler, jwt middleware.JWTAuthMiddleware, admin middleware.AdminAuthMiddleware, audit middleware.AuditLogMiddleware, settings *service.SettingService, limiter *middleware.PanelRateLimiter) {
	if h == nil {
		return
	}
	bound := func(c *gin.Context) {
		limit := int64(32 * 1024)
		if c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/file") {
			limit = service.MaxInvoiceFileSize + 1024*1024
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
	u := v1.Group("/invoices")
	u.Use(gin.HandlerFunc(jwt), middleware.BackendModeUserGuard(settings), limiter.Global(), bound)
	u.GET("/config", h.Config)
	u.GET("/eligible-orders", h.Eligible)
	u.GET("/titles", h.Titles)
	u.DELETE("/titles/:titleID", h.DeleteTitle)
	u.GET("", h.List(false))
	u.POST("", h.Create)
	u.GET("/:id", h.Detail(false))
	u.POST("/:id/action", h.Action(false))
	u.GET("/:id/download", h.Download(false))
	a := v1.Group("/admin/invoices")
	a.Use(gin.HandlerFunc(admin), gin.HandlerFunc(audit), middleware.AdminComplianceGuard(settings), limiter.Global(), bound)
	a.GET("/config", h.Config)
	a.PUT("/config", h.UpdateConfig)
	a.GET("", h.List(true))
	a.GET("/:id", h.Detail(true))
	a.POST("/:id/action", h.Action(true))
	a.GET("/:id/download", h.Download(true))
	a.POST("/:id/file", h.Upload)
}
