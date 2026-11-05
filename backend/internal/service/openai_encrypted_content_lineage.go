package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// invalid_encrypted_content 失效密文 lineage。
//
// 上游判定某轮请求中的加密 reasoning/compaction 项不可解后，同一失效密文会随
// 客户端维护的会话历史在后续每一轮重新出现，重复触发"整包被拒→剥离→重试/
// 重连"。这里按会话记录已被上游拒绝过的 encrypted_content 摘要
// （OpenAIWSStateStore，带 TTL 与容量自保护）；后续请求进场时仅剥离摘要命中
// 的项，新生成的密文摘要不同，不会被误删。

const openAIWSFallbackReasonInvalidEncryptedContent = "invalid_encrypted_content"

// openAIWSIngressSessionHashContextKey 在 gin context 中携带 ingress 会话哈希，
// 供 HTTP bridge turn 内的 lineage 记录复用同一会话键。
const openAIWSIngressSessionHashContextKey = "openai_ws_ingress_session_hash"

func openAIEncryptedContentDigest(encrypted string) string {
	sum := sha256.Sum256([]byte(encrypted))
	return hex.EncodeToString(sum[:])
}

// openAIEncryptedLineageItemType 限定 lineage 覆盖的项类型，与剥离端能处理的
// 类型对称；其他类型即使携带 encrypted_content 也不收摘要，避免记入永远
// 剥不掉的摘要后每轮空转解码。function_call_output/agent_message 的密文
// 只在同一 output/content 数组还保留明文片段时才会被安全剥离。
func openAIEncryptedLineageItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "reasoning", "compaction", "compaction_summary", "function_call_output", "agent_message":
		return true
	default:
		return false
	}
}

// collectOpenAIEncryptedContentDigestsRaw 收集 input 中 lineage 覆盖类型所携
// 密文的摘要。payload 须遵守 replay 所有权不变式（零拷贝视图解析）。
func collectOpenAIEncryptedContentDigestsRaw(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	input := gjson.Get(openAIWSPayloadStringView(payload), "input")
	if !input.Exists() {
		return nil
	}
	var digests []string
	appendItem := func(item gjson.Result) {
		itemType := item.Get("type").String()
		if !openAIEncryptedLineageItemType(itemType) {
			return
		}
		encrypted := item.Get("encrypted_content")
		if encrypted.Type == gjson.String && encrypted.String() != "" {
			digests = append(digests, openAIEncryptedContentDigest(encrypted.String()))
		}
		var nested gjson.Result
		switch strings.TrimSpace(itemType) {
		case "function_call_output":
			nested = item.Get("output")
		case "agent_message":
			nested = item.Get("content")
		}
		collectOpenAINestedEncryptedContentDigests(nested, &digests)
	}
	if input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			appendItem(item)
			return true
		})
		return digests
	}
	if input.IsObject() {
		appendItem(input)
	}
	return digests
}

func collectOpenAINestedEncryptedContentDigests(value gjson.Result, digests *[]string) {
	if digests == nil || !value.Exists() {
		return
	}
	if value.Type == gjson.String {
		var decoded any
		if err := json.Unmarshal([]byte(value.String()), &decoded); err == nil {
			collectOpenAINestedEncryptedContentDigestsRaw(decoded, digests)
		}
		return
	}
	if value.IsArray() {
		var encryptedValues []string
		plainSurvivor := false
		value.ForEach(func(_, item gjson.Result) bool {
			if item.Get("type").String() == "encrypted_content" {
				if encrypted := item.Get("encrypted_content"); encrypted.Type == gjson.String && encrypted.String() != "" {
					encryptedValues = append(encryptedValues, encrypted.String())
				}
			} else {
				plainSurvivor = true
			}
			return true
		})
		if plainSurvivor {
			for _, encrypted := range encryptedValues {
				*digests = append(*digests, openAIEncryptedContentDigest(encrypted))
			}
		}
	}
}

