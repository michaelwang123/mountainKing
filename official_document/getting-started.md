# 快速开始

## 环境要求

- Go 1.25 或更高版本
- （可选）StarRocks 实例 — OLAP 数据查询
- （可选）Prometheus 实例 — 时序指标查询
- （可选）Redis — 分布式缓存和分布式限流

## 获取源码

```bash
git clone https://github.com/michaelwang123/mountainKing.git
cd mountainKing
```

## 安装依赖

```bash
go mod download
```

## 运行服务

```bash
go run cmd/server/main.go
```

服务默认监听 `:8080` 端口。

## 开发模式

默认配置为生产模式（JWT 认证、Introspection 禁用）。本地开发时需要切换到开发模式并禁用认证才能顺利启动。

### Windows PowerShell

```powershell
$env:GRAPHQL_SERVER_MODE="development"
$env:GRAPHQL_AUTH_METHOD=""
$env:GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED="true"
go run cmd/server/main.go
```

### Linux / macOS

```bash
export GRAPHQL_SERVER_MODE=development
export GRAPHQL_AUTH_METHOD=
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
go run cmd/server/main.go
```

开发模式下：
- GraphQL Playground 可通过 `http://localhost:8080/playground` 访问
- GET 查询默认启用
- 数据源连不上是正常的，服务会在后台自动重连

详细说明请参阅 [开发模式指南](development-mode.md)。

## 首次查询

### 使用 curl

```bash
# StarRocks 查询示例
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-jwt-token>" \
  -d '{
    "query": "{ starrocks(table: \"orders\", fields: [\"order_id\", \"amount\"], first: 10) { nodes { data } pageInfo { hasNextPage endCursor } totalCount } }"
  }'

# Prometheus 即时查询示例
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-jwt-token>" \
  -d '{
    "query": "{ prometheusInstant(query: \"up\") { resultType vectors { metric { name value } value { timestamp value } } } }"
  }'

# Prometheus 范围查询示例
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-jwt-token>" \
  -d '{
    "query": "{ prometheusRange(query: \"rate(http_requests_total[5m])\", startTime: \"2024-01-01T00:00:00Z\", endTime: \"2024-01-01T01:00:00Z\", step: \"60s\") { resultType matrices { metric { name value } values { timestamp value } } } }"
  }'
```

### 使用 GraphQL Playground

开发模式下访问 `http://localhost:8080/playground`，在左侧编辑器中输入查询语句即可。

## 健康检查

```bash
# 存活检查
curl http://localhost:8080/health

# 就绪检查（包含数据源连接状态）
curl http://localhost:8080/ready

# Prometheus 指标
curl http://localhost:8080/metrics
```

## 环境变量覆盖

所有配置项均可通过 `GRAPHQL_` 前缀的环境变量覆盖：

```bash
export GRAPHQL_SERVER_PORT=9090
export GRAPHQL_LOGGING_LEVEL=debug
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
```

详细配置说明请参阅 [配置参考](configuration.md)。

## Docker 快速启动

```bash
# 构建镜像
docker build -f deploy/Dockerfile -t graphql-api:latest .

# 运行容器
docker run -p 8080:8080 -v $(pwd)/config.yaml:/config.yaml graphql-api:latest
```

## Docker Compose 集成环境

使用 Docker Compose 启动完整的集成测试环境（包含 StarRocks、Prometheus、Redis）：

```bash
cd deploy
docker compose up -d
```

服务启动后可通过 `http://localhost:8080/graphql` 访问 API。

## 下一步

- [开发模式指南](development-mode.md) — 本地启动、调试、环境变量配置
- [配置参考](configuration.md) — 了解所有配置项
- [GraphQL API 参考](graphql-api.md) — 查看完整的 Schema 和查询示例
- [安全指南](security.md) — 配置认证和授权
