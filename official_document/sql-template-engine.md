# SQL 模板引擎

## 1. 功能概述

SQL 模板引擎（TemplateEngine）是 mountainKing GraphQL API 服务的扩展查询组件，用于支持复杂的多表 JOIN、CTE（公用表表达式）、窗口函数等高级 SQL 查询场景。它通过预定义的 Go `text/template` 模板文件，将业务参数渲染为完整的 SQL 语句，经由 StarRocks 执行后返回动态 JSON 结果。

### 与 StarRocks 直接查询的关系

| 特性 | `starrocks` 直接查询 | `templateQuery` 模板查询 |
|------|---------------------|------------------------|
| 查询方式 | 单表 SELECT + WHERE + ORDER BY + LIMIT | 任意复杂度 SQL（CTE、JOIN、窗口函数等） |
| Schema 定义 | 白名单校验（`allowed_tables`） | 模板文件预定义 |
| 参数传递 | GraphQL `filters` 参数 | `parameters` JSON 对象 |
| 安全机制 | 白名单 + 标识符校验 | 安全函数 + 词法扫描器 |
| 分页方式 | Relay cursor 分页 | offset 分页（无稳定游标） |
| 连接池 | 共享 `*sql.DB` | 共享 `*sql.DB`（信号量保护） |

两种查询方式并行工作，互不影响。

### 架构集成方式

TemplateEngine 通过 `RawExecutor` 接口与 StarRocks Adapter 交互，实现接口隔离：

```
┌─────────────────────────────────────────────────────────┐
│                    GraphQL Layer                         │
│                                                         │
│  starrocks(table, filters...)   templateQuery(name, params...)  │
│       │                                │                │
│  queryResolver.Starrocks()    queryResolver.TemplateQuery()     │
└───────┬────────────────────────────────┬────────────────┘
        │                                │
        │                      ┌─────────▼──────────┐
        │                      │   TemplateEngine    │
        │                      │  ┌───────────────┐  │
        │                      │  │ Registry      │  │
        │                      │  │ ParamValidator │  │
        │                      │  │ SQLSanitizer  │  │
        │                      │  │ Semaphore(10) │  │
        │                      │  └───────────────┘  │
        │                      └─────────┬──────────┘
        │                                │ RawExecutor interface
        │                                │ (rendered SQL)
        ▼                                ▼
┌────────────────────────────────────────────────────────┐
│                StarRocks Adapter                        │
│  Execute(QueryRequest)      ExecuteRaw(sql, args...)    │
│  [DataSource interface]     [RawExecutor interface]     │
│       │                           │                     │
│       ▼                           ▼                     │
│  ┌────────────────────────────────────────┐             │
│  │         *sql.DB 连接池 (共享)           │             │
│  └────────────────────────────────────────┘             │
└─────────────────────────────────────────────────────────┘
```

`RawExecutor` 接口仅定义 `ExecuteRaw` 方法，TemplateEngine 无法调用白名单查询等其他 Adapter 方法。`DataSource` 接口不做修改，Prometheus 适配器不受影响。

### 查询执行流程

```
templateQuery 请求
  → 1. Registry 查找模板
  → 2. 参数校验（类型、必填、枚举、长度、正则）
  → 3. 模板渲染（render_timeout 超时控制）
  → 4. SQL 安全检查（7 状态词法扫描器）
  → 5. 分页包装（Pagination Wrapper）
  → 6. 信号量获取（并发控制）
  → 7. 缓存查询 / 数据库执行
  → 8. totalCount 查询（可选）
  → 9. 结果返回
```

> 源码参考：`internal/template/engine.go` — `Execute` 方法

## 2. 配置参考

所有模板引擎配置位于 `config.yaml` 的 `sql_templates` 段。

### 完整配置项

