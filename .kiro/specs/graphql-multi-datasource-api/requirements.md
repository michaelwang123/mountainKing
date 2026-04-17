# 需求文档

## 简介

本项目旨在使用 Go 语言和 GraphQL 框架（gqlgen）开发一个高性能的 API 服务。该服务能够统一接入 StarRocks（OLAP 分析型数据库）和 Prometheus（时序数据库）等多种数据源，并通过 GraphQL 协议向客户端提供灵活的数据查询能力。客户端可以使用任意编程语言通过标准 HTTP POST 请求调用该服务，支持字段选择、排序、过滤、分页以及跨数据源混合查询。此外，该服务还具备完善的安全认证（JWT / API Key）、请求限流（令牌桶算法）、查询结果缓存（内存 / Redis）、Prometheus 指标监控、OpenTelemetry 分布式链路追踪、健康检查与就绪探针等生产级运维能力，确保在高并发场景下的安全性、可观测性和可靠性。

**服务定位：** 本服务定位为只读查询服务（Query-only），不支持通过 GraphQL Mutation 对数据源执行写操作。文档中涉及的 Mutation 操作仅限于服务自身的管理功能（如缓存清除）。指标和 Trace 中的 `operation_type` 标签保留 mutation 值，用于记录这些管理操作。本服务不支持 GraphQL Subscription（实时订阅），所有数据获取均通过客户端主动查询（Pull 模式）完成。

## 术语表

- **API_Service**: 本项目开发的 GraphQL API 服务，基于 Go 语言和 gqlgen 框架构建
- **GraphQL_Engine**: API_Service 中负责解析和执行 GraphQL 查询的核心引擎
- **DataSource_Manager**: API_Service 中负责管理和协调多个数据源连接的组件，支持动态注册和发现数据源适配器
- **Adapter_Registry**: DataSource_Manager 中负责注册、发现和管理数据源适配器的子组件
- **StarRocks_Adapter**: API_Service 中负责与 StarRocks 数据库通信的适配器组件
- **Prometheus_Adapter**: API_Service 中负责与 Prometheus 通信的适配器组件（注意：此处 Prometheus 指作为被查询的时序数据源；与需求 11 中 API_Service 暴露 `/metrics` 端点供外部 Prometheus 抓取指标是两个不同概念——前者是"从 Prometheus 读数据"，后者是"向 Prometheus 写指标"）
- **Query_Resolver**: API_Service 中负责将 GraphQL 查询解析并路由到对应数据源的组件
- **Schema**: GraphQL 类型定义，描述可查询的数据结构和操作
- **Client**: 通过 HTTP POST 请求调用 API_Service 的外部应用程序
- **Connection**: 基于 Relay 规范的分页模型，包含 edges、nodes、pageInfo 和 totalCount
- **DataLoader**: 用于批量加载和缓存数据源请求的组件，减少 N+1 查询问题
- **Trace**: OpenTelemetry 中表示一个完整请求链路的数据结构，由一组有因果关系的 Span 组成
- **Span**: OpenTelemetry 中表示一个操作单元的数据结构，包含操作名称、起止时间、属性和状态等信息
- **Root_Span**: 一条 Trace 中的顶层 Span，代表请求的入口操作
- **OTLP**: OpenTelemetry Protocol，OpenTelemetry 定义的标准遥测数据传输协议，支持 gRPC 和 HTTP 两种传输方式
- **Trace_Context**: W3C 定义的分布式追踪上下文传播标准，通过 `traceparent` HTTP 头在服务间传递追踪信息
- **Sampling_Rate**: 采样率，控制 Trace 数据采集比例的配置参数，取值范围为 0.0 到 1.0
- **Tracing_Provider**: API_Service 中负责初始化和管理 OpenTelemetry Tracing 功能的组件
- **Auth_Middleware**: API_Service 中负责在 GraphQL 解析之前执行认证和授权检查的中间件组件
- **Rate_Limiter**: API_Service 中负责按客户端维度进行请求频率限制的组件
- **Health_Checker**: API_Service 中负责检测服务及各数据源健康状态的组件
- **Cache_Layer**: API_Service 中负责缓存 GraphQL 查询结果的可选组件，支持内存缓存和 Redis 缓存两种后端
- **Token_Bucket**: 令牌桶算法，一种流量控制算法，以固定速率向桶中添加令牌，请求消耗令牌，桶空时拒绝请求，允许突发流量同时控制平均速率
- **Singleflight**: 一种并发控制模式，确保对同一资源的多个并发请求只触发一次实际调用，其他请求等待并共享结果，Go 标准库 `golang.org/x/sync/singleflight` 提供实现
- **Cache_Penetration**: 缓存穿透，指查询一个不存在的数据，由于缓存中没有命中，每次都会穿透到数据源查询
- **Cache_Avalanche**: 缓存雪崩，指大量缓存条目在同一时间过期，导致大量请求同时穿透到数据源
- **Cache_Breakdown**: 缓存击穿，指热点 key 在过期的瞬间，大量并发请求同时穿透到数据源
- **Public_Endpoints**: 公共端点，指无需认证和限流即可访问的服务端点集合，包括 `/health`、`/ready`、`/metrics` 和 `/playground`（仅开发模式）。认证中间件和限流组件均对这些端点豁免检查

## 需求优先级与依赖关系

### 优先级定义

| 优先级 | 含义 | 需求列表 |
|--------|------|----------|
| P0 - 核心 | MVP 必须，服务能跑起来 | 需求 1, 2, 3, 4, 5, 8, 9 |
| P1 - 重要 | 生产可用，安全与扩展性保障 | 需求 6, 7, 10, 13, 15 |
| P2 - 增强 | 运维友好，可观测性与性能优化 | 需求 11, 12, 14, 16, 17, 18 |

### 依赖关系

```
需求 1 (GraphQL 端点) ← 需求 2 (Schema 定义)
需求 3 (数据源管理) ← 需求 4 (StarRocks 适配) ← 需求 6 (混合查询)
需求 3 (数据源管理) ← 需求 5 (Prometheus 适配) ← 需求 6 (混合查询)
需求 1 (GraphQL 端点) ← 需求 7 (字段选择)
需求 1 (GraphQL 端点) ← 需求 8 (高性能) ← 需求 11 (指标埋点)
需求 3 (数据源管理) ← 需求 10 (适配器扩展)
需求 1 (GraphQL 端点) ← 需求 13 (安全认证) ← 需求 14 (请求限流)
需求 3 (数据源管理) ← 需求 15 (运维能力) ← 需求 18 (容器化与部署)
需求 1 (GraphQL 端点) ← 需求 16 (查询缓存)
需求 8 (高性能) ← 需求 12 (链路追踪)
```

