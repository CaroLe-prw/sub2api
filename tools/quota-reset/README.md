# Sub2API 套餐配额重置工具

这个目录是一套独立 Docker 工具，用外部脚本按公告分组重置订阅套餐配额，不需要修改 Sub2API 源码。

## 文件说明

- `reset-subscription-daily-quota-from-announcement.mjs`：定时脚本，按公告分组重置日限额。
- `manual-reset-subscription-quota-from-announcement.mjs`：手动脚本，默认重置日限额和周限额。
- `entrypoint.sh`：容器入口，支持 `schedule`、`once`、`manual` 三种模式。
- `Dockerfile`：构建本地镜像，并预装 `postgresql-client`。
- `docker-compose.yml`：运行容器，加入外部网络 `infra_infra-network`。
- `quota-reset.env`：你的实际配置文件，包含 API 地址、公告 ID、数据库连接等。
- 状态文件在容器内：`/data/subscription-daily-quota-reset-state.json`。

## 初始化

```bash
cd tools/quota-reset
chmod 600 quota-reset.env
```

编辑 `quota-reset.env`：

```ini
SUB2API_BASE_URL=http://sub2api:8080
SUB2API_ADMIN_API_KEY=admin-...

ANNOUNCEMENT_ID=2
RESET_INTERVAL_HOURS=5
RUN_EVERY_SECONDS=300
TZ=Asia/Shanghai

ANNOUNCEMENT_PUBLISH_MODE=create
CREATED_ANNOUNCEMENT_NOTIFY_MODE=popup
QUOTA_RESET_LABEL=5小时配额（日限额）
```

`ANNOUNCEMENT_ID` 是分组来源公告。脚本会读取这个公告里的 `group_ids`，只重置这些套餐组下的订阅。

`ANNOUNCEMENT_PUBLISH_MODE=create` 表示每次重置成功后新建一条公告通知用户。新公告会复制来源公告的分组规则，所以以前读过公告 2 的用户也能看到新的通知。

`QUOTA_RESET_LABEL` 控制公告里展示的重置范围。比如：

```ini
QUOTA_RESET_LABEL=5小时配额（日限额）
QUOTA_RESET_LABEL=每日配额
QUOTA_RESET_LABEL=每周配额
QUOTA_RESET_LABEL=每月配额
```

定时脚本默认会发出类似这样的公告：

```text
5小时配额已重置
本次重置范围：5小时配额（日限额）
```

如果你想改回直接更新公告 2，而不是每次新建公告：

```ini
ANNOUNCEMENT_PUBLISH_MODE=update
```

## Docker 网络

当前配置会加入外部网络 `infra_infra-network`：

```bash
docker network inspect infra_infra-network
docker ps --filter name=sub2api
```

API 地址默认使用：

```ini
SUB2API_BASE_URL=http://sub2api:8080
```

数据库默认使用你当前 infra 网络里的 PostgreSQL：

```ini
DATABASE_HOST=shared-postgres
DATABASE_PORT=5432
DATABASE_USER=sub2api
DATABASE_PASSWORD=你的数据库密码
DATABASE_DBNAME=sub2api
DATABASE_SSLMODE=disable
```

## 避免 0 点额外重置

Sub2API 的订阅日限额窗口本身是 24 小时。官方 Admin reset API 会把 `daily_window_start` 写成当天 0 点，所以如果外部脚本在 22:00 重置，Sub2API 可能在下一个 0 点又触发一次懒重置。

定时脚本通过 `/admin/subscriptions` 分页获取订阅后，会检查
`daily_window_start`、`weekly_window_start` 和 `monthly_window_start`：

- 三个字段都有值：执行日限额重置；
- 任一字段为 `NULL`：跳过该订阅，不提前启动尚未完整激活的配额窗口。

定时脚本和手动脚本都使用这条筛选规则。

定时脚本开启下面配置后，会在官方 API 重置日限额后，再用 `psql` 把
`daily_window_start` 修正为真实重置时间：

```ini
PATCH_DAILY_WINDOW_START=1
PATCH_DAILY_WINDOW_START_STRICT=1
```

这样三个窗口均为空的新订阅会等到用户首次实际请求时，由 Sub2API 自己初始化三个窗口；已经完整激活的订阅，其 24 小时日窗口会从真实重置时间开始计算。

如果 PostgreSQL 短时间连接打满，比如出现 `too many clients already`，定时脚本和手动脚本会对 `psql` 预检/窗口修正自动重试。默认配置如下：

