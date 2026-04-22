# 可观测性

## 概述

本服务提供三大可观测性支柱的完整支持：
- **Metrics** — Prometheus 格式指标
- **Tracing** — OpenTelemetry 分布式链路追踪
- **Logging** — 结构化 JSON 日志

## Prometheus 指标

### 指标端点

`GET /metrics` 返回 Prometheus 文本格式指标（`text/plain; version=0.0.4`）。

### 请求级指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_request_duration_seconds` | Histogram | `operation_name`, `operation_type`, `datasource` | 请求处理延迟 |
| `graphql_requests_total` | Counter | `operation_name`, `operation_type`, `status`, `datasource` | 请求总数 |
| `graphql_requests_in_flight` | Gauge | — | 当前并发请求数 |

### 数据源级指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_datasource_query_duration_seconds` | Histogram | `datasource`, `datasource_type` | 数据源查询延迟 |
| `graphql_datasource_connection_pool_active` | Gauge | `datasource` | 活跃连接数 |
| `graphql_datasource_connection_pool_idle` | Gauge | `datasource` | 空闲连接数 |
| `graphql_datasource_connection_pool_waiting` | Gauge | `datasource` | 等待连接的请求数 |

### 缓存指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_cache_hits_total` | Counter | `datasource`, `cache_backend` | 缓存命中次数 |
| `graphql_cache_misses_total` | Counter | `datasource`, `cache_backend` | 缓存未命中次数 |

### 错误指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_errors_total` | Counter | `error_type`, `datasource` | 错误总数 |

### SQL 模板引擎指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_template_query_duration_seconds` | Histogram | `template_name`, `datasource` | 模板查询处理延迟 |
| `graphql_template_queries_total` | Counter | `template_name`, `status` | 模板查询总数（success/error） |
| `graphql_template_render_duration_seconds` | Histogram | `template_name` | 模板渲染延迟（独立于查询执行） |
| `graphql_template_semaphore_wait_seconds` | Histogram | `template_name` | 信号量等待时间 |
| `graphql_template_cache_hits_total` | Counter | `template_name`, `result` | 模板缓存命中/未命中（hit/miss） |
| `graphql_template_render_goroutine_leaks` | Gauge | — | 泄漏的渲染 goroutine 数量 |

### 自定义标签

通过配置为所有指标附加自定义标签，便于在 Grafana 中多维度筛选：

```yaml
metrics:
  custom_labels:
    env: production
    cluster: cn-east-1
    instance: "${HOSTNAME}"
```

### Histogram 桶边界

- 请求延迟：10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s, 10s
- 数据源查询延迟：5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s

## OpenTelemetry 链路追踪

### 配置

```yaml
tracing:
  enabled: true
  sampling_rate: 0.1        # 10% 采样
  otlp:
    endpoint: tempo:4317
    protocol: grpc           # grpc 或 http
```

禁用时使用 NoopTracerProvider，零开销。

### Span 层级

```
Root Span: GraphQL query GetOrders
├── Resolver Span: Resolver starrocks
│   └── DataSource Span: StarRocks Query
│       └── (SQL 执行)
├── Resolver Span: Resolver templateQuery
│   └── Template Query Span: Template Query fleet_report
│       └── (模板渲染 + SQL 执行)
├── Resolver Span: Resolver prometheusInstant
│   └── DataSource Span: Prometheus Query
│       └── (HTTP API 调用)
└── Cache Span: Redis GET (如使用 Redis 缓存)
```

### Span 属性

#### Root Span
- `graphql.operation.name` — 操作名称
- `graphql.operation.type` — 操作类型（query/mutation）
- `http.method` — HTTP 方法
- `http.url` — 请求 URL

#### Resolver Span
- `graphql.field.name` — 字段名称
- `graphql.field.type` — 字段返回类型
- `graphql.datasource` — 目标数据源名称

#### DataSource Span
- `db.system` — 数据源系统（starrocks/prometheus）
- `db.statement` — 查询语句（经脱敏处理）
- `db.datasource` — 数据源名称

#### Template Query Span
- `template.name` — 模板名称
- `db.system` — starrocks
- `db.statement` — 渲染后的 SQL（经脱敏处理）

#### Redis Span
- `db.system` — redis
- `db.operation` — Redis 命令
- `net.peer.name` — Redis 地址

### W3C Trace Context 传播

- 入站：从 `traceparent` 头提取 Trace Context 作为 Root Span 的父上下文
- 出站：向外部数据源请求注入 `traceparent` 头

### 错误记录

- 数据源查询错误：对应 Span 设置 Error 状态 + Event 记录错误详情
- 未捕获异常：Root Span 设置 Error 状态

### Trace 与日志关联

- `trace_id` 自动注入结构化日志字段
- `extensions.traceId` 在 GraphQL 响应中返回，便于客户端查询链路详情

## 结构化日志

### 配置

```yaml
logging:
  level: info       # debug | info | warn | error
  format: json
```

日志级别支持运行时热更新。

### 日志字段

每条日志包含：
- `level` — 日志级别
- `ts` — 时间戳
- `msg` — 消息
- `request_id` — 请求 ID
- `trace_id` — Trace ID（启用 tracing 时）
- 其他上下文字段

### 审计日志

独立于应用日志，记录认证和操作审计信息：

```yaml
logging:
  audit:
    enabled: true
    output: stdout    # stdout 或 file
    file_path: /var/log/api/audit.log
```

审计日志字段：
- 认证主体标识（JWT sub 或 API Key ID）
- 操作时间
- 操作类型
- 目标数据源
- 请求结果

## Grafana 集成建议

### 推荐 Dashboard

1. **请求概览**：`graphql_requests_total` 按 `operation_name` 和 `status` 分组
2. **延迟分布**：`graphql_request_duration_seconds` 的 P50/P95/P99
3. **数据源健康**：`graphql_datasource_connection_pool_*` 指标
4. **缓存效率**：`graphql_cache_hits_total` / (`hits` + `misses`) 计算命中率
5. **错误趋势**：`graphql_errors_total` 按 `error_type` 分组
6. **模板查询**：`graphql_template_query_duration_seconds` P50/P95/P99 按 `template_name` 分组
7. **模板缓存**：`graphql_template_cache_hits_total` 按 `result` (hit/miss) 分组计算命中率
8. **模板并发**：`graphql_template_semaphore_wait_seconds` 监控信号量等待时间

### HPA 扩缩容指标

推荐基于 `graphql_requests_in_flight` 自定义指标进行 HPA 扩缩容，通过 Prometheus Adapter 暴露为 Kubernetes custom metrics API。
