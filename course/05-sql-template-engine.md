# 模块 05：SQL 模板引擎实战

> 掌握 SQL 模板引擎的完整使用流程：模板编写、安全函数、参数校验、分页、缓存和热加载。

## 5.1 功能定位

SQL 模板引擎是 StarRocks 数据源的扩展查询方式，用于支持单表查询无法满足的复杂场景：
- 多表 JOIN
- CTE（公用表表达式）
- 窗口函数
- 条件分支逻辑
- 聚合计算

模板引擎与现有 `starrocks` 单表查询并行工作，共享同一个 StarRocks 连接池。

## 5.2 架构集成

```
GraphQL Layer
    │
    ├── starrocks(table, filters...)     → QueryResolver → StarRocks Adapter (Execute)
    │
    └── templateQuery(name, params...)   → QueryResolver → TemplateEngine
                                                              │
                                                              ├── Registry（模板查找）
                                                              ├── ParamValidator（参数校验）
                                                              ├── Renderer（模板渲染）
                                                              ├── SQLSanitizer（SQL 安全检查）
                                                              ├── PaginationWrapper（分页包装）
                                                              ├── Semaphore（并发控制）
                                                              └── RawExecutor（SQL 执行）
                                                                    │
                                                                    ▼
                                                              StarRocks Adapter (ExecuteRaw)
```

关键设计：TemplateEngine 通过 `RawExecutor` 接口（仅 `ExecuteRaw` 方法）与 Adapter 交互，无法访问白名单查询等其他方法。

## 5.3 端到端示例

### 步骤 1：编写模板文件

`templates/notification/nc_notification.sql.tmpl`：

```sql
{{/* 通知消息查询 */}}
SELECT event_time, channel, status, msg_id, business_type,
       template_name, user_id, vin, eer_id
FROM nc_notification
WHERE 1=1
{{if index .Params "channel"}}
  AND channel = {{index .Params "channel" | quote}}
{{end}}
{{if index .Params "status"}}
  AND status = {{index .Params "status" | quote}}
{{end}}
{{if index .Params "start_date"}}
  AND event_time >= {{index .Params "start_date" | quote}}
{{end}}
{{if index .Params "end_date"}}
  AND event_time < {{index .Params "end_date" | quote}}
{{end}}
```

### 步骤 2：配置模板

`config.yaml` 中的 `sql_templates` 段：

```yaml
sql_templates:
  enabled: true
  datasource_name: analytics_db
  base_dir: ./templates
  templates:
    - name: nc_notification_query
      file: notification/nc_notification.sql.tmpl
      description: 通知消息查询
      cache_enabled: true
      cache_ttl: 60s
      count_enabled: true
      parameters:
        - name: channel
          type: string
          required: false
          enum: [SMS, EMAIL, APP_PUSH, INBOX_MSG]
        - name: status
          type: string
          required: false
        - name: start_date
          type: string
          required: false
          max_length: 30
        - name: end_date
          type: string
          required: false
          max_length: 30
```

### 步骤 3：GraphQL 调用

```graphql
{
  templateQuery(
    templateName: "nc_notification_query"
    parameters: {
      channel: "SMS"
      start_date: "2026-01-01"
      end_date: "2026-04-01"
    }
    first: 20
    offset: 0
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes
    pageInfo { hasNextPage }
    totalCount
  }
}
```

### 步骤 4：响应结果

```json
{
  "data": {
    "templateQuery": {
      "nodes": [
        { "event_time": "2026-03-15T10:30:00Z", "channel": "SMS", "status": "DELIVERED", ... },
        ...
      ],
      "pageInfo": { "hasNextPage": true },
      "totalCount": 1523
    }
  }
}
```

## 5.4 模板语法

基于 Go `text/template`，核心语法：

### 变量访问

```sql
-- 直接访问（参数必填时）
WHERE driver_id = {{.Params.driver_id | safeInt}}

-- 安全访问（参数可选时，使用 index 避免 missingkey=error）
{{if index .Params "channel"}}
  AND channel = {{index .Params "channel" | quote}}
{{end}}
```

### 条件逻辑

```sql
{{if eq .Params.period "daily"}}
  AND event_time >= DATE_SUB(NOW(), INTERVAL 1 DAY)
{{else if eq .Params.period "weekly"}}
  AND event_time >= DATE_SUB(NOW(), INTERVAL 7 DAY)
{{else}}
  AND event_time >= DATE_SUB(NOW(), INTERVAL 30 DAY)
{{end}}
```

### 循环

```sql
{{if .Params.vehicle_ids}}
  AND vehicle_id IN ({{.Params.vehicle_ids | safeInList}})
{{end}}
```

### 共享模板片段

`templates/_shared/time_filter.sql.tmpl`：

