package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type paymentVerificationAPI interface {
	GetConfig(context.Context) (*service.PaymentVerificationConfig, error)
	UpdateConfig(context.Context, *service.PaymentVerificationConfig) (*service.PaymentVerificationConfig, error)
	ConnectTelegram(context.Context) error
	HandleTelegramCallback(context.Context, string, []byte) error
	Scan(context.Context) (*service.PaymentVerificationScanResult, error)
	ListAlerts(context.Context, int) ([]service.PaymentVerificationRecord, error)
	ResolveAlert(context.Context, int64, string, string) (*service.PaymentVerificationRecord, error)
}

type PaymentVerificationHandler struct{ service paymentVerificationAPI }

func NewPaymentVerificationHandler(svc *service.PaymentVerificationService) *PaymentVerificationHandler {
	return &PaymentVerificationHandler{service: svc}
}

func (h *PaymentVerificationHandler) GetConfig(c *gin.Context) {
	result, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentVerificationHandler) UpdateConfig(c *gin.Context) {
	var req service.PaymentVerificationConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid payment verification settings")
		return
	}
	result, err := h.service.UpdateConfig(c.Request.Context(), &req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentVerificationHandler) ConnectTelegram(c *gin.Context) {
	if err := h.service.ConnectTelegram(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"connected": true})
}

func (h *PaymentVerificationHandler) Scan(c *gin.Context) {
	result, err := h.service.Scan(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentVerificationHandler) ListAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	result, err := h.service.ListAlerts(c.Request.Context(), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentVerificationHandler) Resolve(c *gin.Context) {
	subject, ok := requireAuth(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid alert ID")
		return
	}
	var req struct {
		Action string `json:"action" binding:"required,oneof=ban ignore"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Action must be ban or ignore")
		return
	}
	result, err := h.service.ResolveAlert(c.Request.Context(), id, req.Action, "admin:"+strconv.FormatInt(subject.UserID, 10))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *PaymentVerificationHandler) TelegramCallback(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			c.Status(http.StatusRequestEntityTooLarge)
		} else {
			c.Status(http.StatusBadRequest)
		}
		return
	}
	if err := h.service.HandleTelegramCallback(c.Request.Context(), c.GetHeader("X-Telegram-Bot-Api-Secret-Token"), body); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Status(http.StatusOK)
}
