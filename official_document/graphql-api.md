# GraphQL API 参考

## 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/graphql` | POST | GraphQL 查询端点 |
| `/graphql` | GET | GraphQL 查询（需配置 `allow_get_queries: true`） |
| `/playground` | GET | GraphiQL 交互式界面（仅开发模式） |
| `/health` | GET | 存活检查 |
| `/ready` | GET | 就绪检查 |
| `/metrics` | GET | Prometheus 指标 |

## 自定义标量类型

| 类型 | 说明 | 示例 |
|------|------|------|
| `DateTime` | ISO 8601 日期时间格式 | `"2024-01-15T10:30:00Z"` |
| `JSON` | 任意 JSON 值 | `{"key": "value"}` |
| `AnyValue` | 任意 JSON 值（对象、数组、字符串、数字、布尔值、null） | `42`、`"hello"`、`[1,2,3]`、`{"key":"val"}` |

## 枚举类型

### SortDirection
- `ASC` — 升序
- `DESC` — 降序

### FilterOperator
- `EQ` — 等于
- `NEQ` — 不等于
- `GT` / `GTE` — 大于 / 大于等于
- `LT` / `LTE` — 小于 / 小于等于
- `LIKE` — 模式匹配
- `IN` / `NOT_IN` — 集合包含 / 不包含
- `IS_NULL` / `IS_NOT_NULL` — 空值检查

### LabelMatchType（Prometheus）
- `EXACT` — 精确匹配 (`=`)
- `NOT_EQUAL` — 不等于 (`!=`)
- `REGEX` — 正则匹配 (`=~`)
- `NOT_REGEX` — 正则不匹配 (`!~`)

## Query 操作

### StarRocks 查询

```graphql
query {
  starrocks(
    table: "orders"
    fields: ["order_id", "user_id", "amount", "status"]
    filters: [
      { field: "status", operator: EQ, value: "completed" }
      { field: "amount", operator: GTE, value: "100" }
    ]
    orderBy: [
      { field: "created_at", direction: DESC }
    ]
    first: 20
    after: "cursor_abc123"
  ) {
    edges {
      node { data }
      cursor
    }
    nodes { data }
    pageInfo {
      hasNextPage
      hasPreviousPage
      startCursor
      endCursor
    }
    totalCount
  }
}
```

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 查询表名（必须在服务端白名单中） |
| `fields` | [String!] | 否 | 请求的字段列表（字段选择优化） |
| `filters` | [StarRocksFilter!] | 否 | 过滤条件 |
| `orderBy` | [StarRocksOrderBy!] | 否 | 排序条件 |
| `first` | Int | 否 | Relay 分页：返回前 N 条 |
| `after` | String | 否 | Relay 分页：游标之后 |
| `offset` | Int | 否 | 传统分页：偏移量 |
| `limit` | Int | 否 | 传统分页：限制数 |

### Prometheus 即时查询

```graphql
query {
  prometheusInstant(
    query: "up"
    time: "2024-01-15T10:30:00Z"
    filters: [
      { name: "job", value: "api-server", matchType: EXACT }
    ]
  ) {
    resultType
    vectors {
      metric { name value }
      value { timestamp value }
    }
  }
}
```

### Prometheus 范围查询

```graphql
query {
  prometheusRange(
    query: "rate(http_requests_total[5m])"
    startTime: "2024-01-15T00:00:00Z"
    endTime: "2024-01-15T01:00:00Z"
    step: "60s"
    filters: [
      { name: "method", value: "GET", matchType: EXACT }
    ]
  ) {
    resultType
    matrices {
      metric { name value }
      values { timestamp value }
    }
  }
}
```

### 跨数据源混合查询

在一个请求中同时查询多个数据源，服务会并行执行：

```graphql
query DashboardData {
  orders: starrocks(table: "orders", first: 10) {
    nodes { data }
    totalCount
  }
  cpuUsage: prometheusInstant(query: "process_cpu_seconds_total") {
    vectors {
      metric { name value }
      value { timestamp value }
    }
  }
}
```

