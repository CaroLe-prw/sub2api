package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Codex 0.155 declares exec as a custom child of the functions namespace,
// including when tools are carried in input[].additional_tools.
func TestResponsesChatBridge_NamespacedCustomDeclarations(t *testing.T) {
	for _, carrier := range []string{"tools", "additional_tools", "children"} {
		t.Run(carrier, func(t *testing.T) {
			ns := ResponsesTool{Type: "namespace", Name: "functions", Tools: []ResponsesTool{
				{Type: "custom", Name: "exec", Description: "Run JavaScript"},
				{Type: "function", Name: "wait", Parameters: json.RawMessage(`{"type":"object"}`)},
			}}
			if carrier == "children" {
				ns.Children, ns.Tools = ns.Tools, nil
			}
			tools := []ResponsesTool{ns, {Type: "custom", Name: "exec"}, {Type: "function", Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}
			req := &ResponsesRequest{Model: "gpt-5.6-luna", Input: json.RawMessage(`"run pwd"`), Tools: tools, ToolChoice: json.RawMessage(`{"type":"custom","namespace":"functions","name":"exec"}`)}
			if carrier == "additional_tools" {
				req.Tools = nil
				var err error
				req.Input, err = json.Marshal([]any{map[string]any{"type": "additional_tools", "role": "developer", "tools": tools}, map[string]any{"role": "user", "content": "run pwd"}})
				require.NoError(t, err)
			}
			chat, err := ResponsesToChatCompletionsRequest(req)
			require.NoError(t, err)
			require.Len(t, chat.Tools, 4)
			require.Equal(t, "functions__exec", chat.Tools[0].Function.Name)
			require.JSONEq(t, customToolInputSchema, string(chat.Tools[0].Function.Parameters))
			require.Equal(t, "Run JavaScript", chat.Tools[0].Function.Description)
			require.Equal(t, "functions__wait", chat.Tools[1].Function.Name)
			require.JSONEq(t, `{"type":"object"}`, string(chat.Tools[1].Function.Parameters))
			require.Equal(t, "exec", chat.Tools[2].Function.Name)
			require.Equal(t, "lookup", chat.Tools[3].Function.Name)
			require.JSONEq(t, `{"type":"function","function":{"name":"functions__exec"}}`, string(chat.ToolChoice))
			effective, err := EffectiveResponsesTools(req)
			require.NoError(t, err)
			require.True(t, CustomToolNames(effective)["functions__exec"])
			require.True(t, CustomToolNames(effective)["exec"])
			require.False(t, CustomToolNames(effective)["functions__wait"])
		})
	}
}

func TestResponsesChatBridge_NamespacedCustomRoundTrip(t *testing.T) {
	for _, namespace := range []string{"functions", strings.Repeat("long_namespace_", 6)} {
		for _, stream := range []bool{false, true} {
			name := namespace + "/buffered"
			if stream {
				name = namespace + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				tools := []ResponsesTool{{Type: "namespace", Name: namespace, Tools: []ResponsesTool{{Type: "custom", Name: "exec"}, {Type: "function", Name: "wait"}}}, {Type: "custom", Name: "exec"}}
				custom, namespaces := CustomToolNames(tools), NamespaceToolNames(tools)
				code := "text(await tools.exec_command({cmd: \"pwd\"}));\n"
				args, err := json.Marshal(map[string]string{"input": code})
				require.NoError(t, err)
				idx := 0
				call := ChatToolCall{Index: &idx, ID: "call_exec", Type: "function", Function: ChatFunctionCall{Name: flattenNamespaceToolName(namespace, "exec"), Arguments: string(args)}}
				var output []ResponsesOutput
				if !stream {
					response := ChatCompletionsResponseToResponses(&ChatCompletionsResponse{Choices: []ChatChoice{{Message: ChatMessage{Role: "assistant", ToolCalls: []ChatToolCall{call}}}}}, "gpt-5.6-luna", custom, FunctionToolNames(tools), false, namespaces)
					output = response.Output
				} else {
					state := NewChatCompletionsToResponsesStreamState("gpt-5.6-luna")
					state.CustomTools, state.NamespaceTools = custom, namespaces
					// The name and arguments arrive in separate chunks, as with real providers.
					first := call
					first.Function.Arguments = ""
					events := ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{first}}}}}, state)
					events = append(events, ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{Index: &idx, Function: ChatFunctionCall{Arguments: string(args)}}}}}}}, state)...)
					events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
					var added, done, inputDone bool
					for _, event := range events {
						require.NotEqual(t, "response.function_call_arguments.delta", event.Type)
						require.NotEqual(t, "response.function_call_arguments.done", event.Type)
						if event.Type == "response.output_item.added" || event.Type == "response.output_item.done" {
							require.NotNil(t, event.Item)
							require.Equal(t, "custom_tool_call", event.Item.Type)
							require.Equal(t, "exec", event.Item.Name)
							require.Equal(t, namespace, event.Item.Namespace)
							require.Equal(t, "call_exec", event.Item.CallID)
							wire, err := json.Marshal(event)
							require.NoError(t, err)
							var decoded struct {
								Item map[string]any `json:"item"`
							}
							require.NoError(t, json.Unmarshal(wire, &decoded))
							require.Equal(t, namespace, decoded.Item["namespace"], "namespace must survive SSE serialization")
							if event.Type == "response.output_item.added" {
								added = true
							} else {
								done = true
								require.Equal(t, code, event.Item.Input)
							}
						}
						if event.Type == "response.custom_tool_call_input.done" {
							inputDone = true
							require.Equal(t, code, event.Input)
						}
					}
					require.True(t, added && done && inputDone)
					output = events[len(events)-1].Response.Output
				}
				require.Len(t, output, 1)
				require.Equal(t, "custom_tool_call", output[0].Type)
				require.Equal(t, namespace, output[0].Namespace)
				require.Equal(t, "exec", output[0].Name)
				require.Equal(t, "call_exec", output[0].CallID)
				require.Equal(t, code, output[0].Input)
				input, err := json.Marshal([]any{output[0], map[string]any{"type": "custom_tool_call_output", "call_id": "call_exec", "output": "/tmp/probe"}})
				require.NoError(t, err)
				chat, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Model: "gpt-5.6-luna", Tools: tools, Input: input})
				require.NoError(t, err)
				require.Len(t, chat.Messages, 2)
				require.Equal(t, call.Function.Name, chat.Messages[0].ToolCalls[0].Function.Name)
				require.JSONEq(t, string(args), chat.Messages[0].ToolCalls[0].Function.Arguments)
				require.Equal(t, "call_exec", chat.Messages[1].ToolCallID)
				require.JSONEq(t, `"/tmp/probe"`, string(chat.Messages[1].Content))
			})
		}
	}
}

func TestResponsesChatBridge_NamespacedCustomCollision(t *testing.T) {
	for _, other := range []ResponsesTool{
		{Type: "function", Name: "functions__exec"},
		{Type: "custom", Name: "functions__exec"},
		{Type: "namespace", Name: "functions", Tools: []ResponsesTool{{Type: "function", Name: "exec"}}},
	} {
		_, err := ResponsesToChatCompletionsRequest(&ResponsesRequest{Input: json.RawMessage(`"run pwd"`), Tools: []ResponsesTool{
			{Type: "namespace", Name: "functions", Tools: []ResponsesTool{{Type: "custom", Name: "exec"}}}, other,
		}})
		require.Error(t, err, "ambiguous declarations must not route to the wrong tool")
	}
}
