package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestFunctionCallOutputValidationIdentifiesMissingCallIDWithoutPayload(t *testing.T) {
	for _, field := range []string{"", `,"call_id":""`, `,"call_id":" \t "`, `,"call_id":null`} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"model":"test","input":[{"role":"user","content":"private prompt"},{"type":"function_call_output","name":"private_tool","output":"private result"` + field + `}]}`)
			core, logs := observer.New(zap.WarnLevel)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			h := &OpenAIGatewayHandler{}
			require.False(t, h.validateFunctionCallOutputRequest(c, body, zap.New(core)))
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "function_call_output requires call_id")
			require.Contains(t, w.Body.String(), "input[1].call_id")
			require.NotContains(t, w.Body.String(), "only supported on Responses WebSocket v2")
			entries := logs.FilterMessage("openai.request_validation_failed").All()
			require.Len(t, entries, 1)
			fields := entries[0].ContextMap()
			require.Equal(t, "function_call_output_missing_call_id", fields["reason"])
			require.EqualValues(t, 1, fields["input_index"])
			encoded, err := json.Marshal(fields)
			require.NoError(t, err)
			for _, secret := range []string{"private prompt", "private_tool", "private result"} {
				require.NotContains(t, string(encoded), secret)
				require.NotContains(t, w.Body.String(), secret)
			}
		})
	}
}

func TestFunctionCallOutputValidationPreservesContinuationRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		allowed bool
	}{
		{"no tool output", `{"input":[{"role":"user","content":"hello"}]}`, true},
		{"orphan output", `{"input":[{"type":"function_call_output","call_id":"call_a","output":"result"}]}`, false},
		{"matching reference", `{"input":[{"type":"function_call_output","call_id":"call_a","output":"result"},{"type":"item_reference","id":"call_a"}]}`, true},
		{"mismatching reference", `{"input":[{"type":"function_call_output","call_id":"call_a","output":"result"},{"type":"item_reference","id":"call_b"}]}`, false},
		{"replayed call", `{"input":[{"type":"function_call","call_id":"call_a","name":"tool","arguments":"{}"},{"type":"function_call_output","call_id":"call_a","output":"result"}]}`, true},
		{"HTTP continuation", `{"previous_response_id":"resp_previous","input":[{"type":"function_call_output","call_id":"call_a","output":"result"}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			h := &OpenAIGatewayHandler{}
			require.Equal(t, tc.allowed, h.validateFunctionCallOutputRequest(c, []byte(tc.body), zap.NewNop()))
			if tc.allowed {
				require.False(t, c.Writer.Written())
				return
			}
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "item_reference")
			require.NotContains(t, w.Body.String(), "only supported on Responses WebSocket v2")
		})
	}
}

func TestFunctionCallOutputValidationFindsMissingIDInMixedToolOutputs(t *testing.T) {
	for _, itemType := range []string{"function_call_output", "custom_tool_call_output", "tool_search_output", "mcp_tool_call_output", " tool_search_output "} {
		t.Run(itemType, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"input": []any{
				map[string]any{"role": "user", "content": "hello"},
				map[string]any{"type": "function_call_output", "call_id": "call_valid", "output": "ok"},
				map[string]any{"type": itemType, "output": "private result"},
			}})
			require.NoError(t, err)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			h := &OpenAIGatewayHandler{}
			require.False(t, h.validateFunctionCallOutputRequest(c, body, zap.NewNop()))
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Contains(t, w.Body.String(), "input[2].call_id")
			require.NotContains(t, w.Body.String(), "private result")
		})
	}
}
