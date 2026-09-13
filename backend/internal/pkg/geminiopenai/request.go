// Package geminiopenai translates Gemini generation requests to Chat Completions.
package geminiopenai

import (
	"encoding/json"
	"fmt"
	"strings"
)

type object = map[string]any

func obj(v any) object { m, _ := v.(map[string]any); return m }
func arr(v any) []any  { a, _ := v.([]any); return a }
func str(v any) string { s, _ := v.(string); return s }

// Request preserves message order and pairs tool responses with their call IDs.
// Features without a Chat Completions equivalent fail explicitly before forwarding.
func Request(body []byte, model string, stream bool) ([]byte, error) {
	var in object
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("invalid Gemini request: %w", err)
	}
	if len(arr(in["contents"])) == 0 {
		return nil, fmt.Errorf("contents must not be empty")
	}
	for _, key := range []string{"cachedContent", "safetySettings"} {
		if v := in[key]; v != nil && v != "" && (key != "safetySettings" || len(arr(v)) > 0) {
			return nil, fmt.Errorf("%s is not supported by an OpenAI compatible upstream", key)
		}
	}
	out := object{"model": model, "stream": stream}
	if stream {
		out["stream_options"] = object{"include_usage": true}
	}
	messages := []any{}
	if system := obj(in["systemInstruction"]); system != nil {
		texts := []string{}
		for _, p := range arr(system["parts"]) {
			text, ok := obj(p)["text"].(string)
			if !ok {
				return nil, fmt.Errorf("systemInstruction only supports text")
			}
			texts = append(texts, text)
		}
		messages = append(messages, object{"role": "system", "content": strings.Join(texts, "\n")})
	}
	type pendingCall struct{ id, name string }
	pending := []pendingCall{}
	serial := 0
	for _, item := range arr(in["contents"]) {
		content := obj(item)
		role := str(content["role"])
		if role == "model" {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("unsupported Gemini role: %s", role)
		}
		parts, calls, results := []any{}, []any{}, []any{}
		reasoning := ""
		for _, raw := range arr(content["parts"]) {
			p := obj(raw)
			switch {
			case p["text"] != nil:
				if p["thought"] == true {
					reasoning += str(p["text"])
					continue
				}
				parts = append(parts, object{"type": "text", "text": p["text"]})
			case p["functionCall"] != nil:
				if role != "assistant" {
					return nil, fmt.Errorf("functionCall requires model role")
				}
				fc := obj(p["functionCall"])
				name, id := str(fc["name"]), str(fc["id"])
				if name == "" {
					return nil, fmt.Errorf("functionCall name is required")
				}
				serial++
				if id == "" {
					id = fmt.Sprintf("call_gemini_%d", serial)
				}
				args := fc["args"]
				if args == nil {
					args = object{}
				}
				if obj(args) == nil {
					return nil, fmt.Errorf("functionCall args must be an object")
				}
				encoded, _ := json.Marshal(args)
				calls = append(calls, object{"id": id, "type": "function", "function": object{"name": name, "arguments": string(encoded)}})
				pending = append(pending, pendingCall{id, name})
			case p["functionResponse"] != nil:
				if role != "user" {
					return nil, fmt.Errorf("functionResponse requires user role")
				}
				fr := obj(p["functionResponse"])
				if len(arr(fr["parts"])) > 0 {
					return nil, fmt.Errorf("multimodal functionResponse is not supported by an OpenAI compatible upstream")
				}
				match := -1
				for i, call := range pending {
					if (str(fr["id"]) != "" && call.id == str(fr["id"])) || (str(fr["id"]) == "" && call.name == str(fr["name"])) {
						match = i
						break
					}
				}
				if match < 0 {
					return nil, fmt.Errorf("functionResponse has no matching functionCall")
				}
				encoded, _ := json.Marshal(fr["response"])
				results = append(results, object{"role": "tool", "tool_call_id": pending[match].id, "content": string(encoded)})
				pending = append(pending[:match], pending[match+1:]...)
			case p["inlineData"] != nil:
				data := obj(p["inlineData"])
				mime := str(data["mimeType"])
				if !strings.HasPrefix(mime, "image/") {
					return nil, fmt.Errorf("inlineData MIME type %s is not supported by an OpenAI compatible upstream", mime)
				}
				parts = append(parts, object{"type": "image_url", "image_url": object{"url": "data:" + mime + ";base64," + str(data["data"])}})
			case p["fileData"] != nil:
				data := obj(p["fileData"])
				uri := str(data["fileUri"])
				if !strings.HasPrefix(str(data["mimeType"]), "image/") || (!strings.HasPrefix(uri, "https://") && !strings.HasPrefix(uri, "http://")) {
					return nil, fmt.Errorf("fileData requires an HTTP image URL for an OpenAI compatible upstream")
				}
				parts = append(parts, object{"type": "image_url", "image_url": object{"url": uri}})
			default:
				return nil, fmt.Errorf("unsupported Gemini content part for an OpenAI compatible upstream")
			}
		}
		messages = append(messages, results...)
		if len(parts) > 0 || len(calls) > 0 || reasoning != "" {
			m := object{"role": role, "content": parts}
			if len(parts) == 0 {
				m["content"] = nil
			}
			if len(calls) > 0 {
				m["tool_calls"] = calls
			}
			if reasoning != "" {
				m["reasoning_content"] = reasoning
			}
			messages = append(messages, m)
		}
	}
	out["messages"] = messages
	tools := []any{}
	for _, raw := range arr(in["tools"]) {
		tool := obj(raw)
		for key := range tool {
			if key != "functionDeclarations" {
				return nil, fmt.Errorf("Gemini tool %s is not supported by an OpenAI compatible upstream", key)
			}
		}
		for _, rawFn := range arr(tool["functionDeclarations"]) {
			fn := obj(rawFn)
			f := object{"name": fn["name"]}
			if fn["description"] != nil {
				f["description"] = fn["description"]
			}
			if fn["parametersJsonSchema"] != nil {
				f["parameters"] = fn["parametersJsonSchema"]
			} else if fn["parameters"] != nil {
				f["parameters"] = schema(fn["parameters"])
			}
			tools = append(tools, object{"type": "function", "function": f})
		}
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if tc := obj(obj(in["toolConfig"])["functionCallingConfig"]); tc != nil {
		names := arr(tc["allowedFunctionNames"])
		switch str(tc["mode"]) {
		case "", "AUTO", "MODE_UNSPECIFIED":
			out["tool_choice"] = "auto"
		case "NONE":
			out["tool_choice"] = "none"
		case "ANY":
			out["tool_choice"] = "required"
		default:
			return nil, fmt.Errorf("unsupported function calling mode: %s", str(tc["mode"]))
		}
		if len(names) > 0 {
			if len(names) != 1 || str(tc["mode"]) != "ANY" {
				return nil, fmt.Errorf("allowedFunctionNames requires one function with ANY mode for an OpenAI compatible upstream")
			}
			out["tool_choice"] = object{"type": "function", "function": object{"name": names[0]}}
		}
	}
	gc := obj(in["generationConfig"])
	for from, to := range map[string]string{"temperature": "temperature", "topP": "top_p", "maxOutputTokens": "max_tokens", "stopSequences": "stop", "candidateCount": "n", "presencePenalty": "presence_penalty", "frequencyPenalty": "frequency_penalty", "seed": "seed"} {
		if v, ok := gc[from]; ok {
			out[to] = v
		}
	}
	for _, key := range []string{"topK", "responseModalities", "imageConfig", "speechConfig", "audioTimestamp", "mediaResolution"} {
		if gc[key] != nil {
			return nil, fmt.Errorf("generationConfig.%s is not supported by an OpenAI compatible upstream", key)
		}
	}
	if mime := str(gc["responseMimeType"]); mime != "" && mime != "text/plain" {
		if mime != "application/json" {
			return nil, fmt.Errorf("unsupported responseMimeType: %s", mime)
		}
		out["response_format"] = object{"type": "json_object"}
		responseSchema := gc["responseJsonSchema"]
		if responseSchema == nil && gc["responseSchema"] != nil {
			responseSchema = schema(gc["responseSchema"])
		}
		if responseSchema != nil {
			out["response_format"] = object{"type": "json_schema", "json_schema": object{"name": "response", "schema": responseSchema}}
		}
	}
	if thinking := obj(gc["thinkingConfig"]); thinking != nil {
		if thinking["thinkingBudget"] != nil {
			return nil, fmt.Errorf("thinkingBudget is not supported by an OpenAI compatible upstream; use thinkingLevel")
		}
		if level := str(thinking["thinkingLevel"]); level != "" {
			out["reasoning_effort"] = strings.ToLower(level)
		}
	}
	return json.Marshal(out)
}

// schema converts Gemini's uppercase schema types without changing property names.
func schema(v any) any {
	m := obj(v)
	if m == nil {
		return v
	}
	out := object{}
	for k, v := range m {
		switch k {
		case "type":
			out[k] = strings.ToLower(str(v))
		case "propertyOrdering", "nullable":
		case "properties", "$defs", "definitions":
			props := object{}
			for name, child := range obj(v) {
				props[name] = schema(child)
			}
			out[k] = props
		case "items", "additionalProperties":
			out[k] = schema(v)
		case "anyOf", "allOf", "oneOf":
			items := []any{}
			for _, child := range arr(v) {
				items = append(items, schema(child))
			}
			out[k] = items
		default:
			out[k] = v
		}
	}
	if m["nullable"] == true && out["type"] != nil {
		out["type"] = []any{out["type"], "null"}
	}
	return out
}
