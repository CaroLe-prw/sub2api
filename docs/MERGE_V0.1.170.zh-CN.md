# Sub2API v0.1.170 上游合并方案与实施记录

> 审计日期：2026-08-02
> 本地基线：`866068f9d`（`fix: enforce group scheduling cost limits`）
> 上游发布基线：`v0.1.170^{}` = `c043c2477`
> 共同祖先：`7ceabb3fd`（v0.1.169）

## 1. 结论

建议以完整的上游 `v0.1.170` 标签做一次真实 merge，不要只挑利润控制的首个提交，也不要用上游版本覆盖本地分支。

最终目标不是在“本地实现”和“上游实现”之间二选一，而是：

1. 采用上游的利润控制执行链，保留它的请求级定价时刻、候选过滤、抢槽后终检、粘性会话延迟绑定、重试否决预算和非 Token 路径抑制。
2. 保留本地的绝对账号成本上限 `max_account_cost_multiplier`，把它作为上游动态利润阈值之外的第二道约束。
3. 保留本地的分组 OpenAI 调度模板、自定义策略、账号优先级、调度评分和 NewAPI 比率/余额同步。
4. 上游通用倍率自动写回与本地 NewAPI 同步并存，但同一个账号只能有一个倍率写入者。
5. 其余 v0.1.170 修复原则上完整接收；对共享文件做语义合并，不能整文件选择 `ours` 或 `theirs`。

利润控制部分，上游最终实现总体更完整。本地实现最值得保留的是“绝对成本上限”和现有管理端组件化设计；本地当前的隐式门控链路不应与上游门控并行运行，否则会出现两个不同口径的过滤器。

### 1.1 实施结果（2026-08-02）

本方案已经在 `merge/upstream-v0.1.170` 分支实际落地。合并前工作保存于 `backup/pre-v0.1.170-20260802`，并拆成以下提交，仓库根目录现有 `*.tar.gz` 构建产物没有加入版本控制：

- `2ba6ddad3`：可编辑 OpenAI 调度模板。
- `29cf4c5cf`：分组账号关系管理。
- `c8422b5ad`：GPT-5.6 priority 定价与 NewAPI 配额解析。
- `e27a58607`：本合并方案初稿。

随后对 `v0.1.170^{}` 执行真实 merge。23 个文本冲突文件均已完成语义合并，没有使用整仓或高风险共享文件的单边覆盖。

最终实现采用以下确定语义：

1. 运行时保留上游完整利润控制执行链；本地旧的 OpenAI 并行成本过滤器已移除。
2. `max_account_cost_multiplier` 是独立硬上限，不依赖 `profit_control_enabled`；空值表示“不设置独立硬上限”，不再隐式沿用分组售价。
3. 动态利润阈值、实际调度分组硬上限和 Composite/认证父分组硬上限统一由 `resolveGroupAccountCostThreshold` 计算，取最严格值。
4. 运行时否决、离线 `profit-preview` 和管理端调度评分共用账号倍率合法性、epsilon 和硬上限口径；不存在另一套比较公式。
5. 利润门只消费持久化 `accounts.rate_multiplier`。0 是合法成本；`nil`、负数、NaN 和 Inf 在门启用时拒绝。
6. 上游通用倍率同步与本地 NewAPI 同步保留，但同一账号只允许一个自动写入者；任何自动写入者存在时都拒绝手工单笔和批量改倍率。
7. 开启通用同步会关闭 NewAPI 同步；启用/保存 NewAPI 同步会关闭通用探测和通用倍率同步。仓储层在行锁内读取当前 `extra`，防止旧编辑表单覆盖并发更新后的 owner、密钥和同步快照。
8. 复制或创建账号不会继承 NewAPI 的加密身份、同步 owner 或运行快照。
9. 前端账号编辑使用“手工 / Sub2API 探测 / NewAPI”显式来源选择；倍率输入和来源提示按真正 owner 控制。
10. 上游其余发布功能与修复均保留，包括 Anthropic 中断用量、WS 关闭帧、流内 429、pool 容量重试、Responses 工具桥接、订阅窗口、审核代理、最新输入审计、筛选结果全选、批量删除并发限制和精简首页等。

### 1.2 实际数据模型与缓存改动

Ent `Group` schema 和生成代码已按字段并集重新生成，包含：

- 本地：`max_account_cost_multiplier`、`openai_scheduler_profile`、`openai_scheduler_config`。
- 上游：`profit_control_enabled`、`profit_min_margin`、`profit_safety_buffer`。

迁移文件保留本地和上游两组 192/193 原文件，并新增：

```text
195_group_profit_control_max_cost_auth_cache.sql
```

该迁移把绝对成本上限纳入 API Key auth cache 失效触发器。Auth snapshot 版本从 18 升至 19，查询投影、序列化、反序列化、缓存往返和失效测试均已覆盖 `max_account_cost_multiplier`。没有修改或重命名已经存在的 migration。

### 1.3 实际代码合并要点

利润门控：