```ini
PSQL_RETRY_ATTEMPTS=5
PSQL_RETRY_DELAY_MS=3000
PSQL_RETRY_MAX_DELAY_MS=30000
```

手动脚本默认会对本次选择的日/周窗口执行同类修正：

```ini
MANUAL_WINDOW_START_MODE=refresh
```

`refresh` 表示刷新日/周窗口有效期。比如手动重置周限额成功后，
`weekly_window_start` 会写成真实重置时间，下次刷新时间就是约 7 天后。
月限额的 `monthly_window_start` 不会刷新；如果本次选择了月限额，只清零月用量并保留原月窗口时间。

如果只想清零本次用量，不想刷新日/周窗口有效期，可以改成：

```ini
MANUAL_WINDOW_START_MODE=preserve
```

`preserve` 会在官方 API 重置成功后，把选中窗口的 `*_window_start`
恢复成重置前的值，因此下次刷新时间仍沿用原来的窗口。月限额在两种模式下都会保留原窗口时间。

实际执行手动重置前，脚本会先用 `psql` 检查数据库连接是否可用；检查失败时不会调用 Admin reset API。

已经存在的“部分窗口有值、部分窗口为 `NULL`”记录也会被跳过，脚本不会擅自重置其周/月用量或修改窗口。

如果当前这一轮已经重置过，只想修正完整激活订阅的日窗口起点，不想再次清零用量：

```bash
docker compose run --rm -e NOTICE_ONLY=1 -e UPDATE_ANNOUNCEMENT=0 -e PATCH_DAILY_WINDOW_START=1 quota-reset once
```

## 启动定时任务

第一次启动或改了脚本、`entrypoint.sh`、`Dockerfile` 时：

```bash
docker compose up -d --build
docker compose logs -f quota-reset
```

容器每 `RUN_EVERY_SECONDS` 秒醒来检查一次。脚本会根据状态文件里的 `last_success_at` 判断是否到达 `RESET_INTERVAL_HOURS`，没到时间就跳过。

只改 `quota-reset.env` 时，不需要 rebuild，只要重建容器让环境变量生效：

```bash
docker compose up -d --force-recreate
```

## 手动执行

预览公告分组下会命中的订阅，不实际重置：

```bash
docker compose run --rm quota-reset manual
```

手动重置日限额和周限额：

```bash
docker compose run --rm quota-reset manual --yes
```

默认会刷新被重置的日/周窗口有效期。周限额重置成功后，下次刷新时间会从本次手动重置时间起重新计算 7 天；月限额只清零用量，不刷新窗口时间。

也可以在命令里显式指定模式：

```bash
# 刷新有效期（默认）
docker compose run --rm quota-reset manual --yes --window-start-mode refresh

# 不刷新日/周有效期，只清零用量，保留原下次刷新时间
docker compose run --rm quota-reset manual --yes --window-start-mode preserve
```

成功后会新建一条公告，默认标题类似：

```text
每日配额、每周配额已手动重置
```

正文会写明：

```text
本次重置范围：每日配额、每周配额
```

手动重置日限额、周限额、月限额：

```bash
docker compose run --rm quota-reset manual --yes --windows daily,weekly,monthly
```

如果想自定义手动公告的重置范围文案，可以在 `quota-reset.env` 设置：

```ini
MANUAL_QUOTA_RESET_LABEL=每日配额、每周配额
```

手动公告默认保留 24 小时，可以调整：

```ini
MANUAL_ANNOUNCEMENT_TTL_HOURS=24
```

运行一次定时脚本：

```bash
docker compose run --rm quota-reset once
```

只根据最新状态补发公告，不再次重置额度：

```bash
docker compose run --rm -e NOTICE_ONLY=1 quota-reset once
```

## 查看状态

查看上次成功重置时间和下次预计时间：

```bash
docker compose exec quota-reset cat /data/subscription-daily-quota-reset-state.json
```

如果要提前预约下一次重置，可以停容器后修改这个 JSON 里的 `last_success_at`，规则是：

```text
下一次重置时间 = last_success_at + RESET_INTERVAL_HOURS
```

修改后再启动：

```bash
docker compose stop quota-reset
docker compose up -d
```

## 迁移

迁移到新服务器时带走：

- 整个 `tools/quota-reset` 目录。
- Docker volume `quota-reset_quota_reset_state`，里面保存上次重置状态。

查看 volume 路径：

```bash
docker volume inspect quota-reset_quota_reset_state
```
