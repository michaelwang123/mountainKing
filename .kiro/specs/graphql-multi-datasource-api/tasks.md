# 实现计划：GraphQL Multi-DataSource API

## 概述

Go + gqlgen GraphQL API 服务的增量实现计划。按 P0 → P1 → P2 优先级迭代，每个任务构建在前一个任务之上。使用 chi 路由、Viper 配置、zap 日志、database/sql (MySQL) 连接 StarRocks、net/http 查询 Prometheus HTTP API。

## Tasks

- [x] 1. 项目脚手架与核心接口定义
  - [x] 1.1 初始化 Go module，创建项目目录结构（cmd/server/, internal/config/, internal/datasource/, internal/graphql/, internal/middleware/, internal/server/, internal/cache/, internal/ratelimit/, internal/health/, internal/observability/, internal/context/, internal/audit/, internal/sanitize/, internal/errors/, pkg/retry/, deploy/），初始化 go.mod 并添加核心依赖（gqlgen, chi, viper, zap, go-sql-driver/mysql, prometheus/client_golang, golang-jwt/jwt/v5, go.opentelemetry.io/otel, hashicorp/golang-lru/v2, cespare/xxhash/v2, golang.org/x/sync/singleflight, golang.org/x/time/rate, go-redis/redis/v9）
    - _Requirements: 17.4_
  - [x] 1.2 配置 golangci-lint（.golangci.yml）：启用 govet、errcheck、staticcheck，确保后续所有代码从一开始就通过 lint 检查
    - _Requirements: 17.3_
  - [x] 1.3 定义 DataSource 核心接口（interface.go）：DataSource, AdapterFactory, QueryRequest, QueryResult, FilterCondition, OrderByClause, PaginationParams，定义 FilterOperator 和 SortDirection 枚举类型
    - _Requirements: 10.1, 10.2_
  - [x] 1.4 实现 Adapter Registry（registry.go）：Register, Get, List 方法，重复注册返回错误
    - _Requirements: 10.3, 10.4, 10.5_
  - [x] 1.5 编写 Adapter Registry 属性测试
    - **Property 37: 适配器注册表操作**
    - **Validates: Requirements 10.3, 10.4, 10.5**
  - [x] 1.6 定义统一错误码（errors/errors.go, errors/types.go）：AUTH_*, VALIDATION_*, DATASOURCE_*, RATELIMIT_*, INTERNAL_* 常量和错误类型
    - _Requirements: 9.8, 9.9_
  - [x] 1.7 定义 Context key（context/keys.go）：CtxKeyRequestID, CtxKeyAuthIdentity, CtxKeyTraceID
    - _Requirements: 9.3_

- [x] 2. 配置管理与校验
  - [x] 2.1 实现配置结构定义（config/config.go）：Config 及所有子结构体（ServerConfig, GraphQLConfig, DataSourceConfig, AuthConfig, JWTConfig, APIKeyConfig, CacheConfig, MemoryCacheConfig, RedisCacheConfig, RateLimitConfig, TracingConfig, OTLPConfig, CORSConfig, CompressionConfig, LoggingConfig, AuditConfig, RetryConfig, CircuitBreakerConfig, AuthFailureConfig, SanitizationConfig, MetricsConfig, ShutdownConfig, RedisConfig, DatasourceCacheConfig），使用 Viper 加载 YAML + 环境变量覆盖（GRAPHQL_ 前缀）
    - _Requirements: 3.1, 17.7, 17.8_
  - [x] 2.2 实现配置校验（config/validation.go）：ValidateConfig 函数，校验数据源连接参数、认证配置互斥（JWT algorithm/secret/public_key_file）、StarRocks 白名单必填、数据源名称唯一性、trusted_proxies CIDR 格式、max_memory_size 格式等
    - _Requirements: 3.10_
  - [x] 2.3 编写配置校验属性测试
    - **Property 11: 配置校验拒绝无效值** — Validates: Requirements 3.10
    - **Property 72: 环境变量覆盖配置** — Validates: Requirements 17.8
    - **Property 91: StarRocks 白名单必填校验** — Validates: Design - StarRocks 白名单安全默认
    - **Property 94: JWT 配置互斥校验** — Validates: Design - 配置校验规则
    - **Property 95: 数据源名称唯一性** — Validates: Design - 配置校验规则
  - [x] 2.4 实现配置热更新（config/hotreload.go）：使用 fsnotify 监听配置文件变更，500ms debounce 防抖，支持热更新日志级别、限流参数、缓存 TTL，K8s ConfigMap 符号链接替换兼容
    - _Requirements: 17.9_
  - [x] 2.5 编写配置热更新属性测试
    - **Property 73: 配置热更新** — Validates: Requirements 17.9
    - **Property 90: 配置热更新 Debounce** — Validates: Design - ConfigMap 兼容性

