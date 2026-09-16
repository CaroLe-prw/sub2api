package service

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeOpenAIResponsesMessageContent wraps a recognizable singleton content
// part without changing its payload. Unknown objects remain upstream validation
// errors rather than being silently serialized into user text.
func normalizeOpenAIResponsesMessageContent(body []byte) ([]byte, bool, error) {
	root := parseRawJSONView(body)
	input := root.Get("input")
	if !input.IsArray() {
		return body, false, nil
	}
	var items []string
	changed := false
	var normalizeErr error
	input.ForEach(func(_, item gjson.Result) bool {
		raw := item.Raw
		content := item.Get("content")
		if isOpenAIResponsesSingletonContent(item, content) && !hasDuplicateJSONObjectKeys(item) {
			updated, err := sjson.SetRaw(raw, "content", "["+content.Raw+"]")
			if err != nil {
				normalizeErr = fmt.Errorf("normalize Responses message content: %w", err)
				return false
			}
			raw = updated
			changed = true
		}
		items = append(items, raw)
		return true
	})
	if normalizeErr != nil {
		return body, false, normalizeErr
	}
	if !changed || !gjson.ValidBytes(body) || hasDuplicateJSONObjectKeys(root) {
		return body, false, nil
	}
	return replaceOpenAIRawInput(body, input, items), true, nil
}

func isOpenAIResponsesSingletonContent(item, content gjson.Result) bool {
	if !item.IsObject() || !content.IsObject() {
		return false
	}
	itemType := item.Get("type")
	if itemType.Exists() && itemType.String() != "message" {
		return false
	}
	switch item.Get("role").String() {
	case "system", "developer", "user", "assistant":
	default:
		return false
	}
	if hasDuplicateJSONObjectKeys(content) {
		return false
	}
	switch content.Get("type").String() {
	case "input_text", "input_image", "input_file", "output_text", "refusal":
		return true
	default:
		return false
	}
}
