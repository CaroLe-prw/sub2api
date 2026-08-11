package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOpenAIStreamInvalidRequestLabeledUpstreamFailureRetriesBeforeOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	payload := `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"type":"invalid_request_error","message":"Upstream request failed"}}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_1"}}`,
			"",
			"event: response.failed",
			"data: " + payload,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-upstream-request-failed"}},
	}
	account := &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Name: "newapi-pool",
		Credentials: map[string]any{"pool_mode": true},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.False(t, failoverErr.RequestScopedTransient)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "Upstream request failed", events[0].Message)
	require.JSONEq(t, `{"error":{"type":"invalid_request_error"}}`, events[0].Detail)
}

func TestOpenAIStreamInvalidRequestClientFaultStillPassesThrough(t *testing.T) {
	payload := []byte(`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","code":"invalid_value","param":"temperature","message":"Invalid value for temperature"}}}`)
	require.False(t, openAIStreamFailedEventShouldFailover(payload, "Invalid value for temperature"))
	require.False(t, isOpenAIStreamRetryableUpstreamFailure(payload, "Invalid value for temperature"))
}

func TestOpenAIStreamFailedAfterOutputRecordsErrorMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.output_text.delta",
			`data: {"type":"response.output_text.delta","delta":"partial"}`,
			"",
			"event: response.failed",
			`data: {"type":"response.failed","response":{"error":{"type":"server_error","code":"internal_error","message":"Upstream response failed"}}}`,
			"",
		}, "\n"))),
		Header: http.Header{"X-Request-Id": []string{"rid-failed-after-output"}},
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 2, Platform: PlatformOpenAI, Name: "acc"}, time.Now(), "gpt-5.5", "gpt-5.5")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.Contains(t, rec.Body.String(), "partial")

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "http_error", events[0].Kind)
	require.JSONEq(t, `{"error":{"type":"server_error","code":"internal_error"}}`, events[0].Detail)
}