> 箭头 `←` 表示右侧需求依赖左侧需求。建议按 P0 → P1 → P2 的顺序迭代开发，同一优先级内按依赖关系排序。

## 需求

### 需求 1：GraphQL 端点服务 `P0`

**用户故事：** 作为客户端开发者，我希望通过标准 HTTP POST 请求访问 GraphQL API，以便使用任意编程语言进行数据查询。

#### 验收标准

1. THE API_Service SHALL 在配置的端口上提供一个 HTTP POST 端点（`/graphql`）用于接收 GraphQL 查询请求
2. WHEN Client 发送一个包含有效 GraphQL 查询的 HTTP POST 请求时，THE GraphQL_Engine SHALL 返回符合 GraphQL 规范的 JSON 响应
3. WHEN Client 发送的请求体不符合 GraphQL 规范时，THE GraphQL_Engine SHALL 返回包含错误描述的 JSON 响应，HTTP 状态码为 400
4. THE API_Service SHALL 接受 Content-Type 为 `application/json` 的请求体，请求体包含 `query` 字段和可选的 `variables` 字段
5. THE API_Service SHALL 在启动时加载 GraphQL Schema 并验证其完整性
6. THE API_Service SHALL 支持 HTTP GET 方法用于简单查询，Client 通过 URL 查询字符串传递 `query` 和可选的 `variables` 参数
7. WHILE API_Service 运行在开发模式时，THE API_Service SHALL 在 `/playground` 路径上提供 GraphQL Playground（GraphiQL）交互式查询界面；WHILE API_Service 运行在生产模式时，THE API_Service SHALL 禁用 GraphQL Playground 端点并返回 404
8. THE API_Service SHALL 支持配置 HTTP 请求体的最大大小限制（默认 1MB），WHEN Client 发送的请求体超过配置的最大大小时，THE API_Service SHALL 返回 HTTP 状态码 413（Payload Too Large）
9. THE API_Service SHALL 支持 GraphQL 批量查询（Query Batching），Client 可在一个 HTTP POST 请求中发送 GraphQL 查询数组，API_Service 并行执行所有查询并返回结果数组
10. THE API_Service SHALL 支持配置批量查询的最大查询数（默认 10），WHEN Client 发送的批量查询数超过配置上限时，THE API_Service SHALL 返回 HTTP 状态码 400 和包含错误描述的 JSON 响应
11. THE Rate_Limiter SHALL 按批量查询中包含的实际查询数（而非 HTTP 请求数）进行限流计数

### 需求 2：GraphQL Schema 定义 `P0`

**用户故事：** 作为客户端开发者，我希望通过 GraphQL Schema 了解所有可查询的数据类型和操作，以便构建精确的查询请求。

#### 验收标准

1. THE Schema SHALL 为每个数据源定义对应的 GraphQL 类型（Type），包含该数据源可查询的所有字段
2. THE Schema SHALL 定义 Query 根类型，包含访问各数据源的入口字段
3. THE Schema SHALL 为支持分页的查询定义 Connection 类型，包含 `edges`、`nodes`、`pageInfo` 和 `totalCount` 字段
4. THE Schema SHALL 为每个可过滤的查询定义对应的 Filter 输入类型（Input Type）
5. THE Schema SHALL 为每个可排序的查询定义对应的 OrderBy 输入类型（Input Type），支持指定排序字段和排序方向（ASC/DESC）
6. THE API_Service SHALL 提供 GraphQL Introspection 查询支持，允许 Client 动态获取 Schema 信息
7. THE Schema SHALL 采用模块化结构，每个数据源适配器独立定义自身的 `.graphql` Schema 文件，通过 gqlgen 代码生成阶段（`go generate`）自动合并为完整 Schema（详见需求 10：Schema 扩展）
8. THE API_Service SHALL 支持通过配置文件禁用 GraphQL Introspection 查询，WHEN 配置文件中 `graphql.introspection_enabled` 字段为 false 时，THE GraphQL_Engine SHALL 拒绝所有 Introspection 查询并返回错误响应（生产环境建议禁用以防止 Schema 信息泄露）

##### Mutation 与 Subscription 边界

9. THE Schema SHALL 定义 Mutation 根类型，仅包含服务管理类操作：`clearCache(datasource: String)` 用于清除缓存（参见需求 16），不支持对数据源的写入操作
10. THE Schema SHALL 在 Mutation 根类型的描述注释中明确说明：本服务仅支持管理类 Mutation 操作，不支持数据写入
11. WHEN Client 尝试通过 GraphQL 执行未定义的 Mutation 操作时，THE GraphQL_Engine SHALL 返回标准的 GraphQL 验证错误
12. THE Schema SHALL 不定义 Subscription 根类型，THE GraphQL_Engine SHALL 拒绝所有 Subscription 操作并返回不支持的错误响应

### 需求 3：多数据源管理 `P0`

**用户故事：** 作为系统管理员，我希望通过配置文件管理多个数据源的连接信息，以便灵活地添加和调整数据源。

#### 验收标准

1. THE DataSource_Manager SHALL 在 API_Service 启动时从 YAML 格式的配置文件中读取所有数据源的连接参数
2. THE DataSource_Manager SHALL 为每个数据源维护独立的连接池
3. IF 某个数据源在启动时无法建立连接，THEN THE DataSource_Manager SHALL 记录错误日志并将该数据源标记为不可用，API_Service 继续启动
4. WHILE 某个数据源处于不可用状态时，THE DataSource_Manager SHALL 按照可配置的重连策略定期尝试重新建立连接，默认初始重连间隔为 5 秒，采用指数退避策略，最大重连间隔不超过 60 秒
5. THE DataSource_Manager SHALL 支持配置每个数据源的连接池大小、连接超时时间和查询超时时间
6. WHEN 某个数据源的连接池中所有连接均被占用时，THE DataSource_Manager SHALL 将新的查询请求排队等待可用连接，等待时间不超过配置的连接获取超时时间（默认 5 秒），IF 等待超时，THEN THE DataSource_Manager SHALL 返回连接池耗尽错误
7. THE DataSource_Manager SHALL 采用统一的数据源配置结构，每个数据源配置包含类型标识（type）、连接参数（connection）和自定义选项（options）三个部分
8. WHEN 配置文件中声明了一个新的数据源类型时，THE DataSource_Manager SHALL 通过类型标识自动查找 Adapter_Registry 中已注册的对应适配器进行实例化
9. IF 配置文件中声明的数据源类型在 Adapter_Registry 中未找到对应适配器，THEN THE DataSource_Manager SHALL 记录错误日志并跳过该数据源的初始化
10. THE API_Service SHALL 在启动时对 YAML 配置文件进行完整性校验，IF 配置文件包含无效值（如负数的连接池大小、空的连接地址、不支持的数据源类型），THEN THE API_Service SHALL 输出明确的错误信息并拒绝启动

