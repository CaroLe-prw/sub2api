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
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const maxOpenAIResponsesRejectedFieldRetries = 6

var (
	openAIResponsesRejectedNamespaceParamPattern  = regexp.MustCompile(`(?i)^input\[(\d+)\]\.namespace$`)
	openAIResponsesRejectedSummaryParamPattern    = regexp.MustCompile(`(?i)^input\[(\d+)\]\.summary$`)
	openAIResponsesRejectedInputParamPattern      = regexp.MustCompile(`(?i)^input\[(\d+)\]\.input$`)
	openAIResponsesRejectedArgumentsParamPattern  = regexp.MustCompile(`(?i)^input\[(\d+)\]\.arguments$`)
	openAIResponsesRejectedIDParamPattern         = regexp.MustCompile(`(?i)^input\[(\d+)\]\.id$`)
	openAIResponsesMissingEncryptedParamPattern   = regexp.MustCompile(`(?i)^input\[(\d+)\]\.encrypted_content$`)
	openAIResponsesRejectedIndexedParamPattern    = regexp.MustCompile(`(?i)^input\[(\d+)\]\.([a-z][a-z0-9_]*)$`)
	openAIResponsesRejectedStatusParamPattern     = regexp.MustCompile(`(?i)^input\[(\d+)\]\.status$`)
	openAIResponsesRejectedContentParamPattern    = regexp.MustCompile(`(?i)^input\[(\d+)\]\.content$`)
	openAIResponsesRejectedCacheParamPattern      = regexp.MustCompile(`(?i)^input\[(\d+)\]\.prompt_cache_breakpoint$`)
	openAIResponsesInvalidTypeMessageParamPattern = regexp.MustCompile(`(?i)invalid[ _-]+type\s+for\s+["']?(input\[\d+\]\.content)(?:["']|\b)[^\n]*\b(?:got|received)\s+null\b`)
	openAIResponsesMaxZeroContentMessagePattern   = regexp.MustCompile(`(?i)invalid\s+["']?(input\[\d+\]\.content)["']?\s*:\s*array too long\.[^\n]*maximum length 0\b`)
	openAIResponsesCacheModelRejectionPattern     = regexp.MustCompile(`(?i)["']?(prompt_cache_breakpoint|input\[\d+\]\.prompt_cache_breakpoint)["']?\s+is\s+not\s+supported\s+on\s+this\s+model\b`)
	openAIResponsesToolParametersParamPattern     = regexp.MustCompile(`(?i)^(?:tools|input)\[\d+\](?:\.tools\[\d+\])*(?:\.function)?\.parameters$`)
	openAIResponsesMissingSchemaTypePattern       = regexp.MustCompile(`(?i)\bgot\s+["']?type\s*:\s*["']?none["']?`)
	openAIResponsesRejectedMessageParamPattern    = regexp.MustCompile(`(?i)(?:(?:unknown|unsupported)[ _-]+parameter|missing required parameter|invalid)\s*(?::|=|is)?\s*["']?(context_management(?:\[\d+\](?:\.[a-z][a-z0-9_]*)?)?|max_output_tokens|truncation|input\[\d+\]\.[a-z][a-z0-9_]*)(?:["']|\.(?:\s|$)|[,:;](?:\s|$)|\s|$)`)
)

type openAIResponsesRejectedFieldRetryState struct {
	mu             sync.Mutex
	budget         *openAIResponsesRejectedFieldRetryBudget
	seenBodyHashes map[[sha256.Size]byte]struct{}
}

type openAIResponsesRejectedFieldRetryBudget struct {
	mu       sync.Mutex
	attempts int
}

const openAIResponsesRejectedFieldRetryBudgetContextKey = "openai_responses_rejected_field_retry_budget"

// openAIResponsesRejectedFieldRetryStateForRequest returns a fresh loop guard
// for one account attempt backed by the inbound request's shared retry budget.
// A later account may apply the same compatibility transform, while all account
// attempts together remain bounded.
func openAIResponsesRejectedFieldRetryStateForRequest(c *gin.Context, initialBody []byte) *openAIResponsesRejectedFieldRetryState {
	var budget *openAIResponsesRejectedFieldRetryBudget
	if c != nil {
		if existing, ok := c.Get(openAIResponsesRejectedFieldRetryBudgetContextKey); ok {
			budget, _ = existing.(*openAIResponsesRejectedFieldRetryBudget)
		}
	}
	if budget == nil {
		budget = &openAIResponsesRejectedFieldRetryBudget{}
		if c != nil {
			c.Set(openAIResponsesRejectedFieldRetryBudgetContextKey, budget)
		}
	}
	return newOpenAIResponsesRejectedFieldRetryStateWithBudget(initialBody, budget)
}

