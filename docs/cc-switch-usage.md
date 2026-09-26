# CC Switch 通用用量查询

CC Switch 的通用模板可以直接请求以下接口，无需改写查询脚本：

- `GET /user/balance`：Base URL 为 `https://你的域名`。
- `GET /v1/user/balance`：Base URL 为 `https://你的域名/v1`。

使用 Sub2API 创建的模型调用 API Key，不使用后台网页登录令牌：

```http
Authorization: Bearer <API_KEY>
```

成功响应为顶层 JSON，不包裹在 `data` 中，并带 `Cache-Control: no-store`：

```json
{
  "is_active": true,
  "balance": 12.5,
  "unit": "USD"
}
```

`balance` 按当前 Key 的计费模式返回，复用 `/v1/usage` 的额度计算：

| 模式 | `balance` 含义 |
| --- | --- |
| 普通余额 Key | 当前用户最新钱包余额 |
| Key 配置了总额度 | 该 Key 剩余总额度，耗尽时为 `0` |
| 仅配置周期限额 | 所有已配置周期中最小的剩余额度 |
| 订阅 Key | 订阅各限额周期中的最小剩余额度，无有效订阅时为 `0` |
| 无限额订阅 | 沿用 `/v1/usage` 的 `-1` 标记 |

`is_active` 沿用用量查询的有效性口径，不表示余额大于零。过期或配额耗尽的 Key 仍可查询自身额度；禁用／无效 Key、停用用户、分组限制及 IP 限制仍按原鉴权规则拒绝。查询失败返回非成功 HTTP 状态，不伪造零余额。

余额接口不查询历史用量、每日统计或模型统计。需要这些明细时继续使用 `GET /v1/usage`，其响应格式保持不变。

CC Switch 通用脚本：

```javascript
({
  request: {
    url: "{{baseUrl}}/user/balance",
    method: "GET",
    headers: {
      "Authorization": "Bearer {{apiKey}}",
      "User-Agent": "cc-switch/1.0"
    }
  },
  extractor: function(response) {
    return {
      isValid: response.is_active || true,
      remaining: response.balance,
      unit: "USD"
    };
  }
})
```

部署包含本接口的后端版本后，原有 CC Switch 通用模板即可使用。
