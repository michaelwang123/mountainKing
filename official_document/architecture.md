# 架构概览

## 设计理念

GraphQL Multi-DataSource API 采用分层架构设计，核心理念包括：

- **接口驱动**：所有核心组件通过 Go 接口定义解耦，便于测试和扩展
- **适配器模式**：数据源通过统一的 `DataSource` 接口接入，新增数据源只需实现接口并注册
- **中间件链**：HTTP 请求处理采用 chi 中间件链模式，职责清晰、可组合
- **防御性设计**：熔断器、限流、缓存防护等机制确保系统在异常情况下的稳定性

## 整体架构

```
┌─────────────┐
│   客户端     │
└──────┬──────┘
       │ HTTP POST /graphql
       ▼
┌──────────────────────────────────────────────────────┐
│                    API Service                        │
│                                                      │
│  ┌────────────────────────────────────────────────┐  │
│  │              中间件层 (Middleware)               │  │
│  │  RequestID → BodyLimit → CORS → CSRF →         │  │
│  │  Auth → AuthFailureLimiter → RateLimit →       │  │
│  │  Compression                                    │  │
│  └────────────────────┬───────────────────────────┘  │
│                       ▼                              │
│  ┌────────────────────────────────────────────────┐  │
│  │           GraphQL Engine (gqlgen)               │  │
│  │     复杂度检查 · 深度检查 · Schema 验证          │  │
│  └────────────────────┬───────────────────────────┘  │
│                       ▼                              │
│  ┌────────────────────────────────────────────────┐  │
│  │            Query Resolver                       │  │
│  │     字段选择优化 · 并行调度多数据源查询           │  │
│  └────────────────────┬───────────────────────────┘  │
│                       ▼                              │
│  ┌────────────────────────────────────────────────┐  │
│  │             DataLoader                          │  │
│  │     Per-Request 实例 · 批量合并同数据源请求       │  │
│  └────────────────────┬───────────────────────────┘  │
│                       ▼                              │
│  ┌────────────────────────────────────────────────┐  │
│  │             Cache Layer                         │  │
│  │  Singleflight · 穿透防护 · 雪崩防护 · 击穿防护   │  │
│  └────────────────────┬───────────────────────────┘  │
│                       ▼                              │
│  ┌────────────────────────────────────────────────┐  │
│  │          DataSource Manager                     │  │
│  │   连接池 · 熔断器 · 指数退避重连 · 重试           │  │
│  └───────┬────────────────────────┬───────────────┘  │
│          ▼                        ▼                  │
│   ┌─────────────┐         ┌──────────────┐          │
│   │  StarRocks   │         │  Prometheus   │          │
│   │  Adapter     │         │  Adapter      │          │
│   │ (MySQL协议)  │         │ (HTTP API)    │          │
│   └─────────────┘         └──────────────┘          │
└──────────────────────────────────────────────────────┘
       │              │              │
       ▼              ▼              ▼
  ┌─────────┐  ┌───────────┐  ┌──────────┐
  │ Jaeger/ │  │Prometheus │  │   Redis   │
  │ Tempo   │  │  Server   │  │(可选)     │
  │ (OTLP)  │  │(/metrics) │  │          │
  └─────────┘  └───────────┘  └──────────┘
```

## 请求处理流程

1. 客户端发送 HTTP POST 请求到 `/graphql`
2. 中间件链依次处理：生成 RequestID → 请求体大小检查 → CORS → CSRF 防护 → 认证 → 暴力破解防护 → 限流 → 压缩
3. GraphQL Engine 解析请求体（支持单查询和批量查询），验证查询语法，执行复杂度和深度检查
4. Query Resolver 分析查询涉及的数据源，并行调度多数据源查询
5. DataLoader 将同一数据源的多个 resolver 请求批量合并
6. Cache Layer 查询缓存，未命中时通过 singleflight 确保同一 key 只回源一次
7. DataSource Manager 通过熔断器检查后执行查询，瞬时错误自动重试
8. 结果逐层返回，最终组装为 GraphQL JSON 响应

## 核心组件

### DataSource 接口

所有数据源适配器实现的统一接口，包含 7 个方法：

| 方法 | 说明 |
|------|------|
| `Name()` | 返回数据源名称 |
| `Type()` | 返回数据源类型标识 |
| `Connect(ctx)` | 建立连接（幂等） |
| `IsAvailable()` | 检查是否可用 |
| `Execute(ctx, query)` | 执行查询 |
| `HealthCheck(ctx)` | 健康检查 |
| `Close(ctx)` | 关闭连接 |

### Adapter Registry

适配器注册表，通过类型名称注册和查找适配器工厂函数。支持：
- `Register(typeName, factory)` — 注册适配器，重复注册返回错误
- `Get(typeName)` — 按类型名称查找
- `List()` — 列出所有已注册类型

### DataSource Manager

管理所有数据源的生命周期：
- 启动时从配置初始化数据源，失败的标记为不可用
- 后台指数退避重连（初始 5s，最大 60s）
- 熔断器保护（CLOSED → OPEN → HALF_OPEN 状态机）
- 查询执行带自动重试（瞬时错误重试，业务错误立即返回）

### 熔断器状态机

```
         连续失败 ≥ threshold
CLOSED ─────────────────────────► OPEN
  ▲                                 │
  │ 连续成功 ≥ success_threshold    │ open_duration 到期
  │                                 ▼
  └──────────────────────────── HALF_OPEN
         任一探测失败 ──────────► OPEN
```

## 超时机制

采用双层超时控制：

- **请求级超时**：`context.WithTimeout(ctx, request_timeout)` 作为所有操作的父 context
- **查询级超时**：`context.WithTimeout(parentCtx, min(query_timeout, 剩余时间))` 确保单个数据源查询不超过自身超时，也不超过请求总超时

## 优雅关闭

收到 SIGTERM/SIGINT 后按以下顺序执行：

1. 停止接受新连接
2. 等待 in-flight 请求完成（最长 `max_wait_time`，默认 30s）
3. TracingProvider.Shutdown（独立 5s 超时）
4. 刷新 Prometheus 指标
5. DataSourceManager.CloseAll（关闭所有数据源连接池）
6. Logger.Sync（刷新日志缓冲区）

## 技术栈

| 组件 | 技术选型 | 理由 |
|------|---------|------|
| GraphQL 框架 | gqlgen | Go 生态最成熟的 Schema-first 框架，编译时代码生成 |
| HTTP 路由 | chi | 轻量级、兼容 net/http、中间件生态丰富 |
| 配置管理 | Viper | YAML + 环境变量覆盖 + 热更新 |
| 日志 | zap | 高性能结构化日志 |
| JWT | golang-jwt/jwt/v5 | 支持 HS256/RS256/ES256 多种签名算法 |
| StarRocks 连接 | database/sql + go-sql-driver/mysql | StarRocks 兼容 MySQL 协议 |
| Prometheus 查询 | net/http | 通过 HTTP API 查询 |
| Prometheus 指标 | prometheus/client_golang | 官方 Go 客户端 |
| 链路追踪 | go.opentelemetry.io/otel | 官方 OpenTelemetry Go SDK |
| 内存缓存 | hashicorp/golang-lru/v2 | 标准 LRU 淘汰策略 |
| 缓存 Key 哈希 | cespare/xxhash/v2 | 非密码学高性能哈希 |
| 并发控制 | golang.org/x/sync/singleflight | 防止缓存击穿 |
| 限流 | golang.org/x/time/rate | 标准令牌桶实现 |
| Redis | go-redis/redis/v9 | 分布式缓存和限流 |