### 需求 4：StarRocks 数据源适配 `P0`

**用户故事：** 作为客户端开发者，我希望通过 GraphQL 查询 StarRocks 中的 OLAP 分析数据，以便获取业务报表和统计信息。

#### 验收标准

1. THE StarRocks_Adapter SHALL 通过 MySQL 协议连接 StarRocks 数据库
2. WHEN Query_Resolver 收到针对 StarRocks 数据源的查询时，THE StarRocks_Adapter SHALL 将 GraphQL 查询参数转换为对应的 SQL 查询语句
3. THE StarRocks_Adapter SHALL 支持将 GraphQL 的 Filter 参数转换为 SQL WHERE 子句
4. THE StarRocks_Adapter SHALL 支持将 GraphQL 的 OrderBy 参数转换为 SQL ORDER BY 子句
5. THE StarRocks_Adapter SHALL 支持将 GraphQL 的分页参数（first、after、offset、limit）转换为 SQL LIMIT/OFFSET 子句
6. WHEN Client 请求 `totalCount` 字段时，THE StarRocks_Adapter SHALL 执行一条额外的 COUNT 查询以返回符合过滤条件的总记录数
7. THE StarRocks_Adapter SHALL 使用参数化查询防止 SQL 注入攻击

##### 数据类型映射

8. THE StarRocks_Adapter SHALL 定义 StarRocks SQL 类型到 GraphQL 类型的映射规则，至少包含以下映射：`INT/BIGINT` → `Int`、`FLOAT/DOUBLE` → `Float`、`VARCHAR/STRING` → `String`、`BOOLEAN` → `Boolean`、`DECIMAL` → `String`（保留精度）、`DATETIME/DATE` → `DateTime` 自定义标量类型、`JSON` → `JSON` 自定义标量类型
9. THE StarRocks_Adapter SHALL 对不支持的 SQL 类型记录警告日志并将其映射为 `String` 类型作为兜底处理

### 需求 5：Prometheus 数据源适配 `P0`

**用户故事：** 作为客户端开发者，我希望通过 GraphQL 查询 Prometheus 中的时序监控数据，以便获取系统指标和告警信息。

#### 验收标准

1. THE Prometheus_Adapter SHALL 通过 Prometheus HTTP API 连接 Prometheus 服务
2. WHEN Query_Resolver 收到针对 Prometheus 数据源的查询时，THE Prometheus_Adapter SHALL 将 GraphQL 查询参数转换为对应的 PromQL 查询
3. THE Prometheus_Adapter SHALL 支持即时查询（Instant Query）和范围查询（Range Query）两种查询模式
4. THE Prometheus_Adapter SHALL 支持将 GraphQL 的时间范围参数（startTime、endTime、step）转换为 PromQL 的时间参数
5. THE Prometheus_Adapter SHALL 支持将 GraphQL 的 Filter 参数转换为 PromQL 的标签匹配器（Label Matcher）
6. WHEN Prometheus 返回的数据量超过配置的最大数据点数时，THE Prometheus_Adapter SHALL 返回错误提示，建议 Client 缩小查询时间范围或增大 step 值
7. THE Prometheus_Adapter SHALL 对 GraphQL Filter 参数中的标签值进行输入校验，拒绝包含 PromQL 特殊字符（如 `}`、`{`、`|`、`~`、`"`）的非法输入，防止 PromQL 注入攻击

##### 数据类型映射

8. THE Prometheus_Adapter SHALL 定义 Prometheus 数据类型到 GraphQL 类型的映射规则：`scalar` → `Float`、`string` → `String`、`vector` 和 `matrix` → 对应的自定义 GraphQL 类型（`PrometheusVector`、`PrometheusMatrix`），包含 `metric`（标签集）、`values`（数据点数组）和 `timestamps`（时间戳数组）字段
9. THE Prometheus_Adapter SHALL 将 Prometheus 的 `NaN` 和 `+Inf`/`-Inf` 特殊值转换为 GraphQL 的 `null`，并在响应的 `extensions.warnings` 中记录转换信息

### 需求 6：跨数据源混合查询 `P1`

**用户故事：** 作为客户端开发者，我希望在一个 GraphQL 查询中同时获取来自不同数据源的数据，以便减少网络请求次数并在前端组合展示。

#### 验收标准

1. WHEN Client 发送的 GraphQL 查询包含来自多个数据源的字段时，THE Query_Resolver SHALL 并行地向各数据源发起查询
2. THE Query_Resolver SHALL 等待所有数据源查询完成后，将结果合并为一个统一的 GraphQL 响应返回给 Client
3. IF 混合查询中某个数据源查询失败，THEN THE Query_Resolver SHALL 在响应的 `errors` 字段中包含该数据源的错误信息，同时在 `data` 字段中返回其他数据源的成功结果
4. THE Query_Resolver SHALL 使用 DataLoader 模式批量处理同一数据源的多个查询请求，减少数据源连接开销

### 需求 7：查询字段选择 `P1`

**用户故事：** 作为客户端开发者，我希望在查询中只选择需要的字段，以便减少网络传输量和数据源查询开销。

#### 验收标准

1. WHEN Client 在 GraphQL 查询中指定了特定字段时，THE Query_Resolver SHALL 仅从数据源查询 Client 请求的字段
2. THE StarRocks_Adapter SHALL 根据 Client 请求的字段生成仅包含对应列的 SQL SELECT 语句
3. THE Prometheus_Adapter SHALL 根据 Client 请求的字段仅返回对应的指标数据

### 需求 8：高性能要求 `P0`

**用户故事：** 作为系统管理员，我希望 API 服务具备高性能和高并发处理能力，以便支撑生产环境的查询负载。

#### 验收标准

##### 并发与吞吐量

1. THE API_Service SHALL 支持至少 1000 个并发 GraphQL 查询连接
2. THE API_Service SHALL 使用 Go 的 goroutine 并发模型处理每个 GraphQL 查询请求

##### 延迟目标

