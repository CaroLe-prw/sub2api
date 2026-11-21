package admin

import (
	"errors"
	"io"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// CandyTest runs a single explicit OAuth-only reasoning check.
// POST /api/v1/admin/accounts/:id/candy-test
func (h *AccountHandler) CandyTest(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	var req struct {
		GroupID int64 `json:"group_id" binding:"gte=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid candy test request")
		return
	}
	result, err := h.accountTestService.RunCandyTest(c.Request.Context(), accountID, req.GroupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
