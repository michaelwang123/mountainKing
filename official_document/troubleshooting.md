# 故障排查指南

## 启动问题

### 服务启动失败：配置校验错误

```
FATAL: invalid config: datasource "analytics_db": starrocks adapter requires allowed_tables in options
```

原因：StarRocks 数据源必须配置 `allowed_tables` 白名单。

解决：在 `config.yaml` 的 StarRocks 数据源 `options` 中添加 `allowed_tables` 配置。

### 服务启动失败：端口被占用

```
FATAL: HTTP server error: listen tcp :8080: bind: address already in use
```

解决：修改 `server.port` 配置或通过环境变量覆盖：

```bash
export GRAPHQL_SERVER_PORT=9090
```

### 数据源连接失败但服务正常启动

```
WARN: datasource "analytics_db" connect failed, marked as unavailable
```

这是预期行为。数据源连接失败时服务继续启动，后台会按指数退避策略自动重连。可通过 `/ready` 端点查看各数据源状态。

## 认证问题

### 401 Unauthorized

| 错误码 | 原因 | 解决方案 |
|--------|------|---------|
| `AUTH_MISSING` | 请求未携带认证凭据 | 添加 `Authorization: Bearer <token>` 或 `X-API-Key` 头 |
| `AUTH_TOKEN_EXPIRED` | JWT Token 已过期 | 重新获取 Token |
| `AUTH_TOKEN_INVALID` | JWT 签名验证失败 | 检查签名算法和密钥配置是否匹配 |

### 403 Forbidden

| 错误码 | 原因 | 解决方案 |
|--------|------|---------|
| `AUTH_INSUFFICIENT_PERMISSION` | 认证主体无权访问目标数据源或操作 | 检查 API Key 的 `permissions.datasources` 和 `permissions.operations` 配置 |

### 429 Too Many Requests（认证失败封禁）

```json
{"error":{"code":"AUTH_BRUTE_FORCE_BLOCKED","message":"too many authentication failures"}}
```

原因：同一 IP 在 `auth_failure.window` 内认证失败超过 `auth_failure.threshold` 次，被封禁 `auth_failure.ban_duration`。

解决：等待封禁时间过期，或检查认证凭据是否正确。

## 查询问题

### VALIDATION_COMPLEXITY_EXCEEDED

```json
{"extensions":{"code":"VALIDATION_COMPLEXITY_EXCEEDED"}}
```

查询复杂度超过 `graphql.max_query_complexity` 限制。简化查询或调整配置。

### VALIDATION_DEPTH_EXCEEDED

查询嵌套深度超过 `graphql.max_query_depth` 限制。减少嵌套层级或调整配置。

### DATASOURCE_TIMEOUT

数据源查询超时。可能原因：
- 查询本身耗时过长 → 优化查询条件，缩小数据范围
- 数据源负载过高 → 检查数据源状态
- 超时配置过短 → 调整 `query_timeout` 配置

### DATASOURCE_UNAVAILABLE

数据源不可用（熔断器处于 OPEN 状态）。检查：
- 数据源是否正常运行
- 网络连接是否正常
- `/ready` 端点查看数据源状态

熔断器会在 `circuit_breaker.open_duration` 后自动进入 HALF_OPEN 状态尝试恢复。

### 结果被截断

```json
{"extensions":{"warnings":["Result truncated: returned 10000 of 50000 rows"]}}
```

返回行数超过 `graphql.max_result_rows` 限制。添加过滤条件缩小结果集，或使用分页查询。

## 限流问题

### 429 Too Many Requests（限流）

```json
{"extensions":{"code":"RATELIMIT_EXCEEDED"}}
```

请求频率超过限流阈值。检查响应头：
- `X-RateLimit-Limit` — 限流上限
- `X-RateLimit-Remaining` — 剩余配额
- `X-RateLimit-Reset` — 重置时间

批量查询按实际查询数计数，一个包含 5 个查询的批量请求消耗 5 个令牌。

### 分布式限流降级

```
WARN: redis unavailable, falling back to local rate limiter
```

Redis 不可用时自动降级为本地限流。服务会后台探测 Redis 恢复并自动切回。

## 缓存问题

### 缓存未生效

检查：
1. `cache.enabled` 是否为 `true`
2. 请求是否为 Query 操作（Mutation 不缓存）
3. 请求是否携带 `extensions.cache: false`（客户端绕过缓存）

### 缓存命中率低

