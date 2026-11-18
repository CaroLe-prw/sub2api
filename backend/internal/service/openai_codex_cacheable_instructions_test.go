package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexGPT62StableInstructionsStayInInput(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra", "openai/gpt-6.2-sol-high"} {
		for _, omit := range []bool{false, true} {
			t.Run(model+"/omit="+map[bool]string{true: "true", false: "false"}[omit], func(t *testing.T) {
				var body map[string]any
				require.NoError(t, json.Unmarshal([]byte(`{"instructions":"this turn only","input":[{"role":"system","content":[{"type":"input_text","text":"stable policy"}]},{"role":"user","content":"first"},{"role":"developer","content":"later update"},{"role":"user","content":"second"}]}`), &body))
				body["model"] = model
				result := applyCodexOAuthTransformWithOptions(body, codexOAuthTransformOptions{OmitPromotedSystemMessagesFromInput: omit})
				require.NoError(t, result.Error)
				require.Equal(t, "this turn only", body["instructions"])
				input := body["input"].([]any)
				require.Len(t, input, 4)
				require.Equal(t, "developer", input[0].(map[string]any)["role"])
				require.Equal(t, "stable policy", extractTextFromContent(input[0].(map[string]any)["content"]))
				require.Equal(t, "later update", input[2].(map[string]any)["content"])
				before, err := json.Marshal(body)
				require.NoError(t, err)
				applyCodexOAuthTransformWithOptions(body, codexOAuthTransformOptions{OmitPromotedSystemMessagesFromInput: omit})
				after, err := json.Marshal(body)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}

func TestCodexGPT62DefaultInstructionsAreCacheable(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, input := range []any{"hello", []any{map[string]any{"role": "user", "content": "hello"}}} {
			body := map[string]any{"model": model, "input": input}
			result := applyCodexOAuthTransform(body, false, false)
			require.NoError(t, result.Error)
			require.Equal(t, "", body["instructions"])
			items := body["input"].([]any)
			require.Len(t, items, 2)
			require.Equal(t, "developer", items[0].(map[string]any)["role"])
			require.Equal(t, defaultCodexSynthInstructions(model), extractTextFromContent(items[0].(map[string]any)["content"]))
			require.Equal(t, "hello", items[1].(map[string]any)["content"])
			applyCodexOAuthTransform(body, false, false)
			require.Len(t, body["input"], 2, "must not duplicate the synthesized prefix on retry")
		}
	}
}

func TestCodexGPT62DoesNotInjectOverExistingGuidanceOrContinuation(t *testing.T) {
	for _, raw := range []string{
		`{"model":"gpt-6.2-sol","input":[{"role":"developer","content":"stable policy"},{"role":"user","content":"hello"}]}`,
		`{"model":"gpt-6.2-sol","previous_response_id":"resp_1","input":[{"role":"user","content":"next turn"}]}`,
		`{"model":"gpt-6.2-sol","input":[{"type":"compaction","encrypted_content":"opaque"},{"role":"user","content":"next turn"}]}`,
	} {
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &body))
		before, err := json.Marshal(body["input"])
		require.NoError(t, err)
		result := applyCodexOAuthTransform(body, false, false)
		require.NoError(t, result.Error)
		require.Equal(t, "", body["instructions"])
		after, err := json.Marshal(body["input"])
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after))
	}
}

func TestCodexGPT62CompactDoesNotSynthesizeInstructions(t *testing.T) {
	body := map[string]any{"model": "gpt-6.2-sol", "input": []any{map[string]any{"role": "user", "content": "hello"}}}
	result := applyCodexOAuthTransform(body, true, true)
	require.NoError(t, result.Error)
	require.NotContains(t, body, "instructions")
	require.Len(t, body["input"], 1)
	// Explicit client compact instructions are not ours to silently discard.
	body["instructions"] = "client compact guidance"
	applyCodexOAuthTransform(body, true, true)
	require.Equal(t, "client compact guidance", body["instructions"])
}

func TestCodexGPT62ChatBridgeKeepsStableSystemPrefix(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		body := []byte(`{"model":"` + model + `","messages":[{"role":"system","content":"stable policy"},{"role":"user","content":"hello"}],"stream":false}`)
		upstream := forwardGPT62InstructionsBody(t, "/v1/chat/completions", body, "", nil)
		require.Equal(t, model, gjson.GetBytes(upstream, "model").String())
		require.Equal(t, "", gjson.GetBytes(upstream, "instructions").String())
		require.Equal(t, "developer", gjson.GetBytes(upstream, "input.0.role").String())
		require.Equal(t, int64(2), gjson.GetBytes(upstream, "input.#").Int())
		require.Equal(t, 1, strings.Count(string(upstream), "stable policy"))
	}
}

func TestCodexGPT62ForcedTemplateUsesStableDeveloperPrefix(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		body := map[string]any{
			"model":        model,
			"instructions": "this turn only",
			"input": []any{
				map[string]any{"role": "developer", "content": "client policy"},
				map[string]any{"role": "user", "content": "hello"},
				map[string]any{"role": "developer", "content": "later update"},
			},
		}
		data := forcedCodexInstructionsTemplateData{ExistingInstructions: "client policy", UpstreamModel: model}
		changed, err := applyForcedCodexInstructionsTemplate(body, "server policy\n\n{{ .ExistingInstructions }}", data)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "this turn only", body["instructions"])
		items := body["input"].([]any)
		require.Len(t, items, 3)
		require.Equal(t, "server policy\n\nclient policy", extractTextFromContent(items[0].(map[string]any)["content"]))
		require.Equal(t, "later update", items[2].(map[string]any)["content"])
		changed, err = applyForcedCodexInstructionsTemplate(body, "server policy\n\n{{ .ExistingInstructions }}", data)
		require.NoError(t, err)
		require.False(t, changed)
	}
}

