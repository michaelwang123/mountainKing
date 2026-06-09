# 开发模式指南

## 概述

本服务的 `config.yaml` 默认配置为生产模式，启用了 JWT 认证、Introspection 禁用等安全策略。在本地开发和调试时，你需要切换到开发模式并调整部分配置，才能顺利启动和使用服务。

本文档说明如何在本地以最小依赖启动服务，以及开发模式与生产模式的行为差异。

## 快速启动（推荐：make dev）

项目提供了 `config.dev.yaml` 开发专用配置文件和 Makefile，让你无需设置任何环境变量或外部服务即可启动：

```bash
make dev
```

该命令等价于 `go run ./cmd/server -config config.dev.yaml`，服务在 2 秒内启动并监听 `:8080`。浏览器访问 `http://localhost:8080` 会自动重定向到 Playground 页面。

### config.dev.yaml 说明

`config.dev.yaml` 是专为本地开发设计的零配置文件，预设了以下关键选项：

| 配置项 | 值 | 说明 |
|--------|---|------|
| `server.mode` | `development` | 启用 Playground、GET 查询、启动 Banner |
| `auth.method` | `none` | 禁用认证，无需 JWT 公钥文件 |
| `datasources[0]` | `name: analytics_db, type: mock` | 使用内置 mock 适配器代替真实 StarRocks |
| `sql_templates.enabled` | `false` | 禁用 SQL 模板引擎（无外部数据源依赖） |
| `cache.backend` | `memory` | 使用内存缓存，无需 Redis |
| `rate_limit.mode` | `local` | 本地限流，无需 Redis |
| `tracing.enabled` | `false` | 禁用链路追踪 |
| `logging.level` | `debug` | 开发友好的日志级别 |

完整文件位于项目根目录 `config.dev.yaml`，你可以根据需要修改。

### 自定义配置文件路径

`-config` 命令行参数支持指定任意配置文件：

```bash
go run ./cmd/server -config path/to/your-config.yaml
```

优先级：CLI flag > `GRAPHQL_CONFIG_PATH` 环境变量 > 默认 `config.yaml`。

## Mock 数据源适配器

`config.dev.yaml` 配置了一个名为 `analytics_db` 的 mock 类型数据源。该适配器是内置的内存数据源，无需任何外部数据库连接，启动即可用。

### 工作原理

Mock 适配器实现了完整的 `datasource.DataSource` 接口，其行为如下：

- **Connect / HealthCheck** — 始终成功（无外部依赖）
- **IsAvailable** — 始终返回 `true`
- **Execute** — 从 `Options["table"]` 读取表名，返回内存中的预定义数据

由于 `analytics_db` 这个名称与现有 GraphQL `starrocks` query field 的路由逻辑匹配（`findStarRocksDS()` 返回 `"analytics_db"`），mock 适配器可以无缝接入现有的 GraphQL 查询链路，无需修改 schema。

### 预定义数据集

| 表名 | 行数 | 字段 |
|------|------|------|
| `demo_users` | 5 行 | id, name, email, role, created_at |
| `demo_orders` | 10 行 | id, user_id, amount, status, created_at |
| `demo_metrics` | 20 行 | timestamp, cpu_usage, memory_usage, request_count |

### 分页支持

Mock 适配器完整支持 `Limit`/`Offset` 分页参数和 `NeedCount`（返回 `TotalCount`），与真实数据源行为一致。

## 快速启动（环境变量方式）

如果你不想使用 `config.dev.yaml`，也可以通过环境变量覆盖默认 `config.yaml` 中的关键配置：

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

> **注意**：环境变量方式不会启用 mock 数据源，GraphQL 查询会返回 `DATASOURCE_UNAVAILABLE` 错误。如需完整的开发体验（包含可查询的示例数据），请使用 `make dev`。

## 环境变量说明

> **推荐**：使用 `make dev` 替代手动设置环境变量。以下信息仅供需要自定义启动方式的开发者参考。

