package geminiopenai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

const maxPayload = 16 << 20

type toolCall struct{ ID, Name, Arguments string }
type choiceState struct {
	Calls    map[int]*toolCall
	Finished bool
}
type responseState struct{ Choices map[int]*choiceState }

func number(v any) int { n, _ := v.(float64); return int(n) }

func (s *responseState) convert(data []byte, stream bool) ([]byte, error) {
	var in object
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("invalid Chat Completions response: %w", err)
	}
	if in["error"] != nil {
		return nil, fmt.Errorf("OpenAI compatible upstream returned a stream error")
	}
	if _, ok := in["choices"].([]any); !ok {
		return nil, fmt.Errorf("OpenAI compatible upstream response has no choices")
	}
	out := object{}
	if in["model"] != nil {
		out["modelVersion"] = in["model"]
	}
	if in["id"] != nil {
		out["responseId"] = in["id"]
	}
	candidates := []any{}
	for _, raw := range arr(in["choices"]) {
		choice := obj(raw)
		index := number(choice["index"])
		state := s.Choices[index]
		if state == nil {
			state = &choiceState{Calls: map[int]*toolCall{}}
			s.Choices[index] = state
		}
		message := obj(choice["message"])
		if stream {
			message = obj(choice["delta"])
		}
		parts := []any{}
		if text := str(message["reasoning_content"]); text != "" {
			parts = append(parts, object{"text": text, "thought": true})
		}
		if text := str(message["content"]); text != "" {
			parts = append(parts, object{"text": text})
		}
		if message["content"] != nil && str(message["content"]) == "" {
			if _, ok := message["content"].(string); !ok {
				return nil, fmt.Errorf("unsupported Chat Completions response content")
			}
		}
		for ordinal, rawCall := range arr(message["tool_calls"]) {
			call := obj(rawCall)
			i := ordinal
			if stream {
				i = number(call["index"])
			}
			current := state.Calls[i]
			if current == nil {
				current = &toolCall{}
				state.Calls[i] = current
			}
			if id := str(call["id"]); id != "" {
				current.ID = id
			}
			fn := obj(call["function"])
			current.Name += str(fn["name"])
			current.Arguments += str(fn["arguments"])
			if len(current.Arguments) > maxPayload {
				return nil, fmt.Errorf("upstream tool arguments exceed size limit")
			}
		}
		finish := str(choice["finish_reason"])
		if !stream || finish != "" {
			indices := make([]int, 0, len(state.Calls))
			for i := range state.Calls {
				indices = append(indices, i)
			}
			sort.Ints(indices)
			for _, i := range indices {
				call := state.Calls[i]
				args := object{}
				if call.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || args == nil {
						return nil, fmt.Errorf("invalid upstream tool arguments")
					}
				}
				if call.Name == "" {
					return nil, fmt.Errorf("upstream tool call has no name")
				}
				fc := object{"name": call.Name, "args": args}
				if call.ID != "" {
					fc["id"] = call.ID
				}
				parts = append(parts, object{"functionCall": fc, "thoughtSignature": "skip_thought_signature_validator"})
			}
			state.Calls = map[int]*toolCall{}
		}
		candidate := object{"index": index, "content": object{"role": "model", "parts": parts}}
		if finish != "" {
			state.Finished = true
			switch finish {
			case "stop", "tool_calls", "function_call":
				candidate["finishReason"] = "STOP"
			case "length":
				candidate["finishReason"] = "MAX_TOKENS"
			case "content_filter":
				candidate["finishReason"] = "SAFETY"
			default:
				candidate["finishReason"] = "OTHER"
			}
		}
		if len(parts) > 0 || finish != "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) > 0 {
		out["candidates"] = candidates
	}
	if usage := obj(in["usage"]); usage != nil {
		thoughts := number(obj(usage["completion_tokens_details"])["reasoning_tokens"])
		completion := number(usage["completion_tokens"])
		if thoughts > completion {
			thoughts = completion
		}
		out["usageMetadata"] = object{
			"promptTokenCount":        number(usage["prompt_tokens"]),
			"candidatesTokenCount":    completion - thoughts,
			"totalTokenCount":         number(usage["total_tokens"]),
			"cachedContentTokenCount": number(obj(usage["prompt_tokens_details"])["cached_tokens"]),
			"thoughtsTokenCount":      thoughts,
		}
	}
	if len(candidates) == 0 && out["usageMetadata"] == nil {
		if !stream {
			return nil, fmt.Errorf("empty Chat Completions response")
		}
		return nil, nil
	}
	return json.Marshal(out)
}

