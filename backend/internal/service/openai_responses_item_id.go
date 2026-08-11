package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Invalid replayed IDs are removed rather than rewritten because a fabricated
// msg/fc ID may point at a different upstream object.
func shouldStripOpenAIResponsesInputItemID(itemType, id string) bool {
	return shouldStripOpenAIResponsesInputItemIDWithOptions(itemType, id, false)
}

func shouldStripOpenAIResponsesInputItemIDWithOptions(itemType, id string, stripReasoningIDs bool) bool {
	if id == "" {
		return false
	}
	itemType = strings.ToLower(strings.TrimSpace(itemType))
	if itemType == "reasoning" {
		return stripReasoningIDs && strings.HasPrefix(strings.ToLower(strings.TrimSpace(id)), "rs_")
	}
	if itemType == "message" {
		return !strings.HasPrefix(id, "msg") || len(id) > codexCallIDMaxLength
	}
	if isCodexToolCallInputType(itemType) {
		return !strings.HasPrefix(id, "fc") || len(id) > codexCallIDMaxLength
	}
	return false
}

func sanitizeOpenAIResponsesInputItemIDs(body []byte) ([]byte, bool, error) {
	store := gjson.GetBytes(body, "store")
	return sanitizeOpenAIResponsesInputItemIDsWithOptions(body, store.Exists() && store.Type == gjson.False)
}

func sanitizeOpenAIResponsesInputItemIDsWithOptions(body []byte, stripReasoningIDs bool) ([]byte, bool, error) {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false, nil
	}

	items := make([][]byte, 0)
	changed := false
	var sanitizeErr error
	index := 0
	input.ForEach(func(_, item gjson.Result) bool {
		currentIndex := index
		index++
		itemBody := []byte(item.Raw)
		if item.IsObject() {
			itemType := item.Get("type")
			id := item.Get("id")
			if itemType.Type == gjson.String && id.Type == gjson.String &&
				shouldStripOpenAIResponsesInputItemIDWithOptions(itemType.String(), id.String(), stripReasoningIDs) {
				itemBody, sanitizeErr = sjson.DeleteBytes(itemBody, "id")
				if sanitizeErr != nil {
					sanitizeErr = fmt.Errorf("delete input.%d.id: %w", currentIndex, sanitizeErr)
					return false
				}
				changed = true
			}
		}
		items = append(items, itemBody)
		return true
	})
	if sanitizeErr != nil {
		return nil, false, sanitizeErr
	}
	if !changed {
		return body, false, nil
	}

	rebuiltInput := make([]byte, 0, len(input.Raw))
	rebuiltInput = append(rebuiltInput, '[')
	for i, item := range items {
		if i > 0 {
			rebuiltInput = append(rebuiltInput, ',')
		}
		rebuiltInput = append(rebuiltInput, item...)
	}
	rebuiltInput = append(rebuiltInput, ']')

	sanitized, err := sjson.SetRawBytes(body, "input", rebuiltInput)
	if err != nil {
		return nil, false, fmt.Errorf("replace sanitized input: %w", err)
	}
	return sanitized, true, nil
}
