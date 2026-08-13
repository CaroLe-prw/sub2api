package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// HasCompactionTriggerInInput detects an input item with
// type="compaction_trigger". The handler combines this body signal with the
// request path, stream flag, and Codex beta feature header to distinguish the
// native remote compaction v2 wire from the legacy /responses/compact bridge.
func HasCompactionTriggerInInput(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	found := false
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "compaction_trigger" {
			found = true
			return false
		}
		return true
	})
	return found
}

// hasOpenAIServerSideCompactionInBody recognizes the two Responses request
// shapes that ask the upstream to compact context. Encrypted compaction items
// replayed in input are history, not a new compaction request, and are excluded.
func hasOpenAIServerSideCompactionInBody(body []byte) bool {
	if HasCompactionTriggerInInput(body) {
		return true
	}
	strategies := gjson.GetBytes(body, "context_management")
	if !strategies.IsArray() {
		return false
	}
	found := false
	strategies.ForEach(func(_, strategy gjson.Result) bool {
		if strategy.Get("type").String() == "compaction" {
			found = true
			return false
		}
		return true
	})
	return found
}

func isOpenAIServerSideCompactionRequest(c *gin.Context, body []byte) bool {
	return isOpenAIResponsesCompactPath(c) || hasOpenAIServerSideCompactionInBody(body)
}

// stripOpenAIResponsesLiteWSMetadataForCompaction removes the websocket
// carrier for X-OpenAI-Internal-Codex-Responses-Lite when the same frame asks
// for server-side compaction. The upstream rejects that protocol combination.
func stripOpenAIResponsesLiteWSMetadataForCompaction(body []byte) ([]byte, bool, error) {
	if !hasOpenAIServerSideCompactionInBody(body) || !isOpenAIResponsesLiteWebSocketPayload(body) {
		return body, false, nil
	}
	updated, err := sjson.DeleteBytes(body, "client_metadata."+responsesLiteWSMetadataKey)
	if err != nil {
		return body, false, fmt.Errorf("remove Responses Lite metadata from compaction request: %w", err)
	}
	return updated, true, nil
}

// stripOpenAIResponsesLiteInputForCompaction removes Lite-only tool carriers
// after a compaction request has been promoted to the full Responses protocol.
// Compaction summarizes history and does not need the current tool catalog.
func stripOpenAIResponsesLiteInputForCompaction(body []byte) ([]byte, bool, error) {
	if !hasOpenAIServerSideCompactionInBody(body) {
		return body, false, nil
	}
	return stripOpenAIResponsesLiteInput(body)
}

// stripOpenAIResponsesLiteInput removes Lite-only input items after the caller
// has already established compaction semantics, including by request path.
func stripOpenAIResponsesLiteInput(body []byte) ([]byte, bool, error) {
	if !gjson.GetBytes(body, "input").IsArray() {
		return body, false, nil
	}
	var requestBody map[string]any
	if err := json.Unmarshal(body, &requestBody); err != nil {
		return body, false, fmt.Errorf("decode Responses compaction request: %w", err)
	}
	input, _ := requestBody["input"].([]any)
	filtered := make([]any, 0, len(input))
	changed := false
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if ok && strings.TrimSpace(firstNonEmptyString(item["type"])) == "additional_tools" {
			changed = true
			continue
		}
		filtered = append(filtered, rawItem)
	}
	if !changed {
		return body, false, nil
	}
	requestBody["input"] = filtered
	updated, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return body, false, fmt.Errorf("encode Responses compaction request: %w", err)
	}
	return updated, true, nil
}
