package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The Codex endpoint rejects this top-level field with HTTP 400. Fields with
// the same name inside a user tool schema must not be removed recursively.
const transformerMetadataRequest = `{"model":"gpt-6.2-sol","instructions":"test","stream":true,"store":false,"transformer_metadata":{"source":"client"},"input":[{"type":"message","role":"user","content":"keep this message"}],"tools":[{"type":"function","name":"capture","parameters":{"type":"object","properties":{"transformer_metadata":{"type":"string"}},"required":["transformer_metadata"],"additionalProperties":false}}]}`

func requireTransformerMetadataRemoved(t *testing.T, body []byte) {
	t.Helper()
	require.False(t, gjson.GetBytes(body, "transformer_metadata").Exists(), "unsupported top-level field must not reach Codex")
	require.Equal(t, "keep this message", gjson.GetBytes(body, "input.0.content").String())
	require.Equal(t, "string", gjson.GetBytes(body, "tools.0.parameters.properties.transformer_metadata.type").String())
	require.Equal(t, "transformer_metadata", gjson.GetBytes(body, "tools.0.parameters.required.0").String())
}

func TestApplyCodexOAuthTransform_StripsTransformerMetadata(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, codexCLI := range []bool{false, true} {
			t.Run(fmt.Sprintf("compact=%t/codexCLI=%t", compact, codexCLI), func(t *testing.T) {
				var body map[string]any
				require.NoError(t, json.Unmarshal([]byte(transformerMetadataRequest), &body))
				result := applyCodexOAuthTransform(body, codexCLI, compact)
				require.True(t, result.Modified)
				normalized, err := json.Marshal(body)
				require.NoError(t, err)
				requireTransformerMetadataRemoved(t, normalized)
			})
		}
	}
}

func TestNormalizeOpenAIPassthroughOAuthBody_StripsTransformerMetadata(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprintf("compact=%t", compact), func(t *testing.T) {
			body, changed, err := normalizeOpenAIPassthroughOAuthBody([]byte(transformerMetadataRequest), compact)
			require.NoError(t, err)
			require.True(t, changed)
			requireTransformerMetadataRemoved(t, body)
		})
	}
	for _, value := range []string{"null", `"opaque"`, "[]", "{}"} {
		t.Run(value, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6.2-sol","input":"keep","transformer_metadata":` + value + `}`)
			normalized, changed, err := normalizeOpenAIPassthroughOAuthBody(body, false)
			require.NoError(t, err)
			require.True(t, changed)
			require.False(t, gjson.GetBytes(normalized, "transformer_metadata").Exists())
		})
	}
}

func TestNormalizeOpenAIResponsesWebSocketCompatibilityBody_TransformerMetadataScope(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			body, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody([]byte(transformerMetadataRequest), &Account{Platform: PlatformOpenAI, Type: accountType}, false)
			require.NoError(t, err)
			if accountType == AccountTypeOAuth {
				requireTransformerMetadataRemoved(t, body)
			} else {
				require.Equal(t, "client", gjson.GetBytes(body, "transformer_metadata.source").String(), "do not change public API-key behavior based on a Codex-only error")
			}
		})
	}
}

func TestOpenAIGatewayService_OAuthStripsTransformerMetadataBeforeFirstAttempt(t *testing.T) {
	body := []byte(transformerMetadataRequest)
	response := newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n")
	response.Header.Set("Content-Type", "text/event-stream")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{response}}
	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIOAuthNamespaceTestAccount(), body,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	requireTransformerMetadataRemoved(t, upstream.bodies[0])
}