如果某个数据源查询失败，其他数据源的结果仍会正常返回，失败信息包含在 `errors` 字段中。

### SQL 模板查询

通过预定义的 SQL 模板执行复杂查询（多表 JOIN、CTE、窗口函数等）：

```graphql
query {
  templateQuery(
    templateName: "fleet_report"
    parameters: { eerid: "EER001", period: "monthly", vehicle_ids: ["V001", "V002"] }
    fields: ["vehicle_id", "plate_number", "event_count"]
    first: 20
    offset: 0
    orderBy: [{ field: "event_count", direction: DESC }]
  ) {
    nodes
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
    totalCount
  }
}
```

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `templateName` | String! | 是 | 模板名称 |
| `parameters` | JSON | 否 | 业务参数键值对 |
| `fields` | [String!] | 否 | 返回字段列表（字段选择优化） |
| `first` | Int | 否 | 返回行数限制 |
| `offset` | Int | 否 | 偏移量 |
| `orderBy` | [TemplateOrderBy!] | 否 | 排序条件 |

返回类型 `TemplateQueryConnection`：
- `nodes` — `[JSON!]!` 结果行数组
- `pageInfo` — 分页信息（hasNextPage, hasPreviousPage）
- `totalCount` — 总记录数（模板禁用 count 时返回 -1）

### 模板列表查询

查询所有已注册模板的元信息：

```graphql
query {
  templateList {
    name
    description
    countEnabled
    parameters {
      name
      type
      required
      defaultValue
    }
  }
}
```

## Mutation 操作

本服务支持管理类 Mutation 和 CUD（Create/Update/Delete）数据写入 Mutation。所有 Mutation 操作需要认证主体具有 `mutation` 操作权限。

### 管理类 Mutation

#### 清除缓存

```graphql
mutation {
  clearCache(datasource: "analytics_db")  # 清除指定数据源缓存
}

mutation {
  clearCache  # 清除全部缓存
}
```

需要认证主体具有 `mutation` 操作权限。

#### 重新加载 SQL 模板

```graphql
mutation {
  reloadTemplates {
    successCount
    failures {
      name
      error
    }
    duration
  }
}
```

需要认证主体具有 `mutation` 操作权限。支持 10s 冷却时间防止高频调用。模板文件变更也会通过 fsnotify 自动触发重新加载（500ms 防抖）。

### CUD Mutation 操作

CUD Mutation 提供对 StarRocks 数据源的写入能力，包括单行插入、批量插入、条件更新和条件删除。

使用前提：
- 配置 `mutations.enabled: true` 启用写操作功能
- 在数据源配置中定义 `writable_tables` 白名单
- 认证主体的 operations 中包含 `"mutation"` 权限

#### 输入类型

##### ColumnValueInput

表示一个列及其对应值的键值对，用于 insert 和 update 操作。