- 上游 request/turn 级 `pricingAt`、候选预过滤、槽位后终检、否决预算和粘性延迟绑定完整保留。
- OpenAI 与其他平台门控通过同一个阈值 helper 叠加本地硬上限。
- 管理端调度评分只展示满足独立硬上限且倍率有效的账号，避免 UI 分数与实际调度资格冲突。
- `profit-preview` 与线上复用阈值和账号倍率 helper，并能区分 `manual`、`upstream_probe_sync`、`newapi_sync` 三种倍率来源。

倍率所有权：

- 管理 service 的单笔编辑、批量编辑均统一检查两类自动同步 owner。
- 通用探测 CAS 排除 `newapi_sync_enabled=true` 的账号。
- 通用账号更新在 `FOR NO KEY UPDATE` 行锁内保留 NewAPI 托管字段，并依据数据库当前 owner 拒绝陈旧的手工倍率写入。
- 专用 NewAPI 保存接口和通用探测接口互相关闭对方 owner，前端切换来源时按不会触发旧 owner 冲突的顺序保存。

管理端：

- 新增 `GroupProfitControlField.vue`，采用 Vue 3 `<script setup lang="ts">` 与 `defineModel`，集中管理利润开关、最低利润率和安全缓冲。
- `GroupAccountCostLimitField.vue` 保持独立，只负责绝对硬上限；两类字段在 UI 中明确提示“取最严格约束”。
- `EditAccountModal.vue`、`AccountsView.vue` 和倍率单元格同时保留本地 NewAPI/校准能力与上游通用探测能力。

### 1.4 已完成验证

后端使用 `golang:1.26.5-alpine` 容器执行：

```bash
go test ./...
```

结果通过，覆盖 `cmd/profit-preview`、Ent schema、handler、repository、service 和 migrations。为避免只读挂载造成 Ent schema 测试无法创建 `.entc` 临时目录，最终全量测试以当前宿主用户在可写挂载内执行；测试结束后没有遗留 `.entc` 源文件。

前端执行：

```bash
pnpm run test:run
pnpm run lint:check
pnpm run typecheck
pnpm run build
```

结果：216 个测试文件、1456 个测试用例全部通过；ESLint、`vue-tsc` 和 Vite 生产构建通过。构建仅有既有的动态/静态导入和大 chunk 警告，没有构建错误。

静态合并检查：

```bash
rg -n '^(<<<<<<<|=======|>>>>>>>)' backend frontend
git diff --check
```

均无错误。Ent 已从合并后的 schema 重新生成，避免了双边新增字段导致的运行时字段索引错位。

### 1.5 部署前仍需执行的环境验收

代码合并已经完成，但以下动作依赖生产配置或真实升级数据库，不应由通用 migration 自动替用户决定：

1. 用生产只读导出的分组、用户倍率和账号数据运行 `profit-preview`，确认每个主力模型在默认倍率与最差用户倍率下都有足够账号。
2. 在生产数据库副本分别演练“干净建库”和“已应用本地 191/192/193/194 后升级”两条路径，并核对 migration checksum。
3. 首次部署保持 `profit_control_enabled=false` 和通用倍率自动写回关闭；先观察探测结果，再逐账号、逐分组开启。
4. 对历史上可能同时开启 NewAPI 与通用同步的脏数据做一次查询；若存在，按本文规则保留 NewAPI owner 并关闭通用 owner。

## 2. 当前仓库状态

### 2.1 分支差异

相对共同祖先：

- 本地有 51 个提交。
- `v0.1.170` 有 61 个提交。
- `upstream/main` 还多一个版本号同步提交 `7e2e9ba05`；本次应先合并发布标签，而不是追随这个额外提交。

本地与上游已经是明显的双向演进，适合 merge，不适合 rebase，也不适合把几十个上游提交逐个 cherry-pick。

### 2.2 未提交修改

审计时工作区有 24 个已跟踪文件被修改，另有以下尚未跟踪的源文件：

- `frontend/src/components/admin/group/GroupAccountsModal.vue`
- `frontend/src/components/admin/group/__tests__/GroupAccountsModal.spec.ts`
- `frontend/src/views/admin/settings/OpenAISchedulerTemplateEditor.vue`

还有多份 `*.tar.gz` 构建产物。24 个已修改文件全部与上游发布涉及的文件有重叠，直接执行 merge 风险很高。

合并前必须先保存这些工作。不要执行 `git add -A`，否则很容易把构建包提交进去。

建议先拆成至少三个本地提交：

1. OpenAI 调度模板设置、后端 DTO/解析和管理端编辑器。
2. 分组账号管理弹窗及 `GroupsView.vue` 接入。
3. GPT-5.6 定价、NewAPI 客户端/余额等剩余修改。

如果当前修改还不适合正式提交，也应放在专门的 WIP 分支中；WIP 提交可以在合并完成后再整理，但不能只依赖未命名的 stash。

示例流程：

```bash
git switch -c backup/pre-v0.1.170
git status --short

# 按功能显式 git add 文件，不要添加仓库根目录的 tar.gz。
git add backend/internal/handler backend/internal/service frontend/src
git commit -m "wip: preserve scheduler settings and group admin changes"

git switch -c merge/upstream-v0.1.170
git config rerere.enabled true
git merge --no-ff 'v0.1.170^{}'
```

