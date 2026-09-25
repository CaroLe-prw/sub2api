# Responses 流式延迟诊断

此功能需要部署包含本次修改的后端。旧请求没有记录逐阶段时间，不能回填。

## 查询

HTTP `/v1/responses` 和 `/v1/responses/compact` 流式请求结束后，后端输出一条 `INFO` 日志：

```text
gateway.response_stream_timing
```

日志携带请求上下文中的 `request_id` 和 `client_request_id`，计时数据位于 `stream_timing`。
容器部署可查：

```bash
docker logs --since 30m sub2api 2>&1 | grep -F 'gateway.response_stream_timing'
```

追加 `| grep -F '完整客户端请求ID'` 可以定位单条请求。容器名称按实际部署替换。

要在管理员后台查询，先在“运维监控 → 系统日志”勾选“将访问日志写入数据库”，保存并应用，日志级别使用 `info` 或 `debug`。随后复现新请求，在 `client_request_id` 中填写去掉 `client:` 前缀后的完整 ID，关键词填 `gateway.response_stream_timing`。这条日志的组件为 `http.access.stream_timing`，沿用访问日志入库开关；不需要将正常请求标为警告。

## 读数

所有 `*_ms` 都是从请求进入响应诊断采集时起算的毫秒数；字段缺失表示没有观测到，不能解释为 0。它们不是使用记录里单次转发的 `first_token_ms`，也不是客户端实收时间。

| 字段 | 含义 |
| --- | --- |
| `upstream.first_event.observed_ms` | 最后一次捕获的上游 HTTP 响应中，读到首个完整 Responses SSE 事件的时间 |
| `upstream.first_content.observed_ms` | 读到首个有效内容事件的时间 |
| `downstream.first_event` | 实际写入响应流的首个 Responses 事件，可与上游的类型和序号对照，识别开头事件缺失 |
| `downstream.first_content.write_started_ms` | 开始写出首个有效内容事件首段字节的时间 |
| `downstream.first_content.observed_ms` | 首个有效内容事件的完整字节已从底层 `Write` 返回的时间 |
| `downstream.first_content.flush_started_ms` | 第一次覆盖该完整事件的显式 `Flush` 开始时间 |
| `downstream.first_content.flush_completed_ms` | 该次 `Flush` 返回时间；不保证客户端已经收到 |
| `upstream.compaction_started` / `upstream.compaction_completed` | 上游压缩进度开始、压缩条目完成事件，避免把开始压缩当成完成结果 |
| `upstream_responses` | 本次请求捕获过的 HTTP 响应次数；不包含拿到响应头之前的连接失败，不能当作完整重试次数 |

每个事件包含 `type`、可用时的 `sequence_number`。有效内容还包含 `kind`：`text`、`reasoning`、`tool`、`image` 或 `compaction`。空白文字和心跳不算有效内容；压缩进行中和加密推理元数据也不算完成结果。日志不包含回答正文、工具参数、请求体或密钥。

比较收发时间前，先检查两侧的事件类型和序号是否对应同一个事件。上游首个内容是空白时会跳过；若首个有效事件本身被中转丢失，两侧 `first_content` 可能是不同事件，时间差不能直接算作缓冲耗时。

例如，同一有效事件：

- 上游 `observed_ms=90000`，下发刷新 `flush_completed_ms=90020`：本次有效内容大部分时间是在上游读取之前等待。
- 上游 `observed_ms=6000`，下发 `write_started_ms=90000`：重点检查中转读取后到写出之间的等待。
- 下发 `write_started_ms=6000`，`observed_ms=90000`：底层写入出现了长时间阻塞。
- 下发 `observed_ms=6000`，刷新 `flush_started_ms=90000`：写入后未及时调用刷新。
- 刷新 `flush_started_ms=6000`，`flush_completed_ms=90000`：刷新调用本身出现了长时间阻塞。
- 刷新在 6 秒完成、客户端同一事件到 90 秒才收到：继续检查反向代理、网络、客户端读取和显示。

## 范围和限制

- 复用既有响应原文采集，不改写、预读、重排或额外刷新响应。
- 计时基于实际 `Read` / `Write` 返回时观察到的完整 SSE 事件，不是上游服务器内部生成时间。
- 仅保留最后一次上游 HTTP 响应；两侧采用同一个本地计时起点，包含之前尝试的等待。
- 每侧最多保留 1024 个读写时间边界，最多 1024 个刷新边界，响应正文沿用既有捕获上限。`truncated=true` 表示至少一项捕获达到上限；缺失字段此时不能证明事件没有发生。
- `unparsed_events` 非零表示存在无法解析的 JSON 事件。未闭合的 SSE 尾部不会被当作完整事件。
- 日志在请求处理结束时生成。仍在运行、进程被终止的请求没有完成汇总。
- WebSocket 入站不在此范围。HTTP 转 WebSocket 上游时可能只有下发记录。
- 诊断采集本身不改变响应刷新或首字计时口径。

## 首批内容刷新

包含首批内容提交修复的版本中，Responses 原生转发路径在首个可下发事件和首个可见内容的完整 SSE 边界及时刷新，不等待后续读取队列清空，也不依赖 `first_token_ms` 是否已经记录。暂存内容成功写出后才标记为已提交，避免后续输出跳过尚未发送的开头事件。

“历史兼容（语义事件）”和“真实可见输出”的统计规则保持不变。空结构事件仍可在有效输出前暂存，以保留无输出失败时的换号能力；因此历史兼容首字仍可能早于实际内容下发时间。比较下游体验时，应继续使用同一事件的读、写、刷新时间，而不是把后台首字当作客户端实收时间。