3. THE API_Service SHALL 确保单数据源简单查询（无聚合、无 JOIN）的 P95 延迟不超过 200ms，P99 延迟不超过 500ms（不含数据源自身查询耗时）
4. THE API_Service SHALL 确保跨数据源混合查询的 P95 延迟不超过 500ms，P99 延迟不超过 1000ms（不含数据源自身查询耗时）

##### 超时控制

5. WHEN 单个数据源查询在配置的超时时间内未返回结果时，THE Query_Resolver SHALL 取消该查询并返回超时错误
6. THE API_Service SHALL 支持配置单个 HTTP 请求的总超时时间（默认 30 秒），WHEN 请求处理时间超过配置的总超时时间时，THE API_Service SHALL 取消所有进行中的数据源查询并返回超时错误

##### 查询保护

7. THE API_Service SHALL 支持配置查询复杂度限制，拒绝超过复杂度阈值的查询请求
8. THE API_Service SHALL 支持配置查询深度限制，拒绝超过深度阈值的嵌套查询
9. THE API_Service SHALL 支持配置单次查询的最大返回行数（默认 10000），WHEN 数据源返回的结果集超过配置上限时，THE API_Service SHALL 截断结果并在响应的 `extensions.warnings` 中包含截断提示信息

##### 可观测性

10. THE API_Service SHALL 暴露 Prometheus 格式的性能指标端点（`/metrics`），具体指标定义参见需求 11：Prometheus 可观测性指标埋点

### 需求 9：错误处理与日志 `P0`

**用户故事：** 作为系统管理员，我希望 API 服务具备完善的错误处理和日志记录能力，以便快速定位和排查问题。

#### 验收标准

1. WHEN 数据源查询发生错误时，THE API_Service SHALL 在 GraphQL 响应的 `errors` 数组中返回结构化的错误信息，包含错误码、错误消息和错误路径
2. THE API_Service SHALL 使用结构化日志格式（JSON）记录所有请求和错误信息
3. THE API_Service SHALL 为每个请求生成唯一的请求 ID，并在日志和响应头中包含该请求 ID
4. IF API_Service 收到格式错误的 GraphQL 查询，THEN THE GraphQL_Engine SHALL 返回包含语法错误位置信息的错误响应
5. THE API_Service SHALL 支持配置日志级别（DEBUG、INFO、WARN、ERROR）
6. THE DataSource_Manager SHALL 支持可配置的自动重试机制，配置项包含最大重试次数（max_retries）、初始重试间隔（retry_interval）和退避策略（指数退避）
7. WHEN 数据源查询发生瞬时错误（如连接超时、网络中断）时，THE DataSource_Manager SHALL 按照配置的退避策略自动重试查询；WHEN 数据源查询发生业务错误（如 SQL 语法错误、PromQL 语法错误）时，THE DataSource_Manager SHALL 立即返回错误，不进行重试

##### 错误码体系

8. THE API_Service SHALL 定义统一的错误码命名规范，格式为 `{CATEGORY}_{ERROR_NAME}`，错误码分类包括：`AUTH_*`（认证授权错误，如 `AUTH_TOKEN_EXPIRED`、`AUTH_INSUFFICIENT_PERMISSION`）、`VALIDATION_*`（请求验证错误，如 `VALIDATION_SYNTAX_ERROR`、`VALIDATION_COMPLEXITY_EXCEEDED`）、`DATASOURCE_*`（数据源错误，如 `DATASOURCE_TIMEOUT`、`DATASOURCE_UNAVAILABLE`）、`RATELIMIT_*`（限流错误，如 `RATELIMIT_EXCEEDED`）、`INTERNAL_*`（内部错误，如 `INTERNAL_UNEXPECTED`）
9. THE API_Service SHALL 在 GraphQL 响应的 `errors[].extensions` 中包含 `code`（错误码）和 `classification`（错误分类）字段，方便 Client 程序化处理错误

### 需求 10：数据源适配器扩展 `P1`

**用户故事：** 作为开发者，我希望系统具备良好的扩展性，以便未来能够方便地添加新的数据源类型，仅通过实现接口和添加配置即可完成集成。

#### 验收标准

##### 适配器接口与注册机制

1. THE DataSource_Manager SHALL 定义统一的数据源适配器接口（DataSource Interface），所有数据源适配器实现该接口
2. THE DataSource Interface SHALL 包含连接管理、查询执行、健康检查、Schema 提供和关闭连接五个方法
3. THE Adapter_Registry SHALL 提供 `Register(typeName string, factory AdapterFactory)` 方法，允许新的数据源适配器通过类型名称注册自身的工厂函数
4. WHEN 新的数据源适配器调用 Register 方法注册后，THE Adapter_Registry SHALL 将该适配器工厂函数存储在内部注册表中，供 DataSource_Manager 按类型名称查找和实例化
5. IF 注册时提供的类型名称与已注册的适配器类型名称重复，THEN THE Adapter_Registry SHALL 返回错误并拒绝覆盖已有注册

##### Schema 扩展（编译时）

6. THE DataSource Interface SHALL 定义 `SchemaFiles() []string` 方法，每个适配器通过该方法声明自身提供的 `.graphql` Schema 文件路径
7. WHEN 开发者添加新的数据源适配器时，SHALL 将适配器的 `.graphql` Schema 文件放置在约定目录下，通过 gqlgen 的代码生成阶段（`go generate`）自动合并到完整 Schema 中
8. THE GraphQL_Engine SHALL 在代码生成阶段验证合并后的 Schema 中不存在类型名称冲突，IF 存在冲突，THEN 代码生成 SHALL 失败并输出冲突的类型名称和来源文件

> **注意：** 由于 gqlgen 采用代码生成模式，Schema 合并发生在编译时而非运行时。添加新数据源适配器需要重新执行 `go generate` 和编译，不支持运行时热加载新适配器。

##### 配置驱动集成

9. WHEN 添加新的数据源类型时，THE DataSource_Manager SHALL 仅需要实现 DataSource Interface 并在配置文件中添加对应的数据源条目即可完成集成，无需修改核心代码
10. THE DataSource_Manager SHALL 将数据源配置中的 `options` 部分作为原始配置传递给对应适配器的工厂函数，由适配器自行解析其特有的配置项
11. THE API_Service SHALL 支持通过配置文件启用或禁用已注册的数据源适配器，WHEN 某个数据源配置项的 `enabled` 字段为 false 时，THE DataSource_Manager SHALL 跳过该数据源的初始化

##### 独立测试能力