- [x] 3. 结构化日志初始化
  - [x] 3.1 实现结构化日志初始化（observability/logging.go）：zap JSON 格式，AtomicLevel 支持热更新，请求日志分级策略（DEBUG/INFO/WARN/ERROR），慢查询告警阈值
    - _Requirements: 9.2, 9.5_
  - [x] 3.2 编写日志属性测试
    - **Property 32: 结构化日志格式** — Validates: Requirements 9.2
    - **Property 35: 日志级别配置** — Validates: Requirements 9.5

- [ ] 4. Checkpoint - 确保脚手架、配置、日志基础设施就绪
  - 验证：golangci-lint 通过 + 所有单元测试通过 + 配置加载/校验/热更新正常 + 日志输出为合法 JSON

- [x] 5. 通用重试与错误分类
  - [x] 5.1 实现错误分类器（pkg/retry/classifier.go）：区分瞬时错误（连接超时、ECONNREFUSED、ECONNRESET、io.EOF）和业务错误（SQL 语法错误、PromQL 语法错误）
    - _Requirements: 9.7_
  - [x] 5.2 实现通用重试逻辑（pkg/retry/retry.go）：指数退避策略，支持 max_retries、retry_interval 配置，瞬时错误重试、业务错误立即返回
    - _Requirements: 9.6_
  - [x] 5.3 编写重试策略属性测试
    - **Property 36: 重试策略区分瞬时与业务错误** — Validates: Requirements 9.6, 9.7

- [x] 6. DataSource Manager 与连接管理
  - [x] 6.1 实现 DataSource Manager（datasource/manager.go）：Init（从配置初始化数据源，失败标记不可用）、Get、ExecuteWithRetry、HealthCheckAll、CloseAll，通过 Adapter_Registry 按类型名称查找并实例化适配器，未注册类型跳过并记录错误日志，支持 enabled=false 跳过初始化
    - _Requirements: 3.1, 3.2, 3.3, 3.5, 3.7, 3.8, 3.9, 10.9, 10.10, 10.11_
  - [x] 6.2 实现后台重连（datasource/reconnect.go）：指数退避重连策略，初始间隔 5s，最大间隔 60s
    - _Requirements: 3.4_
  - [x] 6.3 实现熔断器（datasource/circuit_breaker.go）：CLOSED/OPEN/HALF_OPEN 状态转换，线程安全的状态检查与更新（同一把锁内完成）
    - _Design: 熔断器弹性设计_
  - [x] 6.4 实现 MockDataSource（datasource/mock.go）：符合 DataSource Interface 的测试辅助实现
    - _Requirements: 10.13_
  - [x] 6.5 编写 DataSource Manager 属性测试
    - **Property 12: 指数退避重连间隔** — Validates: Requirements 3.4
    - **Property 13: 连接池耗尽超时** — Validates: Requirements 3.6
    - **Property 14: 适配器发现与实例化** — Validates: Requirements 3.8, 3.9
    - **Property 38: 数据源启用/禁用** — Validates: Requirements 10.11
  - [x] 6.6 编写熔断器属性测试
    - **Property 74: 熔断器状态转换** — Validates: Design - 熔断器弹性设计
    - **Property 75: 熔断器 OPEN 状态快速失败** — Validates: Design - 熔断器弹性设计

- [ ] 7. Checkpoint - 确保核心基础设施就绪
  - 验证：golangci-lint 通过 + DataSource 接口/Registry/Manager/MockDataSource 可用 + 重试/熔断器逻辑正确

