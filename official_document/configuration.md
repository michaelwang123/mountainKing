# 配置参考

## 概述

配置通过 YAML 文件加载，支持环境变量覆盖（`GRAPHQL_` 前缀，遵循 12-Factor App 规范）。配置文件默认路径为项目根目录的 `config.yaml`。

环境变量映射规则：将配置路径中的 `.` 替换为 `_`，全部大写，加 `GRAPHQL_` 前缀。例如：
- `server.port` → `GRAPHQL_SERVER_PORT`
- `logging.level` → `GRAPHQL_LOGGING_LEVEL`

## 热更新

以下配置项支持运行时热更新（通过 fsnotify 监听文件变更，500ms 防抖），无需重启服务：
- 日志级别 (`logging.level`)
- 限流参数 (`rate_limit.requests_per_window`, `rate_limit.window_size`)
- 缓存 TTL (`cache.default_ttl`, `cache.per_datasource.*.ttl`)

其他配置项（如数据源连接、端口）的变更需要重启服务。

兼容 Kubernetes ConfigMap 符号链接替换机制。

## 配置项详解

### server — 服务基础配置

| 配置项　　　　　　　　　| 类型　　 | 默认值　　　 | 说明　　　　　　　　　　　　　　　　　　　　　　　　|
| -------------------------| ----------| --------------| -----------------------------------------------------|
| `port`　　　　　　　　　| int　　　| `8080`　　　 | HTTP 监听端口　　　　　　　　　　　　　　　　　　　 |
| `mode`　　　　　　　　　| string　 | `production` | 运行模式：`production` 或 `development`　　　　　　 |
| `max_request_body_size` | string　 | `1MB`　　　　| 请求体最大大小，超限返回 413　　　　　　　　　　　　|
| `request_timeout`　　　 | duration | `30s`　　　　| 单个 HTTP 请求的总超时时间　　　　　　　　　　　　　|
| `max_batch_queries`　　 | int　　　| `10`　　　　 | 批量查询最大查询数，超限返回 400　　　　　　　　　　|
| `allow_get_queries`　　 | bool　　 | `false`　　　| 是否允许 HTTP GET 查询（生产模式建议禁用以防 CSRF） |

### graphql — GraphQL 引擎配置

| 配置项　　　　　　　　　| 类型 | 默认值　| 说明　　　　　　　　　　　　　　　　　　　　　　　 |
| -------------------------| ------| ---------| ----------------------------------------------------|
| `introspection_enabled` | bool | `false` | 是否启用 Introspection 查询（生产环境建议禁用）　　|
| `max_query_complexity`　| int　| `100`　 | 查询复杂度上限　　　　　　　　　　　　　　　　　　 |
| `max_query_depth`　　　 | int　| `10`　　| 查询嵌套深度上限　　　　　　　　　　　　　　　　　 |
| `max_result_rows`　　　 | int　| `10000` | 单次查询最大返回行数，超限截断并在 warnings 中提示 |
| `apq_enabled`　　　　　 | bool | `false` | 是否启用 Automatic Persisted Queries　　　　　　　 |

### datasources — 数据源配置

数据源配置为数组，每个条目包含：

| 配置项　　　 | 类型　 | 说明　　　　　　　　　　　　　　　　　　　|
| --------------| --------| -------------------------------------------|
| `name`　　　 | string | 数据源唯一名称　　　　　　　　　　　　　　|
| `type`　　　 | string | 数据源类型（`starrocks` 或 `prometheus`） |
| `enabled`　　| bool　 | 是否启用　　　　　　　　　　　　　　　　　|
| `connection` | map　　| 连接参数（因类型而异）　　　　　　　　　　|
| `options`　　| map　　| 适配器特有选项　　　　　　　　　　　　　　|

#### StarRocks 连接参数

```yaml
connection:
  host: starrocks-fe
  port: 9030
  username: root
  password: "${GRAPHQL_STARROCKS_PASSWORD}"
  database: analytics
options:
  pool_size: 20                    # 连接池大小
  connection_timeout: 5s           # 连接超时
  query_timeout: 30s               # 查询超时
  pool_acquire_timeout: 5s         # 连接池获取超时
  reconnect_interval: 5s           # 初始重连间隔
  max_reconnect_interval: 60s      # 最大重连间隔
  allowed_tables:                  # 表名/字段名白名单（必填）
    orders:
      columns: [order_id, user_id, amount, status, created_at]
```

#### Prometheus 连接参数

```yaml
connection:
  url: http://prometheus:9090
options:
  query_timeout: 15s
  max_data_points: 11000           # 最大数据点数
  reconnect_interval: 5s
  max_reconnect_interval: 60s
```

### auth — 认证配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `method` | string | — | 认证方式：`jwt`、`apikey` 或 `none`（禁用认证） |
| `trusted_proxies` | []string | — | 可信代理 CIDR 列表，用于 X-Forwarded-For IP 提取 |

#### JWT 配置

