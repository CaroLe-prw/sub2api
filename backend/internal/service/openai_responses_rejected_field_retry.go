package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxOpenAIResponsesRejectedFieldRetries = 6

var (
	openAIResponsesRejectedNamespaceParamPattern = regexp.MustCompile(`(?i)^input\[(\d+)\]\.namespace$`)
	openAIResponsesRejectedSummaryParamPattern   = regexp.MustCompile(`(?i)^input\[(\d+)\]\.summary$`)
	openAIResponsesRejectedInputParamPattern     = regexp.MustCompile(`(?i)^input\[(\d+)\]\.input$`)
	openAIResponsesRejectedArgumentsParamPattern = regexp.MustCompile(`(?i)^input\[(\d+)\]\.arguments$`)
	openAIResponsesRejectedIDParamPattern        = regexp.MustCompile(`(?i)^input\[(\d+)\]\.id$`)
	openAIResponsesRejectedIndexedParamPattern   = regexp.MustCompile(`(?i)^input\[(\d+)\]\.([a-z][a-z0-9_]*)$`)
	openAIResponsesRejectedMessageParamPattern   = regexp.MustCompile(`(?i)(?:(?:unknown|unsupported)[ _-]+parameter|missing required parameter|invalid)\s*(?::|=|is)?\s*["']?(max_output_tokens|input\[\d+\]\.[a-z][a-z0-9_]*)(?:["']|\.(?:\s|$)|[,:;](?:\s|$)|\s|$)`)
)

type openAIResponsesRejectedFieldRetryState struct {
	attempts       int
	seenBodyHashes map[[sha256.Size]byte]struct{}
}

func newOpenAIResponsesRejectedFieldRetryState(initialBody []byte) *openAIResponsesRejectedFieldRetryState {
	state := &openAIResponsesRejectedFieldRetryState{
		seenBodyHashes: make(map[[sha256.Size]byte]struct{}, maxOpenAIResponsesRejectedFieldRetries+1),
	}
	state.remember(initialBody)
	return state
}

func (s *openAIResponsesRejectedFieldRetryState) Allow(nextBody []byte) bool {
	if s == nil || len(nextBody) == 0 || s.attempts >= maxOpenAIResponsesRejectedFieldRetries {
		return false
	}
	bodyHash := sha256.Sum256(nextBody)
	if _, seen := s.seenBodyHashes[bodyHash]; seen {
		return false
	}
	s.seenBodyHashes[bodyHash] = struct{}{}
	s.attempts++
	return true
}

func (s *openAIResponsesRejectedFieldRetryState) remember(body []byte) {
	if s == nil || len(body) == 0 {
		return
	}
	if s.seenBodyHashes == nil {
		s.seenBodyHashes = make(map[[sha256.Size]byte]struct{}, maxOpenAIResponsesRejectedFieldRetries+1)
	}
	s.seenBodyHashes[sha256.Sum256(body)] = struct{}{}
}

func normalizeOpenAIResponsesRejectedFieldRetryBody(statusCode int, body, responseBody []byte) ([]byte, string, bool, error) {
	if statusCode != http.StatusBadRequest || len(body) == 0 || len(responseBody) == 0 {
		return nil, "", false, nil
	}

	code := strings.ToLower(strings.TrimSpace(extractUpstreamErrorCode(responseBody)))
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	if !isExplicitOpenAIResponsesFieldRejection(code, message) {
		return nil, "", false, nil
	}

	param := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "error.param").String()))
	if param == "" {
		param = openAIResponsesRejectedParamFromMessage(message)
	}
	if index, ok := openAIResponsesRejectedNamespaceIndex(param); ok {
		return removeOpenAIResponsesRejectedNamespaceAtIndex(body, index)
	}
	if index, ok := openAIResponsesRejectedSummaryIndex(param); ok {
		return removeOpenAIResponsesRejectedSummaryAtIndex(body, index)
	}
	if index, ok := openAIResponsesRejectedArgumentsIndex(param); ok {
		return backfillOpenAIResponsesFunctionCallArguments(body, index)
	}
	if index, ok := openAIResponsesRejectedIDIndex(param); ok {
		return removeOpenAIResponsesRejectedOverlongID(body, index)
	}
	if param == "max_output_tokens" && gjson.GetBytes(body, "max_output_tokens").Exists() {
		retryBody, err := sjson.DeleteBytes(body, "max_output_tokens")
		if err != nil {
			return nil, "", false, fmt.Errorf("delete rejected max_output_tokens: %w", err)
		}
		return retryBody, "max_output_tokens parameter rejection", true, nil
	}
	if index, field, ok := openAIResponsesRejectedIndexedParam(param); ok &&
		isExplicitOpenAIResponsesUnknownParameter(code, message) {
		switch field {
		case "input", "arguments", "id":
			// These fields need semantic conversion/backfill rather than deletion.
			return nil, "", false, nil
		default:
			return removeOpenAIResponsesRejectedFieldForItemType(body, index, field)
		}
	}
	return nil, "", false, nil
}