```graphql
input ColumnValueInput {
  column: String!
  value: AnyValue!
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `column` | String! | 是 | 列名（必须在 writable_tables 白名单中） |
| `value` | AnyValue! | 是 | 列值，支持任意 JSON 值直接传入 |

##### MutationFilterInput

表示一个过滤条件，用于 update 和 delete 操作的 WHERE 子句。

```graphql
input MutationFilterInput {
  field: String!
  operator: FilterOperator!
  value: AnyValue
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `field` | String! | 是 | 过滤字段名 |
| `operator` | FilterOperator! | 是 | 比较操作符（EQ、NEQ、GT、GTE、LT、LTE、LIKE、IN、NOT_IN、IS_NULL、IS_NOT_NULL） |
| `value` | AnyValue | 否 | 比较值（IS_NULL/IS_NOT_NULL 时可省略） |

#### 返回类型

##### MutationResult

所有 CUD 操作的统一返回类型。

```graphql
type MutationResult {
  success: Boolean!
  affectedRows: Int!
  warning: String
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `success` | Boolean! | 操作是否成功 |
| `affectedRows` | Int! | 受影响行数 |
| `warning` | String | 警告信息（如受影响行数超过 max_affected_rows 阈值时返回） |

#### insertStarrocks — 单行插入

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

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 白名单中） |
| `values` | [ColumnValueInput!]! | 是 | 列值对数组，每个元素指定列名和对应值 |

> **注意**：CUD 操作的目标数据源由 `mutations.datasource_name` 全局配置指定，无需在每次请求中传入。

#### insertBatchStarrocks — 批量插入

向指定表批量插入多行数据。

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

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 白名单中） |
| `columns` | [String!]! | 是 | 列名数组，定义插入顺序 |
| `rows` | [[AnyValue!]!]! | 是 | 行数据二维数组，每行值的顺序与 columns 对应。单次请求不超过 max_batch_size（默认 500）行 |

#### updateStarrocks — 条件更新

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

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 白名单中且允许 update 操作） |
| `set` | [ColumnValueInput!]! | 是 | 要更新的列值对数组 |
| `filter` | [MutationFilterInput!]! | 是 | 过滤条件数组（多个条件之间为 AND 关系） |

#### deleteStarrocks — 条件删除

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

参数说明：

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `table` | String! | 是 | 目标表名（必须在 writable_tables 白名单中且允许 delete 操作） |
| `filter` | [MutationFilterInput!]! | 是 | 过滤条件数组（多个条件之间为 AND 关系） |

## 批量查询

在一个 HTTP 请求中发送多个查询：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '[
    {"query": "{ starrocks(table: \"orders\", first: 5) { totalCount } }"},
    {"query": "{ prometheusInstant(query: \"up\") { resultType } }"}
  ]'
```

响应为结果数组，与请求数组一一对应。批量查询数不能超过 `max_batch_queries`（默认 10）。

限流按批量中的实际查询数计数（而非 HTTP 请求数）。

## 分页

支持两种分页模式：

### Relay 游标分页（推荐）

```graphql
# 第一页
{ starrocks(table: "orders", first: 20) { edges { cursor node { data } } pageInfo { hasNextPage endCursor } } }

# 下一页
{ starrocks(table: "orders", first: 20, after: "endCursor_value") { ... } }
```

### 传统 Offset/Limit 分页

```graphql
{ starrocks(table: "orders", offset: 40, limit: 20) { nodes { data } totalCount } }
```

## 缓存控制

客户端可通过 `extensions.cache` 参数绕过缓存：

```json
{
  "query": "{ starrocks(table: \"orders\", first: 10) { nodes { data } } }",
  "extensions": { "cache": false }
}
```

## 错误响应

错误响应遵循 GraphQL 规范，包含结构化错误码：

```json
{
  "errors": [
    {
      "message": "datasource query timeout",
      "path": ["starrocks"],
      "extensions": {
        "code": "DATASOURCE_TIMEOUT",
        "classification": "DATASOURCE"
      }
    }
  ],
  "data": { "starrocks": null }
}
```

### 错误码分类

| 前缀 | 分类 | 示例 |
|------|------|------|
| `AUTH_*` | 认证授权 | `AUTH_TOKEN_EXPIRED`, `AUTH_INSUFFICIENT_PERMISSION` |
| `VALIDATION_*` | 请求验证 | `VALIDATION_SYNTAX_ERROR`, `VALIDATION_COMPLEXITY_EXCEEDED`, `VALIDATION_TEMPLATE_NOT_FOUND` |
| `DATASOURCE_*` | 数据源 | `DATASOURCE_TIMEOUT`, `DATASOURCE_UNAVAILABLE` |
| `RATELIMIT_*` | 限流 | `RATELIMIT_EXCEEDED` |
| `INTERNAL_*` | 内部错误 | `INTERNAL_UNEXPECTED`, `INTERNAL_TEMPLATE_RENDER_ERROR` |

## 响应扩展字段

成功响应可能包含以下扩展信息：

```json
{
  "data": { ... },
  "extensions": {
    "traceId": "abc123def456",
    "warnings": ["Result truncated: returned 10000 of 50000 rows"]
  }
}
```