```sql
{{define "time_filter"}}
  {{if eq .Params.period "daily"}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 1 DAY)
  {{else if eq .Params.period "weekly"}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 7 DAY)
  {{else}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 30 DAY)
  {{end}}
{{end}}
```

在其他模板中引用：`{{template "time_filter" .}}`

## 5.5 安全函数速查表

| 函数 | 用途 | 输入 → 输出 |
|------|------|-------------|
| `safeString` | 转义 SQL 字符串（不加引号） | `O'Brien` → `O''Brien` |
| `quote` | 转义并加单引号 | `O'Brien` → `'O''Brien'` |
| `safeInt` | 验证整数 | `42` → `42`，`abc` → 错误 |
| `safeFloat` | 验证有限浮点数 | `3.14` → `3.14`，`NaN` → 错误 |
| `safeIdentifier` | 验证并反引号包裹标识符 | `table.col` → `` `table`.`col` `` |
| `safeInList` | 生成 IN 子句值列表 | `["a","b"]` → `'a','b'` |
| `safeLike` | 转义 LIKE 通配符 | `100%` → `100\%` |
| `join` | 逗号连接数组 | `["a","b"]` → `a,b` |
| `default` | 默认值 | `nil \| default 100` → `100` |
| `upper` | 转大写 | `hello` → `HELLO` |
| `lower` | 转小写 | `HELLO` → `hello` |
| `trimSpace` | 去除首尾空白 | `" hi "` → `hi` |

**关键规则**：所有用户输入必须通过安全函数处理，绝不直接拼接到 SQL 中。

## 5.6 参数校验

每个模板参数支持以下校验规则：

```yaml
parameters:
  - name: eerid
    type: string          # string | int | float | bool | string[]
    required: true         # 是否必填
    max_length: 64         # 字符串最大长度
    pattern: '^\w+$'       # 正则校验
    enum: [a, b, c]        # 枚举值限制
    default: monthly       # 默认值（required=false 时）
    max_items: 500         # 数组最大元素数（string[] 类型）
```

校验失败返回对应错误码：
- `VALIDATION_MISSING_PARAMETER` — 必填参数缺失
- `VALIDATION_INVALID_PARAMETER_TYPE` — 类型不匹配
- `VALIDATION_INVALID_PARAMETER_VALUE` — 值不满足约束

## 5.7 分页机制

模板引擎在渲染后的 SQL 外层包裹分页结构：

```sql
SELECT * FROM (
  -- 渲染后的原始 SQL
) AS __inner
ORDER BY `event_time` DESC    -- 外层排序
LIMIT 21 OFFSET 0             -- over-fetch: first+1 用于判断 hasNextPage
```

注意事项：
- 外层 ORDER BY 使用 `safeIdentifier` 校验字段名
- LIMIT/OFFSET 使用参数化绑定（`?` 占位符）
- 模板 SQL 内部不要加 ORDER BY（会被外层覆盖）

## 5.8 缓存集成

模板级缓存配置：

```yaml
templates:
  - name: fleet_report
    cache_enabled: true    # 启用缓存
    cache_ttl: 300s        # 独立 TTL（覆盖数据源默认值）
```

缓存行为：
- 缓存 Key 由模板名 + 参数 + 字段 + 分页 + 排序生成（xxhash）
- `totalCount` 独立缓存（Key 不含分页参数）
- 客户端可通过 `extensions.cache=false` 绕过缓存
- 模板文件变更时自动清除对应缓存（hash 比较）

## 5.9 热加载

两种方式触发模板重载：

**自动重载**：fsnotify 监听模板目录，文件变更后 500ms 防抖自动重载。

**手动重载**：通过 GraphQL Mutation：

```graphql
mutation {
  reloadTemplates {
    successCount
    failures { name error }
    duration
  }
}
```

错误隔离：加载失败的模板保留旧版本，不影响其他模板。Mutation 有 10 秒冷却期。

## 5.10 并发控制

`max_concurrent_queries`（默认 10）通过信号量限制模板查询并发数，防止复杂报表查询饿死共享连接池中的单表查询。

超过并发限制的请求会等待信号量释放，如果等待超过请求超时则返回超时错误。

## 5.11 可观测性

模板引擎暴露的 Prometheus 指标：

| 指标 | 类型 | 说明 |
|------|------|------|
| `graphql_template_query_duration_seconds` | Histogram | 模板查询总耗时 |
| `graphql_template_queries_total` | Counter | 查询计数（按模板名和状态） |
| `graphql_template_render_duration_seconds` | Histogram | 模板渲染耗时 |
| `graphql_template_semaphore_wait_seconds` | Histogram | 信号量等待耗时 |
| `graphql_template_cache_hits_total` | Counter | 缓存命中/未命中计数 |

OpenTelemetry Span：`Template Query {name}`，包含 `template.name`、`db.system`、`db.statement`（脱敏后）属性。

---

下一模块：[安全体系详解](06-security.md)
