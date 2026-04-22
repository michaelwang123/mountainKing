# 迁移指南：从 Java/OData application-api 到 mountainKing GraphQL API

本文档帮助开发者将现有基于 Java/OData 的 application-api 服务迁移到 mountainKing GraphQL API。

## 目录

1. [迁移概述](#迁移概述)
2. [API 映射对照表](#api-映射对照表)
3. [模板迁移](#模板迁移)
4. [配置迁移](#配置迁移)
5. [认证迁移](#认证迁移)
6. [常见问题与注意事项](#常见问题与注意事项)

---

## 迁移概述

### 架构变化

| 维度 | 旧系统 (application-api) | 新系统 (mountainKing) |
|------|-------------------------|----------------------|
| 语言 | Java | Go |
| API 协议 | OData REST | GraphQL |
| 模板引擎 | FreeMarker (.ftl) | Go text/template (.sql.tmpl) |
| 数据库访问 | JDBC 直连 | StarRocks Adapter (MySQL 协议) |
| 监控数据 | — | Prometheus Adapter (HTTP API) |
| 配置格式 | properties / XML | YAML + 环境变量 (12-Factor) |
| 缓存 | 应用内缓存 | 内存 LRU / Redis（可选） |
| 认证 | 自定义 | JWT (HS256/RS256/ES256) / API Key |

### 迁移步骤概览

1. 梳理现有 OData 端点，确定对应的 GraphQL 查询方式
2. 将 FreeMarker 模板转换为 Go text/template 格式
3. 编写 `config.yaml` 配置文件
4. 部署 mountainKing 服务并验证查询结果
5. 切换客户端调用方式（REST → GraphQL）
6. 下线旧服务

---

## API 映射对照表

### OData 端点 → GraphQL 查询

| 旧系统 OData 端点 | 新系统 GraphQL 查询 | 说明 |
|-------------------|-------------------|------|
| `GET /odata/Orders?$filter=...&$top=10` | `query { starrocks(datasource: "analytics_db", sql: "SELECT ... FROM orders WHERE ... LIMIT 10") { columns, rows } }` | 简单查询直接使用 `starrocks` 查询 |
| `GET /odata/Orders?$filter=...&$orderby=...&$skip=0&$top=20` | `query { starrocks(datasource: "analytics_db", sql: "...", first: 20, offset: 0) { columns, rows, totalCount } }` | 分页查询使用 `first`/`offset` 参数 |
| `GET /api/report/fleet?eerid=xxx&period=monthly` | `query { templateQuery(name: "fleet_report", params: {eerid: "xxx", period: "monthly"}) { columns, rows, totalCount } }` | 复杂报表使用模板查询 |
| `GET /api/report/driver-score?id=1&start=...&end=...` | `query { templateQuery(name: "driver_score", params: {driver_id: 1, start_date: "...", end_date: "..."}) { columns, rows } }` | 驾驶员报表使用模板查询 |
| `GET /api/metrics/...` | `query { prometheus(datasource: "monitoring", query: "up") { columns, rows } }` | 监控指标使用 Prometheus 查询 |

### 跨数据源并行查询

旧系统需要客户端分别调用多个端点再合并结果。新系统支持单次 GraphQL 请求并行查询多个数据源：

```graphql
query {
  orders: starrocks(datasource: "analytics_db", sql: "SELECT ...") {
    columns
    rows
  }
  metrics: prometheus(datasource: "monitoring", query: "http_requests_total") {
    columns
    rows
  }
}
```

---

## 模板迁移

### FreeMarker → Go text/template 语法对照

| FreeMarker (.ftl) | Go text/template (.sql.tmpl) | 说明 |
|-------------------|------------------------------|------|
| `${eerid}` | `{{.Params.eerid \| safeString}}` | 变量引用，必须使用安全函数 |
| `<#if condition>...</#if>` | `{{if .Params.condition}}...{{end}}` | 条件判断 |
| `<#if condition>...<#else>...</#if>` | `{{if .Params.condition}}...{{else}}...{{end}}` | 条件分支 |
| `<#list items as item>...</#list>` | `{{range .Params.items}}...{{end}}` | 循环 |
| `<#include "shared/common.ftl">` | `{{template "common" .}}` | 模板引用 |
| `${value?string}` | `{{.Params.value \| safeString}}` | 字符串转义 |
| `${value?c}` | `{{.Params.value \| safeInt}}` | 数值安全输出 |

### 安全函数

旧系统中直接拼接 SQL 的写法在新系统中**必须**使用安全函数：

```
-- 旧系统 (FreeMarker) — 存在 SQL 注入风险
SELECT * FROM orders WHERE eerid = '${eerid}'

-- 新系统 (Go template) — 安全
SELECT * FROM orders WHERE eerid = {{.Params.eerid | safeString}}
```

可用安全函数：`safeString`、`quote`、`safeInt`、`safeFloat`、`safeIdentifier`、`safeInList`、`safeLike`、`join`、`default`、`upper`、`lower`、`trimSpace`。

详见 [SQL 模板引擎文档](sql-template-engine.md)。

### 模板注册

每个模板需要在 `config.yaml` 的 `sql_templates.templates` 中注册：

```yaml
sql_templates:
  enabled: true
  datasource_name: analytics_db
  base_dir: ./templates
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
```

---

## 配置迁移

### 旧系统配置 → config.yaml 对照

| 旧系统配置项 | config.yaml 对应项 | 说明 |
|-------------|-------------------|------|
| `server.port` | `server.port` | 服务端口（默认 8080） |
| `datasource.url` (JDBC) | `datasources[].connection.host/port` | 数据库连接地址 |
| `datasource.username` | `datasources[].connection.username` | 数据库用户名 |
| `datasource.password` | `datasources[].connection.password` | 数据库密码（支持环境变量 `${GRAPHQL_STARROCKS_PASSWORD}`） |
| `datasource.database` | `datasources[].connection.database` | 数据库名 |
| `datasource.pool.maxSize` | `datasources[].options.pool_size` | 连接池大小 |
| `datasource.pool.timeout` | `datasources[].options.connection_timeout` | 连接超时 |
| `template.baseDir` | `sql_templates.base_dir` | 模板文件目录 |
| `cache.enabled` | `cache.enabled` | 缓存开关 |
| `cache.ttl` | `cache.default_ttl` | 缓存 TTL |
| `auth.type` | `auth.method` | 认证方式（jwt / apikey） |
| `logging.level` | `logging.level` | 日志级别 |

### 环境变量

所有配置项支持 `GRAPHQL_` 前缀的环境变量覆盖。例如：

```bash
GRAPHQL_SERVER_PORT=8080
GRAPHQL_SERVER_MODE=development
GRAPHQL_STARROCKS_PASSWORD=your-password
GRAPHQL_AUTH_METHOD=none  # 开发模式跳过认证
```

完整环境变量列表参见项目根目录 `.env.example`。

---

## 认证迁移

| 旧系统 | 新系统 | 说明 |
|--------|--------|------|
| 自定义 Session/Cookie | JWT Bearer Token | 请求头 `Authorization: Bearer <token>` |
| — | API Key | 请求头 `X-API-Key: <key>` |
| — | 开发模式免认证 | `auth.method: none` 或 `GRAPHQL_AUTH_METHOD=none` |

开发阶段建议使用 `auth.method: none` 跳过认证，生产环境使用 JWT (RS256) 或 API Key。

---

## 常见问题与注意事项

### 1. SQL 语法差异

mountainKing 使用 StarRocks（兼容 MySQL 协议）。如果旧系统使用其他数据库（如 PostgreSQL、Oracle），需注意 SQL 方言差异：

- 字符串拼接：使用 `CONCAT()` 而非 `||`
- 分页：使用 `LIMIT offset, count` 而非 `OFFSET ... FETCH`
- 日期函数：参考 StarRocks 文档

### 2. 表/列白名单

新系统要求在 `datasources[].options.allowed_tables` 中显式声明允许查询的表和列。未声明的表/列会被拒绝访问。

```yaml
allowed_tables:
  orders:
    columns: [order_id, user_id, amount, status, created_at]
```

### 3. 分页行为变化

- 旧系统：OData `$skip/$top` 分页
- 新系统：`first`/`offset` 参数或 Relay 游标分页
- 模板查询的分页由引擎自动包装（over-fetch 策略），模板 SQL 中**不要**添加外层 `ORDER BY`

### 4. 响应格式

GraphQL 响应格式与 OData 不同，客户端需要适配：

```json
// OData 响应
{ "value": [{"id": 1, "name": "..."}], "@odata.count": 100 }

// GraphQL 响应
{ "data": { "starrocks": { "columns": ["id","name"], "rows": [[1,"..."]], "totalCount": 100 } } }
```

### 5. 错误处理

GraphQL 错误通过 `errors` 数组返回，而非 HTTP 状态码：

```json
{
  "data": null,
  "errors": [{ "message": "...", "extensions": { "code": "VALIDATION_UNSAFE_SQL" } }]
}
```

常见错误码参见 [错误参考文档](error-reference.md)。

### 6. 性能调优

- 启用缓存（`cache.enabled: true`）减少重复查询
- 合理设置连接池大小（`pool_size`）
- 模板查询使用 `cache_enabled: true` 和合适的 `cache_ttl`
- 使用 `max_concurrent_queries` 控制模板查询并发，防止连接池饿死

### 7. 监控与可观测性

新系统内置 Prometheus 指标端点（`/metrics`）和 OpenTelemetry 链路追踪，建议部署后配置 Grafana 监控面板。预置面板配置位于 `deploy/grafana/dashboard.json`。