上面的 `git add` 只是示意。实际应继续按功能缩小每个提交的文件范围，并单独确认定价 JSON。

## 3. 关键实现对比

| 维度 | 本地实现 | 上游 v0.1.170 | 合并决定 |
| --- | --- | --- | --- |
| 控制模型 | 分组售价倍率与可选绝对成本上限 | 最低利润率 + 安全缓冲 | 使用上游动态阈值，同时保留绝对上限 |
| 用户专属分组倍率 | 门控默认口径没有完整复用用户覆盖 | 与最终计费同源，优先用户覆盖 | 采用上游 |
| 高峰跨界 | 门控和计费读取时刻可能不同 | 请求开始冻结；WS 每 turn 冻结 | 采用上游 |
| 候选过滤 | 已接入 OpenAI 调度器 | 五个平台统一覆盖，且保留原排序行为 | 采用上游，删除重复过滤 |
| 等待后复核 | 缺少完整的抢槽后终检 | 获取槽位后刷新倍率并再次否决 | 采用上游 |
| 粘性会话 | 可能在最终确认前影响绑定 | 终检通过后绑定；越线只跳过、不解绑 | 采用上游 |
| 非 Token 路径 | 图片、live、count_tokens 等边界不完整 | 显式抑制媒体、元数据、计数等路径 | 采用上游 |
| 账号成本来源 | OAuth 配置、校准探测值、本地倍率等多源 | 门控只消费持久化 `accounts.rate_multiplier` | 门控采用上游单一事实源；本地校准只负责写入/排序 |
| 未知成本 | 部分路径放行 | 门启用后 `nil`、负数、NaN、Inf 保守拒绝，0 可用 | 采用上游 |
| 失败策略 | 主要依赖当前查询结果 | 配置/刷新故障 fail-open 并告警 | 先采用上游；通过监控约束风险 |
| 离线预演 | 无统一工具 | `profit-preview` | 接收并扩展绝对上限口径 |
| Composite | 本地绝对上限可覆盖部分 OpenAI-compatible 路径 | 不允许直接开启动态利润控制 | 保留父分组绝对上限；动态利润策略仍取实际调度的具体分组 |

上游利润控制不是单个提交完成的。以下三个提交必须一起接收：

- `20ad5ec50`：主体实现。
- `fad2f215e`：修复图片意图耦合、选号上下文传播和粘性回退。
- `dec47e8fa`：修复策略泄漏、否决活锁和 WebSocket turn 定价。

只 cherry-pick 第一个提交会重新引入上游已经修掉的 P0/P1 问题。

## 4. 目标利润控制语义

### 4.1 统一公式

设：

- `D`：请求在 `pricingAt` 时刻的实际下游倍率，即用户分组覆盖（没有则分组默认）乘高峰倍率。
- `M`：实际被调度具体分组的最低利润率。
- `B`：实际被调度具体分组的安全缓冲。
- `C_group`：实际调度分组配置的绝对成本上限。
- `C_parent`：Composite/认证父分组配置的绝对成本上限。
- `U`：账号持久化的 `rate_multiplier`。

动态利润阈值：

```text
T_profit = D × (1 - M - B)
```

最终阈值取所有已启用约束的最小值：

```text
T_final = min(T_profit, C_group, C_parent)
eligible = U <= T_final
```

没有启用的约束不参与 `min`。比较继续使用上游的 epsilon 规则，避免 `DECIMAL(10,4)` 与浮点计算在边界值上误拒绝。

### 4.2 开关语义

推荐规则：

- `profit_control_enabled=true`：启用动态阈值。
- `max_account_cost_multiplier != nil`：独立启用绝对上限。
- 两者都有：取更严格者。
- 两者都没有：不安装利润门。

这会改变本地 `866068f9d` 之后“没有显式上限时仍按分组售价做隐式门控”的行为。推荐接受这个变化，使最终行为符合上游“默认关闭”的产品语义；上线前通过 `profit-preview` 找出需要继续受保护的分组并显式开启。

不要默认批量开启所有旧分组。上游启用门控后会保守拒绝没有有效 `rate_multiplier` 的账号，自动开启可能让历史脏数据导致整个分组无可用账号。

如果业务必须完全保持现状，可另外做一次性数据迁移，把已受本地隐式门控保护的具体平台分组设为：

```text
profit_control_enabled = true
profit_min_margin = 0
profit_safety_buffer = 0
```

但这应是经过预演后的显式上线选择，不应混入通用 schema migration。

### 4.3 配置来源与计费来源

必须区分两个分组：

- `billingGroup`：API Key 认证分组；决定用户售价倍率和高峰倍率。
- `scheduledGroup`：实际进行账号调度的具体平台分组；决定 `M`、`B` 和平台适用性。

Composite 场景中：

- `D` 必须来自 `billingGroup`，否则门控和最终扣费口径不一致。
- `M/B` 来自 `scheduledGroup`；Composite 本身不直接开启动态利润率。
- 本地绝对上限可以同时来自父分组和具体分组，最终取更小者。

