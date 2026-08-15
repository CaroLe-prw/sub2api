//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type scheduledTestRunNowPlanRepo struct {
	service.ScheduledTestPlanRepository
	plan *service.ScheduledTestPlan
}

func (r *scheduledTestRunNowPlanRepo) GetByID(_ context.Context, _ int64) (*service.ScheduledTestPlan, error) {
	return r.plan, nil
}

type scheduledTestRunNowResultRepo struct {
	service.ScheduledTestResultRepository
}

func (r *scheduledTestRunNowResultRepo) Create(_ context.Context, result *service.ScheduledTestResult) (*service.ScheduledTestResult, error) {
	return result, nil
}

func (r *scheduledTestRunNowResultRepo) PruneOldResults(_ context.Context, _ int64, _ int) error {
	return nil
}

type scheduledTestRunNowProbeRunner struct {
	calls int
}

func (r *scheduledTestRunNowProbeRunner) RunChannelMonitorProbeBackground(_ context.Context, _ int64, _ string) (*service.ScheduledTestResult, error) {
	r.calls++
	now := time.Now()
	return &service.ScheduledTestResult{Status: "success", StartedAt: now, FinishedAt: now}, nil
}

func TestScheduledTestHandlerRunNowUsesProbeEntryPoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	plan := &service.ScheduledTestPlan{
		ID: 7, AccountID: 42, ModelID: "gpt-5", MaxResults: 10,
		ManagedBy: service.ScheduledTestManagedBySchedulerProbe,
	}
	planRepo := &scheduledTestRunNowPlanRepo{plan: plan}
	probeRunner := &scheduledTestRunNowProbeRunner{}
	handler := &ScheduledTestHandler{
		scheduledTestSvc: service.NewScheduledTestService(planRepo, &scheduledTestRunNowResultRepo{}),
		accountTestSvc:   probeRunner,
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/scheduler-observability/probes/plans/7/run", nil)
	c.Params = gin.Params{{Key: "id", Value: "7"}}

	handler.RunNow(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, probeRunner.calls)
}
