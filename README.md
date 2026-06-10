# MountainKing

> **零代码暴露安全的 GraphQL API** — 数据分析师和 BI 工程师只需编写 SQL 模板 + YAML 配置，即可将 StarRocks、Prometheus 等异构数据源统一为生产级 GraphQL 接口，无需掌握 GraphQL 开发知识。

[![CI](https://github.com/michaelwang123/mountainKing/actions/workflows/ci.yml/badge.svg)](https://github.com/michaelwang123/mountainKing/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/michaelwang123/mountainKing/branch/main/graph/badge.svg)](https://codecov.io/gh/michaelwang123/mountainKing)

[English](README_EN.md) | 中文 | [📖 在线文档](https://michaelwang123.github.io/mountainKing/)

基于 Go 语言的生产级 GraphQL API 服务器，提供跨多数据源的统一查询接口 — 当前支持 StarRocks（OLAP 分析型数据库）和 Prometheus（时序指标）。基于 gqlgen、chi 和完整的中间件栈构建，具备企业级安全性、可观测性和弹性能力。

## 功能特性

- 统一 GraphQL API，同时查询 StarRocks 和 Prometheus
- SQL 模板查询引擎 — 通过预定义的 Go `text/template` SQL 模板执行复杂的多表 JOIN、CTE 和窗口函数查询，具备完整的 SQL 注入防护
- Relay 游标分页和传统 offset/limit 分页
- Prometheus 即时查询和范围查询
- 跨数据源并行查询，部分失败隔离
- JWT 和 API Key 认证，支持按数据源授权
- 令牌桶限流（本地 + Redis 分布式，自动降级）
- 熔断器和指数退避重试
- 批量查询，可配置上限
- 基于 DataLoader 的 N+1 查询预防
- 内存（LRU）或 Redis 缓存，穿透/雪崩/击穿三重防护
- OpenTelemetry 分布式链路追踪（OTLP 导出到 Jaeger/Tempo）
- Prometheus 指标端点，支持自定义标签
- 结构化 JSON 日志（zap），审计日志
- 敏感信息脱敏（日志和链路追踪）
- YAML 配置热更新 + 环境变量覆盖（12-Factor 风格）
- SQL 模板热加载（fsnotify 文件监听 + GraphQL Mutation，无需重启）
- 优雅关闭（有序资源释放）
- CSRF 防护、CORS、gzip 压缩、请求体大小限制
- 认证失败暴力破解防护
- Kubernetes 就绪（存活/就绪探针、HPA）
- 多阶段 Docker 构建（distroless 基础镜像，非 root 运行）

## 前置要求

- Go 1.25+
- （可选）StarRocks 实例
- （可选）Prometheus 实例
- （可选）Redis — 用于分布式缓存和限流

## 快速开始（开发模式）

```bash
git clone https://github.com/michaelwang123/mountainKing.git
cd mountainKing
make dev
```

浏览器打开 http://localhost:8080 — GraphQL Playground 自动加载，内含示例查询可直接执行。

无需配置外部依赖。开发模式使用内存 Mock 数据源，开箱即用。

> 完整的入门指南（含 Docker 配置和生产模式说明），请查看 [快速入门指南](official_document/getting-started.md)。

### 生产模式启动

```bash
# 安装依赖
go mod download

# 使用默认 config.yaml（需要配置 StarRocks/Prometheus 等外部数据源）
go run ./cmd/server
```

服务默认监听 `:8080`。生产模式需配置认证和数据源连接，详见 [配置参考](official_document/configuration.md)。

## 配置

配置从 `config.yaml` 加载，支持 `GRAPHQL_` 前缀的环境变量覆盖（12-Factor 风格）。完整配置参考请查看 [配置参考文档](official_document/configuration.md)。

```bash
export GRAPHQL_SERVER_PORT=9090
export GRAPHQL_LOGGING_LEVEL=debug
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
```

运行时可热更新：日志级别、限流参数、缓存 TTL。

主要配置段：

| 配置段 | 说明 |
|--------|------|
| `server` | 端口、模式、超时、批量限制 |
| `graphql` | Schema 自省、复杂度/深度限制、最大结果行数 |
| `datasources` | StarRocks/Prometheus 连接配置和白名单 |
| `auth` | JWT（HS256/RS256/ES256）或 API Key 认证 |
| `rate_limit` | 本地或分布式（Redis）限流 |
| `cache` | 内存/Redis 后端、TTL、抖动、按数据源 TTL |
| `sql_templates` | SQL 模板引擎：目录、模板列表、参数、缓存 TTL |
| `tracing` | OpenTelemetry OTLP 导出（gRPC/HTTP） |
| `metrics` | 自定义 Prometheus 标签 |
| `circuit_breaker` | 失败阈值、熔断持续时间 |
| `retry` | 最大重试次数、指数退避 |
| `cors` | 跨域资源共享 |
| `compression` | gzip 响应压缩 |
| `logging` | 日志级别、格式、审计日志 |
| `sanitization` | 基于正则的敏感信息脱敏 |
| `shutdown` | 优雅关闭最大等待时间 |

## GraphQL Schema

### 查询

```graphql
# StarRocks OLAP 查询（支持过滤、排序、Relay 分页）
starrocks(table: String!, fields: [String!], filters: [StarRocksFilter!],
          orderBy: [StarRocksOrderBy!], first: Int, after: String,
          offset: Int, limit: Int): StarRocksConnection!

# SQL 模板查询 — 执行预定义的 SQL 模板
templateQuery(templateName: String!, parameters: JSON, fields: [String!],
              first: Int, offset: Int, orderBy: [TemplateOrderBy!]): TemplateQueryConnection!

# 列出所有已注册的 SQL 模板
templateList(first: Int, offset: Int): [TemplateInfo!]!

# Prometheus 即时查询
prometheusInstant(query: String!, time: DateTime,
                  filters: [PrometheusLabelFilter!]): PrometheusInstantResult!

# Prometheus 范围查询
prometheusRange(query: String!, startTime: DateTime!, endTime: DateTime!,
                step: String!, filters: [PrometheusLabelFilter!]): PrometheusRangeResult!
```

### Mutation

```graphql
# 清除缓存（全部或按数据源）。需要 mutation 权限。
clearCache(datasource: String): Boolean!

# 重新加载 SQL 模板。需要 mutation 权限。
reloadTemplates: ReloadTemplatesResult!
```

### 端点

| 路径 | 方法 | 说明 |
|------|------|------|
| `/graphql` | POST | GraphQL 查询端点 |
| `/graphql` | GET | GraphQL 查询（需配置 `allow_get_queries`） |
| `/playground` | GET | GraphiQL 交互式界面（仅开发模式） |
| `/health` | GET | 存活检查（200/503） |
| `/ready` | GET | 就绪探针，含数据源状态（200/503） |
| `/metrics` | GET | Prometheus 指标 |

详细的查询示例、分页方式、错误码等，请查看 [GraphQL API 参考](official_document/graphql-api.md)。

## 项目结构

```
cmd/server/              入口（main.go）
internal/
  adapter/
    prometheus/          Prometheus 适配器、查询构建器、类型映射、校验器
    starrocks/           StarRocks 适配器、查询构建器、类型映射、白名单
  audit/                 审计日志
  cache/                 缓存接口、内存/Redis 后端、缓存层、Key 生成
  config/                YAML 配置加载、校验、热更新（fsnotify）
  context/               上下文 Key 定义（RequestID、AuthIdentity、TraceID）
  datasource/            DataSource 接口、Manager、Registry、熔断器、重连
  errors/                统一错误码（AUTH_*、VALIDATION_*、DATASOURCE_* 等）
  graphql/
    dataloader/          Per-request DataLoader 批量加载
    generated/           gqlgen 生成代码
    resolver/            Query 和 Mutation 解析器
    scalar/              自定义标量（DateTime、JSON）
    schema/              GraphQL Schema 文件（.graphql）
  health/                健康检查和就绪探针
  middleware/            认证、授权、限流、CORS、CSRF、压缩、请求体限制
  observability/         Prometheus 指标、OpenTelemetry 追踪、结构化日志
  ratelimit/             限流器接口、本地/分布式/降级实现
  sanitize/              敏感信息脱敏
  server/                HTTP 服务器、路由、优雅关闭、批量查询处理
  template/              SQL 模板引擎：类型、加载器、注册表、渲染器、校验器、
                         脱敏器、函数映射、分页、缓存、引擎、监听器、指标
pkg/
  retry/                 重试逻辑（错误分类器 + 指数退避）
deploy/
  Dockerfile             多阶段构建（distroless，非 root）
  docker-compose.yaml    集成测试环境
  k8s/                   Kubernetes 清单（Deployment、Service、ConfigMap、HPA）
  prometheus.yml         Prometheus 抓取配置
templates/               SQL 模板文件目录（通过 sql_templates.base_dir 配置）
  _shared/               共享模板片段（通过 {{template}} 引用）
  fleet/                 车队报表模板
  driver/                驾驶员评分模板
```

## 文档

完整的项目文档位于 [`official_document/`](official_document/) 目录：

| 文档 | 说明 |
|------|------|
| [架构概览](official_document/architecture.md) | 系统架构、组件关系、请求流程 |
| [快速入门](official_document/getting-started.md) | 环境搭建、构建、运行、首次查询 |
| [配置参考](official_document/configuration.md) | 所有配置项、环境变量覆盖、热更新 |
| [GraphQL API 参考](official_document/graphql-api.md) | Schema、查询、Mutation、分页、错误码 |
| [安全指南](official_document/security.md) | 认证、授权、限流、输入校验 |
| [数据源适配器](official_document/datasource-adapters.md) | StarRocks/Prometheus 详解和扩展指南 |
| [可观测性](official_document/observability.md) | Prometheus 指标、OpenTelemetry 追踪、结构化日志 |
| [部署指南](official_document/deployment.md) | Docker、Kubernetes、CI/CD、生产检查清单 |
| [性能调优](official_document/performance.md) | 缓存、连接池、熔断器、基准测试 |
| [开发者指南](official_document/developer-guide.md) | 项目结构、代码规范、测试、贡献指南 |
| [错误码参考](official_document/error-reference.md) | 完整错误码、HTTP 状态映射、客户端处理 |
| [故障排查](official_document/troubleshooting.md) | 常见问题诊断和解决方案 |
| [FAQ](official_document/faq.md) | 常见问题解答 |

相关链接：[CONTRIBUTING.md](CONTRIBUTING.md) · [CHANGELOG.md](CHANGELOG.md) · [系统课程](course/index.md)

## 代码生成

GraphQL 代码使用 [gqlgen](https://gqlgen.com/) 生成：

```bash
go run github.com/99designs/gqlgen generate
```

## 测试

```bash
# 单元测试
go test ./...

# 带竞态检测和覆盖率
go test -race -coverprofile=coverage.out ./...

# 基准测试
go test -bench=. -benchmem ./internal/server/
```

属性测试使用 [rapid](https://pkg.go.dev/pgregory.net/rapid) 框架，每个属性 100+ 次迭代。

## Docker

```bash
# 构建
docker build -f deploy/Dockerfile \
  --build-arg VERSION=$(git describe --tags) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t graphql-api:latest .

# 运行
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml graphql-api:latest
```

## Kubernetes

```bash
kubectl apply -f deploy/k8s/
```

包含 Deployment（含启动/存活/就绪探针）、Service、ConfigMap 和 HPA（基于 `graphql_requests_in_flight` 自定义指标）。

## 开源协议

详见 [LICENSE](LICENSE)。