func newOpenAIResponsesRejectedFieldRetryState(initialBody []byte) *openAIResponsesRejectedFieldRetryState {
	return newOpenAIResponsesRejectedFieldRetryStateWithBudget(initialBody, &openAIResponsesRejectedFieldRetryBudget{})
}

func newOpenAIResponsesRejectedFieldRetryStateWithBudget(initialBody []byte, budget *openAIResponsesRejectedFieldRetryBudget) *openAIResponsesRejectedFieldRetryState {
	state := &openAIResponsesRejectedFieldRetryState{
		budget:         budget,
		seenBodyHashes: make(map[[sha256.Size]byte]struct{}, maxOpenAIResponsesRejectedFieldRetries+1),
	}
	state.remember(initialBody)
	return state
}

func (s *openAIResponsesRejectedFieldRetryState) Allow(nextBody []byte) bool {
	if s == nil || s.budget == nil || len(nextBody) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bodyHash := sha256.Sum256(nextBody)
	if _, seen := s.seenBodyHashes[bodyHash]; seen {
		return false
	}
	s.budget.mu.Lock()
	defer s.budget.mu.Unlock()
	if s.budget.attempts >= maxOpenAIResponsesRejectedFieldRetries {
		return false
	}
	s.seenBodyHashes[bodyHash] = struct{}{}
	s.budget.attempts++
	return true
}

func (s *openAIResponsesRejectedFieldRetryState) remember(body []byte) {
	if s == nil || len(body) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberLocked(body)
}