选号返回值必须继续携带上游的实际生效 gate，handler 在抢槽后终检时重放该 gate，不能重新按入口分组猜测。

### 4.4 请求生命周期

保留上游完整顺序：

```text
请求/WS turn 开始
  -> 冻结 pricingAt 和 D
  -> 安装 token 请求利润门
  -> 过滤候选账号
  -> 在合格账号之间执行原排序、评分、熔断和负载策略
  -> 获取槽位/等待
  -> 用最新账号快照终检
  -> 不合格则释放槽位、加入本请求排除集、重新选号
  -> 合格后再绑定粘性会话
  -> 计费复用同一个 pricingAt
```

图片、视频、模型列表、用量、`count_tokens`、live 等非 Token 路径必须显式抑制门控。不要继续使用“请求里是否声明图片工具”作为是否装门的判断，因为 Responses 混合请求仍可能产生 Token 费用。

## 5. 倍率探测和自动写回的合并

### 5.1 两套能力不是互相替代

本地 NewAPI 同步比上游通用写回更专用，包含 NewAPI 分组比率、校准、余额、告警和并发写入保护；上游通用能力则覆盖 OpenAI、Anthropic、Gemini、Grok、Antigravity 的全部 API Key 账号。

因此不要删除本地：

- `backend/internal/service/newapi_sync.go`
- `backend/internal/service/newapi_client.go`
- `backend/internal/service/newapi_balance*`
- `frontend/src/components/account/NewAPISyncSettings.vue`
- `frontend/src/components/account/NewAPIBalanceSnapshot.vue`
- 对应路由、仓储 CAS、测试和告警逻辑

同时完整接收上游：

- `f3a3d8684`：探测扩展到全部 API Key 平台。
- `56f3d3c9b`：官方域抑制、unsupported 长退避和非 OpenAI 调度信任边界。
- `b0f5007f0`：可选自动写回。
- `0b6b4ea95`：写回所有权、CAS、值域校验、日志和快照治理。

### 5.2 单写入者规则

建议增加统一判定函数，返回账号倍率所有者：

```go
type AccountRateOwner string

const (
    AccountRateOwnerManual        AccountRateOwner = "manual"
    AccountRateOwnerUpstreamProbe AccountRateOwner = "upstream_probe"
    AccountRateOwnerNewAPISync    AccountRateOwner = "newapi_sync"
)
```

规则：

1. `newapi_sync_enabled=true` 时，NewAPI 同步是唯一写入者，强制关闭 `upstream_billing_rate_sync_enabled`；保留本地已有的“通用探测跳过 NewAPI 同步账号”。
2. `upstream_billing_rate_sync_enabled=true` 时，上游探测是唯一写入者，禁止启用 NewAPI 自动同步。
3. 两者都关闭时，管理员可以手工修改倍率。
4. 开启任一自动同步后，单账号编辑和批量修改倍率都应返回冲突错误。
5. 切换所有者必须在同一个仓储事务/CAS 中完成，不能先关 A 再异步开 B。
6. 对历史上意外同时开启两个标志的账号，优先保留 NewAPI 同步、关闭通用写回，并输出一次结构化告警。

上游自动写回只接受 `0 < rate <= 100`，写入精度为四位小数；0 和越界值保留原倍率并记录告警。本地 NewAPI 校准已经存在合法 0 成本语义，不能用上游写回校验函数去限制 NewAPI 路径。利润门本身仍应允许持久化的 0 倍率账号。

### 5.3 调度信任边界

合并后应明确：

- 利润门只读持久化 `accounts.rate_multiplier`。
- 非 OpenAI 平台的瞬时“上游自报倍率”不参与调度排序。
- OpenAI 可以保留本地“新鲜且校准后的倍率排序”，但它只是排序信号，不能绕过持久化倍率门控。
- NewAPI 同步成功写回 `rate_multiplier` 后，上游利润门自然消费同一个值，不需要专门适配 NewAPI 协议。

### 5.4 管理端表现

`EditAccountModal.vue` 和 `BulkEditAccountModal.vue` 必须同时保留：

- 本地 NewAPI 同步配置、来源选择器、Fast 模式和配额相关字段。
- 上游 `upstream_billing_probe_enabled` 与 `upstream_billing_rate_sync_enabled`。
- 同步开启后倍率输入禁用和后端冲突提示。

`UpstreamBillingRateCell.vue` 建议统一显示一个权威来源标签：

- 手工维护
- 上游探测托管
- NewAPI 同步托管
- 已探测但未托管（仅展示）

前端禁用只用于改善交互，后端所有权校验和 CAS 才是安全边界。

账号复制时推荐复制当前数值倍率，但重置两种自动同步开关、探测快照和 NewAPI 密钥/身份状态，避免新账号拿旧账号的自动化身份继续写入。

## 6. 数据库与 Ent 合并

### 6.1 migration 文件

本地已有：

