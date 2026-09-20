package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/responsediag"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestResponseStreamTimingLogsCorrelatedLocalWriteAndFlush(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := logger.IntoContext(c.Request.Context(), zap.New(core).With(zap.String("request_id", "timing-request"), zap.String("client_request_id", "timing-client")))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.Use(InboundEndpointMiddleware())
	frame := "data: {\"type\":\"response.output_text.delta\",\"sequence_number\":4,\"delta\":\"private answer\"}\n\n"
	router.POST("/v1/responses", func(c *gin.Context) {
		resp := &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(frame))}
		responsediag.WrapUpstream(c.Request.Context(), resp)
		p, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		c.Header("Content-Type", "text/event-stream")
		_, err = c.Writer.Write(p)
		require.NoError(t, err)
		c.Writer.Flush()
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
	require.Equal(t, frame, recorder.Body.String())
	require.True(t, recorder.Flushed)
	entries := logs.FilterMessage("gateway.response_stream_timing").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	require.Equal(t, "timing-request", fields["request_id"])
	require.Equal(t, "timing-client", fields["client_request_id"])
	raw, err := json.Marshal(fields["stream_timing"])
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private answer")
	var timing responsediag.StreamTimingRecord
	require.NoError(t, json.Unmarshal(raw, &timing))
	require.EqualValues(t, 4, *timing.Upstream.FirstContent.SequenceNumber)
	require.EqualValues(t, 4, *timing.Downstream.FirstContent.SequenceNumber)
	require.NotNil(t, timing.Downstream.FirstContent.WriteStartedMS)
	require.NotNil(t, timing.Downstream.FirstContent.FlushCompletedMS)
	require.GreaterOrEqual(t, *timing.Downstream.FirstContent.FlushCompletedMS, timing.Downstream.FirstContent.ObservedMS)
}

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
	require.Equal(t, "extra_content", got.Downstream.Status)
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

func TestRequestDiagnosticsCaptureClientBodyBeforeRewrite(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	original := `{"model":"client-model","api_key":"client-secret"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(original))
	startResponseDiagnostics(c)
	body, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)
	require.Equal(t, original, string(body))
	// Composite routing or a handler replaces the body after reading it.
	c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"mapped-model"}`))
	var record responsediag.Record
	task := wrapUsageRecordTaskContext(c.Request.Context(), func(ctx context.Context) {
		require.NoError(t, json.Unmarshal(responsediag.Snapshot(ctx), &record))
	})
	task(context.Background())
	require.Contains(t, record.IncomingRequest.Body, "client-model")
	require.NotContains(t, record.IncomingRequest.Body, "mapped-model")
	require.NotContains(t, record.IncomingRequest.Body, "client-secret")
}

func TestInboundEndpointMiddlewareConfiguredDiagnosticLimit(t *testing.T) {
	const limit = 1024
	body := `{"text":"` + strings.Repeat("x", 1500) + `"}`
	var record responsediag.Record
	router := gin.New()
	router.Use(InboundEndpointMiddleware(limit))
	router.POST("/v1/responses", func(c *gin.Context) {
		actual, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(actual))
		c.Header("Content-Type", "application/json")
		_, err = c.Writer.WriteString(body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(responsediag.Snapshot(c.Request.Context()), &record))
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
	require.Equal(t, body, recorder.Body.String())
	require.Equal(t, limit, record.IncomingRequest.LimitBytes)
	require.Equal(t, limit, record.Downstream.LimitBytes)
	require.Len(t, record.IncomingRequest.Body, limit)
	require.Len(t, record.Downstream.Body, limit)
}