- [x] 8. StarRocks 适配器
  - [x] 8.1 实现 SQL 查询构建器（adapter/starrocks/query_builder.go）：Build 和 BuildCount 方法，参数化查询（? 占位符），反引号包裹标识符
    - _Requirements: 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 7.2_
  - [x] 8.2 实现标识符校验与白名单（adapter/starrocks/whitelist.go）：ValidateIdentifier（[a-zA-Z0-9_]），表名/字段名白名单校验，白名单从 DataSourceConfig.Options["allowed_tables"] 加载
    - _Requirements: 4.7_
  - [x] 8.3 实现类型映射器（adapter/starrocks/type_mapper.go）：SQL 类型到 GraphQL 类型映射，不支持类型兜底为 String 并记录警告日志
    - _Requirements: 4.8, 4.9_
  - [x] 8.4 实现 StarRocks 适配器（adapter/starrocks/adapter.go）：通过 MySQL 协议连接，实现 DataSource 接口全部方法（Connect/IsAvailable/Execute/HealthCheck/SchemaFiles/Close），注册到 Adapter Registry
    - _Requirements: 4.1_
  - [x] 8.5 编写 StarRocks 查询构建器属性测试
    - **Property 15: StarRocks SQL 查询构建** — Validates: Requirements 4.2, 4.3, 4.4, 4.5, 7.2
    - **Property 16: StarRocks 参数化查询防注入** — Validates: Requirements 4.7
    - **Property 17: StarRocks 标识符白名单校验** — Validates: Requirements 4.7
  - [x] 8.6 编写 StarRocks 类型映射属性测试
    - **Property 18: StarRocks 类型映射** — Validates: Requirements 4.8, 4.9

