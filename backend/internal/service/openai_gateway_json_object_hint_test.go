//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// jsonObjectHintBody 复刻线上真实 case（ops 错误 #1003009）：客户端把
// "Return json only ..." 只写进 instructions，input 里只有一条不含 json 的 user 文本。
const jsonObjectHintBody = `{
	"input": [
		{
			"content": [
				{
					"text": "Target type: document\nAnalysis scope: scheduler_auto\nTitle: What is MegaSpeed?",
					"type": "input_text"
				}
			],
			"role": "user",
			"type": "message"
		}
	],
	"instructions": "You are an intelligence analyst. Return json only with keys: title, summary, why_important.",
	"max_output_tokens": 800,
	"model": "gpt-5.6-terra",
	"store": false,
	"stream": false,
	"text": {"format": {"type": "json_object"}}
}`

func decodeJSONObjectHintBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &body))
	return body
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_InjectsIntoLastUserMessage(t *testing.T) {
	body := decodeJSONObjectHintBody(t, jsonObjectHintBody)

	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(body)
	require.True(t, injected)
	assert.Equal(t, "user_message", site)

	input, ok := body["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)

	item, ok := input[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "user", item["role"])

	// 原始 input_text 片段保留，提示词作为新片段追加。
	parts, ok := item["content"].([]any)
	require.True(t, ok)
	require.Len(t, parts, 2)
	assert.Equal(t, "input_text", parts[0].(map[string]any)["type"])
	assert.Contains(t, parts[0].(map[string]any)["text"], "What is MegaSpeed?")
	assert.Equal(t, openAIResponsesJSONObjectPromptHint, parts[1].(map[string]any)["text"])

	// instructions 与 text.format 不被改写。
	assert.Contains(t, body["instructions"], "Return json only")
	assert.Equal(t, "json_object", gjson.GetBytes(mustMarshal(t, body), "text.format.type").String())

	// 补齐后 input 里出现了 "json"，满足上游校验。
	assert.True(t, openAIResponsesInputMentionsJSON(body["input"]))
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_SkipsWhenInputMentionsJSON(t *testing.T) {
	body := decodeJSONObjectHintBody(t, `{
		"input": [{"role":"user","content":[{"type":"input_text","text":"Summarize and reply as JSON."}]}],
		"text": {"format": {"type": "json_object"}}
	}`)

	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(body)
	assert.False(t, injected)
	assert.Empty(t, site)
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_SkipsOtherFormats(t *testing.T) {
	for _, format := range []string{"json_schema", "text", ""} {
		body := decodeJSONObjectHintBody(t, `{
			"input": [{"role":"user","content":"plain text prompt"}],
			"text": {"format": {"type": "`+format+`"}}
		}`)
		injected, _ := ensureOpenAIResponsesJSONObjectPromptHint(body)
		assert.Falsef(t, injected, "format=%q should not be touched", format)
	}
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_AppendsToUserContentParts(t *testing.T) {
	body := decodeJSONObjectHintBody(t, `{
		"input": [
			{"role":"user","content":"first turn"},
			{"role":"assistant","content":"ok"},
			{"role":"user","content":[{"type":"input_text","text":"second turn"}]}
		],
		"text": {"format": {"type": "json_object"}}
	}`)

	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(body)
	require.True(t, injected)
	assert.Equal(t, "user_message", site)

	input := body["input"].([]any)
	last := input[len(input)-1].(map[string]any)
	parts := last["content"].([]any)
	require.Len(t, parts, 2)
	assert.Equal(t, "second turn", parts[0].(map[string]any)["text"])

	// 之前的轮次保持不变。
	first := input[0].(map[string]any)
	assert.Equal(t, "first turn", first["content"])
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_AppendsDeveloperItemWithoutUserMessage(t *testing.T) {
	body := decodeJSONObjectHintBody(t, `{
		"input": [{"type":"function_call_output","call_id":"call_1","output":"{\"value\":1}"}],
		"text": {"format": {"type": "json_object"}}
	}`)

	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(body)
	require.True(t, injected)
	assert.Equal(t, "developer_item", site)

	input := body["input"].([]any)
	require.Len(t, input, 2)
	added := input[1].(map[string]any)
	assert.Equal(t, "developer", added["role"])
	assert.True(t, openAIResponsesInputMentionsJSON(added))
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_HandlesStringInput(t *testing.T) {
	body := decodeJSONObjectHintBody(t, `{
		"input": "Summarize the article.",
		"text": {"format": {"type": " JSON_OBJECT "}}
	}`)

	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(body)
	require.True(t, injected)
	assert.Equal(t, "input_string", site)

	text, ok := body["input"].(string)
	require.True(t, ok)
	assert.Contains(t, text, "Summarize the article.")
	assert.Contains(t, text, openAIResponsesJSONObjectPromptHint)
}

func TestEnsureOpenAIResponsesJSONObjectPromptHint_IgnoresOpaqueBinaryFields(t *testing.T) {
	// image_url 里偶然出现的 "json" 不应被当作提示词命中。
	body := decodeJSONObjectHintBody(t, `{
		"input": [{"role":"user","content":[
			{"type":"input_image","image_url":"data:image/png;base64,jsonQUJD"},
			{"type":"input_text","text":"describe this image"}
		]}],
		"text": {"format": {"type": "json_object"}}
	}`)

	injected, _ := ensureOpenAIResponsesJSONObjectPromptHint(body)
	assert.True(t, injected, "base64 payload must not satisfy the json word requirement")
}

func TestApplyOpenAIResponsesJSONObjectPromptHint_ByteBodyRoundTrip(t *testing.T) {
	patched, site, err := applyOpenAIResponsesJSONObjectPromptHint([]byte(jsonObjectHintBody))
	require.NoError(t, err)
	require.Equal(t, "user_message", site)

	assert.Equal(t, "json_object", gjson.GetBytes(patched, "text.format.type").String())
	assert.Equal(t, "gpt-5.6-terra", gjson.GetBytes(patched, "model").String())
	assert.Contains(t, gjson.GetBytes(patched, "instructions").String(), "Return json only")
	assert.True(t, openAIResponsesInputMentionsJSON(gjson.GetBytes(patched, "input").Value()))
}

func TestApplyOpenAIResponsesJSONObjectPromptHint_NoopWhenUnneeded(t *testing.T) {
	raw := []byte(`{"input":[{"role":"user","content":"Return JSON please"}],"text":{"format":{"type":"json_object"}}}`)
	patched, site, err := applyOpenAIResponsesJSONObjectPromptHint(raw)
	require.NoError(t, err)
	assert.Empty(t, site)
	assert.Equal(t, raw, patched)

	raw = []byte(`{"input":[{"role":"user","content":"hi"}],"text":{"format":{"type":"json_schema"}}}`)
	patched, site, err = applyOpenAIResponsesJSONObjectPromptHint(raw)
	require.NoError(t, err)
	assert.Empty(t, site)
	assert.Equal(t, raw, patched)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// TestForward_ResponsesJSONObjectHintForInstructionsOnly 复刻 ops 错误 #1003009：
// Codex OAuth 账号、input 里只有不含 json 的 user 文本、json 约束只在 instructions。
// 修复后出站 body 的 input 必须自带 "json"，否则上游固定 400。
func TestForward_ResponsesJSONObjectHintForInstructionsOnly(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-terra",
		"instructions":"You are an intelligence analyst. Return json only with keys: title, summary.",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Target type: document\nTitle: What is MegaSpeed?"}]}],
		"max_output_tokens":800,
		"store":false,
		"stream":false,
		"text":{"format":{"type":"json_object"}}
	}`)
	upstream := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(
		http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"stop before response parsing"}}`,
	)}
	svc := newOpenAIRejectedFieldTestService(upstream)
	c, _ := newOpenAIResponsesJSONHintTestContext(body)

	_, err := svc.Forward(context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body)
	require.Error(t, err)
	require.Len(t, upstream.bodies, 1)

	sent := upstream.bodies[0]
	require.Equal(t, "json_object", gjson.GetBytes(sent, "text.format.type").String())
	require.Equal(t, "You are an intelligence analyst. Return json only with keys: title, summary.", gjson.GetBytes(sent, "instructions").String())
	require.Contains(t, strings.ToLower(gjson.GetBytes(sent, "input").Raw), "json",
		"upstream validator scans input only; the hint must land there")
	require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(sent, "model").String())
}

// TestForward_ResponsesJSONObjectHintSkippedWhenInputAlreadyMentionsJSON 保证已经
// 满足校验的请求不被改写。
func TestForward_ResponsesJSONObjectHintSkippedWhenInputAlreadyMentionsJSON(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-terra",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply as JSON."}]}],
		"stream":false,
		"text":{"format":{"type":"json_object"}}
	}`)
	upstream := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(
		http.StatusBadRequest,
		`{"error":{"type":"invalid_request_error","message":"stop before response parsing"}}`,
	)}
	svc := newOpenAIRejectedFieldTestService(upstream)
	c, _ := newOpenAIResponsesJSONHintTestContext(body)

	_, err := svc.Forward(context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body)
	require.Error(t, err)
	require.Len(t, upstream.bodies, 1)

	sent := upstream.bodies[0]
	require.NotContains(t, string(sent), openAIResponsesJSONObjectPromptHint)
	require.Equal(t, "Reply as JSON.", gjson.GetBytes(sent, "input.0.content.0.text").String())
}

// TestForwardAsChatCompletions_JSONObjectHintWithoutAnyJSONMessage 覆盖 Chat
// Completions 入口：response_format=json_object 且消息里完全没有 json 时，转换后的
// Responses input 也要被补齐。
func TestForwardAsChatCompletions_JSONObjectHintWithoutAnyJSONMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"symbol data"}],"response_format":{"type":"json_object"},"stream":false}`)

	upstreamBody := forwardOAuthChatCompletionsForUpstreamBody(t, body)

	require.Equal(t, "json_object", gjson.GetBytes(upstreamBody, "text.format.type").String())
	require.Contains(t, strings.ToLower(gjson.GetBytes(upstreamBody, "input").Raw), "json")
}

func newOpenAIResponsesJSONHintTestContext(body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}