12. THE DataSource Interface SHALL 定义明确的输入输出契约，使每个适配器可以独立于 API_Service 进行单元测试
13. THE API_Service SHALL 提供 MockDataSource 测试辅助实现，该实现符合 DataSource Interface，开发者可使用 MockDataSource 对 Query_Resolver 和 DataSource_Manager 进行集成测试而无需连接真实数据源


### 需求 11：Prometheus 可观测性指标埋点 `P2`

**用户故事：** 作为系统管理员，我希望 API 服务暴露详细的 Prometheus 指标，以便在 Grafana 中按多维度展示各个 API 的调用指标，实现全面的可观测性监控。

#### 验收标准

##### 指标端点

1. THE API_Service SHALL 在 `/metrics` 路径上暴露一个 HTTP GET 端点，返回符合 Prometheus 文本展示格式（text/plain; version=0.0.4）的指标数据
2. WHEN Prometheus 服务器向 `/metrics` 端点发起抓取请求时，THE API_Service SHALL 在 500ms 内返回当前所有已注册指标的快照

##### 请求级指标

3. THE API_Service SHALL 注册一个名为 `graphql_request_duration_seconds` 的 Histogram 指标，记录每个 GraphQL 请求的处理延迟，标签包含 `operation_name`（操作名称）、`operation_type`（query 或 mutation）和 `datasource`（数据源名称）
4. THE API_Service SHALL 注册一个名为 `graphql_requests_total` 的 Counter 指标，记录 GraphQL 请求总数，标签包含 `operation_name`（操作名称）、`operation_type`（query 或 mutation）、`status`（success 或 error）和 `datasource`（数据源名称）
5. THE API_Service SHALL 注册一个名为 `graphql_requests_in_flight` 的 Gauge 指标，记录当前正在处理的并发请求数

##### 数据源级指标

6. THE API_Service SHALL 注册一个名为 `graphql_datasource_query_duration_seconds` 的 Histogram 指标，记录每次数据源查询的延迟，标签包含 `datasource`（数据源名称）和 `datasource_type`（数据源类型，如 starrocks、prometheus）
7. THE API_Service SHALL 注册一个名为 `graphql_datasource_connection_pool_active` 的 Gauge 指标，记录每个数据源连接池的当前活跃连接数，标签包含 `datasource`（数据源名称）
8. THE API_Service SHALL 注册一个名为 `graphql_datasource_connection_pool_idle` 的 Gauge 指标，记录每个数据源连接池的当前空闲连接数，标签包含 `datasource`（数据源名称）
9. THE API_Service SHALL 注册一个名为 `graphql_datasource_connection_pool_waiting` 的 Gauge 指标，记录每个数据源连接池的当前等待获取连接的请求数，标签包含 `datasource`（数据源名称）

##### 错误指标

10. THE API_Service SHALL 注册一个名为 `graphql_errors_total` 的 Counter 指标，记录错误总数，标签包含 `error_type`（错误类型，如 validation、timeout、datasource_error）和 `datasource`（数据源名称）

##### 命名规范与可扩展性

11. THE API_Service SHALL 确保所有指标名称遵循 Prometheus 命名规范：使用小写字母和下划线分隔，计量单位作为后缀（如 `_seconds`、`_total`），Counter 类型指标以 `_total` 结尾
12. THE API_Service SHALL 支持通过配置文件为所有指标添加自定义标签（custom labels），WHEN 配置文件中定义了自定义标签时，THE API_Service SHALL 将自定义标签附加到每个指标的标签集中，方便在 Grafana 中进行多维度筛选（如按环境、集群、服务实例分组）


### 需求 12：OpenTelemetry 分布式链路追踪 `P2`

**用户故事：** 作为系统管理员，我希望 API 服务集成 OpenTelemetry 分布式链路追踪，以便在生产环境中可视化请求的完整调用链路，快速定位性能瓶颈和故障根因。

#### 验收标准

##### SDK 集成与初始化

1. THE API_Service SHALL 集成 OpenTelemetry Go SDK，在启动时通过 Tracing_Provider 初始化 TracerProvider 和 Tracer 实例
2. THE API_Service SHALL 支持通过配置文件启用或禁用 Tracing 功能，WHEN 配置文件中 `tracing.enabled` 字段为 false 时，THE Tracing_Provider SHALL 跳过 OpenTelemetry 初始化并使用 NoopTracerProvider

##### 请求级 Span

3. WHEN Client 发送 GraphQL 请求时，THE API_Service SHALL 创建一个 Root_Span，Span 名称格式为 `GraphQL {operation_type} {operation_name}`，并设置以下属性：`graphql.operation.name`（操作名称）、`graphql.operation.type`（操作类型：query 或 mutation）、`http.method`（HTTP 方法）、`http.url`（请求 URL）
4. WHEN GraphQL 请求处理完成时，THE API_Service SHALL 关闭该 Root_Span 并记录请求的总耗时

##### Resolver 级 Span

5. WHEN Query_Resolver 执行某个 Resolver 时，THE Query_Resolver SHALL 在 Root_Span 下创建一个子 Span，Span 名称格式为 `Resolver {field_name}`，并设置以下属性：`graphql.field.name`（字段名称）、`graphql.field.type`（字段返回类型）、`graphql.datasource`（目标数据源名称）

##### 数据源查询级 Span

6. WHEN StarRocks_Adapter 执行 SQL 查询时，THE StarRocks_Adapter SHALL 在当前 Resolver Span 下创建一个子 Span，Span 名称格式为 `StarRocks Query`，并设置以下属性：`db.system`（值为 starrocks）、`db.statement`（SQL 查询语句）、`db.datasource`（数据源名称）
7. WHEN Prometheus_Adapter 执行 PromQL 查询时，THE Prometheus_Adapter SHALL 在当前 Resolver Span 下创建一个子 Span，Span 名称格式为 `Prometheus Query`，并设置以下属性：`db.system`（值为 prometheus）、`db.statement`（PromQL 查询语句）、`db.datasource`（数据源名称）

##### 跨服务上下文传播

8. WHEN Client 请求包含 W3C `traceparent` 头时，THE API_Service SHALL 从该头中提取 Trace_Context 并将其作为 Root_Span 的父上下文，实现跨服务链路串联
9. WHEN API_Service 向外部数据源发起请求时，THE API_Service SHALL 将当前 Trace_Context 注入到出站请求的 `traceparent` 头中

##### Trace 数据导出

