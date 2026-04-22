# 开发者指南

## 项目结构

```
graphql-api/
├── cmd/server/              入口程序
│   └── main.go              初始化链：config → logging → tracing → adapters → server
├── internal/
│   ├── adapter/
│   │   ├── prometheus/      Prometheus 适配器、查询构建器、类型映射、输入校验
│   │   └── starrocks/       StarRocks 适配器、查询构建器、类型映射、白名单
│   ├── audit/               审计日志
│   ├── cache/               缓存接口、内存/Redis 后端、Cache Layer、Key 生成、查询规范化
│   ├── config/              配置结构定义、加载、校验、热更新
│   ├── context/             Context key 定义
│   ├── datasource/          DataSource 接口、Manager、Registry、熔断器、重连、Mock
│   ├── errors/              统一错误码和错误类型
│   ├── graphql/
│   │   ├── dataloader/      DataLoader（per-request 批量合并）
│   │   ├── generated/       gqlgen 生成代码（勿手动编辑）
│   │   ├── resolver/        GraphQL Resolver 实现
│   │   ├── scalar/          自定义标量类型（DateTime, JSON）
│   │   └── schema/          GraphQL Schema 文件（.graphql）
│   ├── health/              健康检查与就绪探针
│   ├── middleware/          HTTP 中间件（认证、授权、限流、CORS、压缩等）
│   ├── observability/       Prometheus 指标、OpenTelemetry 链路追踪、结构化日志
│   ├── ratelimit/           限流器接口、本地/分布式实现、降级包装器
│   ├── redis/               共享 Redis 客户端
│   ├── sanitize/            敏感信息脱敏
│   ├── server/              HTTP 服务器、路由、优雅关闭、批量查询
│   └── template/            SQL 模板引擎（类型、加载器、注册表、渲染器、校验器、
│                            安全检查器、安全函数、分页、缓存、引擎、监听器、指标）
├── pkg/
│   └── retry/               通用重试逻辑、错误分类器
├── deploy/
│   ├── Dockerfile           多阶段构建
│   ├── docker-compose.yaml  集成测试环境
│   ├── k8s/                 Kubernetes 部署清单
│   └── prometheus.yml       Prometheus 配置
├── config.yaml              配置文件
├── gqlgen.yml               gqlgen 代码生成配置
├── .golangci.yml            golangci-lint 配置
└── .github/workflows/       CI/CD 流水线
```

## 代码规范

### Go 标准

- 遵循 Go 标准项目布局：`cmd/`、`internal/`、`pkg/`
- 所有导出函数、类型和接口包含 GoDoc 注释
- 使用 `fmt.Errorf` + `%w` 进行 error wrapping
- 接口优先设计，核心组件通过接口解耦

### Lint

使用 golangci-lint，启用的 linter：
- govet
- errcheck
- staticcheck

```bash
golangci-lint run ./...
```

## GraphQL 代码生成

Schema 文件位于 `internal/graphql/schema/*.graphql`，使用 gqlgen 生成代码：

```bash
go run github.com/99designs/gqlgen generate
```

生成的文件（勿手动编辑）：
- `internal/graphql/generated/generated.go`
- `internal/graphql/generated/models_gen.go`

自定义标量映射在 `gqlgen.yml` 中配置：

```yaml
models:
  DateTime:
    model:
      - github.com/michaelwang123/mountainKing/internal/graphql/scalar.DateTime
  JSON:
    model:
      - github.com/michaelwang123/mountainKing/internal/graphql/scalar.JSON
```

## 测试策略

### 单元测试

```bash
go test ./...
```

覆盖率目标：≥ 70%（核心组件 ≥ 80%）。

### 属性测试（Property-Based Testing）

使用 `pgregory.net/rapid` 库，每个属性测试最少运行 100 次迭代。

属性测试覆盖的关键领域：
- 配置校验（拒绝无效值、环境变量覆盖）
- SQL/PromQL 查询构建（参数化、注入防护）
- SQL 模板安全函数（safeString、quote、safeInt 等转义正确性）
- SQL 模板渲染（确定性、超时保护、长度限制）
- SQL 模板参数校验（类型匹配、必填检查、枚举/长度/正则约束）
- SQL 安全检查器（多语句检测、注释移除、Hint 保留、引号安全）
- 模板热加载（原子性、错误隔离、并发安全、hash 追踪）
- 类型映射（StarRocks/Prometheus）
- 缓存行为（Key 确定性、穿透/雪崩/击穿防护）
- 认证授权（JWT/API Key、权限隔离）
- 限流（令牌桶、分布式降级）
- 熔断器状态转换
- 健康检查状态码
- 链路追踪 Span 属性

### 集成测试

使用 Docker Compose 编排依赖服务：

```bash
cd deploy
docker compose up -d
go test -tags=integration ./...
```

### 基准测试

```bash
go test -bench=. -benchmem ./internal/server/
```

## 依赖管理

核心依赖：

| 包 | 用途 |
|---|------|
| `github.com/99designs/gqlgen` | GraphQL 框架 |
| `github.com/go-chi/chi/v5` | HTTP 路由 |
| `github.com/spf13/viper` | 配置管理 |
| `go.uber.org/zap` | 结构化日志 |
| `github.com/golang-jwt/jwt/v5` | JWT 认证 |
| `github.com/go-sql-driver/mysql` | StarRocks 连接 |
| `github.com/prometheus/client_golang` | Prometheus 指标 |
| `github.com/redis/go-redis/v9` | Redis 客户端 |
| `go.opentelemetry.io/otel` | OpenTelemetry SDK |
| `github.com/hashicorp/golang-lru/v2` | LRU 缓存 |
| `github.com/cespare/xxhash/v2` | 高性能哈希 |
| `golang.org/x/sync` | Singleflight |
| `golang.org/x/time` | 令牌桶限流 |
| `pgregory.net/rapid` | 属性测试 |

## 贡献流程

1. Fork 仓库
2. 创建特性分支：`git checkout -b feature/my-feature`
3. 编写代码和测试
4. 确保通过 lint 和测试：
   ```bash
   golangci-lint run ./...
   go test -race ./...
   ```
5. 提交 Pull Request

### 添加新数据源适配器

详见 [数据源适配器 — 扩展新数据源](datasource-adapters.md#扩展新数据源)。

### 添加新中间件

1. 在 `internal/middleware/` 创建中间件文件
2. 实现 `func(http.Handler) http.Handler` 签名
3. 在 `cmd/server/main.go` 的中间件链中注册
4. 编写单元测试和属性测试

## 错误处理

### 错误码体系

格式：`{CATEGORY}_{ERROR_NAME}`

| 分类 | 前缀 | 示例 |
|------|------|------|
| 认证授权 | `AUTH_` | `AUTH_TOKEN_EXPIRED`, `AUTH_INSUFFICIENT_PERMISSION` |
| 请求验证 | `VALIDATION_` | `VALIDATION_SYNTAX_ERROR`, `VALIDATION_COMPLEXITY_EXCEEDED` |
| 数据源 | `DATASOURCE_` | `DATASOURCE_TIMEOUT`, `DATASOURCE_UNAVAILABLE` |
| 限流 | `RATELIMIT_` | `RATELIMIT_EXCEEDED` |
| 内部 | `INTERNAL_` | `INTERNAL_UNEXPECTED` |

### 错误分类

重试逻辑区分两类错误：
- 瞬时错误（自动重试）：连接超时、ECONNREFUSED、ECONNRESET、io.EOF
- 业务错误（立即返回）：SQL 语法错误、PromQL 语法错误
