# GraphQL 查询手册

基于 mountainKing GraphQL API 的实战查询示例，以 `nc_notification` 表为例。

## 1. 基础查询（选择字段 + 分页）

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id", "business_type"]
    first: 10
    offset: 0
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes { data }
    pageInfo { hasNextPage hasPreviousPage }
    totalCount
  }
}
```

## 2. 翻页

第 2 页（每页 10 条）：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id"]
    first: 10
    offset: 10
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes { data }
    pageInfo { hasNextPage hasPreviousPage }
    totalCount
  }
}
```

规则：`offset = (页码 - 1) × 每页条数`

## 3. 单条件过滤

查询 SMS 渠道的通知：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id", "template_name"]
    filters: [
      { field: "channel", operator: EQ, value: "SMS" }
    ]
    first: 20
  ) {
    nodes { data }
    totalCount
  }
}
```

## 4. 多条件过滤（AND）

查询 SMS 渠道中失败的通知：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id", "fail_stage", "fail_reason"]
    filters: [
      { field: "channel", operator: EQ, value: "SMS" }
      { field: "status", operator: EQ, value: "FAIL" }
    ]
    first: 20
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes { data }
    totalCount
  }
}
```

多个 filters 之间是 AND 关系。

## 5. 支持的过滤操作符

| 操作符 | 含义 | 示例 |
|--------|------|------|
| `EQ` | 等于 | `{ field: "channel", operator: EQ, value: "SMS" }` |
| `NEQ` | 不等于 | `{ field: "status", operator: NEQ, value: "FAIL" }` |
| `GT` | 大于 | `{ field: "event_timestamp", operator: GT, value: "1700000000000" }` |
| `GTE` | 大于等于 | `{ field: "event_timestamp", operator: GTE, value: "1700000000000" }` |
| `LT` | 小于 | `{ field: "event_timestamp", operator: LT, value: "1800000000000" }` |
| `LTE` | 小于等于 | `{ field: "event_timestamp", operator: LTE, value: "1800000000000" }` |
| `LIKE` | 模糊匹配 | `{ field: "template_name", operator: LIKE, value: "%Invitation%" }` |
| `IN` | 包含于列表 | `{ field: "channel", operator: IN, value: "SMS,EMAIL" }` |
| `NOT_IN` | 不包含于列表 | `{ field: "status", operator: NOT_IN, value: "FAIL,ACCEPTED" }` |
| `IS_NULL` | 为空 | `{ field: "vin", operator: IS_NULL, value: "" }` |
| `IS_NOT_NULL` | 不为空 | `{ field: "vin", operator: IS_NOT_NULL, value: "" }` |

## 6. 排序

单字段排序：

```graphql
orderBy: [{ field: "event_time", direction: DESC }]
```

多字段排序（先按 channel 升序，再按 event_time 降序）：

```graphql
orderBy: [
  { field: "channel", direction: ASC }
  { field: "event_time", direction: DESC }
]
```

## 7. 只查数据不查总数

不请求 `totalCount` 字段可以跳过 COUNT 查询，提升性能：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status"]
    first: 10
  ) {
    nodes { data }
  }
}
```

## 8. 查询所有白名单中的字段

不传 `fields` 参数时查询所有白名单列（`SELECT *`）：

```graphql
{
  starrocks(
    table: "nc_notification"
    first: 5
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes { data }
  }
}
```

## 9. Relay 游标分页

除了 offset 分页，还支持 Relay cursor 分页：

```graphql
# 第一页
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id"]
    first: 10
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    edges {
      node { data }
      cursor
    }
    pageInfo { hasNextPage endCursor }
  }
}
```

下一页用上一页返回的 `endCursor`：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id"]
    first: 10
    after: "上一页返回的 endCursor 值"
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    edges {
      node { data }
      cursor
    }
    pageInfo { hasNextPage endCursor }
  }
}
```

## 10. 查询其他表

换个表名就行，只要在白名单中：

```graphql
# 查询 consent_event 表
{
  starrocks(
    table: "consent_event"
    fields: ["sign_time", "oneid", "consent_key", "sign_status"]
    first: 10
    orderBy: [{ field: "sign_time", direction: DESC }]
  ) {
    nodes { data }
    totalCount
  }
}

# 查询 ecu_diagnostics 表
{
  starrocks(
    table: "ecu_diagnostics"
    fields: ["vin", "created_at", "ads_software_version", "ems_software_version"]
    first: 10
  ) {
    nodes { data }
    totalCount
  }
}
```

## 11. curl 调用方式

```bash
# 基础查询
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ starrocks(table: \"nc_notification\", fields: [\"event_time\", \"channel\", \"status\"], first: 5, orderBy: [{field: \"event_time\", direction: DESC}]) { nodes { data } totalCount } }"
  }'

# 带过滤条件
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ starrocks(table: \"nc_notification\", fields: [\"event_time\", \"channel\", \"status\"], filters: [{field: \"channel\", operator: EQ, value: \"SMS\"}], first: 10) { nodes { data } totalCount } }"
  }'
```