func (s *openAIResponsesRejectedFieldRetryState) rememberLocked(body []byte) {
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
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "response.error.code").String()))
	}
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	if message == "" {
		message = strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "response.error.message").String()))
	}
	param := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "error.param").String()))
	if param == "" {
		param = strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "response.error.param").String()))
	}
	if code == "invalid_function_parameters" &&
		openAIResponsesToolParametersParamPattern.MatchString(param) &&
		openAIResponsesMissingSchemaTypePattern.MatchString(message) {
		retryBody, changed, err := sanitizeOpenAIResponsesToolParameterTypes(body)
		if err != nil {
			return nil, "", false, fmt.Errorf("repair rejected tool parameter root type: %w", err)
		}
		if changed {
			return retryBody, "tool parameter root type rejection", true, nil
		}
	}
	messageParam := openAIResponsesRejectedParamFromMessage(message)
	if param != "" && messageParam != "" && param != messageParam {
		return nil, "", false, nil
	}
	if param == "" {
		param = messageParam
	}
	cacheMessageParam := openAIResponsesCacheModelRejectionParamFromMessage(message)
	cacheParam := param
	if cacheParam == "" {
		cacheParam = cacheMessageParam
	}
	cacheParamMatchesMessage := cacheMessageParam == "" || cacheParam == cacheMessageParam
	cacheModelRejection := code == "invalid_parameter" || cacheMessageParam != ""
	if cacheParam != "" && cacheParamMatchesMessage && cacheModelRejection {
		if cacheParam == "prompt_cache_breakpoint" && gjson.GetBytes(body, cacheParam).Exists() {
			retryBody, err := sjson.DeleteBytes(body, cacheParam)
			if err != nil {
				return nil, "", false, fmt.Errorf("delete rejected prompt_cache_breakpoint: %w", err)
			}
			return retryBody, "prompt_cache_breakpoint parameter rejection", true, nil
		}
		if index, ok := openAIResponsesRejectedCacheIndex(cacheParam); ok {
			return removeOpenAIResponsesRejectedCacheAtIndex(body, index)
		}
	}
	messageContentParam := openAIResponsesInvalidTypeParamFromMessage(message)
	contentParam := param
	if contentParam == "" {
		contentParam = messageContentParam
	}
	if index, ok := openAIResponsesRejectedContentIndex(contentParam); ok &&
		contentParam == messageContentParam && isExplicitOpenAIResponsesNullContentRejection(code, message) {
		return normalizeOpenAIResponsesRejectedNullContentAtIndex(body, index)
	}
	responsesLiteCompactionRejection := code == "unsupported_value" &&
		param == "compact_threshold" && strings.Contains(message, "does not support server-side compaction")
	responsesInputIDPrefixRejection := code == "invalid_value" &&
		openAIResponsesRejectedIDParamPattern.MatchString(param) && strings.Contains(message, "expected an id that begins with")
	responsesZeroLengthContentRejection := code == "array_above_max_length" &&
		strings.HasSuffix(param, ".content") && strings.Contains(message, "expected an array with maximum length 0")
	if !isExplicitOpenAIResponsesFieldRejection(code, message) &&
		!responsesLiteCompactionRejection && !responsesInputIDPrefixRejection && !responsesZeroLengthContentRejection {
		return nil, "", false, nil
	}
	if index, ok := openAIResponsesMissingEncryptedContentIndex(param); ok && code == "missing_required_parameter" {
		return removeOpenAIResponsesItemMissingEncryptedContent(body, index)
	}
	if index, ok := openAIResponsesRejectedNamespaceIndex(param); ok {
		return removeOpenAIResponsesRejectedNamespaceAtIndex(body, index)
	}
	if index, ok := openAIResponsesRejectedStatusIndex(param); ok {
		return removeOpenAIResponsesRejectedStatusAtIndex(body, index)
	}
	if index, ok := openAIResponsesRejectedSummaryIndex(param); ok {
		return removeOpenAIResponsesRejectedSummaryAtIndex(body, index)
	}
	if index, ok := openAIResponsesRejectedArgumentsIndex(param); ok {
		return backfillOpenAIResponsesFunctionCallArguments(body, index)
	}
	if index, ok := openAIResponsesRejectedIDIndex(param); ok {
		return removeOpenAIResponsesRejectedID(body, index, message)
	}
	if param == "max_output_tokens" && gjson.GetBytes(body, "max_output_tokens").Exists() {
		retryBody, err := sjson.DeleteBytes(body, "max_output_tokens")
		if err != nil {
			return nil, "", false, fmt.Errorf("delete rejected max_output_tokens: %w", err)
		}
		return retryBody, "max_output_tokens parameter rejection", true, nil
	}
	if param == "truncation" && gjson.GetBytes(body, "truncation").Exists() {
		retryBody, err := sjson.DeleteBytes(body, "truncation")
		if err != nil {
			return nil, "", false, fmt.Errorf("delete rejected truncation: %w", err)
		}
		return retryBody, "truncation parameter rejection", true, nil
	}

	maxZeroContentParam := openAIResponsesMaxZeroContentParamFromMessage(message)
	if index, ok := openAIResponsesRejectedContentIndex(param); ok &&
		param == maxZeroContentParam && code == "array_above_max_length" {
		if retryBody, reason, changed, err := removeOpenAIResponsesRejectedReasoningContentAtIndex(body, index); err != nil || changed {
			return retryBody, reason, changed, err
		}
	}
	if (param == "context_management" || strings.HasPrefix(param, "context_management[") || responsesLiteCompactionRejection) &&
		gjson.GetBytes(body, "context_management").Exists() {
		retryBody, err := sjson.DeleteBytes(body, "context_management")
		if err != nil {
			return nil, "", false, fmt.Errorf("delete rejected context_management: %w", err)
		}
		return retryBody, "context_management parameter rejection", true, nil
	}
	if index, field, ok := openAIResponsesRejectedIndexedParam(param); ok {
		if responsesZeroLengthContentRejection && field == "content" {
			return removeOpenAIResponsesRejectedZeroLengthContent(body, index)
		}
		if !isExplicitOpenAIResponsesUnknownParameter(code, message) {
			return nil, "", false, nil
		}
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

func removeOpenAIResponsesRejectedZeroLengthContent(body []byte, rejectedIndex int) ([]byte, string, bool, error) {
	itemType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, fmt.Sprintf("input.%d.type", rejectedIndex)).String()))
	// Messages carry their actual user/assistant text in content. Never delete
	// that text automatically even if a future upstream validation error uses
	// the same code; this retry is only for non-message history items whose
	// schema explicitly reports that content has a maximum length of zero.
	if itemType == "" || itemType == "message" {
		return nil, "", false, nil
	}
	return removeOpenAIResponsesRejectedFieldForItemType(body, rejectedIndex, "content")
}

func openAIResponsesMissingEncryptedContentIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesMissingEncryptedParamPattern, param)
}

// removeOpenAIResponsesItemMissingEncryptedContent drops one history item that
// cannot be replayed. Encrypted reasoning/compaction state is opaque: when the
// client omitted encrypted_content the gateway cannot reconstruct it, and
// switching accounts only repeats the same deterministic 400. Keep every other
// input item intact and let the same account retry once through the bounded
// rejected-field retry state.
func removeOpenAIResponsesItemMissingEncryptedContent(body []byte, rejectedIndex int) ([]byte, string, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, "", false, fmt.Errorf("decode missing encrypted_content retry body: %w", err)
	}
	input, _ := requestBody["input"].([]any)
	if rejectedIndex < 0 || rejectedIndex >= len(input) || len(input) <= 1 {
		return nil, "", false, nil
	}
	item, _ := input[rejectedIndex].(map[string]any)
	if item == nil {
		return nil, "", false, nil
	}
	switch strings.ToLower(strings.TrimSpace(stringFromAny(item["type"]))) {
	case "reasoning", "compaction", "compaction_summary":
	default:
		return nil, "", false, nil
	}
	if _, present := item["encrypted_content"]; present {
		return nil, "", false, nil
	}
	requestBody["input"] = append(input[:rejectedIndex:rejectedIndex], input[rejectedIndex+1:]...)
	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, "", false, fmt.Errorf("encode missing encrypted_content retry body: %w", err)
	}
	return retryBody, "missing encrypted_content history item", true, nil
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

