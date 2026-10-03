package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type diagnosticUsageRepo struct {
	service.UsageLogRepository
	value json.RawMessage
}

func (r *diagnosticUsageRepo) GetByID(_ context.Context, id int64) (*service.UsageLog, error) {
	if id == 404 {
		return nil, service.ErrUsageLogNotFound
	}
	return &service.UsageLog{ID: id, ResponseDiagnostics: r.value}, nil
}
func TestAdminUsageResponseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		value      json.RawMessage
		status     int
		contains   string
	}{
		{"detail", "/1/response", json.RawMessage(`{"upstream":{"body":"{}tail"}}`), 200, `"bodies_omitted":true`},
		{"metadata retains status", "/1/response", json.RawMessage(`{"bodies_omitted":true,"summary":{"downstream":"extra_content"},"downstream":{"status":"extra_content","bytes":999,"complete":true}}`), 200, `"status":"extra_content"`},
		{"historical truncated capture", "/1/response", json.RawMessage(`{"summary":{"downstream":"incomplete"},"downstream":{"status":"incomplete","body":"data: {\"text\":\"cut","content_type":"text/event-stream","truncated":true,"complete":true,"issues":[{"kind":"invalid_json","frame":1}]}}`), 200, `"kind":"capture_truncated"`},
		{"historical", "/1/response", nil, 200, `"data":null`},
		{"missing", "/404/response", nil, 404, "usage log not found"},
		{"bad id", "/invalid/response", nil, 400, "Invalid usage id"},
		{"negative id", "/-1/response", nil, 400, "Invalid usage id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := NewUsageHandler(service.NewUsageService(&diagnosticUsageRepo{value: tc.value}, nil, nil, nil), nil, nil, nil)
			r := gin.New()
			r.GET("/:id/response", h.ResponseDiagnostics)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, w.Code)
			require.Contains(t, w.Body.String(), tc.contains)
			if tc.status == 200 {
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), `"body":`)
				require.NotContains(t, w.Body.String(), `"json":`)
				require.NotContains(t, w.Body.String(), `"extra":`)
			}
		})
	}
}
