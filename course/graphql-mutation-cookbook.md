# GraphQL Mutation 操作手册

基于 mountainKing GraphQL API 的 CUD（Create/Update/Delete）Mutation 实战示例，以 `orders` 和 `events` 表为例。

## 前置条件

在使用 Mutation 操作之前，需要确保以下三项配置就绪：

### 1. 启用 Mutation 功能

在 `config.yaml` 中设置 `mutations.enabled: true`：

```yaml
mutations:
  enabled: true
  datasource_name: analytics_db
  max_affected_rows: 1000
  max_batch_size: 500
  max_sql_length: 1048576
  rate_limit:
    requests_per_window: 20
    window_size: 60s
```

> `mutations.enabled` 支持热更新，无需重启服务即可启用或禁用。

### 2. 配置 writable_tables 白名单

在数据源的 `options` 中定义可写表和允许的操作：

```yaml
datasources:
  - name: analytics_db
    type: starrocks
    options:
      writable_tables:
        orders:
          columns: [order_id, user_id, amount, status, created_at]
          allowed_operations: [insert, update, delete]
        events:
          columns: [event_id, event_type, payload, timestamp]
          allowed_operations: [insert]
```

- `columns`：该表允许写入的列白名单
- `allowed_operations`：该表允许的操作类型（insert / update / delete）

### 3. 授权要求

Mutation 操作要求认证主体的 `operations` 列表中包含 `"mutation"` 权限。

**JWT Token** 需要在 payload 中声明：

```json
{
  "sub": "service-account",
  "operations": ["query", "mutation"]
}
```

> 缺少 `operations` claim 时默认为 `["query"]`，此时 Mutation 请求会返回 `AUTH_INSUFFICIENT_PERMISSION` 错误。

**API Key** 需要在配置中声明 mutation 权限：

```yaml
auth:
  method: apikey
  apikey:
    keys:
      - id: writer-service
        key: "${GRAPHQL_APIKEY_WRITER}"
        permissions:
          datasources: [analytics_db]
          operations: [query, mutation]
```

---

## 1. 单行插入 (insertStarrocks)

向指定表插入一行数据。

