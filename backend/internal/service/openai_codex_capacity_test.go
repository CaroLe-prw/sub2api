package service

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const codexCapacityTestMessage = "Codex is at capacity, please try again later"

func TestOpenAICodexCapacityClassification(t *testing.T) {
	payload := []byte(`{"error":{"type":"invalid_request_error","message":"Codex is at capacity, please try again later"}}`)
	for _, status := range []int{http.StatusBadRequest, http.StatusServiceUnavailable} {
		require.True(t, isOpenAITransientProcessingError(status, "", payload))
		require.True(t, isOpenAIRequestScopedCapacityError(status, "", payload))
		require.True(t, isOpenAIModelCapacityError(status, "", payload))
		require.True(t, shouldRetryOpenAIOAuthCapacityOnSameAccount(
			&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, status, "", payload))
		require.True(t, shouldRetryOpenAINonOAuthCapacityOnSameAccount(
			&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, status, "", payload))
		failure := newOpenAIUpstreamFailoverError(status, nil, payload, codexCapacityTestMessage, false, true)
		require.True(t, failure.RequestScopedTransient)
		require.True(t, failure.RetryableOnSameAccountIfNoOtherAccount)
		require.False(t, failure.ShouldReportAccountScheduleFailure())
		require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode)
		require.NotContains(t, failure.ClientMessage, "Codex is at capacity")
	}
}

func TestOpenAICodexCapacityMessageSources(t *testing.T) {
	for _, body := range []string{
		`{"error":{"message":"CODEX IS AT CAPACITY, please try again later"}}`,
		`{"response":{"error":{"message":"Codex is at capacity, please try again later"}}}`,
		`{"message":"Codex is at capacity, please try again later"}`,
		codexCapacityTestMessage,
	} {
		require.True(t, isOpenAITransientProcessingError(http.StatusBadRequest, "", []byte(body)), body)
		require.True(t, isOpenAIRequestScopedCapacityError(http.StatusBadRequest, "", []byte(body)), body)
		require.True(t, isOpenAIModelCapacityError(http.StatusBadRequest, "", []byte(body)), body)
	}
	for _, body := range []string{
		`{"error":{"type":"invalid_request_error","message":"Invalid temperature"},"echo":"Codex is at capacity, please try again later"}`,
		`{"error":{"message":"Your account has reached its storage capacity"}}`,
	} {
		require.False(t, isOpenAITransientProcessingError(http.StatusBadRequest, "", []byte(body)), body)
		require.False(t, isOpenAIRequestScopedCapacityError(http.StatusBadRequest, "", []byte(body)), body)
		require.False(t, isOpenAIModelCapacityError(http.StatusBadRequest, "", []byte(body)), body)
	}
}

// No error or lifecycle frame may reach the client before the handler retries.
func TestOpenAICodexCapacityBeforeOutputReturnsFailover(t *testing.T) {
	for _, eventType := range []string{"error", "response.failed"} {
		for _, mode := range []string{"stream", "stream_passthrough", "json", "json_passthrough"} {
			t.Run(eventType+"/"+mode, func(t *testing.T) {
				svc := newNonStreamingFailoverTestService()
				c, rec := newNonStreamingFailoverTestContext()
				providerError := map[string]any{"type": "invalid_request_error", "message": codexCapacityTestMessage}
				event := map[string]any{"type": eventType, "error": providerError}
				if eventType == "response.failed" {
					delete(event, "error")
					event["response"] = map[string]any{"id": "resp_codex_capacity", "status": "failed", "error": providerError}
				}
				payload, err := json.Marshal(event)
				require.NoError(t, err)
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_codex_capacity\"}}\n\nevent: " + eventType + "\ndata: " + string(payload) + "\n\n")),
				}
				account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				switch mode {
				case "stream":
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				case "stream_passthrough":
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")
				case "json":
					_, err = svc.handleNonStreamingResponse(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
				case "json_passthrough":
					_, err = svc.handleNonStreamingResponsePassthrough(c.Request.Context(), resp, c, account, "gpt-5.5", "gpt-5.5")
				}
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.True(t, failure.RequestScopedTransient)
				require.True(t, failure.RetryableOnSameAccountIfNoOtherAccount)
				require.False(t, failure.ShouldReportAccountScheduleFailure())
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
			})
		}
	}
}
