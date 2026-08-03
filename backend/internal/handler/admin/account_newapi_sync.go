package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) GetNewAPISyncConfig(c *gin.Context) {
	if h.upstreamBillingProbe == nil {
		response.ErrorFrom(c, service.ErrNewAPISyncUnavailable)
		return
	}
	accountID, ok := newAPISyncAccountID(c)
	if !ok {
		return
	}
	config, err := h.upstreamBillingProbe.GetNewAPISyncConfig(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

func (h *AccountHandler) UpdateNewAPISyncConfig(c *gin.Context) {
	if h.upstreamBillingProbe == nil {
		response.ErrorFrom(c, service.ErrNewAPISyncUnavailable)
		return
	}
	accountID, ok := newAPISyncAccountID(c)
	if !ok {
		return
	}
	var request service.NewAPISyncConfigUpdate
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid NewAPI synchronization configuration")
		return
	}
	config, err := h.upstreamBillingProbe.UpdateNewAPISyncConfig(c.Request.Context(), accountID, &request)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, config)
}

func (h *AccountHandler) TestNewAPISyncConnection(c *gin.Context) {
	if h.upstreamBillingProbe == nil {
		response.ErrorFrom(c, service.ErrNewAPISyncUnavailable)
		return
	}
	accountID, ok := newAPISyncAccountID(c)
	if !ok {
		return
	}
	result, err := h.upstreamBillingProbe.TestNewAPIConnection(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *AccountHandler) SyncNewAPIRatio(c *gin.Context) {
	if h.upstreamBillingProbe == nil {
		response.ErrorFrom(c, service.ErrNewAPISyncUnavailable)
		return
	}
	accountID, ok := newAPISyncAccountID(c)
	if !ok {
		return
	}
	result, err := h.upstreamBillingProbe.SyncNewAPIAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func newAPISyncAccountID(c *gin.Context) (int64, bool) {
	if c == nil {
		return 0, false
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return accountID, true
}