## 12. 使用 GraphQL 变量（推荐）

变量方式更清晰，适合程序化调用：

```json
{
  "query": "query GetNotifications($table: String!, $first: Int, $offset: Int) { starrocks(table: $table, fields: [\"event_time\", \"channel\", \"status\", \"msg_id\"], first: $first, offset: $offset, orderBy: [{field: \"event_time\", direction: DESC}]) { nodes { data } pageInfo { hasNextPage } totalCount } }",
  "variables": {
    "table": "nc_notification",
    "first": 10,
    "offset": 0
  }
}
```

在 Playground 中，左下角 "Query Variables" 面板输入变量 JSON。

## 13. 健康检查和指标

```bash
# 存活检查
curl http://localhost:8080/health

# 就绪检查（含数据源状态）
curl http://localhost:8080/ready

# Prometheus 指标
curl http://localhost:8080/metrics
```

## 14. 可用表列表

当前白名单中配置的表：

| 表名 | 说明 |
|------|------|
| `nc_notification` | 通知消息 |
| `consent_event` | 用户同意事件 |
| `ecu_diagnostics` | ECU 诊断信息 |
| `hmi_events` | HMI 事件 |
| `ma_events` | MA 事件 |
| `sa_status` | SA 状态 |
| `sdc_config_request` | SDC 配置请求 |
| `sdc_config_response` | SDC 配置响应 |
| `sdc_payload` | SDC 原始数据 |
| `security_control` | 安全控制 |
| `base_vss` | 车辆信号（核心列） |
| `base_vss_snapshot` | 车辆信号快照（核心列） |
| `base_vss_instruction` | VSS 信号定义 |
| `base_vss_quality` | VSS 数据质量 |

## 15. SQL 模板查询

模板查询用于执行复杂的多表 JOIN、CTE 等高级 SQL，通过预定义模板 + 参数调用。

### 基础模板查询

```graphql
{
  templateQuery(
    templateName: "nc_notification_query"
    parameters: { channel: "SMS" }
    first: 10
    offset: 0
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes
    pageInfo { hasNextPage }
    totalCount
  }
}
```

### 多参数模板查询

```graphql
{
  templateQuery(
    templateName: "nc_notification_query"
    parameters: {
      channel: "SMS"
      status: "FAIL"
      start_date: "2026-01-01"
      end_date: "2026-04-01"
    }
    first: 20
  ) {
    nodes
    totalCount
  }
}
```

### 车队报表（CTE + JOIN）

```graphql
{
  templateQuery(
    templateName: "fleet_report"
    parameters: {
      eerid: "VIN001"
      period: "weekly"
      vehicle_ids: ["V001", "V002", "V003"]
    }
    first: 50
  ) {
    nodes
    totalCount
  }
}
```

### 驾驶员评分

```graphql
{
  templateQuery(
    templateName: "driver_score"
    parameters: {
      driver_id: 12345
      start_date: "2026-01-01"
      end_date: "2026-03-31"
    }
  ) {
    nodes
  }
}
```

### 查看可用模板列表

```graphql
{
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

### 绕过缓存

```json
{
  "query": "{ templateQuery(templateName: \"fleet_report\", parameters: {eerid: \"VIN001\"}, first: 10) { nodes totalCount } }",
  "extensions": { "cache": false }
}
```

## 16. Mutation 操作

### 清除缓存

```graphql
# 清除特定数据源缓存
mutation {
  clearCache(datasource: "analytics_db")
}

# 清除所有缓存
mutation {
  clearCache
}
```

### 重载 SQL 模板

```graphql
mutation {
  reloadTemplates {
    successCount
    failures { name error }
    duration
  }
}
```

## 17. 批量查询

一次请求发送多个查询：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '[
    {"query": "{ starrocks(table: \"nc_notification\", first: 5) { nodes { data } } }"},
    {"query": "{ templateList { name description } }"}
  ]'
```

## 18. 错误处理示例

### 表名不在白名单

```graphql
# 返回 VALIDATION_INVALID_TABLE 错误
{ starrocks(table: "unknown_table", first: 5) { nodes { data } } }
```

### 模板不存在

```graphql
# 返回 VALIDATION_TEMPLATE_NOT_FOUND 错误
{ templateQuery(templateName: "nonexistent", first: 5) { nodes } }
```

### 必填参数缺失

```graphql
# fleet_report 的 eerid 是必填参数
# 返回 VALIDATION_MISSING_PARAMETER 错误
{ templateQuery(templateName: "fleet_report", first: 5) { nodes } }
```
