//go:build unit

package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSchedulerProbeRoutesAreIndependentAdminRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		SchedulerObservability: adminhandler.NewSchedulerObservabilityHandler(nil),
		ChannelMonitor:         adminhandler.NewChannelMonitorHandler(nil),
		ScheduledTest:          adminhandler.NewScheduledTestHandler(nil, nil, nil),
	}}

	registerSchedulerObservabilityRoutes(router.Group("/admin"), handlers)

	want := map[string]struct{}{
		http.MethodGet + " /admin/scheduler-observability/probes/policy":                            {},
		http.MethodPut + " /admin/scheduler-observability/probes/policy":                            {},
		http.MethodGet + " /admin/scheduler-observability/probes/overview":                          {},
		http.MethodGet + " /admin/scheduler-observability/probes/accounts/:account_id/model-policy": {},
		http.MethodPut + " /admin/scheduler-observability/probes/accounts/:account_id/model-policy": {},
		http.MethodGet + " /admin/scheduler-observability/probes/plans/:id/results":                 {},
		http.MethodPost + " /admin/scheduler-observability/probes/plans/:id/run":                    {},
	}
	for _, route := range router.Routes() {
		delete(want, route.Method+" "+route.Path)
	}
	require.Empty(t, want)
}
