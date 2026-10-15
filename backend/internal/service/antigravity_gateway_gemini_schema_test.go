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
			tools, ok := payload["tools"].([]any)
			require.True(t, ok)
			require.Len(t, tools, 1)
			tool, ok := tools[0].(map[string]any)
			require.True(t, ok)
			declarations, ok := tool[field].([]any)
			require.True(t, ok)
			require.Len(t, declarations, 1)
			declaration, ok := declarations[0].(map[string]any)
			require.True(t, ok)
			require.NotContains(t, declaration, "strict")
			parameters, ok := declaration["parameters"].(map[string]any)
			require.True(t, ok)
			properties, ok := parameters["properties"].(map[string]any)
			require.True(t, ok)
			require.Contains(t, properties, "strict", "schema properties named strict must be preserved")
		})
	}
}