- [x] 9. Prometheus 适配器
  - [x] 9.1 实现 PromQL 查询构建器（adapter/prometheus/query_builder.go）：BuildInstant 和 BuildRange 方法，标签匹配器转换
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 7.3_
  - [x] 9.2 实现输入校验（adapter/prometheus/validator.go）：ValidateLabelValue（拒绝 PromQL 特殊字符 }{|~"），ValidateQueryExpression（子查询嵌套深度、高开销操作检查）
    - _Requirements: 5.7_
  - [x] 9.3 实现类型映射器（adapter/prometheus/type_mapper.go）：Prometheus 数据类型到 GraphQL 类型映射，NaN/±Inf 转换为 null + extensions.warnings
    - _Requirements: 5.8, 5.9_
  - [x] 9.4 实现 Prometheus 适配器（adapter/prometheus/adapter.go）：通过 HTTP API 连接（Connect 使用 GET /api/v1/status/buildinfo 验证），实现 DataSource 接口全部方法，注册到 Adapter Registry。使用自定义 InstrumentedTransport 包装 http.Transport 跟踪连接池指标（activeConns atomic 计数器）
    - _Requirements: 5.1, 11.7, 11.8, 11.9_
  - [x] 9.5 编写 Prometheus 查询构建器属性测试
    - **Property 19: Prometheus PromQL 查询构建** — Validates: Requirements 5.2, 5.4, 5.5, 7.3
    - **Property 20: PromQL 注入防护** — Validates: Requirements 5.7
  - [x] 9.6 编写 Prometheus 类型映射属性测试
    - **Property 21: Prometheus 类型映射** — Validates: Requirements 5.8
    - **Property 22: Prometheus 特殊值转换** — Validates: Requirements 5.9
    - **Property 23: Prometheus 数据点超限保护** — Validates: Requirements 5.6

- [ ] 10. Checkpoint - 确保两个数据源适配器就绪
  - 验证：golangci-lint 通过 + StarRocks/Prometheus 适配器通过 MockDataSource 单元测试 + SQL/PromQL 构建器属性测试通过


- [ ] 11. GraphQL Schema 与引擎
  - [x] 11.1 定义 GraphQL Schema 文件：base.graphql（自定义标量 DateTime/JSON、分页类型 PageInfo、枚举 SortDirection/FilterOperator/LabelMatchType）、starrocks.graphql（StarRocksRow/Connection/Filter/OrderBy）、prometheus.graphql（PrometheusVector/Matrix/InstantResult/RangeResult/LabelFilter）、mutation.graphql（clearCache，描述注释说明仅支持管理类操作，需要 mutation 权限），不定义 Subscription 根类型
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.9, 2.10, 2.11, 2.12_
  - [x] 11.2 配置 gqlgen.yml 并执行代码生成（go generate）：Schema 文件路径、自定义标量映射（DateTime/JSON → internal/graphql/scalar）、resolver 结构配置
    - _Requirements: 2.7_
  - [x] 11.3 实现自定义标量序列化/反序列化（graphql/scalar/datetime.go, json.go）
    - _Requirements: 4.8_
  - [x] 11.4 实现 Query Resolver（graphql/resolver/query.go）：starrocks、prometheusInstant、prometheusRange 查询入口，字段选择优化，并行调度多数据源查询（sync.WaitGroup + 独立结果收集，非 errgroup.WithContext），结果集截断检查（max_result_rows + extensions.warnings）
    - _Requirements: 6.1, 6.2, 6.3, 7.1, 8.9_
  - [x] 11.5 实现 Mutation Resolver（graphql/resolver/mutation.go）：clearCache 操作（需要 mutation 操作权限），仅清除结果缓存（cache: 前缀），不影响 APQ 缓存
    - _Requirements: 2.9, 16.9_
  - [x] 11.6 实现 DataLoader（graphql/dataloader/dataloader.go）：per-request 实例（禁止跨请求共享），批量合并同数据源请求（窗口 1ms，最大批量 100），通过中间件注入 context，context 取消时立即 flush 当前批次
    - _Requirements: 6.4_
  - [x] 11.7 编写跨数据源查询属性测试
    - **Property 24: 跨数据源并行查询与结果合并** — Validates: Requirements 6.1, 6.2
    - **Property 25: 混合查询部分失败处理** — Validates: Requirements 6.3
    - **Property 92: 跨数据源查询不因单个失败取消其他查询** — Validates: Design - 跨数据源错误隔离
    - **Property 30: 结果集截断** — Validates: Requirements 8.9
  - [x] 11.8 编写 DataLoader 和 Schema 属性测试
    - **Property 82: DataLoader Per-Request 隔离** — Validates: Design - DataLoader 生命周期
    - **Property 9: Introspection 启用/禁用** — Validates: Requirements 2.6, 2.8
    - **Property 10: 不支持的操作类型被拒绝** — Validates: Requirements 2.11, 2.12

- [x] 12. HTTP 服务器与中间件层
  - [x] 12.1 实现 HTTP 服务器（server/server.go）：chi 路由注册（/graphql POST+GET、/playground、/health、/ready、/metrics），请求级超时 context（context.WithTimeout(request_timeout)），数据源查询级超时（min(query_timeout, 剩余时间)），查询复杂度限制和深度限制（gqlgen 内置），优雅关闭（SIGTERM/SIGINT → 停止接受新连接 → 等待 in-flight 请求(max_wait_time) → TracingProvider.Shutdown(独立 5s 超时) → 刷新 Metrics → DataSourceManager.CloseAll → Logger.Sync）
    - _Requirements: 1.1, 8.5, 8.6, 8.7, 8.8, 15.5, 15.6, 15.7, 15.8_
  - [x] 12.2 实现批量查询解析与调度（server/batch.go）：JSON 数组检测，max_batch_queries 校验，并行执行，结果数组返回
    - _Requirements: 1.9, 1.10_
  - [x] 12.3 实现 RequestID 中间件（middleware/requestid.go）：生成唯一请求 ID，注入 context 和响应头 X-Request-ID
    - _Requirements: 9.3_
  - [x] 12.4 实现 BodyLimit 中间件（middleware/bodylimit.go）：请求体大小限制，超限返回 413
    - _Requirements: 1.8_
  - [x] 12.5 实现 CORS 中间件（middleware/cors.go）：根据配置启用/禁用，处理 Origin/Methods/Headers
    - _Requirements: 15.9, 15.10_
  - [x] 12.6 实现 Compression 中间件（middleware/compression.go）：gzip 压缩，Accept-Encoding 检查，最小压缩阈值
    - _Requirements: 15.11, 15.12_
  - [x] 12.7 实现 CSRF 防护中间件（middleware/csrf.go）：生产模式禁用 GET 查询（allow_get_queries 默认 false），Content-Type application/json 检查
    - _Design: CSRF 防护_
  - [x] 12.8 编写 HTTP 端点行为属性测试
    - **Property 1: 有效 GraphQL 查询返回规范响应** — Validates: Requirements 1.2
    - **Property 2: 无效请求体返回 400** — Validates: Requirements 1.3
    - **Property 3: 超大请求体返回 413** — Validates: Requirements 1.8
    - **Property 4: HTTP GET 查询支持** — Validates: Requirements 1.6
    - **Property 5: Playground 开发/生产模式切换** — Validates: Requirements 1.7
    - **Property 6: 批量查询结果数组长度一致** — Validates: Requirements 1.9
    - **Property 7: 超限批量查询返回 400** — Validates: Requirements 1.10
    - **Property 83: CSRF 防护 - GET 查询生产模式禁用** — Validates: Design - CSRF 防护
  - [x] 12.9 编写错误响应与中间件属性测试
    - **Property 31: 错误响应结构** — Validates: Requirements 9.1, 9.8, 9.9
    - **Property 33: 请求 ID 唯一性与传播** — Validates: Requirements 9.3
    - **Property 34: 语法错误位置信息** — Validates: Requirements 9.4
    - **Property 80: HTTP 层错误结构化响应** — Validates: Design - 统一错误响应格式
    - **Property 62: CORS 配置** — Validates: Requirements 15.9, 15.10
    - **Property 63: gzip 压缩条件** — Validates: Requirements 15.11, 15.12
  - [x] 12.10 编写超时与优雅关闭属性测试
    - **Property 26: 单数据源查询超时取消** — Validates: Requirements 8.5
    - **Property 27: 总请求超时取消** — Validates: Requirements 8.6
    - **Property 28: 查询复杂度限制** — Validates: Requirements 8.7
    - **Property 29: 查询深度限制** — Validates: Requirements 8.8
    - **Property 88: 请求超时与查询超时组合** — Validates: Design - 超时组合机制
    - **Property 60: 优雅关闭 - 停止接受新请求** — Validates: Requirements 15.5, 15.6
    - **Property 61: 优雅关闭 - 资源清理顺序** — Validates: Requirements 15.7, 15.8

- [ ] 13. Checkpoint - 确保 HTTP 服务器和 GraphQL 引擎端到端可用
  - 验证：golangci-lint 通过 + GraphQL 查询端到端可用（使用 MockDataSource）+ 中间件链正确 + 超时/优雅关闭行为正确

- [x] 14. 认证与授权
  - [x] 14.1 实现 JWT 认证器（middleware/auth.go - JWTAuthenticator）：从 Authorization: Bearer 头提取 Token，验证签名（HS256/RS256/ES256，支持对称和非对称密钥）、过期时间（exp）、签发者（iss），返回 AuthIdentity
    - _Requirements: 13.1, 13.7, 13.8_
  - [x] 14.2 实现 API Key 认证器（middleware/auth.go - APIKeyAuthenticator）：从 X-API-Key 头提取，bcrypt 哈希比对（constant-time），过期检查，权限范围返回
    - _Requirements: 13.1, 13.9, 13.10, 13.11_
  - [x] 14.3 实现授权检查（middleware/authz.go）：Authorizer 接口，检查 AuthIdentity 对目标数据源和操作类型（query/mutation）的权限
    - _Requirements: 13.4_
  - [x] 14.4 实现 Auth 中间件集成：在 GraphQL 引擎之前执行认证，公共端点（/health, /ready, /metrics, /playground）豁免，401/403 错误返回
    - _Requirements: 13.2, 13.3, 13.5, 13.6_
  - [x] 14.5 实现认证失败暴力破解防护（middleware/auth_failure_limiter.go）：IP 维度失败计数，超阈值封禁，可信代理 IP 提取（trusted_proxies + X-Forwarded-For，取最右侧非信任 IP）
    - _Design: 安全加固_
  - [x] 14.6 编写认证属性测试
    - **Property 48: 缺失认证凭据返回 401** — Validates: Requirements 13.3
    - **Property 49: 权限不足返回 403** — Validates: Requirements 13.4
    - **Property 50: 公共端点豁免认证和限流** — Validates: Requirements 13.6, 14.6
    - **Property 51: JWT 过期 Token 返回 401 + token_expired** — Validates: Requirements 13.8
    - **Property 81: JWT 非对称签名验证** — Validates: Design - JWT 非对称签名支持
  - [x] 14.7 编写授权与安全防护属性测试
    - **Property 52: API Key 权限隔离** — Validates: Requirements 13.10
    - **Property 53: API Key 过期失效** — Validates: Requirements 13.11
    - **Property 76: 认证失败暴力破解防护** — Validates: Design - 安全加固
    - **Property 84: clearCache Mutation 授权** — Validates: Design - Mutation 授权控制
    - **Property 87: 可信代理 IP 提取** — Validates: Design - 代理环境 IP 提取

- [x] 15. 请求限流
  - [x] 15.1 初始化共享 Redis 客户端：统一的 go-redis/redis/v9 客户端实例，供分布式限流（15.3）、Redis 缓存（17.4）和 Redis tracing hook（21.5）共用，通过配置文件指定 Redis 地址和密码
    - _Design: Redis 客户端共享_
  - [x] 15.2 实现本地限流器（ratelimit/local.go）：KeyedRateLimiter，使用 golang.org/x/time/rate，按 key 维度独立令牌桶，maxEntries=100000 防护，后台清理超过 2×window_size 未访问的 limiter
    - _Requirements: 14.1, 14.2_
  - [x] 15.3 实现分布式限流器（ratelimit/distributed.go）：Redis + Lua 脚本原子令牌桶操作
    - _Requirements: 14.7, 14.8_
  - [x] 15.4 实现降级包装器（ratelimit/fallback.go）：FallbackRateLimiter，Redis 不可用时降级为本地模式，后台恢复探测（probeInterval=30s）
    - _Requirements: 14.9_
  - [x] 15.5 实现限流中间件（middleware/ratelimit.go）：限流 Key 优先级（API Key ID > JWT sub > IP），批量查询按实际查询数计数，公共端点豁免，响应头 X-RateLimit-Limit/Remaining/Reset（所有非公共端点请求均包含）
    - _Requirements: 1.11, 14.1, 14.3, 14.4, 14.5, 14.6_
  - [x] 15.6 编写限流核心属性测试
    - **Property 56: 令牌桶限流** — Validates: Requirements 14.1, 14.2, 14.3, 14.4
    - **Property 57: 限流响应头始终存在** — Validates: Requirements 14.4
    - **Property 8: 批量查询按实际查询数限流** — Validates: Requirements 1.11
    - **Property 93: 限流 Key 优先级** — Validates: Design - 限流 Key 选择策略
  - [x] 15.7 编写限流降级与防护属性测试
    - **Property 58: 分布式限流 Redis 降级** — Validates: Requirements 14.9
    - **Property 78: 分布式限流降级恢复** — Validates: Design - 降级恢复机制
    - **Property 96: KeyedRateLimiter 最大 Key 数量限制** — Validates: Design - DDoS 内存防护

- [ ] 16. Checkpoint - 确保认证、授权、限流完整可用
  - 验证：golangci-lint 通过 + JWT/API Key 认证正确 + 授权检查正确 + 限流（本地+分布式+降级）行为正确 + 暴力破解防护生效

- [x] 17. 缓存层
  - [x] 17.1 实现 Cache 接口与缓存 Key 生成（cache/cache.go, cache/key.go）：Cache 接口（Get/Set/Delete/DeleteByPrefix/Clear），CacheKeyGenerator（xxhash64 + 数据源前缀 cache:{datasource}:{hash}）
    - _Requirements: 16.3_
  - [x] 17.2 实现查询规范化（cache/normalize.go）：去除多余空格/换行/注释，统一关键字大小写
    - _Design: 缓存命中率优化_
  - [x] 17.3 实现内存缓存后端（cache/memory.go）：基于 hashicorp/golang-lru/v2，max_entries + max_memory_size 双重限制，LRU 淘汰，gob 序列化
    - _Requirements: 16.2, 16.8_
  - [x] 17.4 实现 Redis 缓存后端（cache/redis.go）：gob 序列化，SCAN + DEL 按前缀删除（异步执行，禁止 KEYS 命令），复用 task 15.1 的共享 Redis 客户端
    - _Requirements: 16.2_
  - [x] 17.5 实现 Cache Layer（cache/layer.go）：GetOrLoad（singleflight 击穿防护），TTL + jitter 雪崩防护，空结果短 TTL 穿透防护，gob 反序列化失败恢复（删除损坏条目 + 回源 + WARN 日志），仅缓存 Query 操作，extensions.cache=false 绕过，totalCount 与数据结果同一缓存条目
    - _Requirements: 16.1, 16.4, 16.5, 16.7, 16.10, 16.11, 16.12_
  - [x] 17.6 实现缓存清除（ClearByDatasource 和 ClearAll 方法，供 Mutation Resolver 调用）
    - _Requirements: 16.9_
  - [x] 17.7 编写缓存核心属性测试
    - **Property 64: 缓存 Key 确定性** — Validates: Requirements 16.3
    - **Property 65: 客户端绕过缓存** — Validates: Requirements 16.5
    - **Property 66: 仅缓存 Query 操作** — Validates: Requirements 16.7
    - **Property 67: LRU 缓存淘汰** — Validates: Requirements 16.8
    - **Property 68: 缓存清除操作** — Validates: Requirements 16.9
    - **Property 77: 缓存 Key 查询规范化** — Validates: Design - 缓存命中率优化
  - [x] 17.8 编写缓存防护与容错属性测试
    - **Property 69: 缓存穿透防护** — Validates: Requirements 16.10
    - **Property 70: 缓存雪崩防护 - TTL 抖动** — Validates: Requirements 16.11
    - **Property 71: 缓存击穿防护 - Singleflight** — Validates: Requirements 16.12
    - **Property 79: totalCount 与数据结果缓存一致性** — Validates: Design - 缓存一致性
    - **Property 85: 内存缓存内存大小限制** — Validates: Design - 内存缓存容量控制
    - **Property 86: 缓存 Gob 反序列化失败恢复** — Validates: Design - 缓存容错

- [x] 18. 健康检查
  - [x] 18.1 实现 Health Checker（health/health.go）：LivenessCheck（/health，所有核心组件正常→200，异常→503），ReadinessCheck（/ready，至少一个数据源可用→200，全部不可用→503），响应包含版本信息和构建时间
    - _Requirements: 15.1, 15.2, 15.3, 15.4_
  - [x] 18.2 编写健康检查属性测试
    - **Property 59: 健康检查状态码** — Validates: Requirements 15.3, 15.4

- [ ] 19. Checkpoint - 确保缓存和健康检查就绪
  - 验证：golangci-lint 通过 + 缓存（内存+Redis）读写正确 + 缓存防护（穿透/雪崩/击穿）生效 + 健康检查端点正确

- [x] 20. 可观测性 - Prometheus 指标
  - [x] 20.1 实现 MetricsCollector（observability/metrics.go）：注册所有 Prometheus 指标（graphql_request_duration_seconds, graphql_requests_total, graphql_requests_in_flight, graphql_datasource_query_duration_seconds, graphql_datasource_connection_pool_active/idle/waiting, graphql_errors_total, graphql_cache_hits_total, graphql_cache_misses_total），自定义 Histogram 桶边界（requestDuration: 10ms-10s，dsQueryDuration: 5ms-5s），自定义标签附加，暴露 /metrics 端点
    - _Requirements: 8.10, 11.1-11.12, 16.6_
  - [x] 20.2 编写指标属性测试
    - **Property 39: Prometheus 指标注册完整性** — Validates: Requirements 11.3-11.10
    - **Property 40: 指标命名规范** — Validates: Requirements 11.11
    - **Property 41: 自定义标签附加** — Validates: Requirements 11.12

- [x] 21. 可观测性 - OpenTelemetry 链路追踪
  - [x] 21.1 实现 TracingProvider（observability/tracing.go）：初始化 TracerProvider（OTLP gRPC/HTTP exporter），采样率配置（默认 1.0），NoopTracerProvider（disabled），Shutdown 独立 5s 超时
    - _Requirements: 12.1, 12.2, 12.10, 12.11, 12.12, 12.13_
  - [x] 21.2 集成请求级 Root Span：名称格式 `GraphQL {operation_type} {operation_name}`，属性 graphql.operation.name/type, http.method, http.url
    - _Requirements: 12.3, 12.4_
  - [x] 21.3 集成 Resolver 级子 Span：名称格式 `Resolver {field_name}`，属性 graphql.field.name/type, graphql.datasource
    - _Requirements: 12.5_
  - [x] 21.4 集成数据源查询级子 Span：StarRocks Query（db.system=starrocks, db.statement 经脱敏处理），Prometheus Query（db.system=prometheus, db.statement）
    - _Requirements: 12.6, 12.7_
  - [x] 21.5 实现 Redis 操作 tracing hook：使用 go-redis AddHook 机制，在 ProcessHook 中创建 Span（名称 `Redis {command}`，属性 db.system=redis, db.operation, net.peer.name），复用 task 15.1 的共享 Redis 客户端
    - _Design: Redis 可观测性_
  - [x] 21.6 实现 W3C Trace Context 传播：入站 traceparent 头提取，出站请求注入 traceparent
    - _Requirements: 12.8, 12.9_
  - [x] 21.7 实现错误 Span 状态记录和 Trace ID 关联：错误时设置 Span Error 状态 + Event，trace_id 注入日志和 extensions.traceId
    - _Requirements: 12.14, 12.15, 12.16, 12.17_
  - [x] 21.8 编写链路追踪属性测试
    - **Property 42: Root Span 创建与属性** — Validates: Requirements 12.3, 12.4
    - **Property 43: Resolver Span 创建与属性** — Validates: Requirements 12.5
    - **Property 44: 数据源查询 Span 创建与属性** — Validates: Requirements 12.6, 12.7
    - **Property 45: W3C Trace Context 传播** — Validates: Requirements 12.8, 12.9
    - **Property 46: 错误 Span 状态** — Validates: Requirements 12.14, 12.15
    - **Property 47: Trace ID 关联** — Validates: Requirements 12.16, 12.17
    - **Property 89: Redis 操作 Span 创建** — Validates: Design - Redis 可观测性

- [x] 22. 审计与脱敏
  - [x] 22.1 实现审计日志（audit/audit.go）：记录认证主体标识、操作时间、操作类型、目标数据源、请求结果，独立于应用日志输出
    - _Requirements: 13.12_
  - [x] 22.2 实现敏感信息脱敏（sanitize/sanitize.go）：正则替换 SQL 字符串字面量和数值参数，应用于日志和 Trace Span 的 db.statement
    - _Requirements: 13.13_
  - [x] 22.3 编写审计与脱敏属性测试
    - **Property 54: 审计日志完整性** — Validates: Requirements 13.12
    - **Property 55: 敏感信息脱敏** — Validates: Requirements 13.13

- [ ] 23. Checkpoint - 确保可观测性完整
  - 验证：golangci-lint 通过 + /metrics 端点返回所有指标 + Tracing Span 层级正确（Root→Resolver→DataSource+Redis）+ 审计日志包含必要字段 + 脱敏规则生效

- [ ] 24. 主入口与端到端集成
  - [-] 24.1 实现 main.go（cmd/server/main.go）：加载配置 → 初始化日志 → 初始化 TracingProvider → 初始化共享 Redis 客户端 → 注册适配器 → 初始化 DataSourceManager → 初始化 CacheLayer → 初始化 RateLimiter → 初始化 MetricsCollector → 初始化 HealthChecker → 构建中间件链（RequestID → BodyLimit → CORS → CSRF → Auth → AuthFailureLimiter → RateLimit → Compression）→ 启动 HTTP 服务器 → 优雅关闭
    - _Requirements: 1.1, 1.4, 1.5_
  - [ ] 24.2 创建示例配置文件（config.yaml）：包含所有配置项的完整示例，与 requirements.md 附录中的 YAML 配置一致
    - _Requirements: 3.1_

- [ ] 25. Checkpoint - 确保端到端集成可用
  - 验证：golangci-lint 通过 + 服务可启动 + GraphQL 查询端到端可用（使用 MockDataSource）+ 所有中间件按正确顺序执行 + 覆盖率 ≥ 70%

- [ ] 26. 容器化与部署
  - [ ] 26.1 创建多阶段 Dockerfile：构建阶段（Go 编译）+ 最终阶段（scratch/distroless），非 root 用户（UID≠0），构建参数注入版本号和构建时间（--build-arg）
    - _Requirements: 18.1, 18.2, 18.3_
  - [ ] 26.2 创建 Kubernetes 部署清单：Deployment（含 startupProbe(failureThreshold:30,periodSeconds:2) + livenessProbe(/health) + readinessProbe(/ready)、资源 requests/limits 通过 ConfigMap 管理）、Service、ConfigMap、HPA（基于 graphql_requests_in_flight 自定义指标）
    - _Requirements: 18.4, 18.5, 18.6_
  - [ ] 26.3 创建 Docker Compose 集成测试环境（deploy/docker-compose.yaml）：StarRocks FE/BE、Prometheus、Redis
    - _Requirements: 17.11_
  - [ ] 26.4 创建 CI/CD 流水线配置示例（GitHub Actions）：lint（golangci-lint）、单元测试、覆盖率报告、镜像构建和推送
    - _Requirements: 17.3, 18.7_

- [ ] 27. 性能基准测试
  - [ ] 27.1 编写性能基准测试（使用 Go testing.B）：单数据源简单查询延迟、跨数据源混合查询延迟、并发查询吞吐量、缓存命中/未命中场景对比，验证需求 8 中定义的延迟目标（P95 单数据源 ≤200ms，P95 混合查询 ≤500ms）
    - _Requirements: 17.12_

- [ ] 28. 代码质量收尾
  - [ ] 28.1 添加 GoDoc 注释：所有导出函数、类型和接口添加符合 GoDoc 规范的注释，关键业务逻辑代码块添加行内注释
    - _Requirements: 17.1, 17.2_

- [ ] 29. Final checkpoint - 确保所有测试通过，代码质量达标
  - 验证：golangci-lint 通过 + 所有单元测试通过 + 属性测试通过（每个 ≥100 次迭代）+ 覆盖率 ≥ 70%（核心组件 ≥ 80%）+ GoDoc 注释完整 + Docker 镜像可构建 + K8s 清单语法正确

## 说明

- 标记 `*` 的任务为可选属性测试，可跳过以加速 MVP 交付
- 每个任务引用具体需求编号，确保可追溯性
- Checkpoint 任务明确验证标准，确保增量质量
- 属性测试使用 `rapid` 库，每个属性测试最少运行 100 次迭代
- 属性测试标签格式：`Feature: graphql-multi-datasource-api, Property {number}: {property_text}`
- 共享 Redis 客户端在 task 15.1 初始化，供限流（15.3）、缓存（17.4）和 tracing hook（21.5）复用
