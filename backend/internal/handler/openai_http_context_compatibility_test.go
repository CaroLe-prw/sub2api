package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestOpenAIResponses_FunctionCallOutputMissingCallIDErrorBody 覆盖 HTTP 预校验的
// 错误体格式与可操作提示：绝不能透传上游 500，也不能给出“复用
// previous_response_id”这种无效指引。
func TestOpenAIResponses_FunctionCallOutputMissingCallIDErrorBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.1","stream":false,"input":[{"type":"function_call_output","output":"{}"}]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.False(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.NewNop()))
	require.Equal(t, http.StatusBadRequest, w.Code)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	errorObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "invalid_request_error", errorObj["type"])
	message, _ := errorObj["message"].(string)
	require.Contains(t, message, "function_call_output requires call_id on HTTP requests")
	require.Contains(t, message, "untrusted_as_tool_output")
	require.NotContains(t, message, "reuse previous_response_id")
}

// TestOpenAIResponses_FunctionCallOutputWithCallIDPassesValidation 覆盖带 call_id
// 的正常工具续链不受影响。
func TestOpenAIResponses_FunctionCallOutputWithCallIDPassesValidation(t *testing.T) {
	body := []byte(`{"model":"gpt-5.1","stream":false,"input":[{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"result"}]}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.NewNop()))
	require.False(t, c.Writer.Written())
}

// TestOpenAIResponses_HTTPContinuationWithPageContextKeepsPreviousResponseID 覆盖
// HTTP + previous_response_id + 页面上下文：续传字段不被删除，归一化后校验通过。
func TestOpenAIResponses_HTTPContinuationWithPageContextKeepsPreviousResponseID(t *testing.T) {
	body := pageContextRequest(t, pageContextItem("codex_apps_open_page", `{"page_id":null}`))
	body = []byte(strings.Replace(string(body), `{"input":`, `{"previous_response_id":"resp_123456","input":`, 1))

	got, changed := normalizeCodexUntrustedContext(body)
	require.True(t, changed)
	require.Equal(t, "resp_123456", gjson.GetBytes(got, "previous_response_id").String())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	require.True(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, got, zap.NewNop()))
	require.False(t, c.Writer.Written())

	// 即使客户端直接带着 previous_response_id 发未归一化的页面上下文，HTTP 预校验
	// 也允许透传（上游按 response.id 解析历史），不会本地 400。
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	require.True(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.NewNop()))
}

// TestOpenAIResponses_HTTPContinuationErrorIsActionable 验证 OAuth/SetupToken
// 账号不允许 HTTP previous_response_id 时的错误可操作、且不是 500。
func TestOpenAIResponses_HTTPContinuationErrorIsActionable(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	(&OpenAIGatewayHandler{}).handleFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode: http.StatusBadRequest,
		Stage:      service.GatewayFailureStageInference,
		Scope:      service.GatewayFailureScopeRequest,
		Reason:     service.OpenAIHTTPContinuationUnsupportedReason,
	}, false)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	errorObj, ok := parsed["error"].(map[string]any)
	require.True(t, ok)
	message, _ := errorObj["message"].(string)
	require.Contains(t, message, "previous_response_id requires an OpenAI API-key account on HTTP requests")
	require.Contains(t, message, "WebSocket v2")
	require.Contains(t, message, "resend the full input without previous_response_id")
}

// Continuation can carry unknown call-less context; preserve it and warn without
// exposing the input. Scan the full input even if paired history appears first.
func TestOpenAIResponses_UnpairedContextPassthroughWarning(t *testing.T) {
	const unpaired = `{"type":"function_call_output","namespace":"agent","name":"unknown_context","output":"private context"}`
	const call = `{"type":"function_call","call_id":"call_1","name":"private_tool","arguments":"{}"}`
	const paired = `{"type":"function_call_output","call_id":"call_1","output":"private result"}`
	cases := []struct {
		name       string
		body       string
		reason     string
		inputIndex int
	}{
		{"previous response", `{"previous_response_id":"resp_1","input":[` + unpaired + `]}`, "function_call_output_missing_call_id_with_previous_response_id", 0},
		{"history before context", `{"input":[` + call + `,` + paired + `,` + unpaired + `]}`, "function_call_output_missing_call_id_with_tool_call_context", 2},
		{"context before history", `{"input":[` + unpaired + `,` + call + `,` + paired + `]}`, "function_call_output_missing_call_id_with_tool_call_context", 0},
		{"previous response with history", `{"previous_response_id":"resp_1","input":[` + call + `,` + paired + `,` + unpaired + `]}`, "function_call_output_missing_call_id_with_previous_response_id", 2},
		{"paired history", `{"input":[` + call + `,` + paired + `]}`, "", -1},
		{"paired previous response", `{"previous_response_id":"resp_1","input":[` + paired + `]}`, "", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			body := []byte(tc.body)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			require.True(t, (&OpenAIGatewayHandler{}).validateFunctionCallOutputRequest(c, body, zap.New(core)))
			require.False(t, c.Writer.Written())
			require.Equal(t, tc.body, string(body))
			if tc.reason == "" {
				require.Zero(t, logs.Len())
				return
			}
			require.Equal(t, 1, logs.Len())
			entry := logs.All()[0]
			require.Equal(t, "openai.request_validation_passthrough", entry.Message)
			require.Equal(t, tc.reason, entry.ContextMap()["reason"])
			require.EqualValues(t, tc.inputIndex, entry.ContextMap()["input_index"])
			logged, err := json.Marshal(entry.ContextMap())
			require.NoError(t, err)
			for _, secret := range []string{"private context", "private_tool", "private result"} {
				require.NotContains(t, entry.Message+string(logged), secret)
			}
		})
	}
}