func removeOpenAIResponsesRejectedID(body []byte, index int, rejectionMessage string) ([]byte, string, bool, error) {
	idPath := fmt.Sprintf("input.%d.id", index)
	id := gjson.GetBytes(body, idPath)
	if id.Type != gjson.String {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, idPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected Responses input ID: %w", err)
	}
	reason := "invalid input item id"
	if strings.Contains(strings.ToLower(rejectionMessage), "string too long") {
		reason = "overlong input item id"
	}
	return retryBody, reason, true, nil
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

func openAIResponsesMaxZeroContentParamFromMessage(message string) string {
	match := openAIResponsesMaxZeroContentMessagePattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func openAIResponsesInvalidTypeParamFromMessage(message string) string {
	match := openAIResponsesInvalidTypeMessageParamPattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func openAIResponsesCacheModelRejectionParamFromMessage(message string) string {
	match := openAIResponsesCacheModelRejectionPattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(match[1]))
}

func isExplicitOpenAIResponsesNullContentRejection(code, message string) bool {
	code = strings.TrimSpace(code)
	return (code == "invalid_type" || code == "invalid_request_error" || code == "") &&
		openAIResponsesInvalidTypeMessageParamPattern.MatchString(strings.TrimSpace(message))
}

func openAIResponsesRejectedNamespaceIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedNamespaceParamPattern, param)
}

func openAIResponsesRejectedStatusIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedStatusParamPattern, param)
}

func openAIResponsesRejectedContentIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedContentParamPattern, param)
}

func openAIResponsesRejectedCacheIndex(param string) (int, bool) {
	return openAIResponsesRejectedIndexedField(openAIResponsesRejectedCacheParamPattern, param)
}

func removeOpenAIResponsesRejectedStatusAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	itemPath := fmt.Sprintf("input.%d", index)
	if !gjson.GetBytes(body, itemPath).IsObject() {
		return nil, "", false, nil
	}
	statusPath := itemPath + ".status"
	if !gjson.GetBytes(body, statusPath).Exists() {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, statusPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected status at input[%d]: %w", index, err)
	}
	return retryBody, "indexed status parameter rejection", true, nil
}

func removeOpenAIResponsesRejectedCacheAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	itemPath := fmt.Sprintf("input.%d", index)
	if !gjson.GetBytes(body, itemPath).IsObject() {
		return nil, "", false, nil
	}
	cachePath := itemPath + ".prompt_cache_breakpoint"
	if !gjson.GetBytes(body, cachePath).Exists() {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, cachePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected prompt_cache_breakpoint at input[%d]: %w", index, err)
	}
	return retryBody, "indexed prompt_cache_breakpoint parameter rejection", true, nil
}

func normalizeOpenAIResponsesRejectedNullContentAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	itemPath := fmt.Sprintf("input.%d", index)
	item := gjson.GetBytes(body, itemPath)
	content := gjson.GetBytes(body, itemPath+".content")
	if !item.IsObject() || !content.Exists() || content.Type != gjson.Null {
		return nil, "", false, nil
	}

	itemType := strings.ToLower(strings.TrimSpace(item.Get("type").String()))
	role := strings.TrimSpace(item.Get("role").String())
	contentPath := itemPath + ".content"
	switch {
	case itemType == "reasoning":
		retryBody, err := sjson.DeleteBytes(body, contentPath)
		if err != nil {
			return nil, "", false, fmt.Errorf("delete rejected null content at input[%d]: %w", index, err)
		}
		return retryBody, "indexed reasoning null content rejection", true, nil
	case itemType == "message" || role != "":
		retryBody, err := sjson.SetBytes(body, contentPath, "")
		if err != nil {
			return nil, "", false, fmt.Errorf("normalize rejected null content at input[%d]: %w", index, err)
		}
		return retryBody, "indexed message null content rejection", true, nil
	default:
		return nil, "", false, nil
	}
}

func removeOpenAIResponsesRejectedReasoningContentAtIndex(body []byte, index int) ([]byte, string, bool, error) {
	itemPath := fmt.Sprintf("input.%d", index)
	item := gjson.GetBytes(body, itemPath)
	content := item.Get("content")
	if !item.IsObject() || strings.TrimSpace(item.Get("type").String()) != "reasoning" || !content.IsArray() || len(content.Array()) == 0 {
		return nil, "", false, nil
	}
	retryBody, err := sjson.DeleteBytes(body, itemPath+".content")
	if err != nil {
		return nil, "", false, fmt.Errorf("delete rejected reasoning content at input[%d]: %w", index, err)
	}
	return retryBody, "indexed reasoning content maximum-length rejection", true, nil
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
