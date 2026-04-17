# Implementation Plan: GraphQL Multi-DataSource API

## Overview

Go + gqlgen GraphQL API 服务的增量实现计划。按 P0 → P1 → P2 优先级迭代，每个任务构建在前一个任务之上。使用 chi 路由、Viper 配置、zap 日志、database/sql (MySQL) 连接 StarRocks、net/http 查询 Prometheus HTTP API。

## Tasks

- [ ] 1. 项目脚手架与核心接口定义
  - [ ] 1.1 初始化 Go module，创建项目目录结构（cmd/server/, internal/config/, internal/datasource/, internal/graphql/, internal/middleware/, internal/server/, internal/cache/, internal/ratelimit/, internal/health/, internal/observability/, internal/context/, internal/audit/, internal/sanitize/, internal/errors/, pkg/retry/, deploy/）
    - 初始化 go.mod，添加核心依赖（gqlgen, chi, viper, zap, go-sql-driver/mysql, prometheus/client_golang, golang-jwt/jwt/v5, go.opentelemetry.io/otel, hashicorp/golang-lru/v2, cespare/xxhash/v2, golang.org/x/sync/singleflight, golang.org/x/time/rate）
    - _Requirements: 17.4_

  - [ ] 1.2 定义 DataSource 核心接口（interface.go）：DataSource, AdapterFactory, QueryRequest, QueryResult, FilterCondition, OrderByClause, PaginationParams
    - 定义 FilterOperator 和 SortDirection 枚举类型
    - _Requirements: 10.1, 10.2_

  - [ ] 1.3 实现 Adapter Registry（registry.go）：Register, Get, List 方法，重复注册返回错误
    - _Requirements: 10.3, 10.4, 10.5_

  - [ ]* 1.4 Write property test for Adapter Registry
    - **Property 37: 适配器注册表操作**
    - **Validates: Requirements 10.3, 10.4, 10.5**

  - [ ] 1.5 定义统一错误码（errors/errors.go, errors/types.go）：AUTH_*, VALIDATION_*, DATASOURCE_*, RATELIMIT_*, INTERNAL_* 常量和错误类型
    - _Requirements: 9.8, 9.9_

  - [ ] 1.6 定义 Context key（context/keys.go）：CtxKeyRequestID, CtxKeyAuthIdentity, CtxKeyTraceID
    - _Requirements: 9.3_


- [ ] 2. 配置管理与校验
  - [ ] 2.1 实现配置结构定义（config/config.go）：Config 及所有子结构体（ServerConfig, GraphQLConfig, DataSourceConfig, AuthConfig, CacheConfig, RateLimitConfig, TracingConfig 等），使用 Viper 加载 YAML + 环境变量覆盖（GRAPHQL_ 前缀）
    - _Requirements: 3.1, 17.7, 17.8_

  - [ ] 2.2 实现配置校验（config/validation.go）：ValidateConfig 函数，校验数据源连接参数、认证配置互斥、StarRocks 白名单必填、数据源名称唯一性、CIDR 格式等
    - _Requirements: 3.10_

  - [ ]* 2.3 Write property tests for configuration validation
    - **Property 11: 配置校验拒绝无效值**
    - **Validates: Requirements 3.10**
    - **Property 72: 环境变量覆盖配置**
    - **Validates: Requirements 17.8**
    - **Property 91: StarRocks 白名单必填校验**
    - **Validates: Design - StarRocks 白名单安全默认**
    - **Property 94: JWT 配置互斥校验**
    - **Validates: Design - 配置校验规则**
    - **Property 95: 数据源名称唯一性**
    - **Validates: Design - 配置校验规则**

  - [ ] 2.3a 实现配置热更新（config/hotreload.go）：使用 fsnotify 监听配置文件变更，500ms debounce，支持热更新日志级别、限流参数、缓存 TTL
    - _Requirements: 17.9_

  - [ ]* 2.4 Write property test for hot reload debounce
    - **Property 73: 配置热更新**
    - **Validates: Requirements 17.9**
    - **Property 90: 配置热更新 Debounce**
    - **Validates: Design - ConfigMap 兼容性**