- `191_normalize_account_group_default_priority.sql`
- `192_initialize_account_group_priorities.sql`
- `193_add_group_max_account_cost_multiplier.sql`
- `194_add_group_openai_scheduler_policy.sql`

上游新增：

- `192_group_profit_control.sql`
- `193_group_profit_control_auth_cache_invalidation.sql`

迁移器以完整文件名作为记录键，并按完整文件名排序，所以重复数字前缀不会互相覆盖。这些文件名也是已经可能在生产数据库中落过 checksum 的身份，禁止重命名、修改旧文件内容或把两个 migration 拼成一个文件。

合并后保留全部文件。排序结果中两组 192/193 迁移互相独立，可以正常执行。

如果绝对成本上限要进入 API Key auth snapshot 或缓存失效触发器，新增一个后续 migration，例如：

```text
195_group_profit_control_max_cost_auth_cache.sql
```

不要修改上游 `193_group_profit_control_auth_cache_invalidation.sql`。同时递增 auth snapshot 版本，并补上本地绝对上限的投影和缓存失效测试。

### 6.2 Ent schema

`backend/ent/schema/group.go` 应合并为字段并集：

本地字段：

- `max_account_cost_multiplier`
- `openai_scheduler_profile`
- `openai_scheduler_config`

上游字段：

- `profit_control_enabled`
- `profit_min_margin`
- `profit_safety_buffer`

只手工解决 schema。`backend/ent/group.go`、`group_create.go`、`group_update.go`、`mutation.go`、`migrate/schema.go` 等生成文件不要逐段人工拼接。

建议顺序：

1. 先把生成文件临时选择一侧，使 Go 源码不再带冲突标记。
2. 手工把 `ent/schema/group.go` 合成字段并集。
3. 执行 `make -C backend generate`，重新生成 Ent 和 Wire。
4. 检查 `wire.go` 的 provider 集合是两边并集，再确认 `wire_gen.go` 没有丢掉本地 NewAPI/配额/告警服务或上游新增依赖。

## 7. 前端合并方案

### 7.1 GroupsView

不能整文件选择任一侧。`GroupsView.vue` 需要同时保留：

- 本地 `GroupAccountCostLimitField.vue`。
- 本地 `GroupSchedulerPolicyField.vue` 和调度模板逻辑。
- 当前未提交的 `GroupAccountsModal.vue`。
- 上游利润控制字段及预览/校验。

推荐继续使用 Vue 3 `<script setup lang="ts">`，让 `GroupsView.vue` 只负责列表、弹窗组合和保存编排。新增同级组件：

```text
frontend/src/components/admin/group/GroupProfitControlField.vue
```

职责划分：

- `GroupProfitControlField.vue`：开关、最低利润率、安全缓冲、支持平台提示。
- `GroupAccountCostLimitField.vue`：绝对成本上限。
- `GroupSchedulerPolicyField.vue`：调度配置。
- `GroupAccountsModal.vue`：分组账号关系维护。

上游 `groupsProfitControl.ts` 的纯转换/校验逻辑应保留。首次合并时可以维持原路径以减少改动；合并稳定后再移动到 `components/admin/group/` 功能目录。

编辑模型建议保持百分比 UI 与小数 API 的单一转换点，不要在模板和保存函数中各自乘除 100。平台改变时清理不支持的利润配置，但不要清理本地绝对上限和调度策略，除非后端业务规则明确要求。

### 7.2 AccountsView

必须同时保留本地：

- 调度评分展示、排序与“不合格账号不显示评分”。
- 校准倍率排序。
- NewAPI 同步/余额入口。
- 手动调度暂停、优先级等私有功能。

并接收上游：

- 按筛选结果全选。
- 批量删除并发限制对应的前端调用和状态。
- 通用倍率自动同步来源提示。

全选状态应由上游的纯工具 `accountSelection.ts` 管理，不要把跨页 ID 集合继续塞回表格行状态。批量操作前仍由后端重新校验目标账号。

### 7.3 SettingsView 和定价

当前未提交的调度模板设置与上游内容审核代理、最新输入审计、精简首页、支付方式等设置共享 DTO、解析器和 `SettingsView.vue`。这些文件需要按 setting key 做并集，不能按代码块外观判断保留哪侧。

`model_prices_and_context_window.json` 同时包含本地 GPT-5.6 优先级改动和上游 Codex Auto-review 调价。应按模型 key 逐项核对：

- 保留上游 Auto-review 有证据支持的值和删除项。
- 保留本地 GPT-5.6 priority 的修正。
- 更新对应 service/repository 测试快照。
- 禁止对整个 JSON 使用 `ours` 或 `theirs`。

## 8. 上游发布功能处置矩阵

