package service

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// GPT-6.2 treats top-level instructions as uncached, turn-scoped developer
// messages appended after input. Only gateway-owned reusable guidance belongs
// in the input prefix; explicit client instructions must retain their semantics.
// https://developers.openai.com/api/docs/guides/keep-prefix-unchanged
func codexUsesCacheableInstructions(model string) bool {
	return openai.GPT62ModelID(model) != ""
}

func applyCodexCacheableInstructions(reqBody map[string]any, opts codexOAuthTransformOptions) bool {
	modified := false
	hasInitialGuidanceOrCompaction := false
	if input, ok := reqBody["input"].([]any); ok {
		for index, item := range input {
			message, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if message["role"] == "system" {
				// The internal endpoint requires developer, but keep the content,
				// position and metadata intact, including mid-conversation updates.
				message["role"] = "developer"
				modified = true
			}
			// Later developer updates must not make a previously synthesized
			// prefix disappear on full-history replay. Compacted history already
			// owns its prefix and must not receive a new default at any position.
			if (index == 0 && message["role"] == "developer") || message["type"] == "compaction" {
				hasInitialGuidanceOrCompaction = true
			}
		}
	}
	if opts.IsCompact {
		// Do not synthesize a new prefix or instructions during compaction.
		// Explicit client compact instructions are preserved, not silently lost.
		return modified
	}

	previousResponseID, _ := reqBody["previous_response_id"].(string)
	if !opts.SkipDefaultInstructions && !hasInitialGuidanceOrCompaction &&
		strings.TrimSpace(previousResponseID) == "" && isInstructionsEmpty(reqBody) {
		model, _ := reqBody["model"].(string)
		if prependCodexDeveloperInstructions(reqBody, defaultCodexSynthInstructions(model)) {
			modified = true
		}
	}
	// Codex OAuth still requires a string field. An empty string avoids adding
	// a second copy of the reusable prompt while satisfying that wire contract.
	if _, ok := reqBody["instructions"].(string); !ok {
		reqBody["instructions"] = ""
		modified = true
	}
	return modified
}

func prependCodexDeveloperInstructions(reqBody map[string]any, text string) bool {
	var input []any
	switch value := reqBody["input"].(type) {
	case []any:
		input = value
	case string:
		if strings.TrimSpace(value) != "" {
			input = []any{map[string]any{"type": "message", "role": "user", "content": value}}
		}
	case nil:
	default:
		// Leave invalid input for upstream validation rather than dropping it.
		return false
	}
	reqBody["input"] = append([]any{map[string]any{
		"type": "message", "role": "developer", "content": text,
	}}, input...)
	return true
}

func codexInitialDeveloperInstructions(reqBody map[string]any) string {
	input, ok := reqBody["input"].([]any)
	if !ok || len(input) == 0 {
		return ""
	}
	first, ok := input[0].(map[string]any)
	if !ok || first["role"] != "developer" {
		return ""
	}
	return strings.TrimSpace(extractTextFromContent(first["content"]))
}

func applyCodexCacheableInstructionsTemplate(reqBody map[string]any, rendered, existing string) bool {
	if input, ok := reqBody["input"].([]any); ok && len(input) > 0 {
		if first, ok := input[0].(map[string]any); ok && first["role"] == "developer" {
			if text, lossless := extractLosslessTextFromContent(first["content"]); lossless {
				if strings.TrimSpace(text) == rendered {
					return false
				}
				// Replace only the initial text-only prompt, and only when the
				// rendered template retains it verbatim. Otherwise keep the client
				// message (including non-text content) and prepend the server policy.
				if existing != "" && strings.TrimSpace(text) == existing && strings.Contains(rendered, existing) {
					first["content"] = rendered
					return true
				}
			}
		}
	}
	return prependCodexDeveloperInstructions(reqBody, rendered)
}
