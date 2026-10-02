package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type incompleteUploadBody struct{}

func (incompleteUploadBody) Read(p []byte) (int, error) {
	return copy(p, `{"model":`), io.ErrUnexpectedEOF
}
func (incompleteUploadBody) Close() error { return nil }

func TestOpsErrorLoggerMiddleware_IncompleteUploadRetainsLogWithoutSLAPenalty(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	gin.SetMode(gin.TestMode)
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(OpsErrorLoggerMiddleware(ops))
	router.POST("/v1/responses", func(c *gin.Context) {
		_, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "Failed to read request body"}})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = incompleteUploadBody{}
	req.ContentLength = 100
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, int64(1), OpsErrorLogQueueLength())
	entry := (<-opsErrorLogQueue).entry
	require.Equal(t, "Failed to read request body", entry.ErrorMessage)
	require.Equal(t, "request", entry.ErrorPhase)
	require.Equal(t, "client", entry.ErrorOwner)
	require.Equal(t, "client_request", entry.ErrorSource)
	require.True(t, entry.IsBusinessLimited)
	require.Nil(t, entry.UpstreamStatusCode)
}

func TestClassifyOpsErrorLog_RequestBodyReadFailureScope(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		status        int
		upstream      bool
		excluded      bool
	}{
		{"inbound", "Failed to read request body", 400, false, true},
		{"upstream same message", "Failed to read request body", 400, true, false},
		{"invalid parameters", "missing required parameter: model", 400, false, false},
		{"upstream stream EOF", "unexpected EOF", 502, true, false},
		{"server read failure", "Failed to read request body", 500, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.upstream {
				service.SetOpsUpstreamError(c, tc.status, tc.message, "")
			}
			_, excluded, _, _ := classifyOpsErrorLog(c, "invalid_request_error", tc.message, "", tc.status)
			require.Equal(t, tc.excluded, excluded)
		})
	}
}

func TestClassifyOpsErrorLog_RequestBodyMessageWithUpstreamEvidenceCountsTowardsSLA(t *testing.T) {
	for _, upstreamType := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		errType := "invalid_request_error"
		if upstreamType {
			errType = "upstream_error"
		} else {
			// Zero represents an attempted upstream call without an HTTP response.
			c.Set(service.OpsUpstreamStatusCodeKey, 0)
		}
		_, excluded, _, _ := classifyOpsErrorLog(c, errType, "Failed to read request body", "", 400)
		require.False(t, excluded)
	}
}