| v0.1.170 功能 | 本地重叠 | 处理方式 |
| --- | --- | --- |
| 分组级利润控制 | 高 | 采用上游执行链，叠加本地绝对上限 |
| 槽位后二次复核、粘性延迟绑定 | 本地不完整 | 完整采用上游 |
| 请求级定价时刻 | 本地口径不一致 | 完整采用上游，并确保计费复用同一时刻 |
| 五平台门控与非 Token 排除 | 本地主要覆盖 OpenAI | 采用上游；Composite 仅保留本地绝对上限扩展 |
| `profit-preview` | 无 | 采用并扩展绝对上限、约束来源统计 |
| 全 API Key 平台探测 | 部分重叠 | 采用上游平台资格和抑制规则 |
| 倍率自动同步 | 与 NewAPI 同步高度重叠 | 两套保留，实施单写入者规则 |
| 同步托管下拒绝手工修改 | 部分有 | 后端统一检查两类 owner |
| 倍率值域、日志、快照 | 本地协议不同 | 上游规则仅用于通用探测写回 |
| 账号倍率来源提示 | 有本地来源 UI | 合成一个来源枚举和徽标 |
| 内容审核代理 | 无直接替代 | 采用上游 |
| 仅审计最新输入 | 现有审计模块有改动 | 语义合并设置、快照、前端和测试 |
| 筛选结果全选 | 本地账号页有大量改动 | 采用上游选择工具，保留本地列和过滤器 |
| 批量删除并发限制 | 无直接替代 | 采用上游后端并发限制和测试 |
| 精简首页 | 无直接替代 | 采用上游，保留本地导航/品牌设置 |
| Ollama 官方域抑制、unsupported 退避 | 本地探测有扩展 | 采用上游规则 |
| 非 OpenAI 自报倍率不影响排序 | 与本地校准排序相关 | 采用上游平台边界；只保留 OpenAI 校准排序 |
| 模型广场 UI | 低 | 采用上游，跑现有样式测试 |
| Codex Auto-review 费率 | 与脏定价文件冲突 | 按模型逐项合并 |
| Anthropic 中断用量记录 | 无 | 完整采用，P0 回归测试 |
| OpenAI WS 取消关闭帧 | 无 | 完整采用 |
| OpenAI 流内 429 重试 | 无 | 完整采用 |
| pool 流式容量重试 | 无 | 完整采用 |
| OAuth Responses 保留 Codex 工具 | 本地 Responses 有改动 | 采用上游测试保护行为 |
| 缺失 instructions 默认值 | 无 | 完整采用 |
| 过期加密压缩恢复 | 无 | 完整采用 |
| Claude 多 system 分类 | 无 | 完整采用 |
| 工具输出图片桥接 | 无 | 完整采用 |
| Grok pool 冷却与 ping 过滤 | 本地 OpenAI-compatible 文件有改动 | 采用上游专用过滤器和测试 |
| 订阅窗口对齐 | 与本地配额重置高度相邻 | 语义合并，保留手动配额重置能力 |
| 支付设置修复 | 无直接替代 | 完整采用 |
| data URL 图片任务解码 | 无 | 完整采用 |
| SMTP 测试/发送统一 | 无 | 完整采用 |

## 9. 冲突解决顺序

推荐按依赖顺序处理，避免在生成代码和大 Vue 文件上反复解冲突。

### 阶段 A：保存基线

1. 保存全部未提交源代码，排除构建包。
2. 记录合并前测试结果和生产配置导出。
3. 给合并前提交打备份 tag。
4. 从该提交创建 `merge/upstream-v0.1.170`。

### 阶段 B：数据模型和生成代码

1. 保留所有 migration 文件。
2. 合并 `ent/schema/group.go` 字段并集。
3. 合并 service `Group`、DTO、mapper、repository 投影。
4. 递增 auth snapshot 版本并纳入绝对上限（如果热路径从快照读取）。
5. 重新生成 Ent；暂不手改生成文件。

### 阶段 C：倍率数据源

1. 合并 `upstream_billing_probe.go`，保留 NewAPI 调度器和跳过逻辑。
2. 引入统一倍率 owner 判定。
3. 合并 repository CAS，保证探测标志、owner 和倍率原子更新。
4. 合并单账号、批量编辑、复制账号和 DTO。
5. 先跑探测/NewAPI 单测，再继续利润门控。

### 阶段 D：利润门控

1. 接收上游 `gateway_profit_control.go`、`openai_profit_control.go`、request pricing context 和所有入口标记。
2. 把本地绝对上限作为 gate 中的附加阈值，不再调用本地旧的第二套判断。
3. 保留上游 `AccountSelectionResult` 的 gate 传播和终检刷新。
4. 保留上游 failover veto budget，防止所有候选被终检否决时活锁。
5. 延后粘性绑定；越线粘性账号只跳过，不清除绑定。
6. 扩展 `profit-preview` 与线上相同的统一 helper，避免预演和运行时公式漂移。

### 阶段 E：管理端

1. 先合并 TypeScript 类型和 API client。
2. 合并账号编辑/批量编辑和倍率来源单元格。
3. 合并 `AccountsView.vue` 的全选结果与本地调度评分。
4. 合并 `GroupsView.vue`，把利润配置抽成子组件。
5. 合并设置页与本地调度模板编辑器。
6. 最后补齐中英文 i18n，使用 key 集合测试防止漏项。

### 阶段 F：其他修复和生成

