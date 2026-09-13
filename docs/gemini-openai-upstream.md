# Gemini 分组使用 OpenAI 兼容上游

Gemini API Key 账号可以独立选择上游接口格式，不必将 Gemini 模型移到 OpenAI 分组。

## 配置

1. 新增或编辑 **Gemini → API Key** 账号。
2. 将「上游接口格式」设为 **OpenAI 兼容（Chat Completions）**。
3. 填入上游地址及密钥。地址可以是 `https://upstream.example.com` 或 `https://upstream.example.com/v1`。
4. 将账号绑定到 Gemini 分组，客户端使用该分组的 API Key。客户端仍调用 `/v1beta/models/{model}:generateContent` 或 `:streamGenerateContent`。

如果此前创建的是 OpenAI 平台账号，应新建 Gemini API Key 账号并填入同一上游地址和密钥。这里独立选择的是上游协议，账号和分组的平台仍为 Gemini。

账号凭据使用现有 JSON 字段保存，无需数据库迁移：

```json
{
  "base_url": "https://upstream.example.com/v1",
  "api_key": "<upstream key>",
  "api_protocol": "chat_completions"
}
```

`api_protocol` 未设置或为 `gemini` 时，沿用 Gemini 原生协议。OAuth 和 Service Account 账号不使用该选项。测试连接、模型同步及未保存凭据的模型预览均遵循所选协议。

## 支持范围

- 文本对话、系统指令、HTTP 图片及内嵌图片输入。
- 普通 JSON 回复、SSE 流式回复、思考文本，以及工具调用和工具结果。流式工具参数在收齐合法 JSON 后输出，支持同轮多个工具调用。
- 模型映射、常用采样参数、JSON 输出与响应 Schema、`thinkingLevel`。
- 公共模型列表和单模型查询，转换成 Gemini 模型资源格式。
- 沿用 Gemini 分组的调度、并发、错误切换及计费流程。缓存 Token 和思考 Token 转换后分别统计，避免重复计算思考 Token。OpenAI 兼容上游不套用 AI Studio 免费/付费档位的请求配额；账号与分组的通用限制仍生效。
- `countTokens` 使用本地估算，不发送付费生成请求，估算结果不计为实际使用量。

Gemini 专属能力没有标准 Chat Completions 等价项：搜索/代码执行等内置工具、缓存资源引用、自定义安全设置、非图片文件、音视频与图片生成、`topK` 和精确的 `thinkingBudget` 会返回明确的不支持错误。上游也必须支持所请求的图片、工具或 JSON Schema 功能。

断流、无效 JSON 和未完成的工具参数会报告失败。限流使用上游 `Retry-After`，缺省等待一分钟，不套用 AI Studio 每日配额重置时间。

## 验证

协议转换测试覆盖多轮工具调用、相同工具名与不同调用 ID、流式参数分片、缓存/思考 Token、无效和中断响应、模型列表。服务测试覆盖真实转发入口的请求格式、模型映射、错误切换、模型发现以及零上游调用的 Token 估算。前端测试覆盖创建、回填及保存上游协议。
