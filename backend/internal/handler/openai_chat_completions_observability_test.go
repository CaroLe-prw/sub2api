//go:build unit

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatCompletionsSuccessFinalizesSchedulerObservabilityTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _, upstream, router, cleanup := newGrokCredentialFailoverHandler(t, "postmap_cancel")
	defer cleanup()
	requestCtx := context.WithValue(context.Background(), ctxkey.RequestID, "chat-observability-success")
	h.gatewayService.RecordOpenAISchedulerObservabilitySelection(
		requestCtx,
		service.OpenAIAccountScheduleRequest{RequestedModel: "grok"},
		service.OpenAIAccountScheduleDecision{Candidates: []service.OpenAISchedulerObservabilityCandidate{
			{AccountID: 801, AccountName: "revoked", Rank: 1},
		}},
		&service.AccountSelectionResult{Account: &service.Account{ID: 801, Name: "revoked"}},
		nil,
	)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/openai/v1/chat/completions",
		bytes.NewBufferString(`{"model":"grok","messages":[{"role":"user","content":"hello"}],"stream":false}`),
	).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, []int64{801}, upstream.accountHits())

	snapshot := h.gatewayService.GetOpenAISchedulerObservabilitySnapshot(context.Background(), service.OpenAISchedulerObservabilityQuery{
		TimeRange: "1h",
		View:      "requests",
	})
	require.Len(t, snapshot.Traces, 1)
	trace := snapshot.Traces[0]
	require.Equal(t, "success", trace.Status)
	require.Equal(t, int64(801), trace.AccountPath[len(trace.AccountPath)-1].ID)
	requestSuccessCount := 0
	for _, attempt := range trace.Attempts {
		if attempt.Kind == "request_success" {
			requestSuccessCount++
		}
	}
	require.Equal(t, 1, requestSuccessCount)
}
