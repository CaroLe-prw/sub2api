package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SchedulerObservabilityHandler struct {
	gatewayService *service.OpenAIGatewayService
}

func NewSchedulerObservabilityHandler(gatewayService *service.OpenAIGatewayService) *SchedulerObservabilityHandler {
	return &SchedulerObservabilityHandler{gatewayService: gatewayService}
}

// GetSnapshot returns the bounded in-process scheduler trace window.
// GET /api/v1/admin/scheduler-observability/snapshot
func (h *SchedulerObservabilityHandler) GetSnapshot(c *gin.Context) {
	if h == nil || h.gatewayService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Scheduler observability is not available")
		return
	}

	page, pageSize := response.ParsePagination(c)
	query := service.OpenAISchedulerObservabilityQuery{
		TimeRange:   strings.TrimSpace(c.Query("time_range")),
		Model:       strings.TrimSpace(c.Query("model")),
		View:        strings.TrimSpace(c.Query("view")),
		Page:        page,
		PageSize:    pageSize,
		Search:      strings.TrimSpace(c.Query("search")),
		TraceFilter: strings.TrimSpace(c.Query("trace_filter")),
		RequestType: strings.TrimSpace(c.Query("request_type")),
	}
	if query.RequestType != "" && query.RequestType != "sync" && query.RequestType != "stream" && query.RequestType != "ws_v2" {
		response.BadRequest(c, "Invalid request_type")
		return
	}
	var ok bool
	if query.GroupID, ok = parseSchedulerObservabilityID(c, "group_id"); !ok {
		return
	}
	if query.AccountID, ok = parseSchedulerObservabilityID(c, "account_id"); !ok {
		return
	}
	if query.APIKeyID, ok = parseSchedulerObservabilityID(c, "api_key_id"); !ok {
		return
	}

	response.Success(c, h.gatewayService.GetOpenAISchedulerObservabilitySnapshot(c.Request.Context(), query))
}

func parseSchedulerObservabilityID(c *gin.Context, name string) (*int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "Invalid "+name)
		return nil, false
	}
	return &value, true
}