func collectOpenAINestedEncryptedContentDigestsRaw(value any, digests *[]string) {
	if digests == nil {
		return
	}
	parts, ok := value.([]any)
	if !ok {
		return
	}
	var encryptedValues []string
	plainSurvivor := false
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(stringFromAny(part["type"])) != "encrypted_content" {
			plainSurvivor = true
			continue
		}
		encrypted := strings.TrimSpace(stringFromAny(part["encrypted_content"]))
		if encrypted != "" {
			encryptedValues = append(encryptedValues, encrypted)
		}
	}
	if plainSurvivor {
		for _, encrypted := range encryptedValues {
			*digests = append(*digests, openAIEncryptedContentDigest(encrypted))
		}
	}
}

func collectOpenAIEncryptedFunctionOutputDigestsRaw(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	input := gjson.Get(openAIWSPayloadStringView(payload), "input")
	if !input.Exists() {
		return nil
	}
	var digests []string
	appendItem := func(item gjson.Result) {
		itemType := strings.TrimSpace(item.Get("type").String())
		if itemType != "function_call_output" && itemType != "agent_message" {
			return
		}
		if itemType == "function_call_output" {
			collectOpenAINestedEncryptedContentDigests(item.Get("output"), &digests)
		} else {
			collectOpenAINestedEncryptedContentDigests(item.Get("content"), &digests)
		}
	}
	if input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			appendItem(item)
			return true
		})
	} else if input.IsObject() {
		appendItem(input)
	}
	return digests
}

func stripOpenAIEncryptedFunctionOutputRaw(payload []byte) ([]byte, int, error) {
	digests := collectOpenAIEncryptedFunctionOutputDigestsRaw(payload)
	if len(digests) == 0 {
		return payload, 0, nil
	}
	invalid := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		invalid[digest] = struct{}{}
	}
	var decoded map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &decoded); err != nil {
		return payload, 0, err
	}
	stripped := stripOpenAIInvalidEncryptedContentItems(decoded, invalid)
	if stripped == 0 {
		return payload, 0, nil
	}
	rebuilt, err := marshalOpenAIUpstreamJSON(decoded)
	if err != nil {
		return payload, 0, err
	}
	return rebuilt, stripped, nil
}

// stripOpenAIInvalidEncryptedContentItems 剥离 input 中摘要命中 invalid 的密文
// 项，剥离语义与 trimOpenAIEncryptedReasoningItems 对齐：reasoning 仅剥
// encrypted_content（连带 content:null 清理与空骨架删除），compaction/
// compaction_summary 整项删除。返回被改写的项数。
func stripOpenAIInvalidEncryptedContentItems(reqBody map[string]any, invalid map[string]struct{}) int {
	if len(reqBody) == 0 || len(invalid) == 0 {
		return 0
	}
	inputValue, has := reqBody["input"]
	if !has {
		return 0
	}
	stripped := 0
	stripItem := func(item any) (next any, changed bool, keep bool) {
		inputItem, ok := item.(map[string]any)
		if !ok {
			return item, false, true
		}
		itemType := strings.TrimSpace(stringFromAny(inputItem["type"]))
		encrypted, ok := inputItem["encrypted_content"].(string)
		if ok && encrypted != "" {
			if _, hit := invalid[openAIEncryptedContentDigest(encrypted)]; hit {
				return sanitizeEncryptedReasoningInputItem(item)
			}
		}
		if itemType == "function_call_output" || itemType == "agent_message" {
			return stripOpenAINestedEncryptedContentItem(inputItem, itemType, invalid)
		}
		return item, false, true
	}
	switch input := inputValue.(type) {
	case []any:
		filtered := input[:0]
		for _, item := range input {
			nextItem, changed, keep := stripItem(item)
			if changed {
				stripped++
			}
			if !keep {
				continue
			}
			filtered = append(filtered, nextItem)
		}
		if stripped == 0 {
			return 0
		}
		if len(filtered) == 0 {
			delete(reqBody, "input")
			return stripped
		}
		reqBody["input"] = filtered
		return stripped
	case map[string]any:
		nextItem, changed, keep := stripItem(input)
		if !changed {
			return 0
		}
		if !keep {
			delete(reqBody, "input")
			return 1
		}
		nextMap, ok := nextItem.(map[string]any)
		if !ok {
			return 0
		}
		reqBody["input"] = nextMap
		return 1
	default:
		return 0
	}
}

