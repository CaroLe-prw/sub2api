package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAIWSClientReadLimitBytesDefault     int64 = 64 * 1024 * 1024
	openAIWSHTTPBridgeThresholdBytesDefault int64 = 15 * 1024 * 1024
	openAIWSHTTPBridgeErrorBodyLimitBytes         = 64 * 1024
)

const openAIWSHTTPBridgeToolStateContextKey = "openai_ws_http_bridge_tool_state"

type openAIWSHTTPBridgeToolState struct {
	ClientMapping apicompat.ResponsesClientToolMapping
	LoweredTools  json.RawMessage
}

func openAIWSHTTPBridgeToolStateFromContext(c *gin.Context) (openAIWSHTTPBridgeToolState, bool) {
	if c == nil {
		return openAIWSHTTPBridgeToolState{}, false
	}
	value, ok := c.Get(openAIWSHTTPBridgeToolStateContextKey)
	state, typed := value.(openAIWSHTTPBridgeToolState)
	return state, ok && typed
}

func setOpenAIWSHTTPBridgeToolState(c *gin.Context, state openAIWSHTTPBridgeToolState) {
	if c == nil {
		return
	}
	state.LoweredTools = append(json.RawMessage(nil), state.LoweredTools...)
	c.Set(openAIWSHTTPBridgeToolStateContextKey, state)
}

func decodeOpenAIWSHTTPBridgeLoweredTools(raw json.RawMessage) []any {
	if len(raw) == 0 {
		return nil
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return nil
	}
	return tools
}

func openAIWSHTTPBridgeRawField(body []byte, name string) (json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, false
	}
	raw, present := fields[name]
	return append(json.RawMessage(nil), raw...), present
}

func openAIWSHTTPBridgeToolUpstreamName(account *Account) string {
	if account != nil && account.Platform == PlatformGrok {
		return "Grok WS HTTP bridge"
	}
	return "OpenAI WS HTTP bridge"
}

