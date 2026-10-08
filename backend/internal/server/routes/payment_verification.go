package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterPaymentVerificationRoutes(v1 *gin.RouterGroup, h *handler.PaymentVerificationHandler, adminAuth middleware.AdminAuthMiddleware, auditLog middleware.AuditLogMiddleware, settings *service.SettingService) {
	if h == nil {
		return
	}
	admin := v1.Group("/admin/payment/verification")
	admin.Use(gin.HandlerFunc(adminAuth), gin.HandlerFunc(auditLog), middleware.AdminComplianceGuard(settings))
	admin.GET("/config", h.GetConfig)
	admin.PUT("/config", h.UpdateConfig)
	admin.POST("/telegram/connect", h.ConnectTelegram)
	admin.POST("/scan", h.Scan)
	admin.GET("/alerts", h.ListAlerts)
	admin.POST("/alerts/:id/resolve", h.Resolve)
	// Telegram authenticates with the registered secret header. Button actions
	// additionally require an allowed actor and a persisted message binding.
	v1.POST("/payment/verification/telegram", h.TelegramCallback)
}
