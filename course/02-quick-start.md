# 模块 02：环境搭建与快速上手

> 从零搭建开发环境，运行服务并执行第一个 GraphQL 查询。

## 2.1 前置依赖

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.25+ | 编译运行 |
| Git | 任意 | 代码管理 |
| Docker | 20+ | 容器化部署（可选） |
| StarRocks | 3.x | OLAP 数据源（可选） |
| Redis | 7+ | 分布式缓存/限流（可选） |

## 2.2 获取代码

```bash
git clone https://github.com/michaelwang123/mountainKing.git
cd mountainKing
go mod download
```

## 2.3 配置文件

项目使用 `config.yaml` 作为主配置文件，支持环境变量覆盖（`GRAPHQL_` 前缀）。

最小化开发配置：

```bash
# 开发模式（启用 Playground）
export GRAPHQL_SERVER_MODE=development

# 禁用认证（仅开发环境）
export GRAPHQL_AUTH_METHOD=none

# 启用 Schema 自省
export GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true
```

环境变量覆盖规则：`GRAPHQL_` + 配置路径（用 `_` 分隔层级）。例如：
- `server.port` → `GRAPHQL_SERVER_PORT`
- `logging.level` → `GRAPHQL_LOGGING_LEVEL`
- `cache.enabled` → `GRAPHQL_CACHE_ENABLED`

## 2.4 启动服务

```bash
# 直接运行
go run cmd/server/main.go

# 或使用 Makefile
make run
```

服务默认监听 `:8080`。开发模式下可访问：
- `http://localhost:8080/playground` — GraphQL Playground
- `http://localhost:8080/health` — 存活检查
- `http://localhost:8080/ready` — 就绪检查
- `http://localhost:8080/metrics` — Prometheus 指标

## 2.5 第一个查询

打开 Playground（`http://localhost:8080/playground`），输入：

```graphql
{
  starrocks(
    table: "nc_notification"
    fields: ["event_time", "channel", "status", "msg_id"]
    first: 5
    orderBy: [{ field: "event_time", direction: DESC }]
  ) {
    nodes { data }
    totalCount
  }
}
```

或使用 curl：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{
    "query": "{ starrocks(table: \"nc_notification\", fields: [\"event_time\", \"channel\", \"status\"], first: 5) { nodes { data } totalCount } }"
  }'
```

## 2.6 带认证的请求

生产环境需要认证。JWT 方式：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-jwt-token>" \
  -d '{"query": "{ starrocks(table: \"nc_notification\", first: 5) { nodes { data } } }"}'
```

API Key 方式：

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "X-API-Key: <your-api-key>" \
  -d '{"query": "{ starrocks(table: \"nc_notification\", first: 5) { nodes { data } } }"}'
```

## 2.7 Docker 快速启动

```bash
# 构建镜像
docker build -f deploy/Dockerfile -t mountainking:latest .

# 运行（开发模式）
docker run -p 8080:8080 \
  -e GRAPHQL_SERVER_MODE=development \
  -e GRAPHQL_AUTH_METHOD=none \
  mountainking:latest
```

使用 Docker Compose 启动完整开发环境（含 Prometheus、Grafana）：

```bash
docker compose -f deploy/docker-compose.dev.yaml up -d
```

## 2.8 构建与测试

```bash
# 编译
make build          # 输出到 bin/server

# 运行测试
make test           # go test -race -coverprofile=coverage.out ./...

# 代码检查
make lint           # golangci-lint run ./...
make vet            # go vet ./...

# 代码生成（修改 .graphql 文件后）
make generate       # go generate ./...
```

## 2.9 常见问题

**Q: 启动报 "datasource connection failed"**
A: 检查 StarRocks 连接配置。开发时可将 `datasources[0].enabled` 设为 `false` 跳过连接。

**Q: Playground 返回 404**
A: 确认 `GRAPHQL_SERVER_MODE=development`，生产模式下 Playground 被禁用。

**Q: 认证报错 "missing Authorization header"**
A: 设置 `GRAPHQL_AUTH_METHOD=none` 禁用认证（仅开发环境），或提供有效的 JWT/API Key。

---

下一模块：[GraphQL Schema 深入解析](03-graphql-schema.md)
