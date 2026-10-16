package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func ProvideGroupHandler(adminService service.AdminService, dashboard *service.DashboardService, capacity *service.GroupCapacityService, cfg *config.Config, preferences *service.UserPreferredAccountService) *GroupHandler {
	h := NewGroupHandlerWithConfig(adminService, dashboard, capacity, cfg)
	h.preferredAccounts = preferences
	return h
}

func (h *GroupHandler) preferredAccountParams(c *gin.Context, withUser bool) (int64, int64, bool) {
	if h.rejectUnsupportedSimpleModeOperation(c, "preferred_accounts") {
		return 0, 0, false
	}
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "Invalid group ID")
		return 0, 0, false
	}
	var userID int64
	if withUser {
		userID, err = strconv.ParseInt(c.Param("user_id"), 10, 64)
		if err != nil || userID <= 0 {
			response.BadRequest(c, "Invalid user ID")
			return 0, 0, false
		}
	}
	return groupID, userID, true
}

func (h *GroupHandler) GetPreferredAccounts(c *gin.Context) {
	groupID, _, ok := h.preferredAccountParams(c, false)
	if !ok {
		return
	}
	entries, err := h.preferredAccounts.List(c.Request.Context(), groupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, entries)
}

func (h *GroupHandler) SetPreferredAccount(c *gin.Context) {
	groupID, userID, ok := h.preferredAccountParams(c, true)
	if !ok {
		return
	}
	var req struct {
		AccountID int64 `json:"account_id" binding:"required,gt=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if err := h.preferredAccounts.Set(c.Request.Context(), userID, groupID, req.AccountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Preferred account saved"})
}

func (h *GroupHandler) DeletePreferredAccount(c *gin.Context) {
	groupID, userID, ok := h.preferredAccountParams(c, true)
	if !ok {
		return
	}
	if err := h.preferredAccounts.Delete(c.Request.Context(), userID, groupID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Preferred account removed"})
}
