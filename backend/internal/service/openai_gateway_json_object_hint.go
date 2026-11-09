package service

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// openAIResponsesJSONObjectPromptHint 是补进 input 的兜底提示词。
//
// OpenAI 的 Responses API 在 text.format.type=json_object 时要求 input 的消息
// 文本里出现 "json"（大小写不限），否则直接 400：
//
//	Response input messages must contain the word 'json' in some form to use
//	'text.format' of type 'json_object'.
//
// 该校验只扫描 input，instructions 不参与；而客户端经常把"只输出 JSON"的约束
// 只写进 instructions，于是请求在网关侧完全合法、到上游却必然被拒。这里补一条
// 与客户端意图一致的极短提示，落点选最后一条 user 消息（追加在尾部，不破坏
// 前缀缓存）。
const openAIResponsesJSONObjectPromptHint = "Return the final answer as a JSON object."

// openAIResponsesBodyUsesJSONObjectFormat 判断 Responses 形态的 body 是否要求
// text.format.type=json_object（容错大小写与首尾空白）。
func openAIResponsesBodyUsesJSONObjectFormat(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(gjson.GetBytes(body, "text.format.type").String()), "json_object")
}

// openAIResponsesMapUsesJSONObjectFormat 是 openAIResponsesBodyUsesJSONObjectFormat
// 的 map 版本，供已解码的请求体使用。
func openAIResponsesMapUsesJSONObjectFormat(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	text, ok := reqBody["text"].(map[string]any)
	if !ok {
		return false
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		return false
	}
	formatType, _ := format["type"].(string)
	return strings.EqualFold(strings.TrimSpace(formatType), "json_object")
}

// openAIResponsesInputMentionsJSON 递归扫描 input 的文本内容，判断其中是否已出现
// "json"。已出现时无需干预。仅跳过承载标识/二进制的字段，避免 base64 数据里偶然
// 出现的 "json" 被当成提示词。
func openAIResponsesInputMentionsJSON(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(v), "json")
	case []any:
		for _, item := range v {
			if openAIResponsesInputMentionsJSON(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if openAIResponsesOpaqueInputKey(key) {
				continue
			}
			if openAIResponsesInputMentionsJSON(item) {
				return true
			}
		}
	}
	return false
}

// openAIResponsesOpaqueInputKey 标记不参与 "json" 检测的字段。
func openAIResponsesOpaqueInputKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "id", "role", "type", "model", "call_id", "image_url", "file_id", "file_data", "detail":
		return true
	default:
		return false
	}
}

// ensureOpenAIResponsesJSONObjectPromptHint 在 json_object 模式下为 input 补齐
// 提示词。返回是否修改以及落点（便于日志排查）。
func ensureOpenAIResponsesJSONObjectPromptHint(reqBody map[string]any) (bool, string) {
	if !openAIResponsesMapUsesJSONObjectFormat(reqBody) {
		return false, ""
	}
	input, ok := reqBody["input"]
	if !ok {
		return false, ""
	}
	if openAIResponsesInputMentionsJSON(input) {
		return false, ""
	}
	switch v := input.(type) {
	case string:
		reqBody["input"] = strings.TrimRight(v, " \t\r\n") + "\n\n" + openAIResponsesJSONObjectPromptHint
		return true, "input_string"
	case []any:
		for i := len(v) - 1; i >= 0; i-- {
			item, ok := v[i].(map[string]any)
			if !ok {
				continue
			}
			if role, _ := item["role"].(string); !strings.EqualFold(strings.TrimSpace(role), "user") {
				continue
			}
			item["content"] = appendOpenAIResponsesInputText(item["content"], openAIResponsesJSONObjectPromptHint)
			v[i] = item
			reqBody["input"] = v
			return true, "user_message"
		}
		// 没有 user 消息（例如纯工具结果续跑）：退化成追加一条 developer 提示项。
		reqBody["input"] = append(v, map[string]any{
			"role": "developer",
			"content": []any{
				map[string]any{"type": "input_text", "text": openAIResponsesJSONObjectPromptHint},
			},
		})
		return true, "developer_item"
	default:
		return false, ""
	}
}

// appendOpenAIResponsesInputText 把提示词追加到消息 content 尾部，保留原有内容。
func appendOpenAIResponsesInputText(content any, text string) any {
	switch v := content.(type) {
	case string:
		return strings.TrimRight(v, " \t\r\n") + "\n\n" + text
	case []any:
		return append(v, map[string]any{"type": "input_text", "text": text})
	case map[string]any:
		return []any{v, map[string]any{"type": "input_text", "text": text}}
	case nil:
		return text
	default:
		return []any{map[string]any{"type": "input_text", "text": text}}
	}
}

// applyOpenAIResponsesJSONObjectPromptHint 在 byte 形态的 Responses body 上执行
// 同一补齐；未命中 json_object 或无需补齐时原样返回（落点为空串）。
func applyOpenAIResponsesJSONObjectPromptHint(body []byte) ([]byte, string, error) {
	if !openAIResponsesBodyUsesJSONObjectFormat(body) {
		return body, "", nil
	}
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		return body, "", err
	}
	injected, site := ensureOpenAIResponsesJSONObjectPromptHint(reqBody)
	if !injected {
		return body, "", nil
	}
	patched, err := marshalOpenAIUpstreamJSON(reqBody)
	if err != nil {
		return body, "", err
	}
	return patched, site, nil
}
