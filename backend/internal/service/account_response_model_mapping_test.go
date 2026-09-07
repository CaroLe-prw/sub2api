//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponseModelMappingValidation(t *testing.T) {
	for _, raw := range []any{
		"k3", []string{"k3"}, map[string]any{"k3": 3},
		map[string]any{"": "kimi-k3"}, map[string]any{"k3": " "},
		map[string]any{"k3": "a\nb"}, map[string]any{"k3": strings.Repeat("x", 201)},
		map[string]any{" k3": "a", "k3 ": "b"},
	} {
		require.Error(t, NormalizeResponseModelMappingCredentials(map[string]any{responseModelMappingKey: raw}))
	}
	creds := map[string]any{responseModelMappingKey: map[string]any{" k3 ": " kimi-k3 "}}
	require.NoError(t, NormalizeResponseModelMappingCredentials(creds))
	require.Equal(t, map[string]any{"k3": "kimi-k3"}, creds[responseModelMappingKey])
	require.NoError(t, NormalizeResponseModelMappingCredentials(nil))
	require.NoError(t, NormalizeResponseModelMappingCredentials(map[string]any{responseModelMappingKey: map[string]any{}}))
}

func TestResponseModelMappingWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, contentType, input, want string
	}{
		{"json", "application/json", `{"model":"k3","text":"k3","usage":{"model":"k3"}}`, `{"model":"kimi-k3","text":"k3","usage":{"model":"k3"}}`},
		{"nested", "application/json", `{"response":{"model":"k3"},"message":{"model":"k3"}}`, `{"response":{"model":"kimi-k3"},"message":{"model":"kimi-k3"}}`},
		{"exact", "application/json", `{"model":"k3-256k"}`, `{"model":"k3-256k"}`},
		{"malformed", "application/json", `{"model":"k3"`, `{"model":"k3"`},
		{"other-content", "text/plain", `{"model":"k3"}`, `{"model":"k3"}`},
		{"stream", "text/event-stream", "data: {\"model\":\"k3\"}\n\ndata: [DONE]\n\n", "data: {\"model\":\"kimi-k3\"}\n\ndata: [DONE]\n\n"},
		{"crlf", "text/event-stream", "event: message_start\r\ndata: {\"message\":{\"model\":\"k3\"}}\r\n\r\n", "event: message_start\r\ndata: {\"message\":{\"model\":\"kimi-k3\"}}\r\n\r\n"},
		{"multiline", "text/event-stream", "id: 1\ndata: {\"model\":\ndata: \"k3\"}\n\n", "id: 1\ndata: {\"model\":\"kimi-k3\"}\n\n"},
		{"partial-stream", "text/event-stream", "data: {\"model\":\"k3\"", "data: {\"model\":\"k3\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise boundaries inside JSON strings and SSE frame delimiters.
			for _, chunkSize := range []int{1, 7, len(tc.input)} {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				original := c.Writer
				account := nativeAnthropicTestAccount()
				account.Credentials[responseModelMappingKey] = map[string]any{"k3": "kimi-k3", "kimi-k3": "must-not-chain"}
				finish := installResponseModelMapping(c, account)
				innerFinish := installResponseModelMapping(c, account)
				c.Header("Content-Type", tc.contentType)
				c.Header("Content-Length", "999")
				for offset := 0; offset < len(tc.input); offset += chunkSize {
					end := min(offset+chunkSize, len(tc.input))
					n, err := c.Writer.WriteString(tc.input[offset:end])
					require.NoError(t, err)
					require.Equal(t, end-offset, n)
					c.Writer.Flush()
				}
				innerFinish()
				finish()
				require.Same(t, original, c.Writer)
				require.Equal(t, tc.want, recorder.Body.String())
				if tc.contentType == "application/json" {
					require.Empty(t, recorder.Result().Header.Get("Content-Length"))
				}
			}
		})
	}
}

func TestResponseModelMappingAttemptIsolationAndErrors(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	before := c.Writer
	a := nativeAnthropicTestAccount()
	require.NotNil(t, installResponseModelMapping(c, a))
	require.Same(t, before, c.Writer, "unconfigured accounts have no output wrapper")
	a.Credentials[responseModelMappingKey] = map[string]any{"k3": "kimi-k3"}
	finish := installResponseModelMapping(c, a)
	finish() // Failed attempt with no output must leave failover possible.
	require.False(t, c.Writer.Written())
	require.Same(t, before, c.Writer)
	recorder := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	finish = installResponseModelMapping(c, a)
	c.JSON(http.StatusBadRequest, gin.H{"model": "k3", "error": "bad request"})
	finish()
	require.Equal(t, "k3", gjson.Get(recorder.Body.String(), "model").String())
}

func TestResponseModelMappingNativeAnthropicForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			account := nativeAnthropicTestAccount()
			account.Credentials[responseModelMappingKey] = map[string]any{"k3": "kimi-k3"}
			body := []byte(fmt.Sprintf(`{"model":"kimi-k3","max_tokens":32,"stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream))
			response := nativeAnthropicBufferedResponse()
			if stream {
				response = nativeAnthropicStreamResponse()
			}
			upstream := &httpUpstreamRecorder{resp: response}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Equal(t, "k3", observedUpstreamResponseModel(c))
			require.Equal(t, "kimi-k3", result.UpstreamModel)
			require.EqualValues(t, 93, result.Usage.InputTokens)
			require.Contains(t, recorder.Body.String(), `"model":"kimi-k3"`)
		})
	}
}

func TestResponseModelMappingRawChatForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			account := nativeAnthropicTestAccount()
			account.Credentials["api_protocol"] = "chat_completions"
			account.Credentials["base_url"] = "https://api.moonshot.cn/v1"
			account.Credentials[responseModelMappingKey] = map[string]any{"k3": "kimi-k3"}
			body := []byte(fmt.Sprintf(`{"model":"kimi-k3","stream":%t,"messages":[{"role":"user","content":"hi"}]}`, stream))
			payload := `{"id":"chat_1","model":"k3","choices":[{"index":0,"message":{"role":"assistant","content":"k3"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`
			contentType := "application/json"
			if stream {
				contentType = "text/event-stream"
				payload = "data: " + strings.ReplaceAll(payload, `"message":`, `"delta":`) + "\n\ndata: [DONE]\n\n"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(payload)),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.Equal(t, "k3", result.UpstreamResponseModel)
			require.Equal(t, "kimi-k3", gjson.GetBytes(upstream.lastBody, "model").String())
			require.EqualValues(t, 5, result.Usage.InputTokens)
			require.Contains(t, recorder.Body.String(), `"model":"kimi-k3"`)
			require.Contains(t, recorder.Body.String(), `"content":"k3"`)
		})
	}
}