- [ ] 3. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.


- [ ] 4. 通用重试与错误分类
  - [ ] 4.1 实现错误分类器（pkg/retry/classifier.go）：区分瞬时错误（连接超时、ECONNREFUSED、ECONNRESET、io.EOF）和业务错误（SQL 语法错误、PromQL 语法错误）
    - _Requirements: 9.7_

  - [ ] 4.2 实现通用重试逻辑（pkg/retry/retry.go）：指数退避策略，支持 max_retries、retry_interval 配置，瞬时错误重试、业务错误立即返回
    - _Requirements: 9.6_

  - [ ]* 4.3 Write property test for retry strategy
    - **Property 36: 重试策略区分瞬时与业务错误**
    - **Validates: Requirements 9.6, 9.7**

- [ ] 5. DataSource Manager 与连接管理
  - [ ] 5.1 实现 DataSource Manager（datasource/manager.go）：Init（从配置初始化数据源，失败标记不可用）、Get、ExecuteWithRetry、HealthCheckAll、CloseAll
    - 通过 Adapter_Registry 按类型名称查找并实例化适配器
    - 未注册类型跳过并记录错误日志
    - 支持 enabled=false 跳过初始化
    - _Requirements: 3.1, 3.2, 3.3, 3.7, 3.8, 3.9, 10.9, 10.10, 10.11_

  - [ ] 5.2 实现后台重连（datasource/reconnect.go）：指数退避重连策略，初始间隔 5s，最大间隔 60s
    - _Requirements: 3.4_

  - [ ]* 5.3 Write property test for reconnect backoff
    - **Property 12: 指数退避重连间隔**
    - **Validates: Requirements 3.4**
    - **Property 14: 适配器发现与实例化**
    - **Validates: Requirements 3.8, 3.9**
    - **Property 38: 数据源启用/禁用**
    - **Validates: Requirements 10.11**

  - [ ] 5.4 实现熔断器（datasource/circuit_breaker.go）：CLOSED/OPEN/HALF_OPEN 状态转换，线程安全的状态检查与更新
    - _Requirements: Design - 熔断器弹性设计_

  - [ ]* 5.5 Write property tests for circuit breaker
    - **Property 74: 熔断器状态转换**
    - **Validates: Design - 熔断器弹性设计**
    - **Property 75: 熔断器 OPEN 状态快速失败**
    - **Validates: Design - 熔断器弹性设计**

  - [ ] 5.6 实现 MockDataSource（datasource/mock.go）：符合 DataSource Interface 的测试辅助实现
    - _Requirements: 10.13_

  - [ ]* 5.7 Write property test for connection pool exhaustion
    - **Property 13: 连接池耗尽超时**
    - **Validates: Requirements 3.6**


- [ ] 6. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 7. StarRocks 适配器
  - [ ] 7.1 实现 SQL 查询构建器（adapter/starrocks/query_builder.go）：Build 和 BuildCount 方法，参数化查询（? 占位符），反引号包裹标识符，白名单校验
    - _Requirements: 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 7.2_

  - [ ] 7.2 实现标识符校验与白名单（adapter/starrocks/whitelist.go）：ValidateIdentifier（[a-zA-Z0-9_]），表名/字段名白名单校验
    - _Requirements: 4.7_

  - [ ]* 7.3 Write property tests for StarRocks query builder
    - **Property 15: StarRocks SQL 查询构建**
    - **Validates: Requirements 4.2, 4.3, 4.4, 4.5, 7.2**
    - **Property 16: StarRocks 参数化查询防注入**
    - **Validates: Requirements 4.7**
    - **Property 17: StarRocks 标识符白名单校验**
    - **Validates: Requirements 4.7**

  - [ ] 7.4 实现类型映射器（adapter/starrocks/type_mapper.go）：SQL 类型到 GraphQL 类型映射，不支持类型兜底为 String
    - _Requirements: 4.8, 4.9_

  - [ ]* 7.5 Write property test for StarRocks type mapping
    - **Property 18: StarRocks 类型映射**
    - **Validates: Requirements 4.8, 4.9**

  - [ ] 7.6 实现 StarRocks 适配器（adapter/starrocks/adapter.go）：通过 MySQL 协议连接，实现 DataSource 接口全部方法，注册到 Adapter Registry
    - _Requirements: 4.1_