10. THE Tracing_Provider SHALL 支持通过 OTLP 协议（gRPC 或 HTTP）将 Trace 数据导出到 Jaeger、Tempo 或其他 OTLP 兼容的后端
11. THE API_Service SHALL 支持通过配置文件指定 OTLP 导出端点地址（`tracing.otlp.endpoint`）和传输协议（`tracing.otlp.protocol`，值为 grpc 或 http）

##### 采样配置

12. THE Tracing_Provider SHALL 支持通过配置文件设置 Sampling_Rate（`tracing.sampling_rate`），取值范围为 0.0（不采样）到 1.0（全量采样）
13. IF 配置文件中未指定 Sampling_Rate，THEN THE Tracing_Provider SHALL 使用默认采样率 1.0（全量采样）

##### 错误记录

14. IF 数据源查询发生错误，THEN THE API_Service SHALL 在对应的 Span 上设置状态为 Error，并通过 Span Event 记录错误类型和错误消息
15. IF GraphQL 请求处理过程中发生未捕获的异常，THEN THE API_Service SHALL 在 Root_Span 上设置状态为 Error 并记录异常信息

##### Trace 与日志关联

16. THE API_Service SHALL 将当前 Trace 的 trace_id 注入到结构化日志的字段中，使日志记录与 Trace 数据可以互相关联查找
17. THE API_Service SHALL 在 GraphQL 响应的扩展字段（`extensions.traceId`）中返回当前请求的 trace_id，方便 Client 根据 trace_id 查询链路详情


### 需求 13：安全认证与授权 `P1`

**用户故事：** 作为系统管理员，我希望 API 服务支持多种认证方式并进行权限控制，以便保护 API 免受未授权访问。

#### 验收标准

##### 通用认证规则

1. THE Auth_Middleware SHALL 支持 JWT Token 认证和 API Key 认证两种认证方式
2. THE Auth_Middleware SHALL 在 GraphQL_Engine 解析查询之前执行认证检查
3. WHEN Client 发送的请求未包含有效的认证凭据时，THE Auth_Middleware SHALL 返回 HTTP 状态码 401 和包含错误描述的 JSON 响应
4. WHEN Client 的认证凭据有效但权限不足以执行请求的操作时，THE Auth_Middleware SHALL 返回 HTTP 状态码 403 和包含错误描述的 JSON 响应
5. THE API_Service SHALL 支持通过 YAML 配置文件选择认证方式（jwt 或 apikey）并配置对应的认证参数
6. THE Auth_Middleware SHALL 对所有 Public_Endpoints（参见术语表）豁免认证检查

##### JWT 认证

7. WHERE JWT 认证方式启用时，THE Auth_Middleware SHALL 从请求的 `Authorization` 头中提取 Bearer Token 进行验证
8. WHERE JWT 认证方式启用时，THE Auth_Middleware SHALL 验证 Token 的签名、过期时间（exp）和签发者（iss），IF Token 已过期，THEN THE Auth_Middleware SHALL 返回 HTTP 状态码 401 并在响应体中包含 `token_expired` 错误码

##### API Key 认证

9. WHERE API Key 认证方式启用时，THE Auth_Middleware SHALL 从请求的 `X-API-Key` 头中提取 API Key 进行验证
10. WHERE API Key 认证方式启用时，THE Auth_Middleware SHALL 支持配置多个 API Key，每个 API Key 关联独立的权限范围（允许访问的数据源列表和操作类型）
11. THE API_Service SHALL 支持 API Key 轮换，允许为同一客户端同时配置新旧两个 API Key，并通过配置旧 Key 的过期时间实现平滑过渡

##### 审计与安全防护

12. THE API_Service SHALL 记录操作审计日志，包含认证主体标识（JWT sub 或 API Key ID）、操作时间、操作类型、目标数据源和请求结果（成功/失败），审计日志独立于应用日志输出
13. THE API_Service SHALL 对 Trace Span 和日志中记录的 `db.statement` 属性进行敏感信息脱敏处理，支持通过配置文件定义脱敏规则（如正则替换），默认对 SQL 查询中的字符串字面量和数值参数进行掩码处理

### 需求 14：请求限流 `P2`

**用户故事：** 作为系统管理员，我希望 API 服务具备请求频率限制能力，以便防止单个客户端过度消耗服务资源。

#### 验收标准

##### 单实例限流

1. THE Rate_Limiter SHALL 支持按客户端 IP 地址和 API Key 两个维度进行请求频率限制
2. THE Rate_Limiter SHALL 使用 Token_Bucket 算法实现频率限制
3. WHEN Client 的请求频率超过配置的限流阈值时，THE Rate_Limiter SHALL 返回 HTTP 状态码 429（Too Many Requests）和包含错误描述的 JSON 响应
4. THE Rate_Limiter SHALL 在每个响应的 HTTP 头中包含限流相关信息：`X-RateLimit-Limit`（限流上限）、`X-RateLimit-Remaining`（剩余可用请求数）和 `X-RateLimit-Reset`（限流重置时间的 Unix 时间戳）
5. THE API_Service SHALL 支持通过 YAML 配置文件调整限流参数，包括每个时间窗口的最大请求数和时间窗口大小
6. THE Rate_Limiter SHALL 对所有 Public_Endpoints（参见术语表）豁免限流检查

##### 分布式限流

7. THE Rate_Limiter SHALL 支持通过配置文件选择限流模式：`local`（单实例本地限流，默认）或 `distributed`（基于 Redis 的分布式限流）
8. WHERE 分布式限流模式启用时，THE Rate_Limiter SHALL 使用 Redis 作为共享存储，通过 Lua 脚本原子性地执行令牌桶的扣减和补充操作，确保多实例部署时全局限流总量准确
9. WHERE 分布式限流模式启用时，IF Redis 连接不可用，THEN THE Rate_Limiter SHALL 自动降级为本地限流模式并记录警告日志

### 需求 15：服务运维能力 `P1`

**用户故事：** 作为系统管理员，我希望 API 服务具备完善的运维能力，以便在生产环境中实现可靠的部署、监控和维护。

#### 验收标准

##### 健康检查

1. THE Health_Checker SHALL 在 `/health` 路径上提供一个 HTTP GET 端点，返回 API_Service 的整体健康状态
2. THE Health_Checker SHALL 在 `/ready` 路径上提供一个 HTTP GET 端点，返回 API_Service 的就绪状态，包含各数据源的连接状态
3. WHEN 所有核心组件运行正常时，THE Health_Checker SHALL 在 `/health` 端点返回 HTTP 状态码 200 和包含各组件状态详情的 JSON 响应；IF 任一核心组件异常，THEN THE Health_Checker SHALL 返回 HTTP 状态码 503
4. WHEN 至少一个数据源连接可用时，THE Health_Checker SHALL 在 `/ready` 端点返回 HTTP 状态码 200；IF 所有数据源连接均不可用，THEN THE Health_Checker SHALL 返回 HTTP 状态码 503

