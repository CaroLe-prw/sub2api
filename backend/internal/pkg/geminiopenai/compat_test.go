package geminiopenai

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestConversationAndToolResults(t *testing.T) {
	body := []byte(`{
	 "systemInstruction":{"parts":[{"text":"Be precise"}]},
	 "contents":[
	  {"role":"user","parts":[{"text":"Compare these"},{"inlineData":{"mimeType":"image/png","data":"YWJj"}}]},
	  {"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"city":"A"},"id":"call_a"}},{"functionCall":{"name":"lookup","args":{"city":"B"},"id":"call_b"}}]},
	  {"role":"user","parts":[{"functionResponse":{"name":"lookup","id":"call_b","response":{"temp":20}}},{"functionResponse":{"name":"lookup","id":"call_a","response":{"temp":10}}},{"text":"Summarize"}]}
	 ],
	 "tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"type":{"type":"STRING"},"city":{"type":"STRING","nullable":true}},"required":["city"]}}]}],
	 "toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}},
	 "generationConfig":{"maxOutputTokens":120,"temperature":0.3,"topP":0.9,"thinkingConfig":{"thinkingLevel":"HIGH"},"responseMimeType":"application/json","responseSchema":{"type":"OBJECT","properties":{"answer":{"type":"STRING"}}}}
	}`)
	converted, err := Request(body, "vendor/gemini", true)
	require.NoError(t, err)
	var out object
	require.NoError(t, json.Unmarshal(converted, &out))
	require.Equal(t, "vendor/gemini", out["model"])
	require.Equal(t, true, obj(out["stream_options"])["include_usage"])
	messages := arr(out["messages"])
	require.Len(t, messages, 6)
	require.Equal(t, "Be precise", obj(messages[0])["content"])
	image := obj(arr(obj(messages[1])["content"])[1])
	require.Equal(t, "data:image/png;base64,YWJj", obj(image["image_url"])["url"])
	require.Equal(t, "call_b", obj(messages[3])["tool_call_id"])
	require.Equal(t, "call_a", obj(messages[4])["tool_call_id"])
	require.Equal(t, "user", obj(messages[5])["role"])
	fn := obj(obj(arr(out["tools"])[0])["function"])
	properties := obj(obj(fn["parameters"])["properties"])
	require.Equal(t, "string", obj(properties["type"])["type"])
	require.Equal(t, []any{"string", "null"}, obj(properties["city"])["type"])
	require.Equal(t, "high", out["reasoning_effort"])
	require.Equal(t, "json_schema", obj(out["response_format"])["type"])
}

func TestRequestSynthesizesToolIDs(t *testing.T) {
	raw, err := Request([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{"ok":true}}}]}]}`), "gemini", false)
	require.NoError(t, err)
	var out object
	require.NoError(t, json.Unmarshal(raw, &out))
	messages := arr(out["messages"])
	call := obj(arr(obj(messages[0])["tool_calls"])[0])
	require.NotEmpty(t, call["id"])
	require.Equal(t, call["id"], obj(messages[1])["tool_call_id"])
}

func TestRequestUnsupportedFeatures(t *testing.T) {
	for _, body := range []string{
		`{"contents":[]}`,
		`{"contents":[{"parts":[{"functionResponse":{"name":"missing"}}]}]}`,
		`{"contents":[{"parts":[{"text":"hi"}]}],"tools":[{"googleSearch":{}}]}`,
		`{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"imageConfig":{}}}`,
		`{"contents":[{"parts":[{"text":"hi"}]}],"safetySettings":[{"category":"HARM_CATEGORY_DANGEROUS_CONTENT","threshold":"BLOCK_NONE"}]}`,
		`{"contents":[{"parts":[{"fileData":{"mimeType":"application/pdf","fileUri":"gs://bucket/file"}}]}]}`,
	} {
		t.Run(body, func(t *testing.T) { _, err := Request([]byte(body), "gemini", false); require.Error(t, err) })
	}
}

func TestNonStreamingResponseAndUsage(t *testing.T) {
	body := Body(io.NopCloser(strings.NewReader(`{"id":"req","model":"vendor/gemini","choices":[{"index":0,"message":{"content":"Hello","reasoning_content":"Think","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"上海\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":10}}}`)), false)
	defer func() { require.NoError(t, body.Close()) }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	var out object
	require.NoError(t, json.Unmarshal(raw, &out))
	candidate := obj(arr(out["candidates"])[0])
	parts := arr(obj(candidate["content"])["parts"])
	require.Equal(t, true, obj(parts[0])["thought"])
	require.Equal(t, "Hello", obj(parts[1])["text"])
	call := obj(obj(parts[2])["functionCall"])
	require.Equal(t, "call_1", call["id"])
	require.Equal(t, "上海", obj(call["args"])["city"])
	require.Equal(t, "STOP", candidate["finishReason"])
	require.Equal(t, float64(20), obj(out["usageMetadata"])["candidatesTokenCount"])
	require.Equal(t, float64(10), obj(out["usageMetadata"])["thoughtsTokenCount"])
}

func TestStreamToolFragmentsAndUsage(t *testing.T) {
	chunks := []string{
		`{"model":"gemini","choices":[{"index":0,"delta":{"content":"Checking "}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"second","arguments":"{\"b\":"}},{"index":0,"id":"a","function":{"name":"first","arguments":"{\"a\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}},{"index":1,"function":{"arguments":"2}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20}}`,
		`[DONE]`,
	}
	stream := ": keepalive\r\n\r\n"
	for _, chunk := range chunks {
		stream += "data: " + chunk + "\r\n\r\n"
	}
	body := Body(io.NopCloser(strings.NewReader(stream)), true)
	defer func() { require.NoError(t, body.Close()) }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	events := strings.Split(strings.TrimSpace(string(raw)), "\n\n")
	require.Len(t, events, 3)
	require.Contains(t, events[0], "Checking ")
	var out object
	require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(events[1], "data: ")), &out))
	parts := arr(obj(obj(arr(out["candidates"])[0])["content"])["parts"])
	require.Equal(t, "first", obj(obj(parts[0])["functionCall"])["name"])
	require.Equal(t, "second", obj(obj(parts[1])["functionCall"])["name"])
	require.Contains(t, events[2], `"promptTokenCount":12`)
	require.NotContains(t, string(raw), "[DONE]")
}

func TestStreamRejectsBrokenUpstream(t *testing.T) {
	for _, raw := range []string{
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: [DONE]\n\n",
		"data: {bad}\n\n",
		"data: {\"error\":{\"message\":\"failed\"}}\n\n",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"f\",\"arguments\":\"{bad\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
	} {
		b := Body(io.NopCloser(strings.NewReader(raw)), true)
		_, err := io.ReadAll(b)
		require.Error(t, err, raw)
		require.NoError(t, b.Close())
	}
}

func TestModels(t *testing.T) {
	raw, err := Models([]byte(`{"data":[{"id":"gemini-3"}]}`), false)
	require.NoError(t, err)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-3","displayName":"gemini-3","supportedGenerationMethods":["generateContent","streamGenerateContent","countTokens"]}]}`, string(raw))
	raw, err = Models([]byte(`{"id":"vendor/gemini"}`), true)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"name":"models/vendor/gemini"`)
}
