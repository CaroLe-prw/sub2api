package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newOpenAIAutoContextCompactionTestAccount(extra map[string]any) *Account {
	return &Account{
		ID:       7001,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":   "sk-test",
			"base_url":  "https://compat.example",
			"pool_mode": true,
		},
		Extra: extra,
	}
}

func newOpenAIAutoContextCompactionTestContext(path string, body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	return c
}

func TestApplyOpenAIAutoContextCompactionToBodyInjectsForSupportedPoolAccount(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"role":"user","content":"` + strings.Repeat("x", 256<<10) + `"}]}`)
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses", body)

	account := newOpenAIAutoContextCompactionTestAccount(map[string]any{openAIContextCompactionSupportedExtraKey: true})
	updated, injected, err := applyOpenAIAutoContextCompactionToBody(c, account, body)

	require.NoError(t, err)
	require.True(t, injected)
	require.Equal(t, "compaction", gjson.GetBytes(updated, "context_management.0.type").String())
	require.Equal(t, defaultOpenAIContextCompactionThreshold, gjson.GetBytes(updated, "context_management.0.compact_threshold").Int())
	require.True(t, openAIAutoContextCompactionInjected(c))
	require.True(t, openAIAutoContextCompactionMayFailover(c))
}

func TestApplyOpenAIAutoContextCompactionToBodyUsesAccountThresholdAndPinsPreviousResponse(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","previous_response_id":"resp_1","input":"next"}`)
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses", body)
	account := newOpenAIAutoContextCompactionTestAccount(map[string]any{
		openAIContextCompactionSupportedExtraKey: true,
		openAIContextCompactionThresholdExtraKey: float64(150_000),
	})

	updated, injected, err := applyOpenAIAutoContextCompactionToBody(c, account, body)

	require.NoError(t, err)
	require.True(t, injected)
	require.Equal(t, int64(150_000), gjson.GetBytes(updated, "context_management.0.compact_threshold").Int())
	require.False(t, openAIAutoContextCompactionMayFailover(c))
}

func TestApplyOpenAIAutoContextCompactionToBodySkipsResponsesLite(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":"hello"}`)
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses", body)
	c.Request.Header.Set(responsesLiteHeader, "true")
	account := newOpenAIAutoContextCompactionTestAccount(map[string]any{openAIContextCompactionSupportedExtraKey: true})

	updated, injected, err := applyOpenAIAutoContextCompactionToBody(c, account, body)

	require.NoError(t, err)
	require.False(t, injected)
	require.JSONEq(t, string(body), string(updated))
	require.False(t, gjson.GetBytes(updated, "context_management").Exists())
	require.False(t, openAIAutoContextCompactionInjected(c))
	require.False(t, openAIAutoContextCompactionMayFailover(c))
}

func TestApplyOpenAIAutoContextCompactionToBodyRespectsCompatibilityGuards(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    []byte
		account *Account
	}{
		{
			name:    "short unknown request",
			path:    "/v1/responses",
			body:    []byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
			account: newOpenAIAutoContextCompactionTestAccount(nil),
		},
		{
			name:    "large unknown request",
			path:    "/v1/responses",
			body:    []byte(`{"model":"gpt-5.6-sol","input":"` + strings.Repeat("x", 256<<10) + `"}`),
			account: newOpenAIAutoContextCompactionTestAccount(nil),
		},
		{
			name: "known unsupported",
			path: "/v1/responses",
			body: []byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
			account: newOpenAIAutoContextCompactionTestAccount(map[string]any{
				openAIContextCompactionSupportedExtraKey: false,
			}),
		},
		{
			name:    "standalone compact endpoint",
			path:    "/v1/responses/compact",
			body:    []byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
			account: newOpenAIAutoContextCompactionTestAccount(nil),
		},
		{
			name:    "client value",
			path:    "/v1/responses",
			body:    []byte(`{"model":"gpt-5.6-sol","context_management":[{"type":"compaction","compact_threshold":123456}],"input":"hello"}`),
			account: newOpenAIAutoContextCompactionTestAccount(nil),
		},
		{
			name:    "responses lite account pool",
			path:    "/v1/responses",
			body:    []byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
			account: newOpenAIAutoContextCompactionTestAccount(map[string]any{openAIContextCompactionSupportedExtraKey: true}),
		},
		{
			name: "non pool account",
			path: "/v1/responses",
			body: []byte(`{"model":"gpt-5.6-sol","input":"hello"}`),
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key": "sk-test",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newOpenAIAutoContextCompactionTestContext(tt.path, tt.body)
			if tt.name == "responses lite account pool" {
				c.Request.Header.Set(responsesLiteHeader, "true")
			}
			updated, injected, err := applyOpenAIAutoContextCompactionToBody(c, tt.account, tt.body)
			require.NoError(t, err)
			require.False(t, injected)
			require.JSONEq(t, string(tt.body), string(updated))
			require.False(t, openAIAutoContextCompactionInjected(c))
			if tt.name == "short unknown request" || tt.name == "large unknown request" || tt.name == "known unsupported" || tt.name == "client value" {
				require.True(t, openAIAutoContextCompactionMayFailover(c))
			} else {
				require.False(t, openAIAutoContextCompactionMayFailover(c))
			}
		})
	}
}