func openAIResponsesRejectedSummaryIndex(param string) (int, bool) {
	match := openAIResponsesRejectedSummaryParamPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(match) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(match[1])
	if err == nil && index >= 0 {
		return index, true
	}
	return 0, false
}

func openAIResponsesRejectedInputIndex(param string) (int, bool) {
	match := openAIResponsesRejectedInputParamPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(match) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(match[1])
	if err == nil && index >= 0 {
		return index, true
	}
	return 0, false
}

func openAIResponsesRejectedArgumentsIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedArgumentsParamPattern, param)
}

func openAIResponsesRejectedIDIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedIDParamPattern, param)
}

func openAIResponsesRejectedIndexedField(pattern *regexp.Regexp, param string) (int, bool) {
	match := pattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(match) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(match[1])
	if err == nil && index >= 0 {
		return index, true
	}
	return 0, false
}

func openAIResponsesRejectedIndexedParam(param string) (int, string, bool) {
	match := openAIResponsesRejectedIndexedParamPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(match) != 3 {
		return 0, "", false
	}
	index, err := strconv.Atoi(match[1])
	field := strings.ToLower(strings.TrimSpace(match[2]))
	if err != nil || index < 0 || field == "" {
		return 0, "", false
	}
	return index, field, true
}

func removeOpenAIResponsesRejectedFieldForItemType(body []byte, rejectedIndex int, field string) ([]byte, string, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, "", false, fmt.Errorf("decode Responses rejected field retry body: %w", err)
	}
	input, _ := requestBody["input"].([]any)
	if rejectedIndex < 0 || rejectedIndex >= len(input) {
		return nil, "", false, nil
	}
	rejectedItem, _ := input[rejectedIndex].(map[string]any)
	if rejectedItem == nil {
		return nil, "", false, nil
	}
	if _, exists := rejectedItem[field]; !exists {
		return nil, "", false, nil
	}
	rejectedType := strings.TrimSpace(stringFromAny(rejectedItem["type"]))
	changed := false
	for index, rawItem := range input {
		item, _ := rawItem.(map[string]any)
		if item == nil || (index != rejectedIndex && (rejectedType == "" || strings.TrimSpace(stringFromAny(item["type"])) != rejectedType)) {
			continue
		}
		if _, exists := item[field]; exists {
			delete(item, field)
			changed = true
		}
	}
	if !changed {
		return nil, "", false, nil
	}
	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, "", false, fmt.Errorf("encode Responses rejected field retry body: %w", err)
	}
	return retryBody, fmt.Sprintf("indexed %s parameter rejection", field), true, nil
}