```yaml
sql_templates:
  enabled: true                            # 是否启用模板引擎（默认 false）
  datasource_name: analytics_db            # 关联的 StarRocks 数据源名称（必填）
  base_dir: ./templates                    # 模板文件基础目录
  shared_dir: ./templates/_shared          # 共享模板片段目录（默认 base_dir/_shared）
  render_timeout: 5s                       # 模板渲染超时（默认 5s）
  max_rendered_sql_length: 65536           # 渲染结果最大字节数（默认 64KB）
  max_concurrent_queries: 10               # 模板查询最大并发数（默认 10）
  templates:                               # 模板定义列表
    - name: fleet_report                   # 模板名称，仅允许 [a-zA-Z0-9_-]，1-64 字符
      file: fleet/fleet_report.sql.tmpl    # 相对于 base_dir 的文件路径
      description: 车队综合报表             # 模板描述
      cache_enabled: true                  # 是否启用缓存（默认 true）
      cache_ttl: 300s                      # 缓存 TTL（不设置则使用数据源默认 TTL）
      count_enabled: true                  # 是否支持 totalCount（默认 true）
      parameters:                          # 参数 Schema 定义
        - name: eerid                      # 参数名称
          type: string                     # 类型：string | int | float | boolean | string[]
          required: true                   # 是否必填
          max_length: 64                   # 字符串最大长度（默认 1024）
        - name: period
          type: string
          required: false
          default: monthly                 # 默认值（未传参时使用）
          enum: [daily, weekly, monthly]   # 枚举约束
        - name: vehicle_ids
          type: "string[]"
          required: false
          max_items: 500                   # 数组最大元素数（默认 1000）
```

### 配置项详解

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 设为 `true` 启用模板引擎。禁用时 `templateQuery` 返回 `VALIDATION_TEMPLATE_NOT_FOUND`，`templateList` 返回空数组 |
| `datasource_name` | string | — | 关联的 StarRocks 数据源名称，必须与 `datasources[].name` 匹配 |
| `base_dir` | string | `./templates` | 模板文件基础目录，模板 `file` 路径相对于此目录解析 |
| `shared_dir` | string | `base_dir/_shared` | 共享模板片段目录，其中的 `.sql.tmpl` 文件自动加载为可引用片段 |
| `render_timeout` | duration | `5s` | 模板渲染阶段超时，独立于 SQL 执行超时 |
| `max_rendered_sql_length` | int | `65536` | 渲染结果最大字节数，超过返回 `VALIDATION_UNSAFE_SQL` |
| `max_concurrent_queries` | int | `10` | 信号量容量，限制同时执行的模板查询数量 |

### 参数 Schema 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `name` | string | — | 参数名称 |
| `type` | string | — | 参数类型：`string`、`int`、`float`、`boolean`、`string[]` |
| `required` | bool | `false` | 是否必填 |
| `default` | string | — | 默认值（未传参时使用） |
| `enum` | []string | — | 枚举约束，参数值必须在列表中 |
| `max_length` | int | `1024` | `string` 类型参数的最大长度 |
| `max_items` | int | `1000` | `string[]` 类型参数的最大元素数 |
| `pattern` | string | — | `string` 类型参数的正则约束（Go RE2 语法），启动时预编译 |

### 超时模型

模板查询的端到端超时由 `server.request_timeout` 控制。在此总超时内：

- `render_timeout` — 控制模板渲染阶段（默认 5s）
- `query_timeout` — 控制 SQL 执行阶段（来自数据源 `options.query_timeout`）

两者独立计时、互不包含。

```
|<─────────── server.request_timeout (30s) ──────────────>|
|<── render_timeout (5s) ──>|<── query_timeout (30s) ──>|
|   模板渲染 + 安全检查      |   SQL 执行 + 结果转换      |
```

> 源码参考：`internal/config/config.go` — `SQLTemplatesConfig` 结构体

## 3. 模板语法

模板文件使用 Go `text/template` 语法，文件扩展名为 `.sql.tmpl`，编码为 UTF-8。

### 变量绑定

客户端传递的参数通过 `.Params` 对象访问：