##### 优雅关闭

5. WHEN API_Service 收到 SIGTERM 或 SIGINT 信号时，THE API_Service SHALL 立即停止接受新的请求
6. WHILE API_Service 处于关闭过程中时，THE API_Service SHALL 等待所有进行中的请求完成处理，等待时间不超过配置的最大等待时间（默认 30 秒）
7. WHEN 所有进行中的请求处理完成或最大等待时间到达后，THE API_Service SHALL 依次关闭所有数据源连接池
8. WHEN API_Service 执行关闭流程时，THE Tracing_Provider SHALL 刷新所有未导出的 OpenTelemetry Trace 数据，THE API_Service SHALL 刷新所有 Prometheus 指标

##### CORS 配置

9. THE API_Service SHALL 支持通过 YAML 配置文件设置 CORS 策略，包括允许的 Origin 列表、Methods 列表和 Headers 列表
10. THE API_Service SHALL 默认禁用 CORS，WHEN 配置文件中 `cors.enabled` 字段为 true 时，THE API_Service SHALL 按照配置的策略处理跨域请求

##### 响应压缩

11. THE API_Service SHALL 支持 gzip 压缩响应体
12. WHEN Client 请求的 `Accept-Encoding` 头包含 gzip 且响应体大小超过配置的最小压缩阈值时，THE API_Service SHALL 使用 gzip 压缩响应体并设置 `Content-Encoding: gzip` 响应头

### 需求 16：查询结果缓存 `P2`

**用户故事：** 作为客户端开发者，我希望 API 服务支持查询结果缓存，以便对重复查询获得更快的响应速度并减少数据源负载。

#### 验收标准

1. THE Cache_Layer SHALL 作为可选组件，支持通过配置文件启用或禁用
2. THE Cache_Layer SHALL 支持内存缓存（默认）和 Redis 缓存两种后端实现
3. THE Cache_Layer SHALL 基于查询语句、变量和数据源名称的组合哈希值生成缓存 key
4. THE Cache_Layer SHALL 支持通过配置文件设置缓存 TTL，每个数据源可独立配置不同的 TTL 值
5. WHEN Client 在 GraphQL 请求的扩展参数中设置 `extensions.cache` 为 false 时，THE Cache_Layer SHALL 跳过缓存直接查询数据源
6. THE Cache_Layer SHALL 将缓存命中次数和未命中次数记录到 Prometheus 指标中，注册名为 `graphql_cache_hits_total` 的 Counter 指标和名为 `graphql_cache_misses_total` 的 Counter 指标，标签包含 `datasource`（数据源名称）和 `cache_backend`（缓存后端类型，如 memory、redis）
7. THE Cache_Layer SHALL 仅缓存 Query 类型的操作，Mutation 类型的操作 SHALL 始终跳过缓存直接执行
8. WHERE 内存缓存后端启用时，THE Cache_Layer SHALL 支持配置最大缓存条目数（默认 10000），WHEN 缓存条目数达到上限时，THE Cache_Layer SHALL 使用 LRU（最近最少使用）策略淘汰旧条目
9. THE Cache_Layer SHALL 提供缓存清除能力，支持通过 GraphQL Mutation 操作 `clearCache(datasource: String)` 清除指定数据源或全部缓存

##### 缓存防护

10. THE Cache_Layer SHALL 实现缓存穿透防护：对数据源返回空结果的查询缓存一个短 TTL 的空值标记（默认 30 秒），避免相同的无效查询反复穿透到数据源
11. THE Cache_Layer SHALL 实现缓存雪崩防护：在配置的 TTL 基础上添加随机抖动（jitter），抖动范围为 TTL 的 ±10%，避免大量缓存条目同时过期
12. THE Cache_Layer SHALL 实现缓存击穿防护：对同一缓存 key 的并发回源请求使用 singleflight 模式，确保同一时刻只有一个请求回源查询数据源，其他请求等待并共享结果

### 需求 17：代码质量与工程规范 `P2`

**用户故事：** 作为开发者，我希望项目遵循统一的代码质量和工程规范，以便保证代码的可维护性、可读性和一致性。

#### 验收标准

##### 代码规范

1. THE API_Service 的所有导出函数、类型和接口 SHALL 包含符合 GoDoc 规范的注释
2. THE API_Service 的关键业务逻辑代码块 SHALL 包含行内注释说明代码意图
3. THE API_Service 的代码 SHALL 通过 golangci-lint 检查，启用的 linter 包含 govet、errcheck 和 staticcheck
4. THE API_Service SHALL 遵循 Go 标准项目布局，使用 `cmd/` 目录存放入口程序、`internal/` 目录存放内部包、`pkg/` 目录存放可复用的公共包
5. THE API_Service 的核心组件 SHALL 通过接口定义进行解耦，采用接口优先的设计原则
6. THE API_Service SHALL 使用 Go 惯用的 error wrapping 模式处理错误，通过 `fmt.Errorf` 配合 `%w` 动词包装底层错误

##### 配置管理

7. THE API_Service SHALL 使用 Viper 库进行配置管理，支持 YAML 格式的配置文件
8. THE API_Service SHALL 支持通过环境变量覆盖 YAML 配置文件中的配置项，环境变量命名规则为大写字母加下划线，前缀为 `GRAPHQL_`（如 `GRAPHQL_SERVER_PORT` 覆盖 `server.port` 配置），符合 12-Factor App 规范
9. THE API_Service SHALL 支持配置热更新，通过监听配置文件变更（fsnotify）自动重新加载以下运行时参数而无需重启服务：日志级别、限流参数、缓存 TTL；其他配置项（如数据源连接、端口）的变更需要重启服务生效

##### 测试策略

10. THE API_Service 的单元测试覆盖率 SHALL 不低于 70%
11. THE API_Service SHALL 提供集成测试套件，使用 Docker Compose 编排 StarRocks、Prometheus 和 Redis 等依赖服务，验证适配器与真实数据源的交互正确性
12. THE API_Service SHALL 提供性能基准测试（benchmark），覆盖单数据源查询和跨数据源混合查询场景，用于验证需求 8 中定义的延迟目标


### 需求 18：容器化与部署 `P2`

