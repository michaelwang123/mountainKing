# Changelog

本文件记录项目的所有重要变更，格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范。

## [Unreleased]

### Added
- ClickHouse datasource adapter with full read/write support via native TCP protocol
- ClickHouse type mapper supporting all ClickHouse 26.5 types (including BFloat16, Time, JSON, Variant, Dynamic)
- ClickHouse query builder with parameterized SQL and whitelist enforcement
- Property-based tests for ClickHouse adapter (parameterization safety, type mapping completeness, recursion depth safety)
- ClickHouse mutation support via WritableDataSource interface
- 新增 Docker 镜像自动发布到 GitHub Container Registry (GHCR)
  - 多架构支持（linux/amd64 + linux/arm64）
  - 语义化版本标签（v1.2.3, v1.2, v1, latest）
  - 预发布版本仅推送完整标签，不更新 latest
  - 发布后自动健康检查验证镜像可用性
- 新增 `.dockerignore` 优化构建上下文（<5MB）
- 新增 `dev-image.yml` 工作流：main 分支自动构建 dev/sha-* 标签镜像
- 新增 `-health` CLI flag 支持容器内 HEALTHCHECK（适配 distroless 镜像）
- Dockerfile 新增 `HEALTHCHECK` 指令和 `templates/` 目录
- README 新增 Docker 快速开始章节（拉取、运行、环境变量、docker-compose 示例）
- 新增 `AnyValue` GraphQL 标量类型，支持任意 JSON 值（对象、数组、字符串、数字、布尔值、null）
- `AnyValue` 类型支持最大 64 层嵌套深度校验，防止恶意深层嵌套载荷

### Changed
- `ci.yml` Docker 构建步骤改为仅验证编译（不再推送），避免与 dev-image.yml 重复
- `release.yml` 使用 metadata-action 管理标签，添加 QEMU/Buildx 多架构构建和并发控制
- **Breaking Change**: Mutation 值字段现使用 `AnyValue` 标量类型替代 `JSON` 标量类型：
  - `ColumnValueInput.value`: `JSON!` → `AnyValue!`
  - `MutationFilterInput.value`: `JSON` → `AnyValue`
  - `insertBatchStarrocks` 的 `rows` 参数: `[[JSON!]!]!` → `[[AnyValue!]!]!`
- 客户端应直接传递值（如 `value: 42`），而非包裹为对象（如 `value: {"v": 42}`）
- 如果客户端仍发送 `value: {"v": 42}`，该值将被视为 map（`map[string]any{"v": 42}`）而非提取内部值

### Removed
- 移除 `extractScalarFromJSONValue` 解析器变通函数
- 移除 `extractArrayFromJSONValue` 解析器变通函数

### Unchanged
- 查询结果类型（如 `StarRocksRow.Data`）仍使用 `JSON` 标量类型，不受影响

## [0.1.0] - 2026-06-08

### Added
- 基于 gqlgen + chi 的 GraphQL API 服务框架
- SQL 模板查询引擎（TemplateEngine）：
  - 模板加载与注册（Go `text/template`，UTF-8 校验，SHA-256 hash 追踪）
  - 模板渲染（render_timeout 超时控制，max_rendered_sql_length 长度限制）
  - 参数校验（类型匹配、必填检查、枚举/长度/正则约束、默认值填充）
  - 分页包装器（over-fetch 策略，参数化 LIMIT/OFFSET，safeIdentifier 字段校验）
  - 缓存集成（模板级 TTL、totalCount 独立缓存、cache_enabled 禁用、extensions.cache 绕过）
  - 热加载（fsnotify 自动重载 + reloadTemplates Mutation 手动重载，500ms 防抖，hash 比较仅清除变更模板缓存）
  - 12 个安全/工具模板函数（safeString、quote、safeInt、safeFloat、safeIdentifier、safeInList、safeLike、join、default、upper、lower、trimSpace）
  - 7 状态词法扫描器（多语句注入检测、SQL 注释移除、StarRocks Optimizer Hint 保留）
  - 并发控制（信号量限制 max_concurrent_queries，防止连接池饿死）
  - Prometheus 指标（graphql_template_query_duration_seconds、graphql_template_queries_total、graphql_template_render_duration_seconds、graphql_template_semaphore_wait_seconds、graphql_template_cache_hits_total）
  - OpenTelemetry Span（Template Query {name}，含 template.name、db.system、db.statement 属性）
  - 审计日志（TemplateName 字段记录模板名称）
  - GraphQL 集成（templateQuery 查询、templateList 元信息列表、reloadTemplates Mutation）
  - RawExecutor 接口隔离（TemplateEngine 仅通过 ExecuteRaw 访问 StarRocks Adapter）
- StarRocks 数据源适配器（MySQL 协议，参数化查询，白名单校验）
- Prometheus 数据源适配器（HTTP API，即时查询和范围查询）
- 跨数据源并行查询与结果合并，部分失败隔离
- DataLoader 批量合并（per-request 实例，防止 N+1 查询）
- JWT 认证（HS256/RS256/ES256）和 API Key 认证（bcrypt 哈希）
- 按数据源和操作类型的细粒度授权
- 令牌桶限流（本地 + Redis 分布式 + 自动降级）
- 查询结果缓存（内存 LRU / Redis，穿透/雪崩/击穿三重防护）
- 熔断器（CLOSED/OPEN/HALF_OPEN 状态机）
- 指数退避重试（瞬时错误重试，业务错误立即返回）
- 后台指数退避重连
- Prometheus 指标端点（请求/数据源/缓存/错误指标，自定义标签）
- OpenTelemetry 链路追踪（Root/Resolver/DataSource/Redis Span 层级）
- W3C Trace Context 传播
- 结构化 JSON 日志（zap，trace_id 关联）
- 审计日志（独立输出）
- 敏感信息脱敏（正则规则）
- 认证失败暴力破解防护
- CSRF 防护、CORS、gzip 压缩、请求体大小限制
- 查询复杂度/深度限制、结果集截断
- 批量查询支持（按实际查询数限流）
- Relay 游标分页和传统 offset/limit 分页
- YAML 配置 + 环境变量覆盖（12-Factor）
- 配置热更新（日志级别、限流参数、缓存 TTL）
- 优雅关闭（有序资源释放）
- 健康检查（/health）和就绪探针（/ready）
- 多阶段 Dockerfile（distroless，非 root）
- Kubernetes 部署清单（Deployment, Service, ConfigMap, HPA）
- Docker Compose 集成测试环境
- GitHub Actions CI/CD 流水线
- 属性测试（rapid，96 个属性）
- 性能基准测试