1. 接受 release matrix 中其余上游行为。
2. 逐模型解决定价 JSON。
3. 合并 `wire.go` provider 集合。
4. 执行 `make -C backend generate`，核对生成差异。
5. 运行完整验证后提交 merge commit。

## 10. 已预判的文本冲突

基于共同祖先执行 merge-tree，当前已提交分支与发布标签之间有 56 个冲突块，分布在 23 个文件：

```text
backend/ent/group.go
backend/internal/handler/dto/mappers.go
backend/internal/handler/dto/types.go
backend/internal/repository/account_repo.go
backend/internal/repository/account_repo_upstream_billing_probe_cas_test.go
backend/internal/service/admin_account.go
backend/internal/service/openai_account_scheduler.go
backend/internal/service/pricing_service_test.go
backend/internal/service/subscription_reset_quota_test.go
backend/internal/service/subscription_service.go
backend/internal/service/upstream_billing_probe.go
backend/internal/service/upstream_billing_probe_test.go
backend/resources/model-pricing/model_prices_and_context_window.json
frontend/src/components/account/EditAccountModal.vue
frontend/src/components/account/__tests__/EditAccountModal.spec.ts
frontend/src/components/account/__tests__/UpstreamBillingRateCell.spec.ts
frontend/src/i18n/locales/en/admin/accounts.ts
frontend/src/i18n/locales/en/admin/settings.ts
frontend/src/i18n/locales/zh/admin/accounts.ts
frontend/src/i18n/locales/zh/admin/settings.ts
frontend/src/types/index.ts
frontend/src/views/admin/AccountsView.vue
frontend/src/views/admin/GroupsView.vue
```

未提交修改保存为提交后，`SettingsView.vue`、settings DTO/解析器、定价和 NewAPI 文件还会增加实际冲突或语义重叠。没有出现文本冲突的共享文件也必须检查；Git 自动合并成功不代表业务语义正确。

高风险文件的处理原则：

| 文件/区域 | 禁止做法 | 正确做法 |
| --- | --- | --- |
| Ent 生成文件 | 逐块拼接或长期保留一侧 | 合并 schema 后重新生成 |
| `wire_gen.go` | 人工维护生成结果 | 合并 `wire.go` 后重新生成 |
| `openai_account_scheduler.go` | 同时保留两套成本过滤 | 保留一个统一 gate，排序逻辑做并集 |
| `upstream_billing_probe.go` | 用通用写回替换 NewAPI 同步 | 平台探测合并，倍率 owner 分流 |
| `admin_account.go` | 仅前端禁用倍率输入 | 后端单笔、批量、CAS 全部校验 owner |
| `GroupsView.vue` | 整文件选择一侧 | 组件化合并利润、绝对上限、策略和账号弹窗 |
| `AccountsView.vue` | 丢弃本地列或上游跨页选择状态 | 合并纯 selection utility 与本地表格能力 |
| 定价 JSON | 整文件选择一侧 | 按模型 key 和测试证据合并 |
| subscription service | 只保留配额窗口修复或本地重置 | 两套能力并存，统一窗口边界口径 |

## 11. 测试与验收

### 11.1 当前可用基线

审计阶段已确认前端 typecheck 通过，利润上限/调度评分相关的 3 组测试共 11 个用例通过。当前环境没有 Go 工具链，因此后端基线没有执行；正式合并环境必须先补跑后端测试，不能把“未执行”当作“通过”。

### 11.2 生成与静态检查

```bash
make -C backend generate
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
```

生成后检查没有冲突标记和意外未生成差异：

```bash
rg -n '^(<<<<<<<|=======|>>>>>>>)' backend frontend
git diff --check
```

### 11.3 后端定向测试

在 `backend/` 下至少执行：

```bash
go test ./internal/service -run 'Profit|UpstreamBilling|NewAPI|OpenAIAccountScheduler|PartialUsage|Subscription'
go test ./internal/handler/... -run 'Profit|Failover|WebSocket|RateLimit|BatchDelete'
go test ./internal/repository/... -run 'Profit|UpstreamBilling|AuthCache|Scheduler'
go test ./cmd/profit-preview
```

然后执行完整测试和 lint：

```bash
make -C backend test
```

### 11.4 前端定向测试

```bash
pnpm --dir frontend exec vitest run \
  src/components/admin/group/__tests__/GroupAccountCostLimitField.spec.ts \
  src/components/admin/group/__tests__/GroupSchedulerPolicyField.spec.ts \
  src/components/admin/group/__tests__/GroupAccountsModal.spec.ts \
  src/views/admin/__tests__/groupsProfitControl.spec.ts \
  src/components/account/__tests__/EditAccountModal.spec.ts \
  src/components/account/__tests__/UpstreamBillingRateCell.spec.ts \
  src/views/admin/__tests__/AccountsView.selectAllResults.spec.ts \
  src/views/admin/__tests__/AccountsView.schedulerScore.spec.ts \
  src/views/admin/__tests__/SettingsView.spec.ts
```

最后执行：

```bash
make test-frontend
make build
```

### 11.5 利润控制验收矩阵

以下场景必须都有测试：

