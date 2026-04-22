# 常见问题 (FAQ)

## 通用

### 本地开发如何启动服务？

默认配置为生产模式（JWT 认证），本地没有公钥文件会启动失败。需要设置 `GRAPHQL_AUTH_METHOD=none` 禁用认证，并切换到开发模式。详见 [开发模式指南](development-mode.md)。

### 本服务支持数据写入吗？

不支持。本服务定位为只读查询网关（Query-only）。Schema 中的 Mutation 仅限服务管理操作（如 `clearCache`），不支持对数据源执行写入。

### 支持 GraphQL Subscription 吗？

不支持。所有数据获取均通过客户端主动查询（Pull 模式）完成。

### 可以同时查询多个数据源吗？

可以。在一个 GraphQL 查询中包含来自不同数据源的字段，服务会并行执行各数据源查询。如果某个数据源失败，其他数据源的结果仍会正常返回。

### 服务支持哪些编程语言的客户端？

任何能发送 HTTP POST 请求的编程语言都可以调用本服务。请求体为标准的 JSON 格式 GraphQL 查询。

## 数据源

### SQL 模板查询和普通 StarRocks 查询有什么区别？

普通 `starrocks` 查询仅支持单表 SELECT + WHERE + ORDER BY + LIMIT，通过白名单校验保证安全。SQL 模板查询支持任意复杂度的 SQL（多表 JOIN、CTE、窗口函数等），通过预定义的 Go `text/template` 模板文件和多层安全函数保证安全。两者共享同一个 StarRocks 连接池。

### 如何创建新的 SQL 模板？

1. 在 `sql_templates.base_dir` 目录下创建 `.sql.tmpl` 文件
2. 在 `config.yaml` 的 `sql_templates.templates` 中添加模板条目（名称、文件路径、参数 Schema）
3. 重启服务或通过 `reloadTemplates` Mutation / fsnotify 自动加载

注意：新增模板条目（config.yaml 变更）需要重启服务，但模板文件内容变更可通过热加载生效。

### SQL 模板支持哪些自定义函数？

12 个函数：`safeString`、`quote`、`safeInt`、`safeFloat`、`safeIdentifier`、`safeInList`、`safeLike`（安全函数）和 `join`、`default`、`upper`、`lower`、`trimSpace`（工具函数）。同时支持 Go `text/template` 内置函数（`eq`、`ne`、`if`、`range` 等）。

### 如何添加新的数据源类型？

实现 `DataSource` 接口 → 注册到 `AdapterRegistry` → 创建 `.graphql` Schema 文件 → 重新执行 `go generate` → 在配置文件中添加数据源条目。详见 [数据源适配器 — 扩展新数据源](datasource-adapters.md#扩展新数据源)。

### 数据源连接断开后会自动恢复吗？

会。DataSource Manager 会按指数退避策略自动重连（初始 5s，最大 60s）。同时熔断器会在数据源持续失败时快速失败，避免连接池耗尽。

### StarRocks 的 allowed_tables 白名单是必须的吗？

是的。这是安全设计要求，防止通过 GraphQL 查询访问未授权的表。

### Prometheus 的 NaN 和 Inf 值如何处理？

自动转换为 GraphQL 的 `null`，并在响应的 `extensions.warnings` 中记录转换信息。

## 认证与安全

### JWT 和 API Key 可以同时启用吗？

不可以。通过 `auth.method` 配置选择其中一种。

### 生产环境推荐哪种 JWT 签名算法？

推荐 RS256 或 ES256（非对称签名）。API 服务仅需配置公钥，私钥留在认证服务端，降低密钥泄露风险。

### API Key 如何安全存储？

配置文件中的 `key` 字段存储 bcrypt 哈希值（非明文），通过环境变量注入。使用 `htpasswd -nbBC 10 "" "raw-api-key" | cut -d: -f2` 生成哈希。

### 哪些端点不需要认证？

`/health`、`/ready`、`/metrics` 和 `/playground`（仅开发模式）为公共端点，豁免认证和限流。

## 缓存

### 缓存支持哪些后端？

内存缓存（LRU，默认）和 Redis 缓存。多实例部署推荐 Redis 以实现缓存共享。

### 如何手动清除缓存？

通过 GraphQL Mutation：

```graphql
mutation { clearCache }                          # 清除全部
mutation { clearCache(datasource: "analytics_db") }  # 清除指定数据源
```

需要认证主体具有 `mutation` 操作权限。

### SQL 模板查询的缓存如何工作？

模板查询结果复用现有 Cache Layer。每个模板可独立配置 `cache_ttl` 和 `cache_enabled`。缓存 Key 基于模板名称 + 参数 + 字段选择 + 分页参数生成（不使用渲染后的 SQL，避免空白差异导致缓存未命中）。`totalCount` 使用独立缓存 Key。模板热加载时仅清除 hash 变化的模板缓存。

### 缓存如何防止穿透/雪崩/击穿？

- 穿透防护：空结果缓存短 TTL（默认 30s）
- 雪崩防护：TTL 添加 ±10% 随机抖动
- 击穿防护：Singleflight 确保同一 key 并发回源只执行一次

## 限流

### 本地限流和分布式限流有什么区别？

- 本地模式：每个实例独立限流，N 个实例 × 100 req/min = 全局最多 N×100 req/min
- 分布式模式：所有实例共享 Redis 令牌桶，全局精确限流 100 req/min

### Redis 不可用时限流会怎样？

自动降级为本地限流模式，后台持续探测 Redis 恢复并自动切回。

### 批量查询如何计算限流？

按批量中的实际查询数计数。一个包含 5 个查询的批量请求消耗 5 个令牌。

## 部署

### 服务需要处理 TLS 吗？

不需要。TLS 终止由前置负载均衡器（Nginx Ingress、ALB、Envoy）负责，服务仅监听 HTTP。

### Docker 镜像以什么用户运行？

非 root 用户（UID 65534），基于 distroless 基础镜像。

### HPA 基于什么指标扩缩容？

基于 `graphql_requests_in_flight` 自定义 Prometheus 指标，需要部署 Prometheus Adapter。

## 可观测性

### 如何将 Trace 和日志关联？

`trace_id` 自动注入结构化日志字段，同时在 GraphQL 响应的 `extensions.traceId` 中返回。可通过 trace_id 在 Jaeger/Tempo 中查找完整链路。

### 采样率设为 0 会有性能开销吗？

`tracing.enabled: false` 时使用 NoopTracerProvider，零开销。`enabled: true` 但 `sampling_rate: 0` 时仍有极小的 context 传播开销。

### 日志级别可以运行时修改吗？

可以。修改 `config.yaml` 中的 `logging.level` 后自动热更新，无需重启服务。
