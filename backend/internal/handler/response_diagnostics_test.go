package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponseDiagnosticsWriterAndUsageSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	startResponseDiagnostics(c)
	c.Header("Content-Type", "text/event-stream")
	first := "data: {\"ok\":true}"
	_, err := c.Writer.WriteString(first)
	require.NoError(t, err)
	c.Writer.Flush()
	tail := "tail\n\n"
	_, err = c.Writer.Write([]byte(tail))
	require.NoError(t, err)
	var got responsediag.Record
	task := wrapUsageRecordTaskContext(c.Request.Context(), func(ctx context.Context) {
		require.NoError(t, json.Unmarshal(responsediag.Snapshot(ctx), &got))
	})
	// Writes after submission cannot change the queued record.
	_, _ = c.Writer.WriteString("data: {}\n\n")
	task(context.Background())
	require.Equal(t, first+tail, got.Downstream.Body)
	require.Equal(t, "extra", got.Downstream.Status)
	require.Equal(t, "tail", got.Downstream.Issues[0].Extra)
	require.Equal(t, first+tail+"data: {}\n\n", recorder.Body.String())
	require.True(t, recorder.Flushed)
}
func TestResponseDiagnosticsSkipsWebsocket(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Upgrade", "websocket")
	original := c.Writer
	startResponseDiagnostics(c)
	require.Same(t, original, c.Writer)
	require.Nil(t, responsediag.Snapshot(c.Request.Context()))
}