```graphql
mutation {
  insertStarrocks(
    table: "orders"
    values: [
      { column: "order_id", value: 1001 }
      { column: "user_id", value: "U123" }
      { column: "amount", value: 99.5 }
      { column: "status", value: "pending" }
      { column: "created_at", value: "2024-01-15T10:30:00Z" }
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

**返回结果**：

```json
{
  "data": {
    "insertStarrocks": {
      "success": true,
      "affectedRows": 1,
      "warning": null
    }
  }
}
```

**参数说明**：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 中） |
| `values` | [ColumnValueInput!]! | 是 | 列值对数组 |

> **注意**：CUD 操作的目标数据源由 `mutations.datasource_name` 全局配置指定，无需在请求中传入。

**AnyValue 直接传值**：`value` 字段使用 AnyValue 标量类型，可直接传入任意 JSON 值：

```graphql
{ column: "order_id", value: 1001 }       # 整数
{ column: "user_id", value: "U123" }      # 字符串
{ column: "amount", value: 99.5 }         # 浮点数
{ column: "is_vip", value: true }         # 布尔值
{ column: "metadata", value: null }       # null
```

> 无需使用 `{"v": 42}` 的包装格式，直接传值即可。

---

## 2. 批量插入 (insertBatchStarrocks)

向指定表批量插入多行数据，适合批量导入场景。

```graphql
mutation {
  insertBatchStarrocks(
    table: "events"
    columns: ["event_id", "event_type", "payload", "timestamp"]
    rows: [
      [1001, "click", {"page": "/home"}, "2024-01-15T10:00:00Z"]
      [1002, "view", {"page": "/products"}, "2024-01-15T10:01:00Z"]
      [1003, "purchase", {"item_id": "SKU001", "qty": 2}, "2024-01-15T10:02:00Z"]
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

**返回结果**：

```json
{
  "data": {
    "insertBatchStarrocks": {
      "success": true,
      "affectedRows": 3,
      "warning": null
    }
  }
}
```

**参数说明**：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 中） |
| `columns` | [String!]! | 是 | 列名数组，定义插入顺序 |
| `rows` | [[AnyValue!]!]! | 是 | 行数据二维数组，每行值的顺序与 columns 对应 |

> 单次请求行数不能超过 `max_batch_size`（默认 500 行），超过将返回 `VALIDATION_BATCH_LIMIT_EXCEEDED` 错误。

---

## 3. 条件更新 (updateStarrocks)

根据过滤条件更新指定表中的数据。

```graphql
mutation {
  updateStarrocks(
    table: "orders"
    set: [
      { column: "status", value: "completed" }
      { column: "amount", value: 150.0 }
    ]
    filter: [
      { field: "order_id", operator: EQ, value: 1001 }
      { field: "status", operator: NEQ, value: "cancelled" }
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

**返回结果**：

```json
{
  "data": {
    "updateStarrocks": {
      "success": true,
      "affectedRows": 1,
      "warning": null
    }
  }
}
```

**参数说明**：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 中且允许 update） |
| `set` | [ColumnValueInput!]! | 是 | 要更新的列值对数组 |
| `filter` | [MutationFilterInput!]! | 是 | 过滤条件数组（AND 关系） |

> `filter` 是必填的，多个条件之间为 AND 关系。建议始终添加精确匹配条件（如主键）以避免大范围更新。

---

## 4. 条件删除 (deleteStarrocks)

根据过滤条件删除指定表中的数据。

```graphql
mutation {
  deleteStarrocks(
    table: "orders"
    filter: [
      { field: "status", operator: EQ, value: "cancelled" }
      { field: "created_at", operator: LT, value: "2023-01-01T00:00:00Z" }
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

**返回结果**：

```json
{
  "data": {
    "deleteStarrocks": {
      "success": true,
      "affectedRows": 42,
      "warning": null
    }
  }
}
```

**参数说明**：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 中且允许 delete） |
| `filter` | [MutationFilterInput!]! | 是 | 过滤条件数组（AND 关系） |

> `filter` 是必填的，防止误删全表数据。

---

## 5. 高级过滤（IN / NOT_IN）

### IN — 批量匹配

删除多个指定状态的订单：

```graphql
mutation {
  deleteStarrocks(
    table: "orders"
    filter: [
      { field: "status", operator: IN, value: ["cancelled", "expired", "rejected"] }
      { field: "created_at", operator: LT, value: "2023-06-01T00:00:00Z" }
    ]
  ) {
    success
    affectedRows
  }
}
```

`IN` 操作符的 `value` 为数组格式，等价于 SQL 的 `WHERE status IN ('cancelled', 'expired', 'rejected')`。

### NOT_IN — 排除匹配

更新除指定状态外的所有订单：

```graphql
mutation {
  updateStarrocks(
    table: "orders"
    set: [
      { column: "status", value: "archived" }
    ]
    filter: [
      { field: "status", operator: NOT_IN, value: ["pending", "processing"] }
      { field: "created_at", operator: LT, value: "2023-01-01T00:00:00Z" }
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

`NOT_IN` 等价于 SQL 的 `WHERE status NOT IN ('pending', 'processing')`。

### 组合过滤示例

结合多种操作符实现复杂条件：

```graphql
mutation {
  updateStarrocks(
    table: "orders"
    set: [
      { column: "status", value: "expired" }
    ]
    filter: [
      { field: "status", operator: IN, value: ["pending", "processing"] }
      { field: "amount", operator: GTE, value: 100 }
      { field: "created_at", operator: LT, value: "2024-01-01T00:00:00Z" }
    ]
  ) {
    success
    affectedRows
    warning
  }
}
```

---

## 6. 错误处理模式

### Mutation 功能未启用

当 `mutations.enabled: false` 时，所有写操作返回：

```json
{
  "errors": [
    {
      "message": "mutation feature is disabled",
      "path": ["insertStarrocks"],
      "extensions": {
        "code": "MUTATION_FEATURE_DISABLED",
        "classification": "VALIDATION"
      }
    }
  ],
  "data": { "insertStarrocks": null }
}
```

**解决方法**：设置 `mutations.enabled: true`（支持热更新，无需重启）。

### 权限不足

JWT/API Key 缺少 mutation 操作权限时返回：

```json
{
  "errors": [
    {
      "message": "insufficient permission for mutation operation",
      "path": ["insertStarrocks"],
      "extensions": {
        "code": "AUTH_INSUFFICIENT_PERMISSION",
        "classification": "AUTH"
      }
    }
  ],
  "data": { "insertStarrocks": null }
}
```

**解决方法**：在 JWT 的 `operations` claim 中添加 `"mutation"`，或在 API Key 配置中添加 `operations: [query, mutation]`。

### 表不支持该操作

当表的 `allowed_operations` 不包含请求的操作类型时：

```json
{
  "errors": [
    {
      "message": "operation 'delete' is not allowed on table 'events'",
      "path": ["deleteStarrocks"],
      "extensions": {
        "code": "MUTATION_OPERATION_NOT_SUPPORTED",
        "classification": "VALIDATION"
      }
    }
  ],
  "data": { "deleteStarrocks": null }
}
```

**解决方法**：在 `writable_tables` 配置中为该表添加相应的操作类型。

### 批量大小超限

`insertBatchStarrocks` 的行数超过 `max_batch_size`（默认 500）时：

```json
{
  "errors": [
    {
      "message": "batch size 600 exceeds maximum allowed 500",
      "path": ["insertBatchStarrocks"],
      "extensions": {
        "code": "VALIDATION_BATCH_LIMIT_EXCEEDED",
        "classification": "VALIDATION"
      }
    }
  ],
  "data": { "insertBatchStarrocks": null }
}
```

**解决方法**：将批量数据分片为 500 行以内的多次请求，或调整 `mutations.max_batch_size` 配置。

### SQL 语句过长

生成的 SQL 超过 `max_sql_length`（默认 1MB）时：

```json
{
  "errors": [
    {
      "message": "generated SQL exceeds maximum length",
      "path": ["insertBatchStarrocks"],
      "extensions": {
        "code": "VALIDATION_PAYLOAD_TOO_LARGE",
        "classification": "VALIDATION"
      }
    }
  ],
  "data": { "insertBatchStarrocks": null }
}
```

**解决方法**：减少单次请求的数据量，或调整 `mutations.max_sql_length` 配置。

### Mutation 限流

写操作频率超过 `mutations.rate_limit` 配置时返回 HTTP 429：

```json
{
  "errors": [
    {
      "message": "mutation rate limit exceeded, try again later",
      "path": ["insertStarrocks"],
      "extensions": {
        "code": "MUTATION_RATELIMIT_EXCEEDED",
        "classification": "RATELIMIT"
      }
    }
  ],
  "data": { "insertStarrocks": null }
}
```

**解决方法**：降低写操作频率，或调整 `mutations.rate_limit.requests_per_window` 和 `window_size` 配置。默认限制为每 60 秒 20 次写操作。

---

## 7. 最佳实践

### Batch Size 建议

- **默认限制**：单次 `insertBatchStarrocks` 最多 500 行
- **推荐分片大小**：200-300 行/批次，在性能和可靠性之间取得平衡
- **大数据导入**：将数据分片，逐批调用，每批之间留出适当间隔

```graphql
# 推荐：分批插入，每批 200 行
mutation Batch1 {
  insertBatchStarrocks(
    table: "events"
    columns: ["event_id", "event_type", "payload", "timestamp"]
    rows: [
      # ... 200 行数据
    ]
  ) {
    success
    affectedRows
  }
}
```

### Rate Limit 感知

- **默认限流**：每 60 秒 20 次写操作（独立于全局查询限流）
- **客户端策略**：收到 `MUTATION_RATELIMIT_EXCEEDED` 后，按指数退避重试
- **批量优于频次**：使用 `insertBatchStarrocks` 合并多行为一次请求，减少限流触发

```bash
# 查看当前限流配置
# mutations.rate_limit.requests_per_window: 20
# mutations.rate_limit.window_size: 60s

# 策略：每批 200 行 × 20 次/分钟 = 4000 行/分钟 的写入吞吐
```

### 审计日志监控

所有 Mutation 操作自动生成审计日志，记录以下关键字段：

| 字段 | 说明 |
|------|------|
| `operation` | 操作类型：insert / insertBatch / update / delete |
| `table` | 目标表名 |
| `affected_rows` | 受影响行数 |
| `success` | 操作结果：true / false |
| `user` | 操作者身份（从 JWT/API Key 提取） |
| `timestamp` | 操作时间 |

**监控建议**：

- 关注 `affectedRows` 超过 `max_affected_rows`（默认 1000）的操作，这些会在响应中附带 `warning`
- 监控 `success: false` 的操作，排查授权或配置问题
- 使用 Prometheus 指标 `graphql_mutation_total{status="error"}` 设置告警

### 其他建议

- **精确过滤**：update 和 delete 操作始终添加主键或唯一键条件，避免大范围修改
- **先查后改**：对关键数据，先用 Query 确认目标范围，再执行 Mutation
- **测试环境验证**：在开发模式下验证 writable_tables 配置和操作权限，再部署到生产环境
- **关注 warning**：当 `affectedRows` 超过阈值时，`warning` 字段会返回提醒信息，据此调整过滤条件