func TestNormalizeOpenAIResponsesRejectedFieldRetryBodyRemovesContextManagement(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","context_management":[{"type":"compaction","compact_threshold":200000}],"input":"hello"}`)
	responses := [][]byte{
		[]byte(`{"error":{"code":"unknown_parameter","param":"context_management","message":"Unknown parameter: context_management"}}`),
		[]byte(`{"error":{"code":"unsupported_parameter","message":"Unsupported parameter: 'context_management[0].type'."}}`),
		[]byte(`{"error":{"code":"unsupported_value","message":"X-OpenAI-Internal-Codex-Responses-Lite does not support server-side compaction.","param":"compact_threshold","type":"invalid_request_error"}}`),
	}
	for _, responseBody := range responses {
		retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, body, responseBody)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "context_management parameter rejection", reason)
		require.False(t, gjson.GetBytes(retryBody, "context_management").Exists())
		require.Equal(t, "hello", gjson.GetBytes(retryBody, "input").String())
	}
}

func TestOpenAIContextWindowFailoverRequiresInjectedStatelessRequest(t *testing.T) {
	payload := []byte(`{"type":"response.failed","response":{"error":{"code":"context_length_exceeded","message":"Your input exceeds the context window"}}}`)
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses", nil)

	require.False(t, openAIStreamFailedEventShouldFailover(payload, "Your input exceeds the context window", c))
	setOpenAIAutoContextCompactionState(c, true, false)
	require.False(t, openAIStreamFailedEventShouldFailover(payload, "Your input exceeds the context window", c))
	setOpenAIAutoContextCompactionState(c, true, true)
	require.True(t, openAIStreamFailedEventShouldFailover(payload, "Your input exceeds the context window", c))
	require.False(t, openAIStreamDataStartsClientOutput(string(payload), "error", c))
}

func TestOpenAIGatewayServiceInjectsAutoContextCompactionIntoPoolRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"input":"hello"}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)),
	}}}
	account := newOpenAIAutoContextCompactionTestAccount(map[string]any{
		"openai_responses_supported":        true,
		openAIContextCompactionModeExtraKey: OpenAIContextCompactionModeForceOn,
	})

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		account,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "compaction", gjson.GetBytes(upstream.bodies[0], "context_management.0.type").String())
}

func TestOpenAIGatewayServicePassthroughRetriesWithoutRejectedAutoContextCompaction(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"input":"hello"}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"code":"unsupported_value","message":"X-OpenAI-Internal-Codex-Responses-Lite does not support server-side compaction.","param":"compact_threshold","type":"invalid_request_error"}}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}}
	account := newOpenAIAutoContextCompactionTestAccount(map[string]any{
		"openai_responses_supported":        true,
		"openai_passthrough":                true,
		openAIContextCompactionModeExtraKey: OpenAIContextCompactionModeForceOn,
	})

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		account,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2)
	require.True(t, gjson.GetBytes(upstream.bodies[0], "context_management").Exists())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "context_management").Exists())
}

func TestOpenAIStreamingContextOverflowFailsOverBeforeClientOutputWhenAutoCompactionActive(t *testing.T) {
	c := newOpenAIAutoContextCompactionTestContext("/v1/responses", nil)
	setOpenAIAutoContextCompactionState(c, true, true)
	account := newOpenAIAutoContextCompactionTestAccount(nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"req_context_overflow"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			"event: response.created",
			`data: {"type":"response.created","response":{"id":"resp_overflow"}}`,
			"",
			"event: error",
			`data: {"type":"error","error":{"code":"context_length_exceeded","message":"Your input exceeds the context window"}}`,
			"",
			"event: response.failed",
			`data: {"type":"response.failed","response":{"id":"resp_overflow","status":"failed","error":{"code":"context_length_exceeded","message":"Your input exceeds the context window"}}}`,
			"",
		}, "\n"))),
	}

	_, err := (&OpenAIGatewayService{}).handleStreamingResponse(
		c.Request.Context(), resp, c, account, time.Now(), "gpt-5.6-sol", "gpt-5.6-sol",
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, c.Writer.Written())
}