func backfillOpenAIResponsesFunctionCallArguments(body []byte, rejectedIndex int) ([]byte, string, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, "", false, fmt.Errorf("decode Responses arguments retry body: %w", err)
	}
	input, _ := requestBody["input"].([]any)
	if rejectedIndex < 0 || rejectedIndex >= len(input) {
		return nil, "", false, nil
	}
	rejectedItem, _ := input[rejectedIndex].(map[string]any)
	if strings.TrimSpace(stringFromAny(rejectedItem["type"])) != "function_call" {
		return nil, "", false, nil
	}
	if arguments, exists := rejectedItem["arguments"]; exists && arguments != nil {
		return nil, "", false, nil
	}

	changed := false
	for _, rawItem := range input {
		item, _ := rawItem.(map[string]any)
		if strings.TrimSpace(stringFromAny(item["type"])) != "function_call" {
			continue
		}
		if arguments, exists := item["arguments"]; exists && arguments != nil {
			continue
		}
		arguments := "{}"
		if customInput, exists := item["input"]; exists {
			encoded, err := json.Marshal(map[string]any{"input": customInput})
			if err != nil {
				return nil, "", false, fmt.Errorf("encode Responses function arguments: %w", err)
			}
			arguments = string(encoded)
			delete(item, "input")
		}
		item["arguments"] = arguments
		changed = true
	}
	if !changed {
		return nil, "", false, nil
	}
	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, "", false, fmt.Errorf("encode Responses arguments retry body: %w", err)
	}
	return retryBody, "missing function call arguments", true, nil
}

func removeOpenAIResponsesRejectedOverlongID(body []byte, index int) ([]byte, string, bool, error) {
	idPath := fmt.Sprintf("input.%d.id", index)
	id := gjson.GetBytes(body, idPath)
	if id.Type != gjson.String || len(id.String()) <= codexCallIDMaxLength {
		return nil, "", false, nil
	}
	retryBody, changed, err := sanitizeOpenAIResponsesInputItemIDs(body)
	if err != nil {
		return nil, "", false, fmt.Errorf("sanitize overlong Responses input ID: %w", err)
	}
	if !changed || gjson.GetBytes(retryBody, idPath).Exists() {
		return nil, "", false, nil
	}
	return retryBody, "overlong input item id", true, nil
}

func removeOpenAIResponsesRejectedSummaryAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	summaryPath := fmt.Sprintf("input.%d.summary", index)
	if !gjson.GetBytes(body, summaryPath).Exists() {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, summaryPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected summary at input[%d]: %w", index, err)
	}
	return retryBody, "indexed summary parameter rejection", true, nil
}

func isExplicitOpenAIResponsesFieldRejection(code, message string) bool {
	switch strings.TrimSpace(code) {
	case "unknown_parameter", "unsupported_parameter", "missing_required_parameter":
		return true
	}
	return strings.Contains(message, "unknown parameter") ||
		strings.Contains(message, "unsupported parameter") ||
		strings.Contains(message, "missing required parameter") ||
		(strings.Contains(message, "invalid") && strings.Contains(message, "string too long"))
}

func isExplicitOpenAIResponsesUnknownParameter(code, message string) bool {
	switch strings.TrimSpace(code) {
	case "unknown_parameter", "unsupported_parameter":
		return true
	}
	return strings.Contains(message, "unknown parameter") || strings.Contains(message, "unsupported parameter")
}

func openAIResponsesRejectedParamFromMessage(message string) string {
	match := openAIResponsesRejectedMessageParamPattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func openAIResponsesRejectedNamespaceIndex(param string) (int, bool) {
	match := openAIResponsesRejectedNamespaceParamPattern.FindStringSubmatch(strings.TrimSpace(param))
	if len(match) != 2 {
		return 0, false
	}
	index, err := strconv.Atoi(match[1])
	if err == nil && index >= 0 {
		return index, true
	}
	return 0, false
}

func removeOpenAIResponsesRejectedNamespaceAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	itemPath := fmt.Sprintf("input.%d", index)
	itemType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, itemPath+".type").String()))
	switch itemType {
	case "function_call", "tool_call", "custom_tool_call", "mcp_tool_call":
	default:
		return nil, "", false, nil
	}

	namespacePath := itemPath + ".namespace"
	if !gjson.GetBytes(body, namespacePath).Exists() {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, namespacePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected namespace at input[%d]: %w", index, err)
	}
	return retryBody, "indexed namespace parameter rejection", true, nil
}