- [ ] 8. Prometheus 适配器
  - [ ] 8.1 实现 PromQL 查询构建器（adapter/prometheus/query_builder.go）：BuildInstant 和 BuildRange 方法，标签匹配器转换
    - _Requirements: 5.2, 5.3, 5.4, 5.5, 7.3_

  - [ ] 8.2 实现输入校验（adapter/prometheus/validator.go）：ValidateLabelValue（拒绝 PromQL 特殊字符 }{|~"），ValidateQueryExpression
    - _Requirements: 5.7_

  - [ ]* 8.3 Write property tests for Prometheus query builder
    - **Property 19: Prometheus PromQL 查询构建**
    - **Validates: Requirements 5.2, 5.4, 5.5, 7.3**
    - **Property 20: PromQL 注入防护**
    - **Validates: Requirements 5.7**

  - [ ] 8.4 实现类型映射器（adapter/prometheus/type_mapper.go）：Prometheus 数据类型到 GraphQL 类型映射，NaN/±Inf 转换为 null + warnings
    - _Requirements: 5.8, 5.9_

  - [ ]* 8.5 Write property tests for Prometheus type mapping
    - **Property 21: Prometheus 类型映射**
    - **Validates: Requirements 5.8**
    - **Property 22: Prometheus 特殊值转换**
    - **Validates: Requirements 5.9**
    - **Property 23: Prometheus 数据点超限保护**
    - **Validates: Requirements 5.6**

  - [ ] 8.6 实现 Prometheus 适配器（adapter/prometheus/adapter.go）：通过 HTTP API 连接，实现 DataSource 接口全部方法，注册到 Adapter Registry
    - _Requirements: 5.1_


- [ ] 9. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 10. GraphQL Schema 与引擎
  - [ ] 10.1 定义 GraphQL Schema 文件：base.graphql（自定义标量 DateTime/JSON、分页类型 PageInfo、枚举 SortDirection/FilterOperator/LabelMatchType）、starrocks.graphql（StarRocksRow/Connection/Filter/OrderBy）、prometheus.graphql（PrometheusVector/Matrix/InstantResult/RangeResult/LabelFilter）、mutation.graphql（clearCache）
    - Schema 中 Mutation 根类型描述注释说明仅支持管理类操作
    - 不定义 Subscription 根类型
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.9, 2.10, 2.11, 2.12_

  - [ ] 10.2 配置 gqlgen.yml 并执行代码生成（go generate）：自定义标量映射、resolver 结构配置
    - _Requirements: 2.7_

  - [ ] 10.3 实现自定义标量序列化/反序列化（graphql/scalar/datetime.go, json.go）
    - _Requirements: 4.8_

  - [ ] 10.4 实现 Query Resolver（graphql/resolver/query.go）：starrocks、prometheusInstant、prometheusRange 查询入口，字段选择优化，并行调度多数据源查询（sync.WaitGroup + 独立结果收集），结果集截断检查
    - _Requirements: 6.1, 6.2, 6.3, 7.1, 8.9_

  - [ ]* 10.5 Write property tests for cross-datasource query behavior
    - **Property 24: 跨数据源并行查询与结果合并**
    - **Validates: Requirements 6.1, 6.2**
    - **Property 25: 混合查询部分失败处理**
    - **Validates: Requirements 6.3**
    - **Property 92: 跨数据源查询不因单个失败取消其他查询**
    - **Validates: Design - 跨数据源错误隔离**
    - **Property 30: 结果集截断**
    - **Validates: Requirements 8.9**

  - [ ] 10.6 实现 Mutation Resolver（graphql/resolver/mutation.go）：clearCache 操作
    - _Requirements: 2.9, 16.9_

  - [ ] 10.7 实现 DataLoader（graphql/dataloader/dataloader.go）：per-request 实例，批量合并同数据源请求，通过中间件注入 context
    - _Requirements: 6.4_

  - [ ]* 10.8 Write property test for DataLoader isolation
    - **Property 82: DataLoader Per-Request 隔离**
    - **Validates: Design - DataLoader 生命周期**

  - [ ]* 10.9 Write property tests for Schema validation
    - **Property 9: Introspection 启用/禁用**
    - **Validates: Requirements 2.6, 2.8**
    - **Property 10: 不支持的操作类型被拒绝**
    - **Validates: Requirements 2.11, 2.12**


- [ ] 11. HTTP 服务器与中间件层
  - [ ] 11.1 实现 HTTP 服务器（server/server.go）：chi 路由注册（/graphql POST+GET、/playground、/health、/ready、/metrics），请求超时 context（request_timeout），优雅关闭（SIGTERM/SIGINT → 停止接受新连接 → 等待 in-flight → 关闭资源）
    - _Requirements: 1.1, 8.6, 15.5, 15.6, 15.7_

  - [ ] 11.2 实现批量查询解析与调度（server/batch.go）：JSON 数组检测，max_batch_queries 校验，并行执行，结果数组返回
    - _Requirements: 1.9, 1.10_

  - [ ] 11.3 实现 RequestID 中间件（middleware/requestid.go）：生成唯一请求 ID，注入 context 和响应头 X-Request-ID
    - _Requirements: 9.3_

  - [ ] 11.4 实现 BodyLimit 中间件（middleware/bodylimit.go）：请求体大小限制，超限返回 413
    - _Requirements: 1.8_

  - [ ] 11.5 实现 CORS 中间件（middleware/cors.go）：根据配置启用/禁用，处理 Origin/Methods/Headers
    - _Requirements: 15.9, 15.10_

  - [ ] 11.6 实现 Compression 中间件（middleware/compression.go）：gzip 压缩，Accept-Encoding 检查，最小压缩阈值
    - _Requirements: 15.11, 15.12_

  - [ ] 11.7 实现 CSRF 防护中间件（middleware/csrf.go）：生产模式禁用 GET 查询（默认），Content-Type 检查
    - _Requirements: Design - CSRF 防护_

  - [ ]* 11.8 Write property tests for HTTP endpoint behavior
    - **Property 1: 有效 GraphQL 查询返回规范响应**
    - **Validates: Requirements 1.2**
    - **Property 2: 无效请求体返回 400**
    - **Validates: Requirements 1.3**
    - **Property 3: 超大请求体返回 413**
    - **Validates: Requirements 1.8**
    - **Property 4: HTTP GET 查询支持**
    - **Validates: Requirements 1.6**
    - **Property 5: Playground 开发/生产模式切换**
    - **Validates: Requirements 1.7**
    - **Property 6: 批量查询结果数组长度一致**
    - **Validates: Requirements 1.9**
    - **Property 7: 超限批量查询返回 400**
    - **Validates: Requirements 1.10**
    - **Property 83: CSRF 防护 - GET 查询生产模式禁用**
    - **Validates: Design - CSRF 防护**

  - [ ]* 11.9 Write property tests for error response structure
    - **Property 31: 错误响应结构**
    - **Validates: Requirements 9.1, 9.8, 9.9**
    - **Property 33: 请求 ID 唯一性与传播**
    - **Validates: Requirements 9.3**
    - **Property 34: 语法错误位置信息**
    - **Validates: Requirements 9.4**
    - **Property 80: HTTP 层错误结构化响应**
    - **Validates: Design - 统一错误响应格式**

  - [ ]* 11.10 Write property tests for CORS and compression
    - **Property 62: CORS 配置**
    - **Validates: Requirements 15.9, 15.10**
    - **Property 63: gzip 压缩条件**
    - **Validates: Requirements 15.11, 15.12**

- [ ] 12. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.


- [ ] 13. 认证与授权
  - [ ] 13.1 实现 JWT 认证器（middleware/auth.go - JWTAuthenticator）：从 Authorization: Bearer 头提取 Token，验证签名（HS256/RS256/ES256）、过期时间、签发者，返回 AuthIdentity
    - _Requirements: 13.1, 13.7, 13.8_

  - [ ] 13.2 实现 API Key 认证器（middleware/auth.go - APIKeyAuthenticator）：从 X-API-Key 头提取，bcrypt 哈希比对（constant-time），过期检查，权限范围返回
    - _Requirements: 13.1, 13.9, 13.10, 13.11_

  - [ ] 13.3 实现授权检查（middleware/authz.go）：Authorizer 接口，检查 AuthIdentity 对目标数据源和操作类型的权限
    - _Requirements: 13.4_

  - [ ] 13.4 实现 Auth 中间件集成：在 GraphQL 引擎之前执行认证，公共端点豁免，401/403 错误返回
    - _Requirements: 13.2, 13.3, 13.5, 13.6_

  - [ ] 13.5 实现认证失败暴力破解防护（middleware/auth_failure_limiter.go）：IP 维度失败计数，超阈值封禁，可信代理 IP 提取（trusted_proxies + X-Forwarded-For）
    - _Requirements: Design - 安全加固_

  - [ ]* 13.6 Write property tests for authentication and authorization
    - **Property 48: 缺失认证凭据返回 401**
    - **Validates: Requirements 13.3**
    - **Property 49: 权限不足返回 403**
    - **Validates: Requirements 13.4**
    - **Property 50: 公共端点豁免认证和限流**
    - **Validates: Requirements 13.6, 14.6**
    - **Property 51: JWT 过期 Token 返回 401 + token_expired**
    - **Validates: Requirements 13.8**
    - **Property 52: API Key 权限隔离**
    - **Validates: Requirements 13.10**
    - **Property 53: API Key 过期失效**
    - **Validates: Requirements 13.11**
    - **Property 76: 认证失败暴力破解防护**
    - **Validates: Design - 安全加固**
    - **Property 81: JWT 非对称签名验证**
    - **Validates: Design - JWT 非对称签名支持**
    - **Property 84: clearCache Mutation 授权**
    - **Validates: Design - Mutation 授权控制**
    - **Property 87: 可信代理 IP 提取**
    - **Validates: Design - 代理环境 IP 提取**


- [ ] 14. 请求限流
  - [ ] 14.1 实现本地限流器（ratelimit/local.go）：KeyedRateLimiter，使用 golang.org/x/time/rate，按 key 维度独立令牌桶，maxEntries 防护，后台清理过期 limiter
    - _Requirements: 14.1, 14.2_

  - [ ] 14.2 实现分布式限流器（ratelimit/distributed.go）：Redis + Lua 脚本原子令牌桶操作
    - _Requirements: 14.7, 14.8_

  - [ ] 14.3 实现降级包装器（ratelimit/fallback.go）：FallbackRateLimiter，Redis 不可用时降级为本地模式，后台恢复探测
    - _Requirements: 14.9_

  - [ ] 14.4 实现限流中间件（middleware/ratelimit.go）：限流 Key 优先级（API Key ID > JWT sub > IP），批量查询按实际查询数计数，公共端点豁免，响应头 X-RateLimit-*
    - _Requirements: 1.11, 14.1, 14.3, 14.4, 14.5, 14.6_

  - [ ]* 14.5 Write property tests for rate limiting
    - **Property 8: 批量查询按实际查询数限流**
    - **Validates: Requirements 1.11**
    - **Property 56: 令牌桶限流**
    - **Validates: Requirements 14.1, 14.2, 14.3, 14.4**
    - **Property 57: 限流响应头始终存在**
    - **Validates: Requirements 14.4**
    - **Property 58: 分布式限流 Redis 降级**
    - **Validates: Requirements 14.9**
    - **Property 78: 分布式限流降级恢复**
    - **Validates: Design - 降级恢复机制**
    - **Property 93: 限流 Key 优先级**
    - **Validates: Design - 限流 Key 选择策略**
    - **Property 96: KeyedRateLimiter 最大 Key 数量限制**
    - **Validates: Design - DDoS 内存防护**

- [ ] 15. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.


- [ ] 16. 缓存层
  - [ ] 16.1 实现 Cache 接口与缓存 Key 生成（cache/cache.go, cache/key.go）：Cache 接口（Get/Set/Delete/DeleteByPrefix/Clear），CacheKeyGenerator（xxhash64 + 数据源前缀）
    - _Requirements: 16.3_

  - [ ] 16.2 实现查询规范化（cache/normalize.go）：去除多余空格/换行/注释，统一关键字大小写
    - _Requirements: Design - 缓存命中率优化_

  - [ ] 16.3 实现内存缓存后端（cache/memory.go）：基于 hashicorp/golang-lru/v2，max_entries + max_memory_size 双重限制，LRU 淘汰
    - _Requirements: 16.2, 16.8_

  - [ ] 16.4 实现 Redis 缓存后端（cache/redis.go）：gob 序列化，SCAN + DEL 按前缀删除
    - _Requirements: 16.2_

  - [ ] 16.5 实现 Cache Layer（cache/layer.go）：GetOrLoad（singleflight 击穿防护），TTL + jitter 雪崩防护，空结果短 TTL 穿透防护，gob 反序列化失败恢复，仅缓存 Query 操作，extensions.cache=false 绕过
    - _Requirements: 16.1, 16.4, 16.5, 16.7, 16.10, 16.11, 16.12_

  - [ ] 16.6 实现缓存清除（通过 ClearByDatasource 和 ClearAll 方法，供 Mutation Resolver 调用）
    - _Requirements: 16.9_

  - [ ]* 16.7 Write property tests for cache behavior
    - **Property 64: 缓存 Key 确定性**
    - **Validates: Requirements 16.3**
    - **Property 65: 客户端绕过缓存**
    - **Validates: Requirements 16.5**
    - **Property 66: 仅缓存 Query 操作**
    - **Validates: Requirements 16.7**
    - **Property 67: LRU 缓存淘汰**
    - **Validates: Requirements 16.8**
    - **Property 68: 缓存清除操作**
    - **Validates: Requirements 16.9**
    - **Property 69: 缓存穿透防护**
    - **Validates: Requirements 16.10**
    - **Property 70: 缓存雪崩防护 - TTL 抖动**
    - **Validates: Requirements 16.11**
    - **Property 71: 缓存击穿防护 - Singleflight**
    - **Validates: Requirements 16.12**
    - **Property 77: 缓存 Key 查询规范化**
    - **Validates: Design - 缓存命中率优化**
    - **Property 79: totalCount 与数据结果缓存一致性**
    - **Validates: Design - 缓存一致性**
    - **Property 85: 内存缓存内存大小限制**
    - **Validates: Design - 内存缓存容量控制**
    - **Property 86: 缓存 Gob 反序列化失败恢复**
    - **Validates: Design - 缓存容错**


- [ ] 17. 健康检查与运维端点
  - [ ] 17.1 实现 Health Checker（health/health.go）：LivenessCheck（/health，所有核心组件正常→200，异常→503），ReadinessCheck（/ready，至少一个数据源可用→200，全部不可用→503），响应包含版本信息
    - _Requirements: 15.1, 15.2, 15.3, 15.4_

  - [ ]* 17.2 Write property test for health check
    - **Property 59: 健康检查状态码**
    - **Validates: Requirements 15.3, 15.4**

- [ ] 18. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 19. 可观测性 - Prometheus 指标
  - [ ] 19.1 实现 MetricsCollector（observability/metrics.go）：注册所有 Prometheus 指标（graphql_request_duration_seconds, graphql_requests_total, graphql_requests_in_flight, graphql_datasource_query_duration_seconds, graphql_datasource_connection_pool_active/idle/waiting, graphql_errors_total, graphql_cache_hits_total, graphql_cache_misses_total），自定义 Histogram 桶边界，自定义标签附加
    - 暴露 /metrics 端点
    - _Requirements: 8.10, 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8, 11.9, 11.10, 11.11, 11.12, 16.6_

  - [ ]* 19.2 Write property tests for metrics
    - **Property 39: Prometheus 指标注册完整性**
    - **Validates: Requirements 11.3-11.10**
    - **Property 40: 指标命名规范**
    - **Validates: Requirements 11.11**
    - **Property 41: 自定义标签附加**
    - **Validates: Requirements 11.12**

- [ ] 20. 可观测性 - OpenTelemetry 链路追踪
  - [ ] 20.1 实现 TracingProvider（observability/tracing.go）：初始化 TracerProvider（OTLP gRPC/HTTP exporter），采样率配置，NoopTracerProvider（disabled），Shutdown 独立 5s 超时
    - _Requirements: 12.1, 12.2, 12.10, 12.11, 12.12, 12.13_

  - [ ] 20.2 集成请求级 Root Span：名称格式 `GraphQL {operation_type} {operation_name}`，属性 graphql.operation.name/type, http.method, http.url
    - _Requirements: 12.3, 12.4_

  - [ ] 20.3 集成 Resolver 级子 Span：名称格式 `Resolver {field_name}`，属性 graphql.field.name/type, graphql.datasource
    - _Requirements: 12.5_

  - [ ] 20.4 集成数据源查询级子 Span：StarRocks Query（db.system=starrocks, db.statement），Prometheus Query（db.system=prometheus, db.statement）
    - _Requirements: 12.6, 12.7_

  - [ ] 20.5 实现 W3C Trace Context 传播：入站 traceparent 头提取，出站请求注入 traceparent
    - _Requirements: 12.8, 12.9_

  - [ ] 20.6 实现错误 Span 状态记录和 Trace ID 关联：错误时设置 Span Error 状态 + Event，trace_id 注入日志和 extensions.traceId
    - _Requirements: 12.14, 12.15, 12.16, 12.17_

  - [ ]* 20.7 Write property tests for tracing
    - **Property 42: Root Span 创建与属性**
    - **Validates: Requirements 12.3, 12.4**
    - **Property 43: Resolver Span 创建与属性**
    - **Validates: Requirements 12.5**
    - **Property 44: 数据源查询 Span 创建与属性**
    - **Validates: Requirements 12.6, 12.7**
    - **Property 45: W3C Trace Context 传播**
    - **Validates: Requirements 12.8, 12.9**
    - **Property 46: 错误 Span 状态**
    - **Validates: Requirements 12.14, 12.15**
    - **Property 47: Trace ID 关联**
    - **Validates: Requirements 12.16, 12.17**
    - **Property 89: Redis 操作 Span 创建**
    - **Validates: Design - Redis 可观测性**


- [ ] 21. 日志、审计与脱敏
  - [ ] 21.1 实现结构化日志初始化（observability/logging.go）：zap JSON 格式，AtomicLevel 支持热更新，请求日志分级策略（DEBUG/INFO/WARN/ERROR）
    - _Requirements: 9.2, 9.5_

  - [ ] 21.2 实现审计日志（audit/audit.go）：记录认证主体标识、操作时间、操作类型、目标数据源、请求结果，独立于应用日志输出
    - _Requirements: 13.12_

  - [ ] 21.3 实现敏感信息脱敏（sanitize/sanitize.go）：正则替换 SQL 字符串字面量和数值参数，应用于日志和 Trace Span 的 db.statement
    - _Requirements: 13.13_

  - [ ]* 21.4 Write property tests for logging and sanitization
    - **Property 32: 结构化日志格式**
    - **Validates: Requirements 9.2**
    - **Property 35: 日志级别配置**
    - **Validates: Requirements 9.5**
    - **Property 54: 审计日志完整性**
    - **Validates: Requirements 13.12**
    - **Property 55: 敏感信息脱敏**
    - **Validates: Requirements 13.13**

- [ ] 22. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 23. 超时控制与查询保护集成
  - [ ] 23.1 集成请求级超时 context（request_timeout）和数据源查询级超时（min(query_timeout, 剩余时间)），查询复杂度限制和深度限制（gqlgen 内置支持）
    - _Requirements: 8.5, 8.6, 8.7, 8.8_

  - [ ]* 23.2 Write property tests for timeout and query protection
    - **Property 26: 单数据源查询超时取消**
    - **Validates: Requirements 8.5**
    - **Property 27: 总请求超时取消**
    - **Validates: Requirements 8.6**
    - **Property 28: 查询复杂度限制**
    - **Validates: Requirements 8.7**
    - **Property 29: 查询深度限制**
    - **Validates: Requirements 8.8**
    - **Property 88: 请求超时与查询超时组合**
    - **Validates: Design - 超时组合机制**

- [ ] 24. 优雅关闭集成
  - [ ] 24.1 实现完整优雅关闭流程：SIGTERM/SIGINT → 停止接受新连接 → 等待 in-flight 请求（max_wait_time）→ TracingProvider.Shutdown（独立 5s 超时）→ 刷新 Metrics → DataSourceManager.CloseAll → Logger.Sync
    - _Requirements: 15.5, 15.6, 15.7, 15.8_

  - [ ]* 24.2 Write property tests for graceful shutdown
    - **Property 60: 优雅关闭 - 停止接受新请求**
    - **Validates: Requirements 15.5, 15.6**
    - **Property 61: 优雅关闭 - 资源清理顺序**
    - **Validates: Requirements 15.7, 15.8**


- [ ] 25. 主入口与端到端集成
  - [ ] 25.1 实现 main.go（cmd/server/main.go）：加载配置 → 初始化日志 → 初始化 TracingProvider → 注册适配器 → 初始化 DataSourceManager → 初始化 CacheLayer → 初始化 RateLimiter → 初始化 MetricsCollector → 初始化 HealthChecker → 构建中间件链（RequestID → BodyLimit → CORS → Auth → RateLimit → Compression）→ 启动 HTTP 服务器 → 优雅关闭
    - _Requirements: 1.1, 1.4, 1.5_

  - [ ] 25.2 创建示例配置文件（config.yaml）：包含所有配置项的完整示例
    - _Requirements: 3.1_

- [ ] 26. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 27. 容器化与部署
  - [ ] 27.1 创建多阶段 Dockerfile：构建阶段（Go 编译）+ 最终阶段（scratch/distroless），非 root 用户，构建参数注入版本号和构建时间
    - _Requirements: 18.1, 18.2, 18.3_

  - [ ] 27.2 创建 Kubernetes 部署清单：Deployment（含 startupProbe + livenessProbe + readinessProbe、资源 requests/limits）、Service、ConfigMap、HPA
    - _Requirements: 18.4, 18.5, 18.6_

  - [ ] 27.3 创建 Docker Compose 集成测试环境（deploy/docker-compose.yaml）：StarRocks FE/BE、Prometheus、Redis
    - _Requirements: 17.11_

  - [ ] 27.4 创建 CI/CD 流水线配置示例（GitHub Actions）：lint（golangci-lint）、单元测试、镜像构建和推送
    - _Requirements: 17.3, 18.7_

- [ ] 28. 代码质量与工程规范
  - [ ] 28.1 配置 golangci-lint（.golangci.yml）：启用 govet、errcheck、staticcheck，确保代码通过检查
    - _Requirements: 17.3_

  - [ ] 28.2 添加 GoDoc 注释：所有导出函数、类型和接口添加符合 GoDoc 规范的注释
    - _Requirements: 17.1, 17.2_

  - [ ] 28.3 创建 gqlgen.yml 配置文件（如未在 10.2 中完成）：Schema 文件路径、自定义标量映射、resolver 配置
    - _Requirements: 2.7_

- [ ] 29. Final checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties using the `rapid` library
- Unit tests validate specific examples and edge cases
- 属性测试标签格式：`Feature: graphql-multi-datasource-api, Property {number}: {property_text}`
- 每个属性测试最少运行 100 次迭代