**用户故事：** 作为 DevOps 工程师，我希望 API 服务提供标准化的容器镜像和 Kubernetes 部署清单，以便快速、可靠地在生产环境中部署和扩缩容。

#### 验收标准

##### 容器化

1. THE API_Service SHALL 提供多阶段构建的 Dockerfile，最终镜像基于 `scratch` 或 `distroless` 基础镜像，确保镜像体积最小化且不包含不必要的系统工具
2. THE API_Service 的容器镜像 SHALL 以非 root 用户运行，UID 不为 0
3. THE API_Service 的 Dockerfile SHALL 支持通过构建参数（`--build-arg`）注入版本号和构建时间，并在 `/health` 端点的响应中包含版本信息

##### Kubernetes 部署

4. THE API_Service SHALL 提供 Kubernetes 部署清单，包含 Deployment、Service、ConfigMap 和 HorizontalPodAutoscaler 资源定义
5. THE Kubernetes Deployment SHALL 配置 `livenessProbe`（指向 `/health`）和 `readinessProbe`（指向 `/ready`），探针参数可通过 ConfigMap 调整
6. THE Kubernetes Deployment SHALL 配置合理的资源请求（requests）和限制（limits），默认值通过 ConfigMap 管理

##### CI/CD 集成

7. THE API_Service SHALL 提供 CI/CD 流水线配置示例（GitHub Actions 或 GitLab CI），包含代码检查（lint）、单元测试、镜像构建和推送阶段


## 附录：配置文件示例

以下为完整的 YAML 配置文件示例，汇总了各需求中涉及的所有配置项：

```yaml
# 服务基础配置
server:
  port: 8080
  mode: production                    # production | development
  max_request_body_size: 1MB
  request_timeout: 30s
  max_batch_queries: 10               # 批量查询最大数量
  allow_get_queries: false            # 是否允许 GET 查询（生产模式默认 false，开发模式默认 true）

# GraphQL 配置
graphql:
  introspection_enabled: false        # 生产环境建议禁用
  max_query_complexity: 100
  max_query_depth: 10
  max_result_rows: 10000              # 单次查询最大返回行数

# 数据源配置
datasources:
  - name: analytics_db
    type: starrocks
    enabled: true
    connection:
      host: starrocks-fe
      port: 9030
      username: root
      password: "${GRAPHQL_STARROCKS_PASSWORD}"
      database: analytics
    options:
      pool_size: 20
      connection_timeout: 5s
      query_timeout: 30s
      pool_acquire_timeout: 5s        # 连接池获取超时
      reconnect_interval: 5s          # 初始重连间隔
      max_reconnect_interval: 60s     # 最大重连间隔
      allowed_tables:                  # 表名/字段名白名单（安全必填）
        orders:
          columns: [order_id, user_id, amount, status, created_at]
        users:
          columns: [user_id, username, email, created_at]

  - name: monitoring
    type: prometheus
    enabled: true
    connection:
      url: http://prometheus:9090
    options:
      query_timeout: 15s
      max_data_points: 11000
      reconnect_interval: 5s
      max_reconnect_interval: 60s

# 认证配置
auth:
  method: jwt                         # jwt | apikey
  trusted_proxies: ["10.0.0.0/8", "172.16.0.0/12"]  # 可信代理 CIDR 列表
  jwt:
    algorithm: RS256                  # HS256 | RS256 | ES256（生产环境推荐 RS256/ES256）
    public_key_file: /etc/secrets/jwt-public.pem  # 非对称签名时使用公钥文件
    # secret: "${GRAPHQL_JWT_SECRET}" # 对称签名（HS256）时使用
    issuer: my-auth-service
  apikey:
    keys:
      - id: client-a
        key: "${GRAPHQL_APIKEY_CLIENT_A}"
        permissions:
          datasources: [analytics_db, monitoring]
          operations: [query]
      - id: client-a-new                # API Key 轮换：新 key
        key: "${GRAPHQL_APIKEY_CLIENT_A_NEW}"
        permissions:
          datasources: [analytics_db, monitoring]
          operations: [query]
      - id: client-a-old                # API Key 轮换：旧 key（设置过期时间）
        key: "${GRAPHQL_APIKEY_CLIENT_A_OLD}"
        expires_at: "2026-06-01T00:00:00Z"
        permissions:
          datasources: [analytics_db]
          operations: [query]

# 限流配置
rate_limit:
  mode: local                         # local | distributed
  requests_per_window: 100
  window_size: 60s
  redis:                              # 分布式限流时使用
    addr: redis:6379
    password: "${GRAPHQL_REDIS_PASSWORD}"

# 缓存配置
cache:
  enabled: true
  backend: memory                     # memory | redis
  default_ttl: 60s
  empty_result_ttl: 30s               # 空结果缓存 TTL（穿透防护）
  ttl_jitter_percent: 10              # TTL 抖动百分比（雪崩防护）
  memory:
    max_entries: 10000
    max_memory_size: 256MB            # 最大内存占用（与 max_entries 双重限制）
  redis:
    addr: redis:6379
    password: "${GRAPHQL_REDIS_PASSWORD}"
    db: 1
  per_datasource:
    analytics_db:
      ttl: 300s
    monitoring:
      ttl: 30s

# CORS 配置
cors:
  enabled: false
  allowed_origins: ["https://dashboard.example.com"]
  allowed_methods: ["GET", "POST", "OPTIONS"]
  allowed_headers: ["Content-Type", "Authorization", "X-API-Key"]

# 响应压缩
compression:
  enabled: true
  min_size: 1KB

# 日志配置
logging:
  level: info                         # debug | info | warn | error
  format: json
  audit:
    enabled: true
    output: stdout                    # stdout | file
    file_path: /var/log/api/audit.log

# 敏感信息脱敏
sanitization:
  enabled: true
  rules:
    - pattern: "'[^']*'"             # SQL 字符串字面量
      replacement: "'***'"
    - pattern: "\\b\\d{4,}\\b"       # 4位以上数值
      replacement: "***"

# Prometheus 指标
metrics:
  custom_labels:
    env: production
    cluster: cn-east-1
    instance: "${HOSTNAME}"

# OpenTelemetry 链路追踪
tracing:
  enabled: true
  sampling_rate: 0.1
  otlp:
    endpoint: tempo:4317
    protocol: grpc                    # grpc | http

# 错误重试
retry:
  max_retries: 3
  retry_interval: 100ms
  backoff: exponential

# 优雅关闭
shutdown:
  max_wait_time: 30s
```