// openAIRawPayloadHasInvalidEncryptedContent 探测 raw payload 是否携带命中
// invalid 摘要的密文项；零命中的常态路径不做任何改写。payload 须遵守 replay
// 所有权不变式（零拷贝视图解析）。
func openAIRawPayloadHasInvalidEncryptedContent(payload []byte, invalid map[string]struct{}) bool {
	if len(payload) == 0 || len(invalid) == 0 {
		return false
	}
	input := gjson.Get(openAIWSPayloadStringView(payload), "input")
	if !input.Exists() {
		return false
	}
	hit := false
	checkItem := func(item gjson.Result) bool {
		itemType := item.Get("type").String()
		encrypted := item.Get("encrypted_content")
		if encrypted.Type == gjson.String && encrypted.String() != "" {
			if _, matched := invalid[openAIEncryptedContentDigest(encrypted.String())]; matched {
				return true
			}
		}
		var nested gjson.Result
		switch strings.TrimSpace(itemType) {
		case "function_call_output":
			nested = item.Get("output")
		case "agent_message":
			nested = item.Get("content")
		default:
			return false
		}
		return openAINestedEncryptedContentHasInvalidDigest(nested, invalid)
	}
	if input.IsArray() {
		input.ForEach(func(_, item gjson.Result) bool {
			if checkItem(item) {
				hit = true
				return false
			}
			return true
		})
		return hit
	}
	if input.IsObject() {
		return checkItem(input)
	}
	return false
}

// stripOpenAIInvalidEncryptedContentFromReplayItems 对 replay 历史序列做同款
// 剥离。有改动时返回新头数组，命中项以重写后的新正文整体替换（遵守 replay
// 所有权不变式，原正文不被修改）；未命中时原样返回。
func stripOpenAIInvalidEncryptedContentFromReplayItems(items []json.RawMessage, invalid map[string]struct{}) ([]json.RawMessage, int) {
	if len(items) == 0 || len(invalid) == 0 {
		return items, 0
	}
	hit := false
	for _, item := range items {
		var decoded map[string]any
		if err := decodeOpenAIJSONUseNumber(item, &decoded); err != nil {
			continue
		}
		if openAIInputItemHasInvalidEncryptedContent(decoded, invalid) {
			hit = true
			break
		}
	}
	if !hit {
		return items, 0
	}
	stripped := 0
	next := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var decoded map[string]any
		if err := decodeOpenAIJSONUseNumber(item, &decoded); err != nil {
			next = append(next, item)
			continue
		}
		nextItem, changed, keep := stripOpenAIInvalidEncryptedContentItem(decoded, invalid)
		if !changed {
			next = append(next, item)
			continue
		}
		stripped++
		if !keep {
			continue
		}
		rebuilt, err := marshalOpenAIUpstreamJSON(nextItem)
		if err != nil {
			next = append(next, item)
			stripped--
			continue
		}
		next = append(next, json.RawMessage(rebuilt))
	}
	if stripped == 0 {
		return items, 0
	}
	return next, stripped
}

func openAIInputItemHasInvalidEncryptedContent(item map[string]any, invalid map[string]struct{}) bool {
	if item == nil {
		return false
	}
	if encrypted := strings.TrimSpace(stringFromAny(item["encrypted_content"])); encrypted != "" {
		if _, ok := invalid[openAIEncryptedContentDigest(encrypted)]; ok {
			return true
		}
	}
	itemType := strings.TrimSpace(stringFromAny(item["type"]))
	if itemType != "function_call_output" && itemType != "agent_message" {
		return false
	}
	value := item["output"]
	if itemType == "agent_message" {
		value = item["content"]
	}
	return openAINestedEncryptedContentHasInvalidDigestRaw(value, invalid)
}

