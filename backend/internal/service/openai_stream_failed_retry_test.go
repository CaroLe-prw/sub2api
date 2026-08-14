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
			"event: response.queued",
			`data: {"type":"response.queued","response":{"id":"resp_1"}}`,
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

func TestOpenAIStreamFailedWithUsageMustNotReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	svc := &OpenAIGatewayService{}
	payload := []byte(`{"type":"response.failed","response":{"error":{"type":"server_error","message":"Upstream failed"},"usage":{"input_tokens":11,"output_tokens":3}}}`)

	err := svc.newOpenAIStreamFailoverError(c, &Account{ID: 9, Platform: PlatformOpenAI}, false, "req_billed", payload, "Upstream failed")

	require.True(t, err.BillingExposurePossible)
	require.False(t, err.ShouldRetryNextAccount())
}

func TestOpenAIStreamWithoutExplicitErrorMustNotReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	svc := &OpenAIGatewayService{}

	err := svc.newOpenAIStreamFailoverError(
		c,
		&Account{ID: 9, Platform: PlatformOpenAI},
		false,
		"req_ambiguous",
		nil,
		"OpenAI stream ended before a terminal event",
	)

	require.True(t, err.BillingExposurePossible)
	require.False(t, err.RetryableOnSameAccount)
	require.False(t, err.ShouldRetryNextAccount())
	require.Equal(t, GatewayFailureReason("stream_outcome_unknown"), err.Reason)
}

func TestOpenAIStreamExplicit429AllowsOneSameAccountRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	payload := []byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"busy"}}}`)
	err := (&OpenAIGatewayService{}).newOpenAIStreamFailoverError(
		c,
		&Account{ID: 7},
		false,
		"req-rate-limit",
		payload,
		"busy",
	)

	require.True(t, err.ShouldRetryNextAccount())
	require.True(t, err.AllowsOneSameAccountRetryBeforeSwitch())
	require.False(t, err.RetryableOnSameAccount)
}

func TestOpenAIStreamExplicit429WithUsageMustNotReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	payload := []byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"busy"},"usage":{"input_tokens":12,"output_tokens":1}}}`)
	err := (&OpenAIGatewayService{}).newOpenAIStreamFailoverError(
		c,
		&Account{ID: 7},
		false,
		"req-billed-rate-limit",
		payload,
		"busy",
	)

	require.True(t, err.BillingExposurePossible)
	require.False(t, err.RetryableOnSameAccount)
	require.False(t, err.ShouldRetryNextAccount())
}

func TestLocallySynthesized502DoesNotAllowSemanticRetry(t *testing.T) {
	err := &UpstreamFailoverError{
		StatusCode:   http.StatusBadGateway,
		ResponseBody: []byte(`{"error":{"message":"local gateway failure"}}`),
	}

	require.False(t, err.AllowsOneSameAccountRetryBeforeSwitch())
}

func TestOpenAIStreamItemNotPersistedRequestsOneRepairRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	payload := []byte(`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","message":"Item with id 'rs_missing' not found. Items are not persisted when store is set to false."}}}`)
	message := extractOpenAISSEErrorMessage(payload)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_missing_item"}}`,
			"",
			"event: codex.rate_limits",
			`data: {"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":12}}}`,
			"",
			"event: response.failed",
			"data: " + string(payload),
			"",
		}, "\n"))),
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 29, Platform: PlatformOpenAI}, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol")
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, openAIResponsesItemReferenceRecoveryRequested(c))
	require.False(t, openAIResponsesItemReferenceRecoveryApplied(c))
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.Equal(t, 1, failoverErr.MinimumSameAccountRetries)
	require.Equal(t, 1, failoverErr.SameAccountRetryLimit(0))
	require.Equal(t, openAIResponsesItemNotPersistedReason, failoverErr.Reason)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())

	markOpenAIResponsesItemReferenceRecoveryApplied(c)
	require.False(t, openAIStreamFailedEventShouldFailover(payload, message, c), "repair failure must not loop forever")
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

func TestOpenAIStreamKeepaliveBeforeUpstreamFailureStillAllowsFailover(t *testing.T) {
	recorder := newOpenAIResponseFlushRecorder()
	reader, writer := io.Pipe()
	resultCh, errCh := runOpenAIResponseFlushTestAsync(recorder, reader, config.GatewayConfig{
		StreamKeepaliveInterval: 1,
		MaxLineSize:             defaultMaxLineSize,
	})

	_, err := writer.Write([]byte(strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_keepalive_retry"}}`,
		"",
		"event: response.in_progress",
		`data: {"type":"response.in_progress","response":{"id":"resp_keepalive_retry"}}`,
		"",
		"event: response.queued",
		`data: {"type":"response.queued","response":{"id":"resp_keepalive_retry"}}`,
		"",
	}, "\n") + "\n"))
	require.NoError(t, err)
	waitOpenAIResponseFlushCount(t, recorder, 1)
	body, _ := recorder.snapshot()
	require.Equal(t, ":\n\n", body)

	_, err = writer.Write([]byte(strings.Join([]string{
		"event: response.failed",
		`data: {"type":"response.failed","response":{"id":"resp_keepalive_retry","status":"failed","error":{"code":"upstream_error","message":"Upstream request failed"},"output":[]}}`,
		"",
	}, "\n") + "\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	result := <-resultCh
	streamErr := <-errCh
	require.NotNil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, streamErr, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Nil(t, result.firstTokenMs)
	body, _ = recorder.snapshot()
	require.Equal(t, ":\n\n", body)
	require.NotContains(t, body, "response.created")
	require.NotContains(t, body, "response.queued")
	require.NotContains(t, body, "response.failed")
}

func TestOpenAIResponseSemanticAdjustedWrittenSizeExcludesStreamKeepalive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	require.Equal(t, -1, OpenAIResponseSemanticAdjustedWrittenSize(c))

	n, err := c.Writer.Write([]byte(":\n\n"))
	require.NoError(t, err)
	recordOpenAIStreamKeepaliveBytes(c, n)
	require.Equal(t, -1, OpenAIResponseSemanticAdjustedWrittenSize(c))

	_, err = c.Writer.Write([]byte("data: semantic\n\n"))
	require.NoError(t, err)
	require.Greater(t, OpenAIResponseSemanticAdjustedWrittenSize(c), 0)
}

func TestGatewayResponseSemanticAdjustedWrittenSizeExcludesAnthropicKeepalive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	before := GatewayResponseSemanticAdjustedWrittenSize(c)

	n, err := c.Writer.Write([]byte("event: ping\ndata: {\"type\": \"ping\"}\n\n"))
	require.NoError(t, err)
	recordGatewayStreamKeepaliveBytes(c, n)
	require.Equal(t, before, GatewayResponseSemanticAdjustedWrittenSize(c),
		"transport-only Anthropic ping must not prevent pre-output account failover")

	_, err = c.Writer.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
	require.NoError(t, err)
	require.Greater(t, GatewayResponseSemanticAdjustedWrittenSize(c), before,
		"semantic Anthropic output must still prevent account failover")
}
