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

## Mutation 操作

本服务仅支持管理类 Mutation，不支持数据写入。

### 清除缓存

```graphql
mutation {
  clearCache(datasource: "analytics_db")  # 清除指定数据源缓存
}

mutation {
  clearCache  # 清除全部缓存
}
```

需要认证主体具有 `mutation` 操作权限。

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
| `VALIDATION_*` | 请求验证 | `VALIDATION_SYNTAX_ERROR`, `VALIDATION_COMPLEXITY_EXCEEDED` |
| `DATASOURCE_*` | 数据源 | `DATASOURCE_TIMEOUT`, `DATASOURCE_UNAVAILABLE` |
| `RATELIMIT_*` | 限流 | `RATELIMIT_EXCEEDED` |
| `INTERNAL_*` | 内部错误 | `INTERNAL_UNEXPECTED` |

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