func stripOpenAIInvalidEncryptedContentItem(item map[string]any, invalid map[string]struct{}) (any, bool, bool) {
	if item == nil {
		return item, false, true
	}
	if encrypted := strings.TrimSpace(stringFromAny(item["encrypted_content"])); encrypted != "" {
		if _, ok := invalid[openAIEncryptedContentDigest(encrypted)]; ok {
			return sanitizeEncryptedReasoningInputItem(item)
		}
	}
	itemType := strings.TrimSpace(stringFromAny(item["type"]))
	if itemType == "function_call_output" || itemType == "agent_message" {
		return stripOpenAINestedEncryptedContentItem(item, itemType, invalid)
	}
	return item, false, true
}

func openAINestedEncryptedContentHasInvalidDigest(value any, invalid map[string]struct{}) bool {
	if value == nil {
		return false
	}
	if raw, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(raw), &decoded) != nil {
			return false
		}
		return openAINestedEncryptedContentHasInvalidDigest(decoded, invalid)
	}
	parts, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(stringFromAny(part["type"])) != "encrypted_content" {
			continue
		}
		if _, matched := invalid[openAIEncryptedContentDigest(strings.TrimSpace(stringFromAny(part["encrypted_content"])))]; matched {
			return true
		}
	}
	return false
}

func openAINestedEncryptedContentHasInvalidDigestRaw(value any, invalid map[string]struct{}) bool {
	return openAINestedEncryptedContentHasInvalidDigest(value, invalid)
}

func stripOpenAINestedEncryptedContentItem(item map[string]any, itemType string, invalid map[string]struct{}) (any, bool, bool) {
	field := "output"
	if itemType == "agent_message" {
		field = "content"
	}
	value, exists := item[field]
	if !exists {
		return item, false, true
	}
	cleaned, changed, safe := stripOpenAINestedEncryptedContentValue(value, invalid)
	if !changed || !safe {
		return item, false, true
	}
	item[field] = cleaned
	return item, true, true
}

func stripOpenAINestedEncryptedContentValue(value any, invalid map[string]struct{}) (any, bool, bool) {
	stringEncoded, isString := value.(string)
	if isString {
		var decoded any
		if json.Unmarshal([]byte(stringEncoded), &decoded) != nil {
			return value, false, true
		}
		cleaned, changed, safe := stripOpenAINestedEncryptedContentValue(decoded, invalid)
		if !changed || !safe {
			return value, false, safe
		}
		encoded, err := json.Marshal(cleaned)
		if err != nil {
			return value, false, true
		}
		return string(encoded), true, true
	}
	parts, ok := value.([]any)
	if !ok {
		return value, false, true
	}
	filtered := make([]any, 0, len(parts))
	changed := false
	plainSurvivor := false
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(stringFromAny(part["type"])) != "encrypted_content" {
			filtered = append(filtered, raw)
			plainSurvivor = true
			continue
		}
		encrypted := strings.TrimSpace(stringFromAny(part["encrypted_content"]))
		if _, matched := invalid[openAIEncryptedContentDigest(encrypted)]; !matched {
			filtered = append(filtered, raw)
			continue
		}
		changed = true
	}
	if !changed || !plainSurvivor {
		// A pure encrypted output has no recoverable context. Keep it intact so the
		// caller can surface the upstream error instead of silently changing the task.
		return value, false, len(filtered) > 0
	}
	return filtered, true, true
}

// stripOpenAIInvalidEncryptedContentRaw 是 raw JSON 版剥离：先零成本探测，命中
// 才解码改写重编码。返回改写后的 payload 与剥离项数；未命中时原样返回。
func stripOpenAIInvalidEncryptedContentRaw(payload []byte, invalid map[string]struct{}) ([]byte, int, error) {
	if !openAIRawPayloadHasInvalidEncryptedContent(payload, invalid) {
		return payload, 0, nil
	}
	var decoded map[string]any
	if err := decodeOpenAIJSONUseNumber(payload, &decoded); err != nil {
		return payload, 0, err
	}
	stripped := stripOpenAIInvalidEncryptedContentItems(decoded, invalid)
	if stripped == 0 {
		return payload, 0, nil
	}
	rebuilt, err := marshalOpenAIUpstreamJSON(decoded)
	if err != nil {
		return payload, 0, err
	}
	return rebuilt, stripped, nil
}