// Exercise the real forwarding entry points; no live provider is contacted.
func forwardGPT62InstructionsBody(t *testing.T, path string, body []byte, template string, account *Account) []byte {
	t.Helper()
	upstream := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(
		http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"stop before response parsing"}}`,
	)}
	svc := newOpenAIRejectedFieldTestService(upstream)
	svc.cfg.Gateway.ForcedCodexInstructionsTemplate = template
	c := newOpenAIRejectedFieldTestContext(body)
	c.Request.URL.Path = path
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	if account == nil {
		account = newOpenAIOAuthNamespaceTestAccount()
	}
	var err error
	switch path {
	case "/v1/chat/completions":
		_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", gjson.GetBytes(body, "model").String())
	case "/v1/messages":
		_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", gjson.GetBytes(body, "model").String())
	default:
		_, err = svc.Forward(context.Background(), c, account, body)
	}
	require.Error(t, err)
	require.NotEmpty(t, upstream.lastBody, "must reach the mocked upstream, rather than fail local validation")
	return upstream.lastBody
}

func TestCodexGPT62ResponsesForwardUsesMappedModel(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, input := range []string{
			`[{"role":"user","content":"hello"}]`,
			`[{"role":"system","content":"stable policy"},{"role":"user","content":"hello"}]`,
		} {
			account := newOpenAIOAuthNamespaceTestAccount()
			account.Credentials["model_mapping"] = map[string]any{"public-alias": model}
			body := []byte(`{"model":"public-alias","stream":false,"input":` + input + `}`)
			upstream := forwardGPT62InstructionsBody(t, "/v1/responses", body, "", account)
			require.Equal(t, model, gjson.GetBytes(upstream, "model").String())
			require.Equal(t, "", gjson.GetBytes(upstream, "instructions").String())
			require.Equal(t, "developer", gjson.GetBytes(upstream, "input.0.role").String())
			require.Equal(t, int64(2), gjson.GetBytes(upstream, "input.#").Int())
		}
	}
}

func TestCodexGPT62MessagesTemplateDoesNotDuplicateClientPolicy(t *testing.T) {
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra"} {
		body := []byte(`{"model":"` + model + `","max_tokens":16,"system":"client policy","messages":[{"role":"user","content":"hello"}],"stream":false}`)
		for _, template := range []string{"", "server policy\n\n{{ .ExistingInstructions }}", "server policy"} {
			upstream := forwardGPT62InstructionsBody(t, "/v1/messages", body, template, nil)
			require.Equal(t, model, gjson.GetBytes(upstream, "model").String())
			require.Equal(t, "", gjson.GetBytes(upstream, "instructions").String())
			require.Equal(t, "developer", gjson.GetBytes(upstream, "input.0.role").String())
			require.Equal(t, 1, strings.Count(string(upstream), "client policy"))
			if template != "" {
				require.Contains(t, gjson.GetBytes(upstream, "input.0.content").String(), "server policy")
			}
		}
	}
}

func TestCodexGPT62GatePreservesLegacyInstructionBehavior(t *testing.T) {
	for _, model := range []string{"gpt-5.4", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-unknown"} {
		body := map[string]any{"model": model, "instructions": "turn", "input": []any{
			map[string]any{"role": "system", "content": "stable policy"},
			map[string]any{"role": "user", "content": "hello"},
		}}
		result := applyCodexOAuthTransformWithOptions(body, codexOAuthTransformOptions{OmitPromotedSystemMessagesFromInput: true})
		require.NoError(t, result.Error)
		require.Equal(t, "stable policy\n\nturn", body["instructions"], model)
		require.Len(t, body["input"], 1, model)
	}
}

func TestCodexGPT62LaterDeveloperUpdateKeepsSynthesizedPrefix(t *testing.T) {
	first := map[string]any{"model": "gpt-6.2-sol", "input": []any{
		map[string]any{"role": "user", "content": "hello"},
	}}
	applyCodexOAuthTransform(first, false, false)
	next := map[string]any{"model": "gpt-6.2-sol", "input": []any{
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "assistant", "content": "hi"},
		map[string]any{"role": "developer", "content": "later update"},
		map[string]any{"role": "user", "content": "continue"},
	}}
	applyCodexOAuthTransform(next, false, false)
	items := next["input"].([]any)
	require.Len(t, items, 5)
	require.Equal(t, first["input"].([]any)[:2], items[:2], "new developer updates must not remove the already-synthesized prefix")
	require.Equal(t, "later update", items[3].(map[string]any)["content"])
}

func TestCodexGPT62MixedTemplateContentIsNotDiscarded(t *testing.T) {
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"model":"gpt-6.2-sol","input":[{"role":"developer","content":[{"type":"input_text","text":"client policy"},{"type":"input_image","image_url":"https://example.com/image.png"}]},{"role":"user","content":"hello"}]}`), &body))
	original, err := json.Marshal(body["input"])
	require.NoError(t, err)
	changed, err := applyForcedCodexInstructionsTemplate(body, "server\n{{ .ExistingInstructions }}", forcedCodexInstructionsTemplateData{ExistingInstructions: "client policy", UpstreamModel: "gpt-6.2-sol"})
	require.NoError(t, err)
	require.True(t, changed)
	items := body["input"].([]any)
	require.Len(t, items, 3)
	retained, err := json.Marshal(items[1:])
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(retained))
}
