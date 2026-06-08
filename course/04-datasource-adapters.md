# 模块 04：数据源适配器详解

> 深入理解 DataSource 接口、StarRocks/Prometheus 适配器实现，以及如何扩展新数据源。

## 4.1 DataSource 接口

所有数据源适配器必须实现 `DataSource` 接口（`internal/datasource/interface.go`）：

```go
type DataSource interface {
    Name() string                                    // 数据源名称
    Type() string                                    // 类型标识（starrocks/prometheus）
    Connect(ctx context.Context) error               // 建立连接
    IsAvailable() bool                               // 是否可用
    Execute(ctx context.Context, query QueryRequest) (*QueryResult, error)  // 执行查询
    HealthCheck(ctx context.Context) error           // 健康检查
    SchemaFiles() []string                           // 提供的 .graphql 文件
    Close(ctx context.Context) error                 // 关闭连接
}
```

这个接口是 mountainKing 多数据源架构的核心契约。

## 4.2 适配器注册

适配器通过工厂函数注册到 `AdapterRegistry`：

```go
type AdapterFactory func(name string, config DataSourceConfig) (DataSource, error)
```

`DataSourceManager` 在启动时遍历配置中的数据源列表，根据 `type` 字段查找对应工厂，创建适配器实例并调用 `Connect()`。

## 4.3 StarRocks 适配器

StarRocks 使用 MySQL 协议，适配器基于 `database/sql` + `go-sql-driver/mysql`。

核心能力：
- **参数化查询**：所有用户输入通过 SQL 参数绑定，防止注入
- **白名单校验**：表名和列名必须在配置的 `allowed_tables` 中
- **连接池管理**：`pool_size`、`connection_timeout`、`pool_acquire_timeout`
- **SQL 构建**：`SQLQueryBuilder` 根据 `QueryRequest` 生成 SELECT 语句

### 白名单配置

```yaml
datasources:
  - name: analytics_db
    type: starrocks
    options:
      allowed_tables:
        nc_notification:
          columns: [event_time, channel, status, msg_id, ...]
        consent_event:
          columns: [sign_time, oneid, consent_key, ...]
```

查询未在白名单中的表或列会返回验证错误。这是 StarRocks 适配器的核心安全机制。

### ExecuteRaw 接口

SQL 模板引擎通过 `RawExecutor` 接口与 StarRocks Adapter 交互：

```go
type RawExecutor interface {
    ExecuteRaw(ctx context.Context, query string, args ...any) (*QueryResult, error)
}
```

`ExecuteRaw` 复用 Adapter 的连接池，但不经过 `SQLQueryBuilder` 和白名单校验（因为模板 SQL 已经过模板引擎自身的安全检查）。这种接口隔离确保模板引擎无法调用 Adapter 的其他方法。

## 4.4 Prometheus 适配器

Prometheus 适配器通过 HTTP API 查询时序数据。

支持两种查询类型：
- **即时查询**（Instant Query）：查询某一时刻的指标值
- **范围查询**（Range Query）：查询时间范围内的指标序列

```yaml
datasources:
  - name: monitoring
    type: prometheus
    connection:
      url: http://prometheus:9090
    options:
      query_timeout: 15s
      max_data_points: 11000
```

### 标签过滤

Prometheus 查询支持标签匹配：

```graphql
{
  prometheusInstant(
    query: "http_requests_total"
    filters: [
      { label: "method", matchType: EXACT, value: "GET" }
      { label: "status", matchType: REGEX, value: "2.." }
    ]
  ) {
    metric { labels }
    value
    timestamp
  }
}
```

匹配类型：`EXACT`（=）、`NOT_EQUAL`（!=）、`REGEX`（=~）、`NOT_REGEX`（!~）

## 4.5 跨数据源并行查询

当一个 GraphQL 请求同时查询多个数据源时，Query Resolver 会并行调度：

```graphql
{
  # 这两个查询会并行执行
  starrocks(table: "nc_notification", first: 10) {
    nodes { data }
  }
  prometheusInstant(query: "up") {
    metric { labels }
    value
  }
}
```

部分失败隔离：一个数据源查询失败不会影响其他数据源的结果返回。

## 4.6 DataSource Manager

`DataSourceManager` 负责数据源的生命周期管理：

- **启动**：按配置创建适配器、建立连接
- **查询路由**：根据数据源名称分发查询
- **健康检查**：定期检查各数据源可用性
- **重连**：连接断开后自动指数退避重连
- **关闭**：优雅关闭时按序释放所有连接

## 4.7 扩展新数据源

添加新数据源的步骤：

1. 在 `internal/adapter/` 下创建新目录（如 `clickhouse/`）
2. 实现 `DataSource` 接口
3. 编写对应的 `.graphql` Schema 文件
4. 注册工厂函数到 `AdapterRegistry`
5. 在 `config.yaml` 中添加数据源配置
6. 运行 `make generate` 重新生成 GraphQL 代码

```go
// 示例：注册新适配器
registry.Register("clickhouse", func(name string, cfg DataSourceConfig) (DataSource, error) {
    return NewClickHouseAdapter(name, cfg)
})
```

## 4.8 连接池与超时配置

StarRocks 连接池关键参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `pool_size` | 20 | 最大连接数 |
| `connection_timeout` | 5s | 建立连接超时 |
| `query_timeout` | 30s | 查询执行超时 |
| `pool_acquire_timeout` | 5s | 从池中获取连接超时 |
| `reconnect_interval` | 5s | 初始重连间隔 |
| `max_reconnect_interval` | 60s | 最大重连间隔（指数退避上限） |

---

下一模块：[SQL 模板引擎实战](05-sql-template-engine.md)
