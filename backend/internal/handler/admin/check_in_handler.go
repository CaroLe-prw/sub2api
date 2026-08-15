package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type CheckInHandler struct {
	service *service.CheckInService
}

func NewCheckInHandler(checkInService *service.CheckInService) *CheckInHandler {
	return &CheckInHandler{service: checkInService}
}

func (h *CheckInHandler) ListRecords(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	records, total, err := h.service.AdminListRecords(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, records, total, page, pageSize)
}