```sql
SELECT * FROM orders WHERE user_id = {{.Params.user_id | safeInt}}
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

可选参数的条件判断：

```sql
{{if .Params.vehicle_ids}}
  AND vehicle_id IN ({{.Params.vehicle_ids | safeInList}})
{{end}}
```

### 循环

```sql
{{range $i, $col := .Params.columns}}
  {{if $i}}, {{end}}{{$col | safeIdentifier}}
{{end}}
```

### 模板继承与共享片段

共享片段存放在 `shared_dir`（默认 `templates/_shared/`），通过 `{{define}}` 和 `{{template}}` 实现复用：

**共享片段** (`templates/_shared/time_filter.sql.tmpl`)：

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

**引用共享片段**：

```sql
SELECT * FROM events
WHERE eerid = {{.Params.eerid | quote}}
{{template "time_filter" .}}
```

### 内置函数

Go `text/template` 内置函数均可使用：`eq`、`ne`、`lt`、`gt`、`and`、`or`、`not`、`len`、`index` 等。

> 源码参考：`internal/template/funcmap.go` — `buildFuncMap` 函数；`templates/` 目录下的模板文件示例

## 4. 安全函数速查表

模板引擎注册了 12 个自定义函数，分为安全函数和工具函数两类。

### 安全函数

| 函数 | 用途 | 示例 | 输出 |
|------|------|------|------|
| `safeString` | SQL 字符串转义（不加引号）。处理顺序：移除 NULL 字节 → 转义反斜杠 `\` → `\\` → 转义单引号 `'` → `''` | `{{.Params.name \| safeString}}` | `O''Brien` |
| `quote` | SQL 字符串转义 + 包裹单引号。等价于 `safeString` + 加引号 | `{{.Params.name \| quote}}` | `'O''Brien'` |
| `safeInt` | 验证参数为有效整数，返回整数字符串。支持 int、int64、float64（无小数部分）、string | `{{.Params.limit \| safeInt}}` | `100` |
| `safeFloat` | 验证参数为有效有限浮点数，拒绝 NaN 和 ±Inf | `{{.Params.threshold \| safeFloat}}` | `3.14` |
| `safeIdentifier` | SQL 标识符校验。仅允许 `[a-zA-Z0-9_.]`，按 `.` 拆分（最多 2 段），每段 1-64 字符，用反引号包裹 | `{{.Params.col \| safeIdentifier}}` | `` `column_name` `` |
| `safeInList` | 字符串数组转 IN 子句值列表。对每个元素独立转义后用单引号包裹，逗号分隔。空数组返回错误 | `{{.Params.ids \| safeInList}}` | `'a','b','c'` |
| `safeLike` | LIKE 通配符转义。处理顺序：`\` → `\\` → `%` → `\%` → `_` → `\_`。需配合 `ESCAPE '\\'` 使用 | `{{.Params.keyword \| safeLike}}` | `100\%` |

### 工具函数

| 函数 | 用途 | 示例 | 输出 |
|------|------|------|------|
| `join` | 字符串数组转逗号分隔字符串 | `{{.Params.cols \| join}}` | `a,b,c` |
| `default` | 零值时返回默认值 | `{{.Params.limit \| default 100}}` | `100`（当 limit 未传时） |
| `upper` | 字符串转大写 | `{{.Params.status \| upper}}` | `ACTIVE` |
| `lower` | 字符串转小写 | `{{.Params.status \| lower}}` | `active` |
| `trimSpace` | 去除首尾空白字符 | `{{.Params.name \| trimSpace}}` | `hello` |

### 推荐用法

- 字符串值统一使用 `{{.Params.x | quote}}`（更简洁、不易遗漏引号）
- `safeString` 仅用于需要手动控制引号的特殊场景（如 LIKE 模式拼接）
- 数值参数使用 `safeInt` / `safeFloat` 而非直接输出
- 标识符（表名、列名）使用 `safeIdentifier`
- IN 子句使用前先用 `{{if .Params.ids}}` 判断非空

> 源码参考：`internal/template/funcmap.go` — 所有安全函数和工具函数的完整实现

## 5. 最佳实践

### SQL 注入防护

1. **所有用户输入必须经过安全函数处理**，禁止直接输出原始参数值：

   ```sql
   -- ✅ 正确
   WHERE name = {{.Params.name | quote}}

   -- ❌ 危险：直接输出未转义的参数
   WHERE name = '{{.Params.name}}'
   ```

2. **数值参数使用类型安全函数**：

   ```sql
   -- ✅ 正确
   WHERE id = {{.Params.id | safeInt}}

   -- ❌ 危险：字符串 "1 OR 1=1" 会被直接输出
   WHERE id = {{.Params.id}}
   ```

3. **LIKE 查询使用 `safeLike` + `ESCAPE`**：

   ```sql
   WHERE name LIKE CONCAT('%', {{.Params.keyword | safeLike | quote}}, '%') ESCAPE '\\'
   ```

4. **渲染后安全检查**：模板引擎在渲染完成后自动执行 7 状态词法扫描器检查，检测字符串外的分号（多语句注入）和 SQL 注释（注释注入），同时保留 StarRocks Optimizer Hint（`/*+ ... */`）。

### 模板组织

- 将公共 SQL 片段（如时间过滤、权限过滤）提取到 `shared_dir` 目录
- 按业务域组织模板文件目录结构（如 `fleet/`、`driver/`）
- 模板名称使用 `snake_case`，仅包含 `[a-zA-Z0-9_-]`

### 分页注意事项

- 模板 SQL **避免在最外层使用 `ORDER BY`**，排序由客户端通过 `orderBy` 参数控制
- 内部子查询的排序（如窗口函数 `PARTITION BY ... ORDER BY`）不受此限制
- 分页包装器使用参数化查询（`?` 占位符）传递 LIMIT/OFFSET 值
- 未指定 `first` 时，默认使用 `graphql.max_result_rows`（默认 10000）作为 LIMIT

### 并发控制

- `max_concurrent_queries`（默认 10）通过信号量限制同时执行的模板查询数
- 防止长时间运行的复杂报表查询饿死共享连接池中的单表查询
- 信号量等待时间计入 `query_timeout`，超时返回 `DATASOURCE_TIMEOUT`
- 生产环境建议根据连接池大小（`pool_size`）和业务负载调整此值

> 源码参考：`internal/template/sanitizer.go` — 7 状态词法扫描器；`internal/template/pagination.go` — 分页包装器

## 6. 端到端查询示例

以下展示从模板文件到最终响应的完整流程。

### 步骤 1：编写模板文件

创建 `templates/fleet/fleet_report.sql.tmpl`：

```sql
{{/* 车队综合报表 - 支持条件分支、CTE、多表 JOIN */}}
WITH base_events AS (
  SELECT vehicle_id, event_type, event_time, duration_seconds
  FROM vehicle_events
  WHERE eerid = {{.Params.eerid | quote}}
  {{if eq .Params.period "daily"}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 1 DAY)
  {{else if eq .Params.period "weekly"}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 7 DAY)
  {{else}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 30 DAY)
  {{end}}
  {{if .Params.vehicle_ids}}
    AND vehicle_id IN ({{.Params.vehicle_ids | safeInList}})
  {{end}}
)
SELECT
  b.vehicle_id,
  v.plate_number,
  COUNT(*) AS event_count,
  SUM(b.duration_seconds) AS total_duration,
  AVG(b.duration_seconds) AS avg_duration
