//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardResponses_ChatFallbackNamespacedCustom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		name := "buffered"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"model": "gpt-5.6-luna", "stream": stream,
				"input": []any{
					map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{
						map[string]any{"type": "namespace", "name": "functions", "tools": []any{
							map[string]any{"type": "custom", "name": "exec", "description": "Run JavaScript"},
							map[string]any{"type": "function", "name": "wait", "parameters": map[string]any{"type": "object"}},
						}},
					}},
					map[string]any{"role": "user", "content": "run pwd"},
				},
			})
			require.NoError(t, err)
			upstreamBody := `{"id":"chatcmpl_probe","model":"gpt-5.6-luna","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_probe","type":"function","function":{"name":"functions__exec","arguments":"{\"input\":\"text(await tools.exec_command({cmd: 'pwd'}));\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`
			contentType := "application/json"
			if stream {
				contentType = "text/event-stream"
				upstreamBody = "data: " + `{"id":"chatcmpl_probe","model":"gpt-5.6-luna","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_probe","type":"function","function":{"name":"functions__exec","arguments":"{\"input\":\"text(await tools.exec_command({cmd: 'pwd'}));\"}"}}]},"finish_reason":null}]}` + "\n\n" +
					"data: " + `{"id":"chatcmpl_probe","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}` + "\n\ndata: [DONE]\n\n"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			result, err := svc.Forward(context.Background(), c, forceChatResponsesFallbackAccount(), body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.Equal(t, int64(2), gjson.GetBytes(upstream.lastBody, "tools.#").Int())
			require.Equal(t, "functions__exec", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
			require.Equal(t, "string", gjson.GetBytes(upstream.lastBody, "tools.0.function.parameters.properties.input.type").String())
			require.Equal(t, "functions__wait", gjson.GetBytes(upstream.lastBody, "tools.1.function.name").String())
			require.Equal(t, 20, result.Usage.InputTokens)
			require.Equal(t, 10, result.Usage.OutputTokens)
			response := rec.Body.String()
			if stream {
				var completed string
				for _, line := range strings.Split(response, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					event := strings.TrimPrefix(line, "data: ")
					if gjson.Get(event, "type").String() == "response.output_item.done" {
						require.Equal(t, "custom_tool_call", gjson.Get(event, "item.type").String())
						require.Equal(t, "functions", gjson.Get(event, "item.namespace").String())
						require.Equal(t, "exec", gjson.Get(event, "item.name").String())
					}
					if gjson.Get(event, "type").String() == "response.completed" {
						completed = gjson.Get(event, "response").Raw
					}
				}
				require.NotEmpty(t, completed)
				response = completed
			}
			require.Equal(t, "custom_tool_call", gjson.Get(response, "output.0.type").String())
			require.Equal(t, "functions", gjson.Get(response, "output.0.namespace").String())
			require.Equal(t, "exec", gjson.Get(response, "output.0.name").String())
			require.Equal(t, "call_probe", gjson.Get(response, "output.0.call_id").String())
			require.Equal(t, "text(await tools.exec_command({cmd: 'pwd'}));", gjson.Get(response, "output.0.input").String())
		})
	}
}
