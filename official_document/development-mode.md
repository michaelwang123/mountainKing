# 开发模式指南

## 概述

本服务的 `config.yaml` 默认配置为生产模式，启用了 JWT 认证、Introspection 禁用等安全策略。在本地开发和调试时，你需要切换到开发模式并调整部分配置，才能顺利启动和使用服务。

本文档说明如何在本地以最小依赖启动服务，以及开发模式与生产模式的行为差异。

## 快速启动（无外部依赖）

本地开发时通常没有 StarRocks、Prometheus、Redis 等外部服务。以下命令通过环境变量覆盖关键配置，让服务在无外部依赖的情况下启动：

### Windows PowerShell

```powershell
$env:GRAPHQL_SERVER_MODE="development"
$env:GRAPHQL_AUTH_METHOD="none"
$env:GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED="true"
go run cmd/server/main.go
```

### Linux / macOS

```bash
export GRAPHQL_SERVER_MODE=development
export GRAPHQL_AUTH_METHOD=none
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
go run cmd/server/main.go
```

服务启动后监听 `:8080`。

## 为什么需要这些环境变量

| 环境变量 | 值 | 原因 |
|---------|---|------|
| `GRAPHQL_SERVER_MODE` | `development` | 启用 GraphQL Playground（`/playground`）和 GET 查询支持 |
| `GRAPHQL_AUTH_METHOD` | `none` | 禁用认证。设为 `none` 表示不启用任何认证方式。默认配置为 `jwt` 并指向公钥文件，本地不存在会导致启动 fatal |
| `GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED` | `true` | 启用 GraphQL Introspection 查询，Playground 和客户端工具需要它来获取 Schema 信息 |

## 启动后验证

### 健康检查

```bash
# 存活检查（始终可用）
curl http://localhost:8080/health

# 就绪检查（数据源连不上会返回 503，服务本身正常）
curl http://localhost:8080/ready

# Prometheus 指标
curl http://localhost:8080/metrics
```

### GraphQL Playground

浏览器打开 `http://localhost:8080/playground`，这是一个交互式 GraphQL 查询界面，可以直接编写和执行查询。

### 测试查询

在 Playground 或 curl 中尝试查询。由于数据源未连接，查询会返回 `DATASOURCE_UNAVAILABLE` 错误，但这证明 GraphQL 引擎和中间件链工作正常：

```bash
curl -X POST http://localhost:8080/graphql ^
  -H "Content-Type: application/json" ^
  -d "{\"query\": \"{ starrocks(table: \\\"orders\\\", first: 10) { totalCount } }\"}"
```

预期响应（数据源不可用时）：

```json
{
  "errors": [{
    "message": "datasource unavailable",
    "extensions": {
      "code": "DATASOURCE_UNAVAILABLE",
      "classification": "DATASOURCE"
    }
  }],
  "data": null
}
```

### Introspection 查询

验证 Schema 是否正确加载：

```bash
curl -X POST http://localhost:8080/graphql ^
  -H "Content-Type: application/json" ^
  -d "{\"query\": \"{ __schema { queryType { name } mutationType { name } } }\"}"
```

## 开发模式 vs 生产模式

| 行为 | 开发模式 (`development`) | 生产模式 (`production`) |
|------|------------------------|----------------------|
| GraphQL Playground | 启用（`/playground`） | 禁用（返回 404） |
| GET 查询 | 默认启用 | 默认禁用（CSRF 防护） |
| Introspection | 建议启用 | 建议禁用（防止 Schema 泄露） |
| 认证 | 可禁用方便调试 | 必须启用 |
| 日志级别 | 建议 `debug` | 建议 `info` |

## 完整集成环境（Docker Compose）

如果你想验证包括数据源在内的完整功能，使用 Docker Compose 启动集成环境：

```bash
cd deploy
docker compose up -d
```

这会启动：
- StarRocks FE/BE（端口 9030）
- Prometheus（端口 9090）
- Redis（端口 6379）
- API 服务（端口 8080，开发模式）

启动后所有数据源可用，可以执行真实的 GraphQL 查询。

## 常见启动问题

### `failed to init JWT authenticator`

原因：`auth.method` 配置为 `jwt`，但公钥文件不存在。

解决：设置 `GRAPHQL_AUTH_METHOD="none"` 禁用认证，或提供有效的公钥文件。

### 数据源连接失败的 WARN 日志

```
WARN: datasource "analytics_db" connect failed, marked as unavailable
```

这是预期行为。本地没有 StarRocks/Prometheus 时，服务会标记数据源为不可用并在后台自动重连。服务本身正常运行。

### Redis 连接失败

```
WARN: redis ping failed, features requiring Redis may degrade
```

Redis 是可选依赖。不可用时分布式限流自动降级为本地限流，Redis 缓存后端不可用。内存缓存和本地限流仍正常工作。

## 运行测试

```powershell
# 全部测试
go test ./...

# 带竞态检测和覆盖率
go test -race -coverprofile=coverage.out ./...

# 单个包
go test ./internal/config/...

# 基准测试
go test -bench=. -benchmem ./internal/server/
```

## 相关文档

- [快速开始](getting-started.md) — 环境准备和首次查询
- [配置参考](configuration.md) — 所有配置项详解
- [故障排查](troubleshooting.md) — 更多问题诊断
