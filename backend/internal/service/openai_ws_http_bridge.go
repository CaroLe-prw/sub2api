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

func prepareOpenAIWSHTTPBridgeBody(payload []byte) ([]byte, error) {
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, errors.New("response.create payload must be a JSON object")
	}
	delete(body, "type")
	delete(body, "generate")
	body["stream"] = true
	return json.Marshal(body)
}

type openAIWSToolCallReplayCollector struct {
	items   []json.RawMessage
	indexes map[string]int
}

func (c *openAIWSToolCallReplayCollector) AddEvent(eventType string, message []byte) {
	switch strings.TrimSpace(eventType) {
	case "response.function_call_arguments.delta":
		c.addFunctionCallArgumentsEvent(message, true)
	case "response.function_call_arguments.done":
		c.addFunctionCallArgumentsEvent(message, false)
	case "response.output_item.done":
		c.addItem(gjson.GetBytes(message, "item"))
	case "response.completed", "response.done":
		output := gjson.GetBytes(message, "response.output")
		if !output.IsArray() {
			return
		}
		for _, item := range output.Array() {
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

	body, err := prepareOpenAIWSHTTPBridgeBody(payload)
	if err != nil {
		return nil, fmt.Errorf("prepare http bridge body: %w", err)
	}

	if account.Platform == PlatformGrok {
		upstreamModel := resolveGrokWSUpstreamModel(account, body, originalModel)
		grokIntentSourceBody := body
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
	clientToolMapping := apicompat.ResponsesClientToolMapping{}
	var resp *http.Response
	for {
		upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
		var upstreamReq *http.Request
		if account.Platform == PlatformGrok {
			upstreamReq, err = buildGrokResponsesRequest(upstreamCtx, c, account, body, token, grokCacheIdentity, s.cfg)
		} else {
			upstreamReq, err = s.buildUpstreamRequestOpenAIPassthrough(upstreamCtx, c, account, body, token)
		}
		releaseUpstreamCtx()
		if err != nil {
			return nil, err
		}
		if account.Platform != PlatformGrok && isOpenAIResponsesLiteWebSocketPayload(payload) {
			upstreamReq.Header.Set(responsesLiteHeader, "true")
		}

		resp, err = s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
		if err != nil {
			if turn == 1 {
				return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, true)
			}
			safeErr := sanitizeUpstreamErrorMessage(err.Error())
			_ = writeClientMessage(buildOpenAIWSHTTPBridgeErrorEvent(http.StatusBadGateway, "Upstream request failed"))
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
				if hasGrokResponsesClientToolMapping(retryMapping) {
					clientToolMapping = retryMapping
				}
				logOpenAIWSModeInfo(
					"ingress_ws_http_bridge_sse_rejected_field_retry account_id=%d turn=%d status=%d reason=%s",
					account.ID,
					turn,
					statusCode,
					normalizeOpenAIWSLogValue(reason),
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
		retryBody, retryMapping, reason, changed, retryErr := normalizeOpenAIWSHTTPBridgeRejectedRequest(resp.StatusCode, body, respBody)
		if retryErr != nil {
			return nil, fmt.Errorf("normalize http bridge rejected Responses field: %w", retryErr)
		}
		if changed && rejectedFieldRetryState.Allow(retryBody) {
			body = retryBody
			if hasGrokResponsesClientToolMapping(retryMapping) {
				clientToolMapping = retryMapping
			}
			logOpenAIWSModeInfo(
				"ingress_ws_http_bridge_rejected_field_retry account_id=%d turn=%d reason=%s",
				account.ID,
				turn,
				normalizeOpenAIWSLogValue(reason),
			)
			continue
		}
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		break
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, openAIWSHTTPBridgeErrorBodyLimitBytes))
		upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(respBody)))
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(resp.StatusCode)
		}
		shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody)
		if account.Platform == PlatformGrok {
			shouldFailover = s.shouldFailoverGrokUpstreamError(resp.StatusCode, respBody)
			s.handleGrokAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			if turn == 1 && shouldFailover {
				return nil, newOpenAIUpstreamFailoverError(resp.StatusCode, resp.Header, respBody, upstreamMsg, false)
			}
		} else if turn == 1 && shouldFailover {
			return nil, s.handleFailoverErrorResponsePassthrough(ctx, resp, c, account, body, respBody)
		}
		if account.Platform != PlatformGrok && (shouldFailover || shouldCooldownOpenAITransientUpstreamError(resp.StatusCode, respBody)) {
			canonicalModel := canonicalOpenAIAccountSchedulingModel(account, originalModel)
			s.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody, canonicalModel)
		}
		_ = writeClientMessage(buildOpenAIWSHTTPBridgeErrorEvent(resp.StatusCode, upstreamMsg))
		return nil, fmt.Errorf("upstream http bridge error: status=%d message=%s", resp.StatusCode, upstreamMsg)
	}
	if account.Platform == PlatformGrok {
		s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
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
	pendingDownstream := make([][]byte, 0, 3)
	clientDisconnected := false
	mappedModel := ""
	needModelReplace := false
	var mappedModelBytes []byte
	if originalModel != "" {
		mappedModel = strings.TrimSpace(gjson.GetBytes(body, "model").String())
		if mappedModel == "" {
			mappedModel = normalizeOpenAIModelForUpstream(account, account.GetMappedModel(originalModel))
		}
		needModelReplace = mappedModel != "" && mappedModel != originalModel
		if needModelReplace {
			mappedModelBytes = []byte(mappedModel)
		}
	}

	resultWithUsage := func() *OpenAIForwardResult {
		imageCount := imageCounter.Count()
		result := &OpenAIForwardResult{
			RequestID:             responseID,
			Usage:                 usage,
			Model:                 originalModel,
			UpstreamModel:         mappedModel,
			ServiceTier:           extractOpenAIServiceTierFromBody(body),
			ReasoningEffort:       ApplyThinkingEnabledFallback(extractOpenAIReasoningEffortFromBody(body, mappedModel, originalModel), body, mappedModel),
			Stream:                reqStream,
			OpenAIWSMode:          true,
			UpstreamTerminalEvent: upstreamTerminalEvent,
			ResponseHeaders:       cloneHeader(resp.Header),
			Duration:              time.Since(turnStart),
			FirstTokenMs:          firstTokenMs,
		}
		if replayInput := replayCollector.Items(); len(replayInput) > 0 {
			result.wsReplayInput = replayInput
			result.wsReplayInputExists = true
		}
		if imageCount > 0 {
			result.ImageCount = imageCount
			result.ImageSize = imageSizeTier
			result.ImageInputSize = imageInputSize
			result.ImageOutputSizes = imageCounter.Sizes()
			result.BillingModel = imageBillingModel
		}
		return result
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanBuf := getSSEScannerBuf64K()
	scanner.Buffer(scanBuf[:0], maxLineSize)
	defer putSSEScannerBuf64K(scanBuf)

	for scanner.Scan() {
		line := scanner.Text()
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

		upstreamMessage := []byte(trimmedData)
		if normalized, changed := normalizeCompletedImageGenerationStatus(upstreamMessage); changed {
			upstreamMessage = normalized
		}
		eventType, eventResponseID, _ := parseOpenAIWSEventEnvelope(upstreamMessage)
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
		if openAIWSEventShouldParseUsage(eventType) {
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
		if eventType == "error" {
			errCodeRaw, errTypeRaw, errMsgRaw := parseOpenAIWSErrorEventFields(upstreamMessage)
			errMessage := strings.TrimSpace(errMsgRaw)
			if errMessage == "" {
				errMessage = "upstream error event"
			}
			statusCode := openAIWSErrorHTTPStatusFromRaw(errCodeRaw, errTypeRaw)
			shouldFailover := s.shouldFailoverOpenAIUpstreamResponse(statusCode, errMessage, upstreamMessage)
			if account.Platform == PlatformGrok {
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
			} else if shouldFailover {
				accountStatus := statusCode
				if transientStatus := openAIWSPayloadTransientStatus(upstreamMessage); transientStatus != 0 {
					accountStatus = transientStatus
				}
				canonicalModel := canonicalOpenAIAccountSchedulingModel(account, originalModel)
				s.handleOpenAIAccountUpstreamError(ctx, account, accountStatus, resp.Header, upstreamMessage, canonicalModel)
			}
			if turn == 1 && !wroteDownstream && shouldFailover {
				return nil, newOpenAIUpstreamFailoverError(statusCode, resp.Header, upstreamMessage, errMessage, false)
			}
			upstreamEventErr = errors.New(errMessage)
		}

		if !clientDisconnected {
			if !wroteDownstream && isOpenAIWSRetryPreambleEvent(eventType) {
				pendingDownstream = append(pendingDownstream, append([]byte(nil), upstreamMessage...))
				continue
			}
			pendingDownstream = append(pendingDownstream, upstreamMessage)
			messages := pendingDownstream
			pendingDownstream = nil
			for _, downstreamMessage := range messages {
				if err := writeClientMessage(downstreamMessage); err != nil {
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
					} else {
						return nil, wrapOpenAIWSIngressTurnError(
							"write_client",
							fmt.Errorf("write client websocket event: %w", err),
							wroteDownstream,
						)
					}
					break
				}
				wroteDownstream = true
			}
		}

		if upstreamEventErr != nil {
			return resultWithUsage(), upstreamEventErr
		}
		if isOpenAIWSTerminalEvent(eventType) {
			upstreamTerminalEvent = s.handleOpenAIWSTerminalTransientFailure(ctx, account, canonicalOpenAIAccountSchedulingModel(account, originalModel), resp.Header, upstreamMessage)
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
	if err := scanner.Err(); err != nil {
		streamErr := fmt.Errorf("read upstream http bridge stream: %w", err)
		if turn == 1 && !wroteDownstream {
			return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, streamErr, true)
		}
		return resultWithUsage(), streamErr
	}
	if !clientDisconnected {
		for _, downstreamMessage := range pendingDownstream {
			if err := writeClientMessage(downstreamMessage); err != nil {
				if isOpenAIWSClientDisconnectError(err) {
					clientDisconnected = true
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
	terminalErr := errors.New("upstream http bridge stream ended before terminal event")
	if sawDone {
		terminalErr = errors.New("upstream http bridge stream sent [DONE] before terminal event")
	}
	if turn == 1 && !wroteDownstream {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, terminalErr, true)
	}
	return resultWithUsage(), terminalErr
}

func resolveGrokWSCacheIdentity(c *gin.Context, account *Account, seedPayload, currentPayload []byte, originalModel string) (string, error) {
	body, err := prepareOpenAIWSHTTPBridgeBody(seedPayload)
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