FROM base_events b
JOIN vehicles v ON b.vehicle_id = v.vehicle_id
GROUP BY b.vehicle_id, v.plate_number
```

### 步骤 2：在 config.yaml 中注册模板

```yaml
sql_templates:
  enabled: true
  datasource_name: analytics_db
  base_dir: ./templates
  max_concurrent_queries: 10
  templates:
    - name: fleet_report
      file: fleet/fleet_report.sql.tmpl
      description: 车队综合报表
      cache_enabled: true
      cache_ttl: 300s
      count_enabled: true
      parameters:
        - name: eerid
          type: string
          required: true
          max_length: 64
        - name: period
          type: string
          required: false
          default: monthly
          enum: [daily, weekly, monthly]
        - name: vehicle_ids
          type: "string[]"
          required: false
          max_items: 500
```

### 步骤 3：发送 GraphQL 查询

```graphql
query {
  templateQuery(
    templateName: "fleet_report"
    parameters: {
      eerid: "EER001"
      period: "monthly"
      vehicle_ids: ["V001", "V002"]
    }
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

使用 curl：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-jwt-token>" \
  -d '{
    "query": "{ templateQuery(templateName: \"fleet_report\", parameters: {eerid: \"EER001\", period: \"monthly\", vehicle_ids: [\"V001\", \"V002\"]}, fields: [\"vehicle_id\", \"plate_number\", \"event_count\"], first: 20, offset: 0, orderBy: [{field: \"event_count\", direction: DESC}]) { nodes pageInfo { hasNextPage hasPreviousPage } totalCount } }"
  }'
```

### 步骤 4：响应结果

```json
{
  "data": {
    "templateQuery": {
      "nodes": [
        {"vehicle_id": "V001", "plate_number": "京A12345", "event_count": 156},
        {"vehicle_id": "V002", "plate_number": "京B67890", "event_count": 89}
      ],
      "pageInfo": {
        "hasNextPage": false,
        "hasPreviousPage": false
      },
      "totalCount": 2
    }
  }
}
```

### 查询可用模板列表

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

> 源码参考：`internal/graphql/schema/template.graphql` — GraphQL Schema 定义；`internal/graphql/resolver/template.resolvers.go` — Resolver 实现

## 7. 错误处理

模板引擎定义了 8 个专用错误码，通过 GraphQL 响应的 `errors[].extensions.code` 返回。

### 错误码一览

| 错误码 | HTTP | 触发条件 | 处理建议 |
|--------|------|---------|---------|
| `VALIDATION_TEMPLATE_NOT_FOUND` | 400 | 请求的模板名称未在 Registry 中注册，或模板引擎未启用 | 检查 `templateName` 拼写，确认模板已在 `config.yaml` 中注册且 `sql_templates.enabled: true` |
| `INTERNAL_TEMPLATE_RENDER_ERROR` | 500 | 模板渲染失败：语法错误、引用未定义参数、渲染超时 | 检查模板文件语法，确认所有参数已正确传递。如为超时，考虑简化模板逻辑或增大 `render_timeout` |
| `VALIDATION_UNSAFE_SQL` | 400 | 渲染结果包含多条 SQL 语句（分号检测）或超过 `max_rendered_sql_length` | 检查模板是否意外生成分号。如为长度超限，考虑简化 SQL 或增大 `max_rendered_sql_length` |
| `VALIDATION_MISSING_PARAMETER` | 400 | 模板定义的必填参数（`required: true`）未在请求中提供 | 补充缺失的必填参数 |
| `VALIDATION_INVALID_PARAMETER_TYPE` | 400 | 参数值的数据类型与 Schema 定义不匹配（如 `int` 类型传入字符串） | 检查参数值类型，确保与 Schema 定义一致 |
| `VALIDATION_INVALID_PARAMETER_VALUE` | 400 | 参数值不在枚举范围内、超过 `max_length`、超过 `max_items`、或不匹配 `pattern` 正则 | 检查参数值是否符合 Schema 中定义的约束 |
| `VALIDATION_INVALID_FIELD` | 400 | `fields` 或 `orderBy` 中的字段名包含非法字符（不符合 `[a-zA-Z0-9_.]` 规则） | 检查字段名是否包含特殊字符 |
| `DATASOURCE_TEMPLATE_QUERY_ERROR` | 200 | StarRocks 执行模板 SQL 失败（SQL 语法错误、表不存在等） | 检查模板生成的 SQL 是否正确，确认 StarRocks 中相关表和列存在 |

### 错误响应示例

```json
{
  "errors": [{
    "message": "template \"nonexistent\" not found",
    "path": ["templateQuery"],
    "extensions": {
      "code": "VALIDATION_TEMPLATE_NOT_FOUND",
      "classification": "VALIDATION"
    }
  }],
  "data": null
}
```

### 客户端错误处理建议

```
if error.extensions.code == "VALIDATION_TEMPLATE_NOT_FOUND":
    # 模板不存在，检查名称或调用 templateList 查看可用模板
    check_template_name()

elif error.extensions.code.startswith("VALIDATION_"):
    # 请求参数问题，需要修改请求
    fix_request_parameters(error)

elif error.extensions.code == "DATASOURCE_TEMPLATE_QUERY_ERROR":
    # 数据源执行错误，可能是临时问题，可重试
    retry_with_backoff()

elif error.extensions.code == "INTERNAL_TEMPLATE_RENDER_ERROR":
    # 模板渲染错误，通常需要修复模板文件
    report_to_admin(error)
```

> 源码参考：`internal/errors/types.go` — 错误码常量定义；`internal/template/validator.go` — 参数校验逻辑

## 8. 热加载

模板引擎支持在不重启服务的情况下更新 SQL 模板，提供两种触发方式。

### fsnotify 自动重载

模板引擎启动时通过 fsnotify 监听 `base_dir` 和 `shared_dir` 目录（递归包含子目录）。当检测到文件变更时，使用 500ms 防抖合并快速连续的文件事件（如编辑器保存产生的多次写入），然后触发模板重新加载。

动态创建的子目录会自动加入监听范围。

### reloadTemplates Mutation 手动重载

通过 GraphQL Mutation 手动触发重新加载（需要 `mutation` 操作权限）：

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

响应示例：

```json
{
  "data": {
    "reloadTemplates": {
      "successCount": 5,
      "failures": [
        {
          "name": "broken_template",
          "error": "parse error: unexpected \"}\" in command"
        }
      ],
      "duration": "12.345ms"
    }
  }
}
```

### 重载机制

- **错误隔离**：如果某个模板文件存在语法错误，该模板保留旧版本，其他模板正常更新
- **并发安全**：fsnotify 触发和 Mutation 触发共享同一个互斥锁（`sync.Mutex`），防止并发重载。Registry 使用独立的读写锁（`sync.RWMutex`），确保重载期间正在执行的查询不受影响
- **缓存清理**：通过比较模板文件的 SHA-256 hash 判断是否变更，仅对 hash 变化的模板清除缓存条目，未变更的模板保留缓存
- **冷却机制**：Mutation 触发的重载有 10 秒冷却期，频繁调用会返回上次的缓存结果

> 源码参考：`internal/template/watcher.go` — fsnotify 文件监听实现；`internal/template/engine.go` — `Reload` 方法

## 9. 缓存策略

模板查询复用现有的 Cache Layer 组件，支持模板级别的精细缓存控制。

### 模板级缓存 TTL

每个模板可独立配置缓存 TTL：

```yaml
templates:
  - name: fleet_report
    cache_enabled: true
    cache_ttl: 300s          # 5 分钟缓存
  - name: realtime_dashboard
    cache_enabled: false     # 实时性要求高，禁用缓存
```

未配置 `cache_ttl` 时，使用数据源级别的默认 TTL（`cache.per_datasource.{datasource_name}.ttl`）。

### 缓存禁用

三种方式禁用缓存：

1. **模板级别**：`cache_enabled: false` — 该模板所有查询不缓存
2. **请求级别**：GraphQL 请求中设置 `extensions.cache: false` — 本次查询跳过缓存
3. **全局级别**：`cache.enabled: false` — 关闭所有缓存

### totalCount 独立缓存

当客户端请求 `totalCount` 字段时，COUNT 查询使用独立的缓存 key（在数据查询 key 后追加 `:count` 后缀），与数据查询分别缓存，TTL 相同。

### 缓存 Key 生成规则

数据查询缓存 key 格式：

```
cache:template:{template_name}:{xxhash64(sorted_params + sorted_fields + first + offset + orderBy)}
```

COUNT 查询缓存 key 格式：

```
cache:template:{template_name}:{xxhash64(sorted_params)}:count
```

Key 生成规则：
- 参数按 key 字母序排序后拼接为 `key=value&key=value` 格式
- 字段按字母序排序后逗号分隔
- `first`、`offset` 直接拼接（未指定时为 `nil`）
- `orderBy` 保持原始顺序，每项格式为 `field:direction`
- 各部分用 `|` 分隔后计算 xxhash64
- 不使用渲染后的 SQL 作为 key 输入（避免模板空白差异导致缓存未命中）

> 源码参考：`internal/template/cache.go` — 缓存 key 生成和缓存集成逻辑

## 10. 可观测性

### Prometheus 指标

模板引擎注册了 5 个专用 Prometheus 指标：

| 指标名称 | 类型 | 标签 | 说明 |
|---------|------|------|------|
| `graphql_template_query_duration_seconds` | Histogram | `template_name`, `datasource` | 模板查询端到端延迟（含渲染 + 执行 + 结果转换） |
| `graphql_template_queries_total` | Counter | `template_name`, `status` | 模板查询总数。`status` 为 `success` 或 `error` |
| `graphql_template_render_duration_seconds` | Histogram | `template_name` | 模板渲染延迟（独立于 SQL 执行） |
| `graphql_template_semaphore_wait_seconds` | Histogram | `template_name` | 信号量等待时间（反映并发压力） |
| `graphql_template_cache_hits_total` | Counter | `template_name`, `result` | 缓存命中/未命中计数。`result` 为 `hit` 或 `miss` |

模板查询同时计入现有的 `graphql_request_duration_seconds`（HTTP 层），两者维度不同。

### PromQL 查询示例

```promql
# 模板查询 P99 延迟
histogram_quantile(0.99, rate(graphql_template_query_duration_seconds_bucket[5m]))

# 模板查询 QPS（按模板名称分组）
sum(rate(graphql_template_queries_total[5m])) by (template_name)

# 模板缓存命中率
sum(rate(graphql_template_cache_hits_total{result="hit"}[5m]))
/ sum(rate(graphql_template_cache_hits_total[5m]))

# 信号量饱和度（P99 等待时间 > 1s 表示饱和）
histogram_quantile(0.99, rate(graphql_template_semaphore_wait_seconds_bucket[5m]))
```

### OpenTelemetry Span

每次模板查询在当前 Resolver Span 下创建一个子 Span：

| 属性 | 值 | 说明 |
|------|-----|------|
| Span 名称 | `Template Query {template_name}` | 如 `Template Query fleet_report` |
| `template.name` | 模板名称 | 如 `fleet_report` |
| `db.system` | `starrocks` | 数据库系统标识 |
| `db.statement` | 渲染后的 SQL（脱敏后） | 经过 `sanitization.rules` 脱敏处理 |

### 审计日志

每次模板查询在审计日志中记录一条条目，包含：
- `principal` — 认证主体
- `operation` — 操作类型（`query`）
- `datasource` — 数据源名称
- `template_name` — 模板名称
- `success` — 是否成功

### 结构化日志

每次模板查询在结构化日志中记录：
- 模板名称
- 渲染耗时
- 查询耗时
- 结果行数
- 渲染后的 SQL（脱敏后，debug 级别）

> 源码参考：`internal/template/engine.go` — 指标记录和 Span 创建逻辑

## 11. 相关文档

- [架构概览](architecture.md) — 系统整体架构和 TemplateEngine 集成说明
- [配置参考](configuration.md) — 完整配置项说明
- [GraphQL API 参考](graphql-api.md) — `templateQuery`、`templateList`、`reloadTemplates` 接口详情
- [错误码参考](error-reference.md) — 所有错误码的完整列表和处理建议
- [安全指南](security.md) — 认证授权和 SQL 注入防护
- [可观测性](observability.md) — Prometheus 指标和 OpenTelemetry 链路追踪
- [性能调优](performance.md) — 缓存策略和查询优化
- [故障排查](troubleshooting.md) — 常见问题和解决方案
- [快速开始](getting-started.md) — 模板查询的 curl 示例
