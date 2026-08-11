package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSchedulerObservabilityHandlerGetSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSchedulerObservabilityHandler(&service.OpenAIGatewayService{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/scheduler-observability/snapshot?time_range=6h&view=sessions&page=2&page_size=50", nil)
	handler.GetSnapshot(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"timeRange":"6h"`)
	require.Contains(t, recorder.Body.String(), `"retentionMode":"memory"`)
	require.Contains(t, recorder.Body.String(), `"retentionMax":1000`)
	require.Contains(t, recorder.Body.String(), `"view":"sessions"`)
	require.Contains(t, recorder.Body.String(), `"pageSize":50`)
	require.Contains(t, recorder.Body.String(), `"switchReasons":[]`)
	require.Contains(t, recorder.Body.String(), `"groups":[]`)
	require.Contains(t, recorder.Body.String(), `"traces":[]`)
	require.Contains(t, recorder.Body.String(), `"sessions":[]`)
}

func TestSchedulerObservabilityHandlerRejectsInvalidDimensionIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSchedulerObservabilityHandler(&service.OpenAIGatewayService{})

	for _, parameter := range []string{"group_id", "account_id", "api_key_id"} {
		t.Run(parameter, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/scheduler-observability/snapshot?"+parameter+"=bad", nil)
			handler.GetSnapshot(ctx)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}