```yaml
jwt:
  algorithm: RS256          # HS256 | RS256 | ES256
  public_key_file: /path    # RS256/ES256 时使用公钥文件
  secret: "${SECRET}"       # HS256 时使用对称密钥
  issuer: my-auth-service
```

#### API Key 配置

```yaml
apikey:
  keys:
    - id: client-a
      key: "${GRAPHQL_APIKEY_CLIENT_A}"   # bcrypt 哈希值
      expires_at: "2026-06-01T00:00:00Z"  # 可选，过期时间
      permissions:
        datasources: [analytics_db, monitoring]
        operations: [query]
```

### rate_limit — 限流配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `mode` | string | `local` | 限流模式：`local`（单实例）或 `distributed`（Redis 分布式） |
| `requests_per_window` | int | `100` | 每个时间窗口最大请求数 |
| `window_size` | duration | `60s` | 时间窗口大小 |
| `redis.addr` | string | — | 分布式模式 Redis 地址 |
| `redis.password` | string | — | Redis 密码 |

### cache — 缓存配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 是否启用缓存 |
| `backend` | string | `memory` | 缓存后端：`memory` 或 `redis` |
| `default_ttl` | duration | `60s` | 默认缓存 TTL |
| `empty_result_ttl` | duration | `30s` | 空结果缓存 TTL（穿透防护） |
| `ttl_jitter_percent` | int | `10` | TTL 抖动百分比（雪崩防护） |
| `memory.max_entries` | int | `10000` | 内存缓存最大条目数 |
| `memory.max_memory_size` | string | `256MB` | 内存缓存最大内存占用 |
| `per_datasource.{name}.ttl` | duration | — | 每个数据源独立 TTL |

### cors — CORS 配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 CORS |
| `allowed_origins` | []string | — | 允许的 Origin 列表 |
| `allowed_methods` | []string | — | 允许的 HTTP 方法 |
| `allowed_headers` | []string | — | 允许的请求头 |

### compression — 响应压缩

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 gzip 压缩 |
| `min_size` | string | `1KB` | 最小压缩阈值 |

### logging — 日志配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `level` | string | `info` | 日志级别：`debug`/`info`/`warn`/`error` |
| `format` | string | `json` | 日志格式 |
| `audit.enabled` | bool | `false` | 是否启用审计日志 |
| `audit.output` | string | `stdout` | 审计日志输出：`stdout` 或 `file` |
| `audit.file_path` | string | — | 审计日志文件路径 |

### sanitization — 敏感信息脱敏

| 配置项　　| 类型　　 | 默认值　| 说明　　　　　　　　　　　　　　　　　|
| -----------| ----------| ---------| ---------------------------------------|
| `enabled` | bool　　 | `false` | 是否启用脱敏　　　　　　　　　　　　　|
| `rules`　 | []object | —　　　 | 脱敏规则列表（pattern + replacement） |

### metrics — Prometheus 指标

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `custom_labels` | map | — | 附加到所有指标的自定义标签（如 env, cluster） |

### tracing — OpenTelemetry 链路追踪

| 配置项　　　　　| 类型　 | 默认值　| 说明　　　　　　　　　　　 |
| -----------------| --------| ---------| ----------------------------|
| `enabled`　　　 | bool　 | `false` | 是否启用链路追踪　　　　　 |
| `sampling_rate` | float　| `1.0`　 | 采样率（0.0 ~ 1.0）　　　　|
| `otlp.endpoint` | string | —　　　 | OTLP 导出端点　　　　　　　|
| `otlp.protocol` | string | —　　　 | 传输协议：`grpc` 或 `http` |

### retry — 错误重试

| 配置项　　　　　 | 类型　　 | 默认值　　　　| 说明　　　　 |
| ------------------| ----------| ---------------| --------------|
| `max_retries`　　| int　　　| `3`　　　　　 | 最大重试次数 |
| `retry_interval` | duration | `100ms`　　　 | 初始重试间隔 |
| `backoff`　　　　| string　 | `exponential` | 退避策略　　 |

### circuit_breaker — 熔断器

| 配置项　　　　　　　　　 | 类型　　 | 默认值 | 说明　　　　　　　　　　　　 |
| --------------------------| ----------| --------| ------------------------------|
| `failure_threshold`　　　| int　　　| `5`　　| 连续失败次数阈值，超过后熔断 |
| `open_duration`　　　　　| duration | `30s`　| 熔断持续时间　　　　　　　　 |
| `half_open_max_requests` | int　　　| `1`　　| 半开状态最大探测请求数　　　 |
| `success_threshold`　　　| int　　　| `2`　　| 半开状态恢复所需连续成功次数 |

### auth_failure — 暴力破解防护

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 |
| `threshold` | int | `10` | 失败次数阈值 |
| `window` | duration | `5m` | 统计窗口 |
| `ban_duration` | duration | `15m` | 封禁时长 |

### shutdown — 优雅关闭

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `max_wait_time` | duration | `30s` | 等待 in-flight 请求完成的最大时间 |

## 完整配置示例

完整配置示例请参阅项目根目录的 `config.yaml` 文件。