// markOpenAIWSInvalidEncryptedContentLineage 把本次被上游拒绝的密文摘要写入
// 会话 lineage。digests 须在剥离前收集。
func (s *OpenAIGatewayService) markOpenAIWSInvalidEncryptedContentLineage(groupID int64, sessionHash string, digests []string) {
	if s == nil || len(digests) == 0 || strings.TrimSpace(sessionHash) == "" {
		return
	}
	stateStore := s.getOpenAIWSStateStore()
	if stateStore == nil {
		return
	}
	stateStore.MarkSessionInvalidEncryptedContent(groupID, sessionHash, digests, s.openAIWSSessionStickyTTL())
}

// sessionInvalidEncryptedContentDigests 返回会话已知失效密文摘要；全局无记录
// 时（常态）零成本返回 nil。
func (s *OpenAIGatewayService) sessionInvalidEncryptedContentDigests(groupID int64, sessionHash string) map[string]struct{} {
	if s == nil || strings.TrimSpace(sessionHash) == "" {
		return nil
	}
	stateStore := s.getOpenAIWSStateStore()
	if stateStore == nil || !stateStore.HasAnySessionInvalidEncryptedContent() {
		return nil
	}
	return stateStore.GetSessionInvalidEncryptedContentDigests(groupID, sessionHash)
}

// openAIWSLineageSessionHashFromContext 取 lineage 会话键：优先 ingress 循环
// 写入的会话哈希（与读取侧同键），否则按请求体派生。
func (s *OpenAIGatewayService) openAIWSLineageSessionHashFromContext(c *gin.Context, body []byte) string {
	if c != nil {
		if fromCtx := strings.TrimSpace(c.GetString(openAIWSIngressSessionHashContextKey)); fromCtx != "" {
			return fromCtx
		}
	}
	return s.GenerateSessionHash(c, body)
}

// markOpenAIWSInvalidEncryptedContentLineageFromPayload 在上游以
// invalid_encrypted_content 拒绝 payload 时记录其密文摘要并输出观测日志。
func (s *OpenAIGatewayService) markOpenAIWSInvalidEncryptedContentLineageFromPayload(
	c *gin.Context,
	payload []byte,
	logKey string,
	accountID int64,
	turn int,
) {
	digests := collectOpenAIEncryptedContentDigestsRaw(payload)
	if len(digests) == 0 {
		return
	}
	s.markOpenAIWSInvalidEncryptedContentLineage(
		getOpenAIGroupIDFromContext(c),
		s.openAIWSLineageSessionHashFromContext(c, payload),
		digests,
	)
	logOpenAIWSModeInfo("%s account_id=%d turn=%d digests=%d", logKey, accountID, turn, len(digests))
}

// stripSessionInvalidEncryptedContentLogged 对 payload 执行会话失效密文剥离并
// 输出观测日志（logKey / logKey+"_skip"），返回（可能已替换的）payload 与剥离
// 项数；未命中或剥离失败时原样返回。
func (s *OpenAIGatewayService) stripSessionInvalidEncryptedContentLogged(
	payload []byte,
	invalid map[string]struct{},
	logKey string,
	accountID int64,
	turn int,
) ([]byte, int) {
	strippedPayload, strippedCount, stripErr := stripOpenAIInvalidEncryptedContentRaw(payload, invalid)
	if stripErr != nil {
		logOpenAIWSModeInfo(
			"%s_skip account_id=%d turn=%d reason=strip_error cause=%s",
			logKey,
			accountID,
			turn,
			truncateOpenAIWSLogValue(stripErr.Error(), openAIWSLogValueMaxLen),
		)
		return payload, 0
	}
	if strippedCount > 0 {
		logOpenAIWSModeInfo(
			"%s account_id=%d turn=%d stripped_items=%d",
			logKey,
			accountID,
			turn,
			strippedCount,
		)
	}
	return strippedPayload, strippedCount
}
