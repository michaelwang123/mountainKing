# 模块 08：可观测性体系

> 掌握 Prometheus 指标、OpenTelemetry 链路追踪、结构化日志和审计日志的配置与使用。

## 8.1 可观测性三支柱

```
                    可观测性
                   /    |    \
              指标    追踪    日志
           (Metrics) (Traces) (Logs)
              │        │        │
         Prometheus  OTLP     zap
              │        │        │
              ▼        ▼        ▼
          Grafana   Jaeger/  stdout/
                    Tempo    文件
```

mountainKing 实现了完整的可观测性三支柱，外加独立的审计日志。

## 8.2 Prometheus 指标

### 端点

`GET /metrics` — 标准 Prometheus 抓取端点（豁免认证和限流）。

### 核心指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `graphql_requests_total` | Counter | method, status | HTTP 请求计数 |
| `graphql_request_duration_seconds` | Histogram | method, operation_type | 请求耗时 |
| `graphql_datasource_query_duration_seconds` | Histogram | datasource, status | 数据源查询耗时 |
| `graphql_datasource_errors_total` | Counter | datasource, error_type | 数据源错误计数 |
| `graphql_cache_hits_total` | Counter | datasource | 缓存命中 |
| `graphql_cache_misses_total` | Counter | datasource | 缓存未命中 |
| `graphql_rate_limit_rejected_total` | Counter | — | 限流拒绝计数 |
| `graphql_circuit_breaker_state` | Gauge | datasource | 熔断器状态 |
| `graphql_template_query_duration_seconds` | Histogram | template_name, datasource | 模板查询耗时 |
| `graphql_template_queries_total` | Counter | template_name, status | 模板查询计数 |
| `graphql_template_render_duration_seconds` | Histogram | template_name | 模板渲染耗时 |
| `graphql_template_semaphore_wait_seconds` | Histogram | template_name | 信号量等待耗时 |
| `graphql_template_cache_hits_total` | Counter | template_name, result | 模板缓存命中 |

### 自定义标签

```yaml
metrics:
  custom_labels:
    env: production
    cluster: cn-east-1
    instance: "${HOSTNAME}"
```

自定义标签会附加到所有指标上，便于多集群/多环境区分。

### Grafana 仪表盘

项目提供预配置的 Grafana 仪表盘（`deploy/grafana/dashboard.json`），包含：
- 请求速率和延迟
- 数据源查询性能
- 缓存命中率
- 熔断器状态
- 模板查询指标

## 8.3 OpenTelemetry 链路追踪

### 配置

```yaml
tracing:
  enabled: true
  sampling_rate: 0.1      # 10% 采样率
  otlp:
    endpoint: tempo:4317
    protocol: grpc         # grpc | http
```

### Span 层级

```
Root Span: HTTP POST /graphql
  ├── GraphQL Operation: query GetData
  │   ├── Resolver: starrocks
  │   │   ├── Cache Lookup
  │   │   └── DataSource Query: analytics_db
  │   │       └── SQL Execute
  │   └── Resolver: templateQuery
  │       ├── Template Query fleet_report
  │       │   ├── Template Render
  │       │   ├── Semaphore Wait
  │       │   └── SQL Execute (RawExecutor)
  │       └── Cache Lookup
  └── Redis Operation (if Redis cache)
```

### Span 属性

| 属性 | 说明 |
|------|------|
| `graphql.operation.type` | query / mutation |
| `graphql.operation.name` | 操作名称 |
| `db.system` | starrocks / prometheus |
| `db.statement` | SQL 语句（脱敏后） |
| `template.name` | 模板名称 |
| `datasource.name` | 数据源名称 |

### W3C Trace Context

支持 `traceparent` HTTP 头传播，实现跨服务链路关联：

```
traceparent: 00-<trace-id>-<span-id>-01
```

## 8.4 结构化日志

基于 zap 的高性能 JSON 日志：

```yaml
logging:
  level: info              # debug | info | warn | error
  format: json
```

日志输出示例：

```json
{
  "level": "info",
  "ts": "2026-04-22T10:30:00.000Z",
  "msg": "template query executed",
  "template_name": "fleet_report",
  "render_duration": "2.5ms",
  "query_duration": "150ms",
  "result_rows": 42,
  "trace_id": "abc123def456"
}
```

每条日志自动关联 `trace_id`，便于从日志跳转到链路追踪。

### 日志级别热更新

日志级别支持运行时热更新（无需重启）：

```bash
# 通过环境变量
export GRAPHQL_LOGGING_LEVEL=debug

# 配置文件修改后自动生效
```

## 8.5 审计日志

独立于应用日志的审计记录，记录所有查询操作：

```yaml
logging:
  audit:
    enabled: true
    output: stdout         # stdout | file
    file_path: /var/log/api/audit.log
```

审计日志字段：

| 字段 | 说明 |
|------|------|
| `principal` | 认证主体（JWT sub 或 API Key ID） |
| `time` | 操作时间 |
| `operation` | 操作类型（query） |
| `datasource` | 目标数据源 |
| `success` | 是否成功 |
| `template_name` | 模板名称（模板查询时） |

## 8.6 敏感信息脱敏

所有可观测性输出（日志、Span、错误消息）都经过脱敏处理：

```yaml
sanitization:
  enabled: true
  rules:
    - pattern: "'[^']*'"
      replacement: "'***'"
```

确保 SQL 字符串字面量、长数字等敏感信息不会泄露到日志和追踪系统中。

## 8.7 告警规则

项目提供 Prometheus 告警规则（`deploy/prometheus-alerts.yml`），覆盖：
- 高错误率告警
- 高延迟告警
- 熔断器打开告警
- 数据源不可用告警
- 缓存命中率过低告警

## 8.8 最佳实践

1. **采样率**：生产环境建议 0.1（10%），高流量场景可降至 0.01
2. **日志级别**：生产用 `info`，排查问题临时切 `debug`（支持热更新）
3. **审计日志**：生产环境必须启用，用于合规和问题追溯
4. **脱敏**：始终启用，防止敏感数据泄露
5. **自定义标签**：配置 `env`、`cluster` 等标签，便于多维度筛选

---

下一模块：[弹性设计模式](09-resilience.md)
