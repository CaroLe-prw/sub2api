package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanGeminiRequestStripsOpenAIFunctionStrict(t *testing.T) {
	for _, field := range []string{"functionDeclarations", "function_declarations"} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"tools":[{"` + field + `":[{"name":"shell","strict":true,"parameters":{"type":"object","properties":{"strict":{"type":"boolean"}}}}]}]}`)

			cleaned, err := cleanGeminiRequest(body)
			require.NoError(t, err)

			var payload map[string]any
			require.NoError(t, json.Unmarshal(cleaned, &payload))
			tool := payload["tools"].([]any)[0].(map[string]any)
			declaration := tool[field].([]any)[0].(map[string]any)
			require.NotContains(t, declaration, "strict")
			properties := declaration["parameters"].(map[string]any)["properties"].(map[string]any)
			require.Contains(t, properties, "strict", "schema properties named strict must be preserved")
		})
	}
}