// Body translates lazily, preserving backpressure and cancellation from the
// upstream body. Close always closes that body; no producer goroutine is needed.
func Body(source io.ReadCloser, stream bool) io.ReadCloser {
	b := &responseBody{source: source, stream: stream, state: responseState{Choices: map[int]*choiceState{}}}
	if stream {
		b.scanner = bufio.NewScanner(source)
		b.scanner.Buffer(make([]byte, 4096), maxPayload)
	}
	return b
}

type responseBody struct {
	source  io.ReadCloser
	stream  bool
	scanner *bufio.Scanner
	state   responseState
	pending bytes.Buffer
	done    bool
}

func (b *responseBody) Close() error { return b.source.Close() }
func (b *responseBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for b.pending.Len() == 0 {
		if b.done {
			return 0, io.EOF
		}
		if !b.stream {
			b.done = true
			raw, err := io.ReadAll(io.LimitReader(b.source, maxPayload+1))
			if err != nil {
				return 0, err
			}
			if len(raw) > maxPayload {
				return 0, fmt.Errorf("upstream response exceeds size limit")
			}
			data, err := b.state.convert(raw, false)
			if err != nil {
				return 0, err
			}
			_, _ = b.pending.Write(data) // bytes.Buffer writes always return a nil error.
			continue
		}
		payload := []string{}
		size := 0
		for b.scanner.Scan() {
			line := b.scanner.Text()
			if line == "" && len(payload) > 0 {
				break
			}
			if strings.HasPrefix(line, "data:") {
				data := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
				size += len(data)
				if size > maxPayload {
					return 0, fmt.Errorf("upstream SSE event exceeds size limit")
				}
				payload = append(payload, data)
			}
		}
		if err := b.scanner.Err(); err != nil {
			return 0, err
		}
		raw := strings.Join(payload, "\n")
		if len(payload) > 0 && strings.TrimSpace(raw) == "" {
			continue
		}
		if raw == "[DONE]" || len(payload) == 0 {
			b.done = true
			if len(b.state.Choices) == 0 {
				return 0, io.ErrUnexpectedEOF
			}
			for _, choice := range b.state.Choices {
				if !choice.Finished {
					return 0, io.ErrUnexpectedEOF
				}
			}
			return 0, io.EOF
		}
		data, err := b.state.convert([]byte(raw), true)
		if err != nil {
			return 0, err
		}
		if len(data) > 0 {
			_, _ = b.pending.WriteString("data: ")
			_, _ = b.pending.Write(data)
			_, _ = b.pending.WriteString("\n\n")
		}
	}
	return b.pending.Read(p)
}

// Models exposes an OpenAI model catalog using Gemini's model resource shape.
func Models(body []byte, single bool) ([]byte, error) {
	var in object
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, err
	}
	model := func(raw any) (object, error) {
		id := str(obj(raw)["id"])
		if id == "" {
			return nil, fmt.Errorf("upstream model has no id")
		}
		return object{"name": "models/" + strings.TrimPrefix(id, "models/"), "displayName": id, "supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"}}, nil
	}
	if single {
		item, err := model(in)
		if err != nil {
			return nil, err
		}
		return json.Marshal(item)
	}
	if _, ok := in["data"].([]any); !ok {
		return nil, fmt.Errorf("upstream model catalog has no data array")
	}
	models := []any{}
	for _, raw := range arr(in["data"]) {
		item, err := model(raw)
		if err != nil {
			return nil, err
		}
		models = append(models, item)
	}
	return json.Marshal(object{"models": models})
}