可能原因：
- 查询参数变化频繁 → 检查查询规范化是否生效
- TTL 过短 → 调整 `cache.default_ttl` 或 `per_datasource` TTL
- 缓存容量不足 → 增大 `memory.max_entries` 或 `memory.max_memory_size`

通过 `/metrics` 端点查看 `graphql_cache_hits_total` 和 `graphql_cache_misses_total` 指标。

## 链路追踪问题

### Trace 数据未出现在 Jaeger/Tempo

检查：
1. `tracing.enabled` 是否为 `true`
2. `tracing.sampling_rate` 是否大于 0
3. `tracing.otlp.endpoint` 是否正确
4. OTLP 后端是否可达

### Trace ID 未出现在日志中

确保 `tracing.enabled: true`，trace_id 会自动注入结构化日志字段。

## 健康检查问题

### /health 返回 503

至少一个核心组件异常。响应体包含各组件状态详情，检查具体哪个组件异常。

### /ready 返回 503

所有数据源连接均不可用。检查：
- 数据源是否正常运行
- 网络连接是否正常
- 配置中的连接参数是否正确

## 性能问题

### 请求延迟高

排查步骤：
1. 检查 `/metrics` 中的 `graphql_request_duration_seconds` 确认延迟分布
2. 检查 `graphql_datasource_query_duration_seconds` 确认是否为数据源查询慢
3. 检查 `graphql_datasource_connection_pool_waiting` 确认是否连接池不足
4. 启用 tracing 查看 Span 层级定位瓶颈

### 内存占用高

检查：
- 缓存配置：`memory.max_entries` 和 `memory.max_memory_size` 是否过大
- 连接池大小：`pool_size` 是否过大
- 限流器 key 数量：大量不同 IP 可能导致 limiter map 增长

## Docker / Kubernetes 问题

### SQL 模板引擎问题

### 模板查询返回 VALIDATION_TEMPLATE_NOT_FOUND

可能原因：
- `sql_templates.enabled` 为 `false`（功能未启用）
- 模板名称拼写错误
- 模板文件加载失败（语法错误、文件不存在、超过 1MB、非 UTF-8）

排查：检查启动日志中的模板加载摘要，确认目标模板是否成功注册。使用 `templateList` 查询查看所有已注册模板。

### 模板渲染返回 INTERNAL_TEMPLATE_RENDER_ERROR

可能原因：
- 模板中引用了未定义的参数（`missingkey=error` 策略）
- 渲染超时（超过 `render_timeout`，默认 5s）
- 模板语法错误

排查：检查模板文件语法，确认所有 `{{.Params.xxx}}` 引用的参数都在请求中提供或有默认值。

### 模板渲染返回 VALIDATION_UNSAFE_SQL

可能原因：
- 渲染结果包含字符串外的分号（多语句注入检测）
- 渲染结果超过 `max_rendered_sql_length`（默认 64KB）
- 渲染结果包含未闭合的引号

排查：检查模板文件内容，确保不会生成包含分号的 SQL。如果需要更大的渲染结果，调整 `max_rendered_sql_length` 配置。

### 模板热加载不生效

可能原因：
- Mutation 触发的 Reload 有 10s 冷却时间
- 文件系统不支持 fsnotify（某些网络文件系统）
- 模板文件语法错误（错误隔离：保留旧版本）

排查：检查日志中的 reload 结果，确认是否有加载失败的模板。使用 `reloadTemplates` Mutation 手动触发并查看返回的 `failures` 列表。

### 模板查询等待超时（DATASOURCE_TIMEOUT）

可能原因：
- 信号量已满（`max_concurrent_queries` 并发数达到上限）
- StarRocks 查询本身耗时过长

排查：检查 `graphql_template_semaphore_wait_seconds` 指标确认是否为信号量等待。如果是，考虑增大 `max_concurrent_queries` 或优化模板 SQL。

## Docker / Kubernetes 问题

### Pod 启动后被 kubelet 杀死

可能是启动探针超时。服务启动时需要建立数据源连接，可能耗时较长。检查 `startupProbe` 配置：

```yaml
startupProbe:
  failureThreshold: 30   # 最多等待 30 × 2s = 60s
  periodSeconds: 2
```

### HPA 不触发扩容

确保：
1. Prometheus Adapter 已部署并正确配置
2. `graphql_requests_in_flight` 指标可通过 custom metrics API 访问
3. HPA 的 `averageValue` 目标值设置合理
