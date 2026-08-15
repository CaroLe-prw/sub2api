package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CheckInHandler struct {
	service *service.CheckInService
}

func NewCheckInHandler(checkInService *service.CheckInService) *CheckInHandler {
	return &CheckInHandler{service: checkInService}
}

func (h *CheckInHandler) GetOverview(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	now := timezone.Now()
	year, month := now.Year(), now.Month()
	if raw := c.Query("year"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 2000 || parsed > 2100 {
			response.BadRequest(c, "year must be between 2000 and 2100")
			return
		}
		year = parsed
	}
	if raw := c.Query("month"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 12 {
			response.BadRequest(c, "month must be between 1 and 12")
			return
		}
		month = time.Month(parsed)
	}
	overview, err := h.service.GetOverview(c.Request.Context(), subject.UserID, year, month)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

func (h *CheckInHandler) Claim(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	result, err := h.service.Claim(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