1. OpenAI、Anthropic、Gemini、Grok、Antigravity 的 Token 请求均能安装正确门控。
2. 图片、视频、模型列表、用量、`count_tokens`、live 不安装门控。
3. 用户分组倍率覆盖分组默认倍率。
4. 请求排队或重试跨越高峰窗口，门控和计费仍使用请求开始时刻。
5. Responses WebSocket 每个 turn 重新冻结时刻。
6. `U == T_final` 放行，略高于 epsilon 才拒绝。
7. `rate_multiplier=nil`、负数和非有限值拒绝；0 成本放行。
8. 仅动态利润、仅绝对上限、两者同时、两者都关闭四种组合。
9. Composite 父分组绝对上限与具体成员分组动态阈值取最小值。
10. 等待获得槽位后倍率被提高，终检释放槽位并重选。
11. 粘性账号越线时跳过但不解绑，倍率恢复后可自动回归。
12. 全部账号被终检否决时有次数预算并正常返回无可用账号，不活锁。
13. 配置/快照读取失败符合 fail-open 约定，并产生可观测日志。
14. `profit-preview` 与线上 helper 对同一输入输出完全一致。

### 11.6 倍率同步验收矩阵

1. 五个平台 API Key 账号均可探测。
2. 官方域和 `ollama.com` 被抑制；手动探测不受 unsupported 退避影响。
3. 通用同步成功写回 `(0, 100]` 的四位小数倍率，快照记录同步值。
4. 0、负数、NaN、Inf、`>100` 不覆盖旧值，探测本身仍可为成功状态。
5. 开启通用同步后，单账号和批量手工修改都被拒绝。
6. 开启 NewAPI 同步后，同样拒绝手工修改。
7. 两种同步不能同时开启；并发切换不能产生双写。
8. NewAPI 0 成本校准仍可写入，并能被利润门识别为合法 0。
9. 非 OpenAI 瞬时自报倍率不改变调度排序。
10. 复制账号不复制自动化身份和开关。

### 11.7 数据库升级测试

至少验证两条路径：

1. 从干净数据库执行全部 migration。
2. 从已经应用本地 191/192/193/194 的生产形态升级，再执行上游 192/193 和新 195。

两条路径都检查：

- migration checksum 无变化。
- `groups` 同时存在本地调度字段和上游利润字段。
- API Key auth 查询包含需要的投影。
- 修改任一利润/绝对上限字段会使相关 auth cache 失效。
- 老数据默认不意外开启动态利润控制。

## 12. 上线步骤

推荐分两次开启能力，而不是合并后立即全开。

### 第一次部署：只合代码，保持开关关闭

1. 部署 schema、代码和管理端。
2. 保持所有分组 `profit_control_enabled=false`。
3. 保持通用 `upstream_billing_rate_sync_enabled=false`。
4. 确认 NewAPI 本地同步仍正常，观察探测、调度、计费和 Anthropic 部分用量日志。
5. 导出分组与账号配置，运行扩展后的 `profit-preview`。

### 第二次部署/配置变更：逐组开启

1. 先在非核心分组开启通用倍率探测，只观察不写回。
2. 核对自报倍率与当前人工/NewAPI 倍率差异。
3. 对非 NewAPI 账号小批量开启通用写回。
4. 先设置较保守的安全缓冲，再逐组开启利润控制。
5. 观察候选数量、invalid-rate、threshold veto、终检 veto 和无可用账号比例。
6. 最后再决定是否把本地绝对上限配置到 Composite 父分组。

上线前应定义告警：

- 某启用分组的合格账号数为 0。
- `profit_control_account_refresh_failed` 或配置读取失败持续出现。
- 自动倍率写回被值域拒绝。
- 同一账号检测到双 owner。
- Anthropic 中断流有 partial usage 但没有最终 usage record。

## 13. 回滚

应用层紧急回滚顺序：

1. 先关闭分组利润控制和通用倍率自动写回。
2. 确认 NewAPI 同步 owner 未被误改。
3. 回滚到合并前镜像，或对 merge commit 执行 `git revert -m 1 <merge-commit>`。

本次上游和建议新增的数据库字段/触发器都是增量结构。应用回滚时不要删除或改写已经执行过的 migration；旧应用可以忽略新增列。真正需要删除数据库结构时应另发向前兼容的清理 migration，而不是回写旧 migration。

## 14. 完成标准

只有同时满足以下条件，才可认为合并完成：

- 本地 51 个提交提供的私有能力没有因上游 merge 被删除。
- 三个上游利润控制提交的后续修复全部保留。
- 运行时只存在一个统一利润 gate，没有本地/上游双重过滤。
- 绝对成本上限与动态利润阈值按统一公式生效。
- NewAPI 和通用探测写回满足单账号单写入者。
- 所有 migration 原文件 checksum 保持不变，Ent/Wire 由源定义重新生成。
- 前后端完整测试、构建和两条数据库升级路径通过。
- `profit-preview` 与线上门控共享公式，并在正式开启前完成生产配置预演。
- 首次部署默认不开启新利润门和通用写回，具备可观察、可分批和可回滚能力。