| 环境变量 | 值 | 原因 |
|---------|---|------|
| `GRAPHQL_SERVER_MODE` | `development` | 启用 GraphQL Playground（`/playground`）和 GET 查询支持 |
| `GRAPHQL_AUTH_METHOD` | `none` | 禁用认证。设为 `none` 表示不启用任何认证方式。默认配置为 `jwt` 并指向公钥文件，本地不存在会导致启动 fatal |
| `GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED` | `true` | 启用 GraphQL Introspection 查询，Playground 和客户端工具需要它来获取 Schema 信息 |
| `GRAPHQL_CONFIG_PATH` | 文件路径 | 指定配置文件路径（优先级低于 `-config` CLI 参数） |

## Makefile 命令

| 命令 | 说明 |
|------|------|
| `make dev` | 使用 `config.dev.yaml` 启动开发服务器（mock 数据源、无认证） |
| `make build` | 编译生产二进制文件到 `bin/` |
| `make test` | 运行全部测试（竞态检测 + 覆盖率） |
| `make lint` | 运行 golangci-lint |
| `make clean` | 清理构建产物 |

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

> **提示**：Development mode 下访问 `http://localhost:8080`（根路径）会自动 302 重定向到 `/playground`，无需手动输入路径。

#### 预配置查询 Tab

使用 `make dev` 启动时，Playground 页面预置了 3 个示例查询 tab，可直接点击执行：

| Tab 名称 | 说明 | 预期结果 |
|----------|------|---------|
| 查询 demo_users | `starrocks(table: "demo_users")` 查询 | 返回 5 行用户数据 |
| 分页 demo_orders | `starrocks(table: "demo_orders", limit: 5, offset: 0)` | 返回前 5 行订单，含分页信息 |
| Schema Introspection | `__schema` 查询 | 返回完整 GraphQL Schema 结构 |

这些示例查询通过现有的 `starrocks` GraphQL field 路由到 mock 适配器，无需额外配置即可返回真实数据。

### 测试查询

使用 `make dev` 启动时，mock 数据源可直接返回数据：

```bash
curl -X POST http://localhost:8080/graphql ^
  -H "Content-Type: application/json" ^
  -d "{\"query\": \"{ starrocks(table: \\\"demo_users\\\", fields: [\\\"id\\\", \\\"name\\\", \\\"email\\\"]) { edges { node } totalCount } }\"}"
```

预期响应（mock 数据源正常时，返回示例数据）：

```json
{
  "data": {
    "starrocks": {
      "edges": [
        {"node": {"id": 1, "name": "Alice Chen", "email": "alice@example.com"}},
        {"node": {"id": 2, "name": "Bob Zhang", "email": "bob@example.com"}}
      ],
      "totalCount": 5
    }
  }
}
```

如果使用环境变量方式启动（未配置 mock 数据源），查询会返回 `DATASOURCE_UNAVAILABLE` 错误，但这证明 GraphQL 引擎和中间件链工作正常：

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
| GET / 重定向 | 302 → `/playground` | 无（返回 404） |
| GET 查询 | 默认启用 | 默认禁用（CSRF 防护） |
| Introspection | 建议启用 | 建议禁用（防止 Schema 泄露） |
| 认证 | 可禁用方便调试 | 必须启用 |
| 日志级别 | 建议 `debug` | 建议 `info` |
| sql_templates 降级 | 数据源不可用时 warn 并跳过 | 数据源不可用时 Fatal |
| 启动 Banner | 打印可视化 Banner 框 | JSON 格式日志行 |

## SQL 模板引擎优雅降级

在开发模式下，如果 `sql_templates.enabled = true` 但其配置的数据源不可用，服务会输出 warn 日志并继续启动（模板引擎功能不可用，模板查询返回错误），而不是像生产模式一样 Fatal 退出。

这使得开发者可以在 `config.dev.yaml` 中保持 `sql_templates.enabled: false`（默认），也可以改为 `true` 进行模板开发——即使数据源暂时连不上，服务本身仍能正常运行。

```
# 开发模式下的 warn 日志示例
WARN: sql_templates: configured datasource unavailable, template engine disabled
      datasource_name=analytics_db
```

在生产模式下，同样的情况会导致服务启动失败（Fatal），确保不会在数据源缺失时提供错误的查询结果。

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
