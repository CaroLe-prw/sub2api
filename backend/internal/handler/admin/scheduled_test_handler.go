package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ScheduledTestHandler handles admin scheduled-test-plan management.
type ScheduledTestHandler struct {
	scheduledTestSvc *service.ScheduledTestService
	accountTestSvc   *service.AccountTestService
	probeReporter    *service.OpenAIGatewayService
}

// NewScheduledTestHandler creates a new ScheduledTestHandler.
func NewScheduledTestHandler(
	scheduledTestSvc *service.ScheduledTestService,
	accountTestSvc *service.AccountTestService,
	probeReporter *service.OpenAIGatewayService,
) *ScheduledTestHandler {
	return &ScheduledTestHandler{scheduledTestSvc: scheduledTestSvc, accountTestSvc: accountTestSvc, probeReporter: probeReporter}
}

type createScheduledTestPlanRequest struct {
	AccountID      int64  `json:"account_id" binding:"required"`
	ModelID        string `json:"model_id"`
	CronExpression string `json:"cron_expression" binding:"required"`
	Enabled        *bool  `json:"enabled"`
	MaxResults     int    `json:"max_results"`
	AutoRecover    *bool  `json:"auto_recover"`
}

type updateScheduledTestPlanRequest struct {
	ModelID        string `json:"model_id"`
	CronExpression string `json:"cron_expression"`
	Enabled        *bool  `json:"enabled"`
	MaxResults     int    `json:"max_results"`
	AutoRecover    *bool  `json:"auto_recover"`
}

// ListByAccount GET /admin/accounts/:id/scheduled-test-plans
func (h *ScheduledTestHandler) ListByAccount(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid account id")
		return
	}

	plans, err := h.scheduledTestSvc.ListPlansByAccount(c.Request.Context(), accountID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, plans)
}

// ListChannelMonitorPool returns the admin-only scheduler-probe pool snapshot.
// It never participates in the public status API. The legacy method name is
// retained while old channel-monitor API aliases remain compatible.
func (h *ScheduledTestHandler) ListChannelMonitorPool(c *gin.Context) {
	accountIDs, err := parseSchedulerProbeAccountIDs(c.Query("account_ids"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	items, err := h.scheduledTestSvc.ListChannelMonitorPoolOverview(c.Request.Context(), accountIDs)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	response.Success(c, map[string]any{"items": items})
}

func parseSchedulerProbeAccountIDs(raw string) ([]int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 200 {
		return nil, fmt.Errorf("account_ids must contain at most 200 ids")
	}
	seen := make(map[int64]struct{}, len(parts))
	accountIDs := make([]int64, 0, len(parts))
	for _, part := range parts {
		accountID, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || accountID <= 0 {
			return nil, fmt.Errorf("account_ids must contain positive integers")
		}
		if _, duplicate := seen[accountID]; duplicate {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	return accountIDs, nil
}

// RunNow executes one automatically managed account/model streaming probe.
func (h *ScheduledTestHandler) RunNow(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}
	plan, err := h.scheduledTestSvc.GetPlan(c.Request.Context(), planID)
	if err != nil {
		response.NotFound(c, "plan not found")
		return
	}
	if plan.ManagedBy != service.ScheduledTestManagedBySchedulerProbe {
		response.NotFound(c, "scheduler probe plan not found")
		return
	}
	result, err := h.accountTestSvc.RunTestBackground(c.Request.Context(), plan.AccountID, plan.ModelID)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	if err := h.scheduledTestSvc.SaveResult(c.Request.Context(), plan.ID, plan.MaxResults, result); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	if plan.ManagedBy == service.ScheduledTestManagedBySchedulerProbe && h.probeReporter != nil {
		var firstTokenMs *int
		if result.TTFTMs != nil {
			value := int(*result.TTFTMs)
			firstTokenMs = &value
		}
		h.probeReporter.ReportChannelMonitorProbe(plan.AccountID, plan.ModelID, result.Status == "success", firstTokenMs)
	}
	response.Success(c, result)
}

// ListProbeResults GET /admin/scheduler-observability/probes/plans/:id/results
// Only automatically managed scheduler probes are addressable through this
// API, keeping it semantically separate from general scheduled tests.
func (h *ScheduledTestHandler) ListProbeResults(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}
	plan, err := h.scheduledTestSvc.GetPlan(c.Request.Context(), planID)
	if err != nil || plan.ManagedBy != service.ScheduledTestManagedBySchedulerProbe {
		response.NotFound(c, "scheduler probe plan not found")
		return
	}

	limit := 50
	if value, parseErr := strconv.Atoi(c.Query("limit")); parseErr == nil && value > 0 {
		limit = value
	}
	results, err := h.scheduledTestSvc.ListResults(c.Request.Context(), planID, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, results)
}

// Create POST /admin/scheduled-test-plans
func (h *ScheduledTestHandler) Create(c *gin.Context) {
	var req createScheduledTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	plan := &service.ScheduledTestPlan{
		AccountID:      req.AccountID,
		ModelID:        req.ModelID,
		CronExpression: req.CronExpression,
		Enabled:        true,
		MaxResults:     req.MaxResults,
	}
	if req.Enabled != nil {
		plan.Enabled = *req.Enabled
	}
	if req.AutoRecover != nil {
		plan.AutoRecover = *req.AutoRecover
	}

	created, err := h.scheduledTestSvc.CreatePlan(c.Request.Context(), plan)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, created)
}

// Update PUT /admin/scheduled-test-plans/:id
func (h *ScheduledTestHandler) Update(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	existing, err := h.scheduledTestSvc.GetPlan(c.Request.Context(), planID)
	if err != nil {
		response.NotFound(c, "plan not found")
		return
	}

	var req updateScheduledTestPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if req.ModelID != "" {
		existing.ModelID = req.ModelID
	}
	if req.CronExpression != "" {
		existing.CronExpression = req.CronExpression
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.MaxResults > 0 {
		existing.MaxResults = req.MaxResults
	}
	if req.AutoRecover != nil {
		existing.AutoRecover = *req.AutoRecover
	}

	updated, err := h.scheduledTestSvc.UpdatePlan(c.Request.Context(), existing)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, updated)
}

// Delete DELETE /admin/scheduled-test-plans/:id
func (h *ScheduledTestHandler) Delete(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	if err := h.scheduledTestSvc.DeletePlan(c.Request.Context(), planID); err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// ListResults GET /admin/scheduled-test-plans/:id/results
func (h *ScheduledTestHandler) ListResults(c *gin.Context) {
	planID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid plan id")
		return
	}

	limit := 50
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}

	results, err := h.scheduledTestSvc.ListResults(c.Request.Context(), planID, limit)
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, results)
}