// ResolveOpenAIWSClientFirstMessageTimeout returns the effective client ingress deadline.
func ResolveOpenAIWSClientFirstMessageTimeout(cfg *config.Config) time.Duration {
	seconds := config.DefaultOpenAIWSClientFirstMessageTimeoutSeconds
	if cfg != nil && cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds > 0 {
		seconds = cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func ResolveOpenAIWSClientReadLimitBytes(cfg *config.Config) int64 {
	if cfg == nil || cfg.Gateway.OpenAIWS.ClientReadLimitBytes <= 0 {
		return openAIWSClientReadLimitBytesDefault
	}
	return cfg.Gateway.OpenAIWS.ClientReadLimitBytes
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.HTTPBridgeEnabled
}

func (s *OpenAIGatewayService) openAIWSHTTPBridgeThresholdBytes() int64 {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes <= 0 {
		return openAIWSHTTPBridgeThresholdBytesDefault
	}
	return s.cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSHTTP(account *Account, payloadBytes int, previousResponseID string) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if !s.openAIWSHTTPBridgeEnabled() {
		return false
	}
	if strings.TrimSpace(previousResponseID) != "" {
		return false
	}
	threshold := s.openAIWSHTTPBridgeThresholdBytes()
	return threshold > 0 && int64(payloadBytes) >= threshold
}

func (s *OpenAIGatewayService) shouldBridgeOpenAIWSPassthroughFirstMessage(account *Account, payload []byte) bool {
	if account != nil && account.Platform == PlatformGrok {
		return true
	}
	if !s.openAIWSHTTPBridgeEnabled() || int64(len(payload)) < s.openAIWSHTTPBridgeThresholdBytes() {
		return false
	}
	if !json.Valid(payload) {
		return false
	}

	i := skipOpenAIWSJSONSpace(payload, 0)
	if i >= len(payload) || payload[i] != '{' {
		return false
	}
	i++
	eventType := "response.create"
	previousResponseID := ""
	typeSeen, previousResponseIDSeen := false, false
	for {
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == '}' {
			break
		}
		keyStart := i
		keyEnd := scanOpenAIWSJSONString(payload, keyStart)
		i = skipOpenAIWSJSONSpace(payload, keyEnd)
		i++ // json.Valid guarantees the colon.
		i = skipOpenAIWSJSONSpace(payload, i)
		valueStart := i
		i = skipOpenAIWSJSONValue(payload, i)

		key := ""
		// A critical key is at most 20 decoded bytes. The generous encoded bound
		// covers escaped spellings without allocating attacker-sized key strings.
		if keyEnd-keyStart <= 128 {
			_ = json.Unmarshal(payload[keyStart:keyEnd], &key)
		}
		switch key {
		case "type":
			if typeSeen {
				return false
			}
			typeSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value == nil || strings.TrimSpace(*value) == "" {
				eventType = "response.create"
			} else {
				eventType = strings.TrimSpace(*value)
			}
		case "previous_response_id":
			if previousResponseIDSeen {
				return false
			}
			previousResponseIDSeen = true
			var value *string
			if err := json.Unmarshal(payload[valueStart:i], &value); err != nil {
				return false
			}
			if value != nil {
				previousResponseID = strings.TrimSpace(*value)
			}
		}
		i = skipOpenAIWSJSONSpace(payload, i)
		if payload[i] == ',' {
			i++
		}
	}
	return eventType == "response.create" && previousResponseID == ""
}

func skipOpenAIWSJSONSpace(payload []byte, i int) int {
	for i < len(payload) {
		switch payload[i] {
		case ' ', '\t', '\r', '\n':
			i++
		default:
			return i
		}
	}
	return i
}

func scanOpenAIWSJSONString(payload []byte, i int) int {
	for i++; i < len(payload); i++ {
		switch payload[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(payload)
}

func skipOpenAIWSJSONValue(payload []byte, i int) int {
	if payload[i] == '"' {
		return scanOpenAIWSJSONString(payload, i)
	}
	if payload[i] != '{' && payload[i] != '[' {
		for i < len(payload) && payload[i] != ',' && payload[i] != '}' {
			i++
		}
		return i
	}
	depth := 0
	for ; i < len(payload); i++ {
		switch payload[i] {
		case '"':
			i = scanOpenAIWSJSONString(payload, i) - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(payload)
}

func prepareOpenAIWSHTTPBridgeBody(account *Account, payload []byte) ([]byte, error) {
	var body map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("response.create payload must be a JSON object")
	}
	delete(body, "type")
	delete(body, "generate")
	deleteOpenAIResponsesNoneReasoningEffortFromObject(account, body)
	body["stream"] = true
	return json.Marshal(body)
}

type openAIWSToolCallReplayCollector struct {
	items    []json.RawMessage
	indexes  map[string]int
	allItems []json.RawMessage
	allSeen  map[string]struct{}
}

func (c *openAIWSToolCallReplayCollector) AddEvent(eventType string, message []byte) {
	switch strings.TrimSpace(eventType) {
	case "response.function_call_arguments.delta":
		c.addFunctionCallArgumentsEvent(message, true)
	case "response.function_call_arguments.done":
		c.addFunctionCallArgumentsEvent(message, false)
	case "response.output_item.done":
		item := gjson.GetBytes(message, "item")
		c.addAllItem(item)
		c.addItem(item)
	case "response.completed", "response.done":
		output := gjson.GetBytes(message, "response.output")
		if !output.IsArray() {
			return
		}
		for _, item := range output.Array() {
			c.addAllItem(item)
			c.addItem(item)
		}
	}
}

func (c *openAIWSToolCallReplayCollector) Items() []json.RawMessage {
	items := cloneOpenAIWSRawMessages(c.items)
	for index, raw := range items {
		if strings.TrimSpace(gjson.GetBytes(raw, "type").String()) != "function_call" {
			continue
		}
		arguments := gjson.GetBytes(raw, "arguments")
		if arguments.Exists() {
			if arguments.Type == gjson.String {
				continue
			}
			if arguments.Type == gjson.JSON && strings.TrimSpace(arguments.Raw) != "" && arguments.Raw != "null" {
				updated, err := sjson.SetBytes(raw, "arguments", arguments.Raw)
				if err == nil {
					items[index] = updated
				}
				continue
			}
		}
		// Some compatible upstreams omit arguments from output_item.done and
		// response.completed even for a no-argument function. HTTP /responses
		// requires the field when that item is replayed as input. Delta/done
		// events are merged above first; only use {} as the final no-argument
		// fallback when the stream never supplied a value.
		updated, err := sjson.SetBytes(raw, "arguments", "{}")
		if err == nil {
			items[index] = updated
		}
	}
	return items
}

func (c *openAIWSToolCallReplayCollector) AllItems() []json.RawMessage {
	return cloneOpenAIWSRawMessages(c.allItems)
}

func (c *openAIWSToolCallReplayCollector) addAllItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") || strings.TrimSpace(item.Get("type").String()) == "" {
		return
	}
	key := strings.TrimSpace(item.Get("id").String())
	if key == "" {
		key = strings.TrimSpace(item.Get("call_id").String())
	}
	if key == "" {
		key = raw
	}
	if c.allSeen == nil {
		c.allSeen = make(map[string]struct{})
	}
	if _, ok := c.allSeen[key]; ok {
		return
	}
	c.allSeen[key] = struct{}{}
	c.allItems = append(c.allItems, json.RawMessage(raw))
}

func (c *openAIWSToolCallReplayCollector) addItem(item gjson.Result) {
	if !item.Exists() || item.Type != gjson.JSON {
		return
	}
	raw := strings.TrimSpace(item.Raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return
	}
	if !isCodexToolCallContextItemType(item.Get("type").String()) {
		return
	}
	c.mergeItem(json.RawMessage(raw))
}

func (c *openAIWSToolCallReplayCollector) addFunctionCallArgumentsEvent(message []byte, delta bool) {
	itemID := strings.TrimSpace(gjson.GetBytes(message, "item_id").String())
	callID := strings.TrimSpace(gjson.GetBytes(message, "call_id").String())
	name := strings.TrimSpace(gjson.GetBytes(message, "name").String())
	argumentPath := "arguments"
	if delta {
		argumentPath = "delta"
	}
	arguments := gjson.GetBytes(message, argumentPath)
	if itemID == "" && callID == "" {
		return
	}

	item := map[string]any{"type": "function_call"}
	if itemID != "" {
		item["id"] = itemID
	}
	if callID != "" {
		item["call_id"] = callID
	}
	if name != "" {
		item["name"] = name
	}
	if arguments.Exists() {
		item["arguments"] = arguments.String()
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return
	}
	c.mergeItemWithArgumentMode(raw, delta)
}

func (c *openAIWSToolCallReplayCollector) mergeItem(raw json.RawMessage) {
	c.mergeItemWithArgumentMode(raw, false)
}

func (c *openAIWSToolCallReplayCollector) mergeItemWithArgumentMode(raw json.RawMessage, appendArguments bool) {
	var incoming map[string]any
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return
	}
	keys := openAIWSReplayItemKeys(incoming)
	if len(keys) == 0 {
		keys = []string{"raw:" + string(raw)}
	}
	index := -1
	for _, key := range keys {
		if existing, ok := c.indexes[key]; ok {
			index = existing
			break
		}
	}
	if index < 0 {
		index = len(c.items)
		c.items = append(c.items, json.RawMessage(append([]byte(nil), raw...)))
	} else {
		var existing map[string]any
		if err := json.Unmarshal(c.items[index], &existing); err != nil {
			return
		}
		// Compatible upstreams can describe the same custom call as a
		// custom_tool_call in output_item.done and as a function_call in the
		// completed response. Never let the latter overwrite the former: doing
		// so leaves a function_call carrying the custom-only `input` field, which
		// the Responses HTTP endpoint rejects when the item is replayed.
		existingType := strings.TrimSpace(stringFromAny(existing["type"]))
		incomingType := strings.TrimSpace(stringFromAny(incoming["type"]))
		preserveCustomToolType := existingType == "custom_tool_call" || incomingType == "custom_tool_call"
		for key, value := range incoming {
			if key == "type" && preserveCustomToolType {
				existing[key] = "custom_tool_call"
				continue
			}
			if key == "arguments" && appendArguments {
				previous, _ := existing[key].(string)
				next, _ := value.(string)
				existing[key] = previous + next
				continue
			}
			if !openAIWSReplayValuePresent(existing[key]) || openAIWSReplayValuePresent(value) {
				existing[key] = value
			}
		}
		merged, err := json.Marshal(existing)
		if err != nil {
			return
		}
		c.items[index] = merged
	}
	if c.indexes == nil {
		c.indexes = make(map[string]int)
	}
	var merged map[string]any
	if err := json.Unmarshal(c.items[index], &merged); err != nil {
		return
	}
	for _, key := range openAIWSReplayItemKeys(merged) {
		c.indexes[key] = index
	}
	if len(openAIWSReplayItemKeys(merged)) == 0 {
		c.indexes["raw:"+string(c.items[index])] = index
	}
}

func openAIWSReplayItemKeys(item map[string]any) []string {
	keys := make([]string, 0, 2)
	if id := strings.TrimSpace(stringFromAny(item["id"])); id != "" {
		keys = append(keys, "id:"+id)
	}
	if callID := strings.TrimSpace(stringFromAny(item["call_id"])); callID != "" {
		keys = append(keys, "call_id:"+callID)
	}
	return keys
}

func openAIWSReplayValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func buildOpenAIWSHTTPBridgeErrorEvent(statusCode int, message string) []byte {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = "upstream request failed"
	}
	event := map[string]any{
		"type":   "error",
		"status": statusCode,
		"error": map[string]any{
			"type":    "upstream_error",
			"message": message,
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return []byte(`{"type":"error","error":{"type":"upstream_error","message":"upstream request failed"}}`)
	}
	return body
}

func adaptOpenAIWSHTTPBridgeRejectedCustomToolInput(
	statusCode int,
	body []byte,
	responseBody []byte,
) ([]byte, apicompat.ResponsesClientToolMapping, bool, error) {
	if statusCode != http.StatusBadRequest || len(body) == 0 || len(responseBody) == 0 {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	code := strings.ToLower(strings.TrimSpace(extractUpstreamErrorCode(responseBody)))
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(responseBody)))
	if !isExplicitOpenAIResponsesFieldRejection(code, message) {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	param := strings.ToLower(strings.TrimSpace(gjson.GetBytes(responseBody, "error.param").String()))
	if param == "" {
		param = openAIResponsesRejectedParamFromMessage(message)
	}
	index, ok := openAIResponsesRejectedInputIndex(param)
	if !ok {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	itemPath := fmt.Sprintf("input.%d", index)
	itemType := strings.TrimSpace(gjson.GetBytes(body, itemPath+".type").String())
	if (itemType != "custom_tool_call" && itemType != "function_call") ||
		!gjson.GetBytes(body, itemPath+".input").Exists() {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, false, fmt.Errorf("decode HTTP bridge custom tool request: %w", err)
	}
	mapping, changed, err := apicompat.AdaptResponsesClientTools(requestBody)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, false, err
	}
	input, _ := requestBody["input"].([]any)
	if index < 0 || index >= len(input) {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	item, _ := input[index].(map[string]any)
	if strings.TrimSpace(stringFromAny(item["type"])) != "function_call" {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	if rejectedInput, exists := item["input"]; exists {
		// A mixed replay item can already say function_call while retaining the
		// custom-tool `input` field. The general client-tool adapter intentionally
		// does not guess in that case, so repair the exact field rejected by the
		// upstream. Preserve an existing arguments value; otherwise encode the
		// custom input using the same one-field schema as lowered custom tools.
		if _, hasArguments := item["arguments"]; !hasArguments {
			arguments, marshalErr := json.Marshal(map[string]any{"input": rejectedInput})
			if marshalErr != nil {
				return nil, apicompat.ResponsesClientToolMapping{}, false, fmt.Errorf("encode HTTP bridge function arguments: %w", marshalErr)
			}
			item["arguments"] = string(arguments)
		}
		delete(item, "input")
		changed = true
	}
	if !changed {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	retryBody, err := marshalOpenAIUpstreamJSON(requestBody)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, false, fmt.Errorf("encode HTTP bridge custom tool request: %w", err)
	}
	if strings.TrimSpace(gjson.GetBytes(retryBody, itemPath+".type").String()) != "function_call" ||
		gjson.GetBytes(retryBody, itemPath+".input").Exists() ||
		!gjson.GetBytes(retryBody, itemPath+".arguments").Exists() {
		return nil, apicompat.ResponsesClientToolMapping{}, false, nil
	}
	return retryBody, mapping, true, nil
}

func normalizeOpenAIWSHTTPBridgeRejectedRequest(
	statusCode int,
	body []byte,
	responseBody []byte,
) ([]byte, apicompat.ResponsesClientToolMapping, string, bool, error) {
	retryBody, mapping, changed, err := adaptOpenAIWSHTTPBridgeRejectedCustomToolInput(statusCode, body, responseBody)
	if err != nil {
		return nil, apicompat.ResponsesClientToolMapping{}, "", false, err
	}
	if changed {
		return retryBody, mapping, "custom tool input parameter rejection", true, nil
	}
	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(statusCode, body, responseBody)
	return retryBody, apicompat.ResponsesClientToolMapping{}, reason, changed, err
}

type openAIWSHTTPBridgePrefetchedBody struct {
	io.Reader
	closer io.Closer
}

func (b *openAIWSHTTPBridgePrefetchedBody) Close() error {
	if b == nil || b.closer == nil {
		return nil
	}
	return b.closer.Close()
}

func prefetchOpenAIWSHTTPBridgeFirstSSEEvent(body io.ReadCloser) (io.ReadCloser, []byte, error) {
	if body == nil {
		return nil, nil, errors.New("upstream response body is nil")
	}
	reader := bufio.NewReaderSize(body, openAIWSHTTPBridgeErrorBodyLimitBytes)
	prefix := make([]byte, 0, openAIWSHTTPBridgeErrorBodyLimitBytes)
	replayBody := func() io.ReadCloser {
		return &openAIWSHTTPBridgePrefetchedBody{
			Reader: io.MultiReader(bytes.NewReader(prefix), reader),
			closer: body,
		}
	}
	for {
		line, readErr := reader.ReadSlice('\n')
		prefix = append(prefix, line...)
		if len(prefix) > openAIWSHTTPBridgeErrorBodyLimitBytes {
			return replayBody(), nil, nil
		}
		if readErr == nil || errors.Is(readErr, io.EOF) {
			if data, ok := extractOpenAISSEDataLine(string(line)); ok {
				trimmed := strings.TrimSpace(data)
				if trimmed != "" && trimmed != "[DONE]" {
					event := []byte(trimmed)
					switch strings.TrimSpace(gjson.GetBytes(event, "type").String()) {
					case "response.created", "response.in_progress", "response.queued":
						// These preamble events do not commit useful output. Keep
						// buffering so a following schema error can still be retried.
					default:
						return replayBody(), event, nil
					}
				}
			}
		}
		switch {
		case readErr == nil:
			continue
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			return replayBody(), nil, nil
		default:
			_ = body.Close()
			return nil, nil, readErr
		}
	}
}

func openAIWSHTTPBridgeErrorEventStatus(message []byte) int {
	for _, path := range []string{
		"error.status_code",
		"error.status",
		"response.error.status_code",
		"response.error.status",
		"status_code",
		"status",
	} {
		status := int(gjson.GetBytes(message, path).Int())
		if status >= 400 && status <= 599 {
			return status
		}
	}
	return openAIWSErrorHTTPStatus(message)
}

func openAIWSHTTPBridgeRejectedEventBody(message []byte) ([]byte, bool) {
	switch strings.TrimSpace(gjson.GetBytes(message, "type").String()) {
	case "error":
		return message, true
	case "response.failed":
		responseError := gjson.GetBytes(message, "response.error")
		if !responseError.Exists() || !responseError.IsObject() {
			return nil, false
		}
		return []byte(`{"error":` + responseError.Raw + `}`), true
	default:
		return nil, false
	}
}

func buildOpenAIWSHTTPBridgeFailedEvent(responseID, model string, source []byte, fallbackMessage string) []byte {
	errorType := strings.TrimSpace(gjson.GetBytes(source, "error.type").String())
	if errorType == "" {
		errorType = strings.TrimSpace(gjson.GetBytes(source, "response.error.type").String())
	}
	code := strings.TrimSpace(gjson.GetBytes(source, "error.code").String())
	if code == "" {
		code = strings.TrimSpace(gjson.GetBytes(source, "response.error.code").String())
	}
	if code == "" {
		code = "upstream_error"
	}
	message := extractOpenAISSEErrorMessage(source)
	if message == "" {
		message = strings.TrimSpace(fallbackMessage)
	}
	if message == "" {
		message = "Upstream response failed"
	}
	errorBody := map[string]any{"code": code, "message": message}
	if errorType != "" {
		errorBody["type"] = errorType
	}
	response := map[string]any{
		"id": responseID, "object": "response", "status": "failed",
		"output": []any{}, "error": errorBody,
	}
	if model = strings.TrimSpace(model); model != "" {
		response["model"] = model
	}
	body, err := json.Marshal(map[string]any{"type": "response.failed", "response": response})
	if err != nil {
		return []byte(`{"type":"response.failed","response":{"status":"failed","output":[],"error":{"code":"upstream_error","message":"Upstream response failed"}}}`)
	}
	return body
}

func (s *OpenAIGatewayService) proxyOpenAIWSHTTPBridgeTurn(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	token string,
	payload []byte,
	payloadBytes int,
	originalModel string,
	imageBillingModel string,
	imageSizeTier string,
	imageInputSize string,
	grokCacheIdentity string,
	turn int,
	writeClientMessage func([]byte) error,
) (*OpenAIForwardResult, error) {
	if s == nil {
		return nil, errors.New("service is nil")
	}
	if s.httpUpstream == nil {
		return nil, errors.New("openai http upstream is nil")
	}
	if account == nil {
		return nil, errors.New("account is nil")
	}
	if writeClientMessage == nil {
		return nil, errors.New("client websocket writer is nil")
	}
	responseModelObserver := &upstreamResponseModelObserver{}

	responsesLite := !hasOpenAIServerSideCompactionInBody(payload) &&
		isOpenAIResponsesLiteRequestedForPayload(c, account, payload, originalModel)
	payload, _, err := stripOpenAIResponsesLiteWSMetadataForUnsupportedModel(payload, account, originalModel)
	if err != nil {
		return nil, err
	}
	payload, _, err = stripOpenAIResponsesLiteWSMetadataForCompaction(payload)
	if err != nil {
		return nil, err
	}
	payload, _, err = stripOpenAIResponsesLiteInputForCompaction(payload)
	if err != nil {
		return nil, err
	}
	if responsesLite {
		litePayload, _, liteErr := normalizeOpenAIResponsesLitePayloadForAccount(payload, account)
		if liteErr != nil {
			return nil, liteErr
		}
		payload = litePayload
	}
	body, err := prepareOpenAIWSHTTPBridgeBody(account, payload)
	if err != nil {
		return nil, fmt.Errorf("prepare http bridge body: %w", err)
	}
	grokIntentSourceBody := append([]byte(nil), body...)
	_, grokExplicitToolsField := openAIWSHTTPBridgeRawField(grokIntentSourceBody, "tools")
	grokExplicitToolIntent := account.Platform == PlatformGrok && hasGrokResponsesToolIntent(grokIntentSourceBody)
	var clientToolMapping apicompat.ResponsesClientToolMapping
	functionToolUpstream := (account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey) || account.Platform == PlatformGrok
	if functionToolUpstream {
		if account.Platform == PlatformGrok {
			body, err = sanitizeGrokResponsesInput(body)
			if err != nil {
				return nil, fmt.Errorf("sanitize Grok WS HTTP bridge input: %w", err)
			}
		}
		inheritedState, _ := openAIWSHTTPBridgeToolStateFromContext(c)
		inheritedLoweredTools := decodeOpenAIWSHTTPBridgeLoweredTools(inheritedState.LoweredTools)
		body, clientToolMapping, err = adaptResponsesClientToolsForFunctionUpstreamWithMapping(
			body,
			openAIWSHTTPBridgeToolUpstreamName(account),
			inheritedState.ClientMapping,
			inheritedLoweredTools,
		)
		if err != nil {
			return nil, fmt.Errorf("adapt %s client tools: %w", openAIWSHTTPBridgeToolUpstreamName(account), err)
		}
		if account.Platform == PlatformGrok && !grokExplicitToolsField && !grokExplicitToolIntent && len(inheritedLoweredTools) > 0 && hasGrokResponsesToolIntent(body) {
			// This continuation omitted tools, so the pre-adapter source cannot
			// represent the effective inherited declarations. Cache routing must
			// see the rehydrated tool intent or it will replace client functions
			// with the native-search tool-free route. Explicit current-turn tool
			// intent still uses the original pre-sanitization source above.
			grokIntentSourceBody = append(grokIntentSourceBody[:0], body...)
		}
		loweredTools := inheritedState.LoweredTools
		if currentTools, present := openAIWSHTTPBridgeRawField(body, "tools"); present {
			loweredTools = currentTools
		}
		setOpenAIWSHTTPBridgeToolState(c, openAIWSHTTPBridgeToolState{
			ClientMapping: clientToolMapping,
			LoweredTools:  loweredTools,
		})
	}
	if account.Platform != PlatformGrok && responsesLite {
		liteBody, liteChanged, liteErr := normalizeOpenAIResponsesLitePayloadForAccount(body, account)
		if liteErr != nil {
			return nil, fmt.Errorf("normalize responses Lite payload: %w", liteErr)
		}
		if liteChanged {
			body = liteBody
		}
	}

	buildUpstreamRequest := func(requestBody []byte) (*http.Request, error) {
		upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
		defer releaseUpstreamCtx()
		var upstreamReq *http.Request
		var buildErr error
		if account.Platform == PlatformGrok {
			upstreamReq, buildErr = buildGrokResponsesRequest(upstreamCtx, c, account, requestBody, token, grokCacheIdentity, s.cfg, s.settingService)
		} else {
			upstreamReq, buildErr = s.buildUpstreamRequestOpenAIPassthrough(upstreamCtx, c, account, requestBody, token)
		}
		if buildErr != nil {
			return nil, buildErr
		}
		if account.Platform != PlatformGrok && responsesLite {
			upstreamReq.Header.Set(responsesLiteHeader, "true")
		}
		return upstreamReq, nil
	}
	if account.Platform == PlatformGrok {
		upstreamModel := resolveGrokWSUpstreamModel(account, body, originalModel)
		body, err = patchGrokResponsesBody(body, upstreamModel)
		if err != nil {
			return nil, err
		}
		grokMixedCacheIntentBody := append([]byte(nil), body...)
		body, err = applyGrokResponsesCacheIdentity(body, grokIntentSourceBody, grokCacheIdentity, account.IsGrokOAuth())
		if err != nil {
			return nil, fmt.Errorf("apply grok prompt cache identity: %w", err)
		}
		body, err = applyGrokFreeRequestToolCacheRoute(c, body, grokMixedCacheIntentBody, account, grokCacheIdentity)
		if err != nil {
			return nil, fmt.Errorf("apply grok Free function-tool cache route: %w", err)
		}
	}
	actualModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if actualModel == "" {
		actualModel = canonicalOpenAIAccountSchedulingModel(account, originalModel)
	}
	SetOpsUpstreamModel(c, actualModel)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	if c != nil {
		c.Set("openai_passthrough", true)
		c.Set("openai_ws_http_bridge", true)
	}

	turnStart := time.Now()
	rejectedFieldRetryState := newOpenAIResponsesRejectedFieldRetryState(body)
	var resp *http.Response
	for {
		upstreamReq, buildErr := buildUpstreamRequest(body)
		if buildErr != nil {
			return nil, buildErr
		}
		resp, err = s.doOpenAIUpstream(upstreamReq, proxyURL, account)
		if err != nil {
			if turn == 1 {
				return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
			}
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			clientError := buildOpenAIWSHTTPBridgeErrorEvent(http.StatusBadGateway, "Upstream request failed")
			if writeErr := writeClientMessage(clientError); writeErr == nil {
				markOpenAIWSClientVisibleFailure(c, "error", clientError)
			}
			return nil, fmt.Errorf("upstream http bridge request failed: %s", safeErr)
		}
		if resp.StatusCode < http.StatusBadRequest {
			prefetchedBody, firstEvent, prefetchErr := prefetchOpenAIWSHTTPBridgeFirstSSEEvent(resp.Body)
			if prefetchErr != nil {
				return nil, fmt.Errorf("prefetch upstream http bridge SSE response: %w", prefetchErr)
			}
			resp.Body = prefetchedBody
			rejectedEventBody, isRejectedEvent := openAIWSHTTPBridgeRejectedEventBody(firstEvent)
			if !isRejectedEvent {
				break
			}
			statusCode := openAIWSHTTPBridgeErrorEventStatus(firstEvent)
			retryBody, retryMapping, reason, changed, retryErr := normalizeOpenAIWSHTTPBridgeRejectedRequest(statusCode, body, rejectedEventBody)
			if retryErr != nil {
				return nil, fmt.Errorf("normalize http bridge SSE rejected request: %w", retryErr)
			}
			if changed && rejectedFieldRetryState.Allow(retryBody) {
				_ = resp.Body.Close()
				body = retryBody
				payloadBytes = len(body)
				if hasGrokResponsesClientToolMapping(retryMapping) {
					clientToolMapping = retryMapping
				}
				logOpenAIWSModeInfo(
					"ingress_ws_http_bridge_sse_rejected_field_retry account_id=%d turn=%d status=%d reason=%s",
					account.ID, turn, statusCode, normalizeOpenAIWSLogValue(reason),
				)
				continue
			}
			break
		}

		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, openAIWSHTTPBridgeErrorBodyLimitBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read upstream http bridge error response: %w", readErr)
		}
		retryBody, retryMapping, retryReason, changed, retryErr := normalizeOpenAIWSHTTPBridgeRejectedRequest(resp.StatusCode, body, respBody)
		if retryErr != nil {
			return nil, fmt.Errorf("normalize websocket http bridge rejected field retry: %w", retryErr)
		}
		if changed && rejectedFieldRetryState.Allow(retryBody) {
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_rejected_field_retry account_id=%d turn=%d reason=%s",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(retryReason, openAIWSLogValueMaxLen),
			)
			body = retryBody
			payloadBytes = len(body)
			if hasGrokResponsesClientToolMapping(retryMapping) {
				clientToolMapping = retryMapping
			}
			continue
		}

		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(resp.StatusCode)
		}
		shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody)
		if account.Platform == PlatformGrok {
			shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
			s.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.StatusCode, resp.Header, respBody)
			if shouldFailover && (turn == 1 || resp.StatusCode == http.StatusTooManyRequests) {
				return nil, newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg, false,
					shouldRetryOpenAIOAuthCapacityOnSameAccount(account, resp.StatusCode, upstreamMsg, respBody))
			}
		} else if shouldFailover && (turn == 1 || resp.StatusCode == http.StatusTooManyRequests) {
			return nil, s.handleFailoverErrorResponsePassthrough(ctx, resp, c, account, body, respBody)
		}
		if account.Platform != PlatformGrok && (shouldFailover || shouldCooldownOpenAITransientUpstreamError(resp.StatusCode, respBody)) {
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, actualModel)
		}
		clientError := buildOpenAIWSHTTPBridgeErrorEvent(resp.StatusCode, upstreamMsg)
		if writeErr := writeClientMessage(clientError); writeErr == nil {
			markOpenAIWSClientVisibleFailure(c, "error", clientError)
		}
		return nil, fmt.Errorf("upstream http bridge error: status=%d message=%s", resp.StatusCode, upstreamMsg)
	}
	defer func() { _ = resp.Body.Close() }()
	stopCancelBody := context.AfterFunc(ctx, func() { _ = resp.Body.Close() })
	defer stopCancelBody()
	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(withGrokTeamRateLimitModel(ctx, resolveGrokWSUpstreamModel(account, body, originalModel)), account, resp.Header, resp.StatusCode)
	}
	if hasGrokResponsesClientToolMapping(clientToolMapping) {
		maxLineSize := defaultMaxLineSize
		if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
			maxLineSize = s.cfg.Gateway.MaxLineSize
		}
		resp.Body = newGrokResponsesClientToolStreamBody(resp.Body, clientToolMapping, maxLineSize)
	}

	responseID := ""
	usage := OpenAIUsage{}
	imageCounter := newOpenAIImageOutputCounter()
	var firstTokenMs *int
	reqStream := openAIWSPayloadBoolFromRaw(body, "stream", true)
	eventCount := 0
	tokenEventCount := 0
	terminalEventCount := 0
	replayCollector := &openAIWSToolCallReplayCollector{}
	firstEventType := ""
	lastEventType := ""
	upstreamTerminalEvent := ""
	sawDone := false
	wroteDownstream := false
	pendingClientMessages := make([][]byte, 0, 4)
	pendingClientMessageBytes := int64(0)
	capacityFailoverSuppressedLogged := false
	clientDisconnected := false
	officialOpenAIResponses := account != nil && account.Platform == PlatformOpenAI
	bareErrorPending := false
	var bareErrorPayload []byte
	bareErrorMessage := ""
	failureAccountSideEffectsApplied := false
	mappedModel := actualModel
	needModelReplace := false
	var mappedModelBytes []byte
	if originalModel != "" {
		needModelReplace = mappedModel != "" && mappedModel != originalModel
		if needModelReplace {
			mappedModelBytes = []byte(mappedModel)
		}
	}

	resultWithUsage := func() *OpenAIForwardResult {
		imageCount := imageCounter.Count()
		result := &OpenAIForwardResult{
			RequestID:                     responseID,
			Usage:                         usage,
			Model:                         originalModel,
			UpstreamModel:                 mappedModel,
			UpstreamResponseModel:         responseModelObserver.Model(),
			UpstreamResponseModelConflict: responseModelObserver.Conflict(),
			UpstreamResponseServiceTier:   responseModelObserver.ServiceTier(),
			ServiceTier:                   resolvedOpenAIUpstreamServiceTierFromObserver(responseModelObserver, extractOpenAIServiceTierFromBody(body)),
			ReasoningEffort:               ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(body, mappedModel, originalModel), body, mappedModel),
			RequestedReasoningEffort:      CanonicalRequestedReasoningEffort(body, originalModel, mappedModel),
			Stream:                        reqStream,
			OpenAIWSMode:                  true,
			UpstreamTerminalEvent:         upstreamTerminalEvent,
			ResponseHeaders:               cloneHeader(resp.Header),
			Duration:                      time.Since(turnStart),
			FirstTokenMs:                  firstTokenMs,
		}
		if replayInput := replayCollector.Items(); len(replayInput) > 0 {
			result.wsReplayInput = replayInput
			result.wsReplayInputExists = true
		}
		result.wsAccountFailoverReplayInput = replayCollector.AllItems()
		if imageCount > 0 {
			result.ImageCount = imageCount
			result.ImageSize = imageSizeTier
			result.ImageInputSize = imageInputSize
			result.ImageOutputSizes = imageCounter.Sizes()
			result.BillingModel = imageBillingModel
		}
		return result
	}

	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	if hasResponsesClientToolMapping(clientToolMapping) {
		resp.Body = newResponsesClientToolStreamBody(resp.Body, clientToolMapping, maxLineSize)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanBuf := getSSEScannerBuf64K()
	scanner.Buffer(scanBuf[:0], maxLineSize)
	defer putSSEScannerBuf64K(scanBuf)

	pendingSSEEventType := ""
	finalizeBareError := func() error {
		if !bareErrorPending {
			return nil
		}
		if !failureAccountSideEffectsApplied {
			failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, bareErrorPayload)
		}
		upstreamTerminalEvent = "response.failed"
		if clientDisconnected {
			return nil
		}
		clientMessage := buildOpenAIWSHTTPBridgeFailedEvent(responseID, originalModel, bareErrorPayload, bareErrorMessage)
		if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
			clientMessage = rewritten
		}
		messages := append(pendingClientMessages, clientMessage)
		pendingClientMessages = nil
		pendingClientMessageBytes = 0
		for _, message := range messages {
			if err := writeClientMessage(message); err != nil {
				if isOpenAIWSClientDisconnectError(err) {
					clientDisconnected = true
					return nil
				}
				return fmt.Errorf("write synthesized websocket response.failed: %w", err)
			}
			wroteDownstream = true
		}
		markOpenAIWSClientVisibleFailure(c, "response.failed", clientMessage)
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if eventType, ok := extractOpenAISSEEventLine(line); ok {
			pendingSSEEventType = eventType
			continue
		}
		if strings.TrimSpace(line) == "" {
			pendingSSEEventType = ""
			continue
		}
		data, ok := extractOpenAISSEDataLine(line)
		if !ok {
			continue
		}
		trimmedData := strings.TrimSpace(data)
		if trimmedData == "" {
			continue
		}
		if trimmedData == "[DONE]" {
			sawDone = true
			continue
		}

		upstreamMessage := []byte(openAICompatPayloadWithEventType(trimmedData, pendingSSEEventType))
		if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
			upstreamMessage = normalized
		}
		eventType, eventResponseID, _ := parseOpenAIWSEventEnvelope(upstreamMessage)
		responseModelObserver.ObserveOpenAI(upstreamMessage, eventType)
		if responseID == "" && eventResponseID != "" {
			responseID = eventResponseID
		}
		if eventType != "" {
			eventCount++
			if firstEventType == "" {
				firstEventType = eventType
			}
			lastEventType = eventType
		}
		if isOpenAIWSTokenEvent(eventType) {
			tokenEventCount++
			if firstTokenMs == nil {
				ms := int(time.Since(turnStart).Milliseconds())
				firstTokenMs = &ms
			}
		}
		if openAIWSMessageShouldParseUsage(eventType, upstreamMessage) {
			parseOpenAIWSResponseUsageFromCompletedEvent(upstreamMessage, &usage)
		}
		imageCounter.AddSSEData(upstreamMessage)

		if needModelReplace && len(mappedModelBytes) > 0 && openAIWSEventMayContainModel(eventType) && strings.Contains(trimmedData, mappedModel) {
			upstreamMessage = replaceOpenAIWSMessageModel(upstreamMessage, mappedModel, originalModel)
		}
		if s.toolCorrector != nil && openAIWSEventMayContainToolCalls(eventType) && openAIWSMessageLikelyContainsToolCalls(upstreamMessage) {
			if corrected, changed := s.toolCorrector.CorrectToolCallsInSSEBytes(upstreamMessage); changed {
				upstreamMessage = corrected
			}
		}
		replayCollector.AddEvent(eventType, upstreamMessage)
		if eventType == "response.failed" {
			failedBody := openAIStreamFailedEventPassthroughBody(upstreamMessage, "")
			failedMessage := extractUpstreamErrorMessage(failedBody)
			if turn == 1 && !wroteDownstream && isOpenAIStreamRequestScopedCapacityError(upstreamMessage, failedMessage) {
				return nil, s.newOpenAIStreamFailoverError(
					c,
					account,
					false,
					responseID,
					upstreamMessage,
					failedMessage,
					resp.Header,
				)
			}
		}

		var upstreamEventErr error
		if officialOpenAIResponses && bareErrorPending && (eventType == "response.completed" || eventType == "response.done") {
			// Some upstreams emit a recoverable bare error before the authoritative
			// successful terminal. Do not replace that terminal with a synthetic
			// failure or retain side effects from the superseded error.
			bareErrorPending = false
			bareErrorPayload = nil
			bareErrorMessage = ""
		}
		suppressClientMessage := officialOpenAIResponses && bareErrorPending && eventType != "response.failed"
		if eventType == "error" || eventType == "response.failed" {
			errMessage := extractOpenAISSEErrorMessage(upstreamMessage)
			if errMessage == "" {
				errMessage = "upstream error event"
			}
			statusCode := openAIStreamFailureStatus(upstreamMessage, errMessage)
			shouldFailover := openAIStreamFailedEventShouldFailover(upstreamMessage, errMessage)
			if eventType == "error" {
				errCodeRaw, errTypeRaw, _ := parseOpenAIWSErrorEventFields(upstreamMessage)
				shouldFailover = openAIStreamErrorEventShouldFailover(upstreamMessage, errMessage)
				if account.Platform == PlatformGrok {
					statusCode = openAIWSErrorHTTPStatusFromRaw(errCodeRaw, errTypeRaw)
				}
			}
			requestScopedCapacity := isOpenAIUpstreamCapacityShedEvent(upstreamMessage)
			if account.Platform == PlatformGrok && eventType == "error" {
				// SSE error events do not carry an HTTP status. The local status
				// mapper therefore defaults unknown xAI codes (for example
				// new_sensitive) to 502; classify the body as a request-scoped
				// 403 before applying status-based failover or account state.
				if isGrokContentPolicyRejection(http.StatusForbidden, upstreamMessage) {
					shouldFailover = false
				} else {
					shouldFailover = s.shouldFailoverGrokUpstreamError(statusCode, upstreamMessage)
					s.handleGrokAccountUpstreamError(ctx, account, statusCode, resp.Header, upstreamMessage)
				}
			}
			if !wroteDownstream && shouldFailover && (turn == 1 || statusCode == http.StatusTooManyRequests) {
				if account.Platform == PlatformGrok {
					return nil, newOpenAIUpstreamFailoverError(statusCode, resp.Header, upstreamMessage, errMessage, false)
				}
				return nil, s.newOpenAIStreamFailoverErrorWithModel(c, account, true, resp.Header.Get("x-request-id"), upstreamMessage, errMessage, mappedModel, resp.Header)
			}
			if account.Platform != PlatformGrok && !failureAccountSideEffectsApplied {
				if eventType == "response.failed" || (!officialOpenAIResponses && shouldFailover && !requestScopedCapacity) {
					failureAccountSideEffectsApplied = s.handleOpenAIWSFailureAccountSideEffects(ctx, account, mappedModel, resp.Header, upstreamMessage)
				}
			}
			if wroteDownstream && requestScopedCapacity && !capacityFailoverSuppressedLogged {
				logOpenAICapacityFailoverSuppressed(ctx, account, "ws_http_bridge", resp.Header.Get("x-request-id"), eventType)
				capacityFailoverSuppressedLogged = true
			}
			if eventType == "error" && !officialOpenAIResponses {
				upstreamEventErr = errors.New(errMessage)
			} else if eventType == "error" {
				bareErrorPending = true
				bareErrorPayload = append(bareErrorPayload[:0], upstreamMessage...)
				bareErrorMessage = errMessage
				suppressClientMessage = true
			} else {
				bareErrorPending = false
			}
		}

		// 客户端写出副本改写容量降载码：Codex 对 error/response.failed 中的
		// server_is_overloaded / slow_down 判致命并终止会话，改写后走客户端内置
		// 重试。账号状态与终止事件判定仍使用未改写的 upstreamMessage。
		clientMessage := upstreamMessage
		if eventType == "error" || eventType == "response.failed" {
			if rewritten, changed := sanitizeOpenAICapacityShedErrorCodeForClient(clientMessage); changed {
				clientMessage = rewritten
			}
		}
		if !clientDisconnected && !suppressClientMessage {
			stageBeforeSemanticOutput := turn == 1 && account.Platform == PlatformOpenAI && !wroteDownstream
			commitStagedMessages := !stageBeforeSemanticOutput ||
				openAIStreamDataStartsClientOutput(string(clientMessage), eventType) ||
				isOpenAIWSTerminalEvent(eventType)
			if stageBeforeSemanticOutput && !commitStagedMessages {
				if pendingClientMessageBytes+int64(len(clientMessage)) > openAIFirstOutputStageMaxBytes {
					return nil, s.newOpenAIStreamFailoverError(
						c,
						account,
						true,
						resp.Header.Get("x-request-id"),
						nil,
						"OpenAI WS HTTP bridge first-output staging limit exceeded",
						resp.Header,
					)
				}
				pendingClientMessages = append(pendingClientMessages, append([]byte(nil), clientMessage...))
				pendingClientMessageBytes += int64(len(clientMessage))
			} else {
				messages := append(pendingClientMessages, clientMessage)
				pendingClientMessages = nil
				pendingClientMessageBytes = 0
				for _, message := range messages {
					if err := writeClientMessage(message); err != nil {
						if isOpenAIWSClientDisconnectError(err) {
							clientDisconnected = true
							closeStatus, closeReason := summarizeOpenAIWSReadCloseError(err)
							logOpenAIWSModeInfo(
								"ingress_ws_http_bridge_client_disconnected_drain account_id=%d turn=%d close_status=%s close_reason=%s",
								account.ID,
								turn,
								closeStatus,
								truncateOpenAIWSLogValue(closeReason, openAIWSHeaderValueMaxLen),
							)
							break
						}
						return nil, wrapOpenAIWSIngressTurnError(
							"write_client",
							fmt.Errorf("write client websocket event: %w", err),
							wroteDownstream,
						)
					}
					wroteDownstream = true
				}
			}
		}
		if !clientDisconnected && !suppressClientMessage {
			markOpenAIWSClientVisibleFailure(c, eventType, upstreamMessage)
		}

		if upstreamEventErr != nil {
			return resultWithUsage(), upstreamEventErr
		}
		if isOpenAIWSTerminalEvent(eventType) && !bareErrorPending {
			if eventType == "response.failed" {
				upstreamTerminalEvent = "response.failed"
			} else {
				upstreamTerminalEvent = s.handleOpenAIWSTerminalTransientFailure(ctx, account, mappedModel, resp.Header, upstreamMessage)
			}
			terminalEventCount++
			firstTokenMsValue := -1
			if firstTokenMs != nil {
				firstTokenMsValue = *firstTokenMs
			}
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_turn_completed account_id=%d turn=%d response_id=%s payload_bytes=%d duration_ms=%d events=%d token_events=%d terminal_events=%d first_event=%s last_event=%s first_token_ms=%d client_disconnected=%v",
				account.ID,
				turn,
				truncateOpenAIWSLogValue(responseID, openAIWSIDValueMaxLen),
				payloadBytes,
				time.Since(turnStart).Milliseconds(),
				eventCount,
				tokenEventCount,
				terminalEventCount,
				truncateOpenAIWSLogValue(firstEventType, openAIWSLogValueMaxLen),
				truncateOpenAIWSLogValue(lastEventType, openAIWSLogValueMaxLen),
				firstTokenMsValue,
				clientDisconnected,
			)
			return resultWithUsage(), nil
		}
	}
	if bareErrorPending {
		if finalizeErr := finalizeBareError(); finalizeErr != nil {
			return resultWithUsage(), finalizeErr
		}
		if scanErr := scanner.Err(); scanErr != nil {
			return resultWithUsage(), fmt.Errorf("read upstream http bridge stream after error event: %w", scanErr)
		}
		return resultWithUsage(), errors.New(bareErrorMessage)
	}
	if err := scanner.Err(); err != nil {
		streamErr := fmt.Errorf("read upstream http bridge stream: %w", err)
		if turn == 1 && !wroteDownstream {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, streamErr, true)
		}
		return resultWithUsage(), streamErr
	}
	terminalErr := errors.New("upstream http bridge stream ended before terminal event")
	if sawDone {
		terminalErr = errors.New("upstream http bridge stream sent [DONE] before terminal event")
	}
	if turn == 1 && !wroteDownstream {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, terminalErr, true)
	}
	if !clientDisconnected {
		for _, downstreamMessage := range pendingClientMessages {
			if err := writeClientMessage(downstreamMessage); err != nil {
				if isOpenAIWSClientDisconnectError(err) {
					break
				}
				return nil, wrapOpenAIWSIngressTurnError(
					"write_client",
					fmt.Errorf("write buffered client websocket event: %w", err),
					wroteDownstream,
				)
			}
			wroteDownstream = true
		}
	}
	return resultWithUsage(), terminalErr
}

func resolveGrokWSCacheIdentity(c *gin.Context, account *Account, seedPayload, currentPayload []byte, originalModel string) (string, error) {
	body, err := prepareOpenAIWSHTTPBridgeBody(account, seedPayload)
	if err != nil {
		return "", err
	}
	upstreamModel := resolveGrokWSUpstreamModel(account, currentPayload, originalModel)
	body, err = patchGrokResponsesBody(body, upstreamModel)
	if err != nil {
		return "", err
	}
	return resolveGrokCacheIdentity(c, body, "", upstreamModel), nil
}

func resolveGrokWSUpstreamModel(account *Account, body []byte, originalModel string) string {
	upstreamModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	originalModel = strings.TrimSpace(originalModel)
	// Shared ingress has already applied channel and account mappings when the
	// body model differs from the client-facing model. Only resolve from the
	// original model when the body still carries that original value.
	if account != nil && originalModel != "" && (upstreamModel == "" || upstreamModel == originalModel) {
		if mappedModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel)); mappedModel != "" {
			upstreamModel = mappedModel
		}
	}
	if upstreamModel == "" {
		upstreamModel = grokDefaultResponsesModel
	}
	return upstreamModel
}
