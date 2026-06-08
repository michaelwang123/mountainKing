# 模块 01：项目概览与架构设计

> 理解 mountainKing 的定位、架构分层和核心设计决策。

## 1.1 项目定位

mountainKing 是一个基于 Go 语言的生产级 GraphQL API 服务，提供跨多数据源的统一查询接口。

核心特点：
- **只读查询服务**：不支持通过 GraphQL 对数据源执行写操作，Mutation 仅用于管理功能（缓存清除、模板重载）
- **多数据源统一接入**：当前支持 StarRocks（OLAP）和 Prometheus（时序指标），通过适配器模式可扩展
- **SQL 模板引擎**：支持复杂的多表 JOIN、CTE、窗口函数查询，替代传统 Java/OData + FreeMarker 方案
- **生产级能力**：认证授权、限流、缓存、熔断、链路追踪、指标监控、审计日志一应俱全

## 1.2 技术栈

| 层面 | 技术选型 | 说明 |
|------|----------|------|
| 语言 | Go 1.25+ | 高性能、强类型、原生并发 |
| GraphQL | gqlgen | Schema-first 代码生成，类型安全 |
| HTTP 路由 | chi | 轻量级、兼容 net/http、中间件链 |
| 配置 | Viper | YAML + 环境变量覆盖（12-Factor） |
| 日志 | zap | 高性能结构化 JSON 日志 |
| 链路追踪 | OpenTelemetry | OTLP 导出到 Jaeger/Tempo |
| 指标 | Prometheus client_golang | 自定义标签、多维度指标 |
| 认证 | golang-jwt/jwt/v5 | HS256/RS256/ES256 |
| 缓存 | golang-lru/v2 + go-redis/v9 | 内存 LRU 或 Redis |
| 数据库 | database/sql + go-sql-driver/mysql | StarRocks MySQL 协议 |
| 测试 | testing + rapid | 单元测试 + 属性测试 |

## 1.3 分层架构

```
客户端 (HTTP POST /graphql)
    │
    ▼
┌─────────────────────────────────────────┐
│           中间件层 (Middleware)           │
│  RequestID → BodyLimit → CORS → CSRF → │
│  Auth → AuthFailureLimiter → RateLimit →│
│  Compression                            │
├─────────────────────────────────────────┤
│         GraphQL Engine (gqlgen)          │
│    复杂度检查 · 深度检查 · Schema 验证    │
├─────────────────────────────────────────┤
│           Query Resolver                 │
│    字段选择优化 · 并行调度多数据源查询     │
├─────────────────────────────────────────┤
│            DataLoader                    │
│    Per-Request 实例 · 批量合并请求        │
├─────────────────────────────────────────┤
│            Cache Layer                   │
│  Singleflight · 穿透/雪崩/击穿防护       │
├─────────────────────────────────────────┤
│         DataSource Manager               │
│    连接池 · 熔断器 · 重连 · 重试          │
├──────────────┬──────────────────────────┤
│  StarRocks   │  Prometheus  │ Template  │
│  Adapter     │  Adapter     │ Engine    │
└──────────────┴──────────────┴───────────┘
```

每一层职责单一，通过 Go 接口解耦，便于测试和扩展。

## 1.4 核心设计原则

**接口驱动**：`DataSource` 接口定义了适配器的统一契约（Connect、Execute、HealthCheck、Close），新增数据源只需实现接口并注册工厂函数。

**适配器模式**：StarRocks 和 Prometheus 各自实现 `DataSource` 接口，通过 `AdapterRegistry` 注册。SQL 模板引擎通过独立的 `RawExecutor` 接口与 StarRocks Adapter 交互，实现接口隔离。

**中间件链**：基于 chi 的中间件链模式，每个中间件只关注一个横切关注点（认证、限流、压缩等），可灵活组合。

**防御性设计**：
- 熔断器防止级联故障
- 限流保护后端资源
- 缓存三重防护（穿透、雪崩、击穿）
- 信号量控制模板查询并发，防止连接池饿死

## 1.5 请求处理流程

1. 客户端发送 HTTP POST 到 `/graphql`
2. 中间件链依次处理：RequestID → 请求体大小检查 → CORS → CSRF → 认证 → 暴力破解防护 → 限流 → 压缩
3. gqlgen 解析请求体（支持单查询和批量查询），验证语法，执行复杂度和深度检查
4. Query Resolver 分析查询涉及的数据源，并行调度
5. DataLoader 将同一数据源的多个 resolver 请求批量合并
6. Cache Layer 查询缓存，未命中时通过 singleflight 确保同一 key 只回源一次
7. DataSource Manager 通过熔断器检查后执行查询，瞬时错误自动重试
8. 结果逐层返回，组装为 GraphQL JSON 响应

## 1.6 项目结构

```
mountainKing/
├── cmd/server/main.go          # 入口，依赖注入和启动编排
├── internal/
│   ├── adapter/                # 数据源适配器
│   │   ├── prometheus/         #   Prometheus HTTP API
│   │   └── starrocks/          #   StarRocks MySQL 协议
│   ├── audit/                  # 审计日志
│   ├── cache/                  # 缓存层（内存 LRU / Redis）
│   ├── config/                 # 配置加载、校验、热更新
│   ├── context/                # 请求上下文 key 定义
│   ├── datasource/             # DataSource 接口、Manager、熔断器、重连
│   ├── errors/                 # 统一错误码和错误类型
│   ├── graphql/                # GraphQL 层
│   │   ├── dataloader/         #   DataLoader 批量加载
│   │   ├── generated/          #   gqlgen 生成代码
│   │   ├── resolver/           #   Resolver 实现
│   │   ├── scalar/             #   自定义标量（DateTime、JSON）
│   │   └── schema/             #   .graphql Schema 文件
│   ├── health/                 # 健康检查
│   ├── middleware/             # HTTP 中间件
│   ├── observability/          # 指标、链路追踪、日志
│   ├── ratelimit/              # 限流实现（本地/分布式）
│   ├── sanitize/               # 敏感信息脱敏
│   ├── server/                 # HTTP 服务器、路由、优雅关闭
│   └── template/               # SQL 模板引擎
├── templates/                  # SQL 模板文件
├── deploy/                     # Docker、K8s、Prometheus、Grafana
├── official_document/          # 官方文档
├── course/                     # 课程材料（本目录）
├── config.yaml                 # 默认配置文件
└── go.mod                      # Go 模块定义
```

## 1.7 小结

mountainKing 通过分层架构和接口驱动设计，将 GraphQL 查询引擎、多数据源管理、安全、缓存、可观测性等关注点清晰分离。这种设计使得每个组件可以独立测试、替换和扩展，同时通过中间件链和防御性机制确保生产环境的稳定性。

---

下一模块：[环境搭建与快速上手](02-quick-start.md)
