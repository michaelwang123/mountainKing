# 数据源适配器

## 概述

本服务采用适配器模式接入数据源。所有适配器实现统一的 `DataSource` 接口，通过 `AdapterRegistry` 注册，由 `DataSourceManager` 统一管理生命周期。

当前内置三个适配器：
- **StarRocks** — OLAP 分析型数据库（通过 MySQL 协议）
- **Prometheus** — 时序数据库（通过 HTTP API）
- **ClickHouse** — OLAP 分析型数据库（通过原生 TCP 协议）

此外，StarRocks 适配器还实现了 `RawExecutor` 接口，供 SQL 模板引擎直接执行渲染后的 SQL 语句（绕过白名单和查询构建器）。

## StarRocks 适配器

### 连接方式

通过 MySQL 协议连接 StarRocks FE 节点，使用 `database/sql` + `go-sql-driver/mysql` 驱动。

### SQL 查询构建

GraphQL 查询参数自动转换为 SQL：

| GraphQL 参数 | SQL 映射 |
|-------------|---------|
| `fields` | `SELECT` 子句（字段选择优化） |
| `filters` | `WHERE` 子句 |
| `orderBy` | `ORDER BY` 子句 |
| `first`/`after`/`offset`/`limit` | `LIMIT`/`OFFSET` 子句 |
| `totalCount` 字段 | 额外 `COUNT(*)` 查询 |

所有 SQL 使用参数化查询（`?` 占位符），标识符使用反引号包裹。

### RawExecutor 接口

StarRocks 适配器额外实现了 `RawExecutor` 接口，供 SQL 模板引擎使用：

```go
type RawExecutor interface {
    ExecuteRaw(ctx context.Context, query string, args ...interface{}) (*QueryResult, error)
}
```

`ExecuteRaw` 复用现有 `*sql.DB` 连接池执行任意 SQL，不经过 `SQLQueryBuilder` 和白名单校验。此接口定义在 `internal/template/types.go` 中，实现接口隔离——模板引擎无法访问 `Execute`、`HealthCheck` 等 `DataSource` 接口方法。

### 白名单校验

StarRocks 适配器要求配置 `allowed_tables` 白名单（必填）。查询时校验：
- 表名必须在白名单中
- 字段名必须在对应表的 `columns` 列表中
- 标识符格式：仅允许 `[a-zA-Z0-9_]`

### 类型映射

| StarRocks 类型 | GraphQL 类型 |
|---------------|-------------|
| INT, BIGINT | Int |
| FLOAT, DOUBLE | Float |
| VARCHAR, STRING | String |
| BOOLEAN | Boolean |
| DECIMAL | String（保留精度） |
| DATETIME, DATE | DateTime（自定义标量） |
| JSON | JSON（自定义标量） |
| 其他 | String（兜底，记录警告日志） |

## Prometheus 适配器

### 连接方式

通过 Prometheus HTTP API（`/api/v1/query` 和 `/api/v1/query_range`）查询。连接验证使用 `GET /api/v1/status/buildinfo`。

### 查询模式

| 模式 | GraphQL 入口 | Prometheus API |
|------|-------------|---------------|
| 即时查询 | `prometheusInstant` | `/api/v1/query` |
| 范围查询 | `prometheusRange` | `/api/v1/query_range` |

### 参数转换

| GraphQL 参数 | PromQL 映射 |
|-------------|------------|
| `query` | PromQL 表达式 |
| `time` | 即时查询时间点 |
| `startTime`/`endTime` | 范围查询时间范围 |
| `step` | 范围查询步长 |
| `filters` | 标签匹配器（Label Matcher） |

### 类型映射

| Prometheus 类型 | GraphQL 类型 |
|----------------|-------------|
| scalar | Float |
| string | String |
| vector | PrometheusVector |
| matrix | PrometheusMatrix |

特殊值处理：`NaN` 和 `+Inf`/`-Inf` 转换为 GraphQL `null`，并在 `extensions.warnings` 中记录。

### 数据点限制

当返回数据量超过 `max_data_points` 配置时，返回错误提示，建议缩小时间范围或增大 step 值。

## ClickHouse Adapter

### Overview

The ClickHouse adapter connects to ClickHouse OLAP analytical databases via the native TCP protocol (port 9000). It uses the official `clickhouse-go/v2` driver (v2.46.0) through Go's `database/sql` interface, providing connection pooling, LZ4/ZSTD compression, and TLS support.

ClickHouse is architecturally similar to StarRocks — both are columnar OLAP engines with compatible SQL syntax. The adapter reuses the same patterns (whitelist validation, parameterized queries, circuit breaker integration) while handling ClickHouse-specific behaviors.

### Connection Configuration

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `connection.host` | string | localhost | ClickHouse server address (required) |
| `connection.port` | int | 9000 | Native TCP port (auto-switches to 9440 when secure=true) |
| `connection.username` | string | default | Username |
| `connection.password` | string | (empty) | Password |
| `connection.database` | string | default | Database name |

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `pool_size` | int | 10 | Maximum open connections (MaxOpenConns) |
| `max_idle_conns` | int | 5 | Maximum idle connections (MaxIdleConns) |
| `connection_timeout` | duration | 5s | Connection dial timeout |
| `read_timeout` | duration | 30s | Read timeout (set higher for long-running OLAP queries) |
| `conn_max_lifetime` | duration | 1h | Maximum connection lifetime |
| `secure` | bool | false | Enable TLS encryption |
| `compress` | string | lz4 | Compression algorithm: lz4, zstd, or none |

### TLS Port Linkage

When `secure: true` is set and the port is not explicitly configured (or is set to the default 9000), the adapter automatically switches to port 9440 (ClickHouse native TLS port):

```
secure=true  + port not set  → connects to port 9440
secure=true  + port=9000     → connects to port 9440 (auto-switch)
secure=true  + port=9440     → connects to port 9440 (explicit)
secure=true  + port=19000    → connects to port 19000 (custom, no auto-switch)
secure=false + port not set  → connects to port 9000 (default)
```

### Whitelist Configuration

The ClickHouse adapter requires an `allowed_tables` whitelist in the options section. Format is identical to StarRocks:

```yaml
options:
  allowed_tables:
    events:
      columns: [event_id, user_id, event_type, created_at, payload]
    metrics:
      columns: [timestamp, metric_name, value, tags]
```

Validation rules:
- Table name must be in the whitelist
- Column names must be in the corresponding table's `columns` list
- Identifier format: `^[a-zA-Z_][a-zA-Z0-9_]*$` (must start with a letter or underscore)

### Type Mapping

| ClickHouse Type | GraphQL Type | Notes |
|----------------|-------------|-------|
| Int8, Int16, Int32, UInt8, UInt16, UInt32 | Int | Fits in 32-bit signed range |
| Int64, UInt64, Int128, Int256, UInt128, UInt256 | String | Exceeds GraphQL Int 32-bit range |
| Float32, Float64, BFloat16 | Float | |
| String, FixedString | String | |
| Bool, Boolean | Boolean | |
| Decimal, Decimal32, Decimal64, Decimal128, Decimal256 | String | Preserves precision |
| Date, Date32, DateTime, DateTime64 | DateTime | Custom scalar |
| Time, Time64 | String | Time-of-day only (HH:MM:SS) |
| UUID | String | |
| IPv4, IPv6 | String | |
| Enum8, Enum16 | String | |
| Array, Tuple, Map, Nested, JSON, Variant, Dynamic | JSON | Custom scalar |
| Point, Ring, Polygon, MultiPolygon | JSON | Geo types |
| LowCardinality(T) | (maps T recursively) | Wrapper stripped |
| Nullable(T) | (maps T recursively) | Wrapper stripped |
| SimpleAggregateFunction(func, T) | (maps T recursively) | Maps inner type |
| AggregateFunction(...) | String | |
| Unknown type | String | Logs a warning |

Recursive type parsing is limited to 10 levels to prevent stack overflow from malformed type strings.

### Differences from StarRocks

| Aspect | StarRocks | ClickHouse |
|--------|-----------|------------|
| Protocol | MySQL protocol (port 9030) | Native TCP (port 9000/9440) |
| Driver | go-sql-driver/mysql | clickhouse-go/v2 v2.46.0 |
| Compression | None | LZ4 enabled by default |
| DELETE behavior | Synchronous, returns affected rows | Lightweight DELETE — `RowsAffected()` always returns 0 |
| Int64 mapping | Int (fits MySQL protocol) | String (exceeds GraphQL 32-bit Int) |
| INSERT performance | No special consideration | Batch insert recommended (1000+ rows per batch) |
| Identifier validation | `^[a-zA-Z0-9_]+$` | `^[a-zA-Z_][a-zA-Z0-9_]*$` (no leading digits) |
| TLS | N/A | Port auto-switches 9000→9440 when secure=true |

**Important behavioral notes:**

1. **DELETE RowsAffected=0** — ClickHouse lightweight DELETE (standard since v22.8) marks rows for deletion without immediately removing them physically. Background merges clean up marked rows later. Therefore, `RowsAffected()` always returns 0 for DELETE operations. This is by design, not a bug. INSERT operations return the actual row count normally.

2. **Int64→String** — ClickHouse integer types that exceed the GraphQL Int 32-bit signed range (Int64, UInt64, Int128, Int256, UInt128, UInt256) are mapped to GraphQL String to preserve precision. StarRocks maps BIGINT to Int because MySQL protocol returns these within a narrower range.

3. **Batch insert recommendation** — ClickHouse performs poorly with single-row INSERT statements. For write-heavy workloads, use the `insertBatch` mutation and insert 1000+ rows per batch for optimal performance.

### Usage Examples

**Configuration:**

```yaml
datasources:
  - name: analytics_ck
    type: clickhouse
    enabled: true
    connection:
      host: "${CK_HOST}"
      port: 9000
      username: "${CK_USERNAME}"
      password: "${CK_PASSWORD}"
      database: default
    options:
      pool_size: 20
      max_idle_conns: 10
      connection_timeout: 5s
      read_timeout: 30s
      conn_max_lifetime: 1h
      secure: false
      compress: lz4
      allowed_tables:
        events:
          columns: [event_id, user_id, event_type, created_at, payload]
        metrics:
          columns: [timestamp, metric_name, value, tags]
```

**GraphQL query:**

```graphql
query {
  analytics_ck(
    table: "events"
    filters: [
      { field: "event_type", operator: EQ, value: "purchase" }
      { field: "created_at", operator: GTE, value: "2024-01-01" }
    ]
    orderBy: [{ field: "created_at", direction: DESC }]
    first: 50
  ) {
    data {
      event_id
      user_id
      event_type
      created_at
      payload
    }
    totalCount
  }
}
```

**Mutation (INSERT):**

```graphql
mutation {
  insert_analytics_ck(
    table: "events"
    objects: [
      { event_id: "evt_001", user_id: "u123", event_type: "click", created_at: "2024-06-15T10:30:00Z" }
      { event_id: "evt_002", user_id: "u456", event_type: "purchase", created_at: "2024-06-15T10:31:00Z" }
    ]
  ) {
    affected_rows
  }
}
```

**Mutation (DELETE):**

```graphql
mutation {
  delete_analytics_ck(
    table: "events"
    where: { event_type: { _eq: "test" } }
  ) {
    affected_rows   # Always returns 0 for ClickHouse (lightweight delete behavior)
  }
}
```

## 扩展新数据源

添加新数据源适配器的步骤：

### 1. 实现 DataSource 接口

在 `internal/adapter/{adapter_name}/` 目录下创建适配器：

```go
package myadapter

import (
    "context"
    "github.com/michaelwang123/mountainKing/internal/datasource"
)

type MyAdapter struct {
    name   string
    config datasource.DataSourceConfig
}

func (a *MyAdapter) Name() string                    { return a.name }
func (a *MyAdapter) Type() string                    { return "myadapter" }
func (a *MyAdapter) Connect(ctx context.Context) error { /* ... */ }
func (a *MyAdapter) IsAvailable() bool               { /* ... */ }
func (a *MyAdapter) Execute(ctx context.Context, q datasource.QueryRequest) (*datasource.QueryResult, error) { /* ... */ }
func (a *MyAdapter) HealthCheck(ctx context.Context) error { /* ... */ }
func (a *MyAdapter) SchemaFiles() []string           { return []string{"internal/graphql/schema/myadapter.graphql"} }
func (a *MyAdapter) Close(ctx context.Context) error { /* ... */ }
```

### 2. 定义工厂函数

```go
func Factory(logger *zap.Logger) datasource.AdapterFactory {
    return func(name string, config datasource.DataSourceConfig) (datasource.DataSource, error) {
        return &MyAdapter{name: name, config: config}, nil
    }
}
```

### 3. 创建 GraphQL Schema 文件

在 `internal/graphql/schema/myadapter.graphql` 中定义数据类型。

### 4. 注册适配器

在 `cmd/server/main.go` 中注册：

```go
registry.Register("myadapter", myadapter.Factory(logger.Logger))
```

### 5. 添加配置

在 `config.yaml` 中添加数据源条目：

```yaml
datasources:
  - name: my_data
    type: myadapter
    enabled: true
    connection: { ... }
    options: { ... }
```

### 6. 重新生成代码

```bash
go run github.com/99designs/gqlgen generate
```

由于 gqlgen 采用编译时代码生成，Schema 合并发生在编译时。添加新适配器需要重新执行 `go generate` 和编译，不支持运行时热加载。

## 连接管理

### 连接池

每个数据源维护独立的连接池，可配置：
- `pool_size` — 连接池大小
- `connection_timeout` — 连接超时
- `query_timeout` — 查询超时
- `pool_acquire_timeout` — 连接获取超时（默认 5s）

### 重连策略

数据源不可用时自动执行指数退避重连：
- 初始间隔：`reconnect_interval`（默认 5s）
- 最大间隔：`max_reconnect_interval`（默认 60s）
- 退避公式：`min(reconnect_interval × 2^(attempt-1), max_reconnect_interval)`

### 熔断器

每个数据源独立的熔断器保护：
- 连续失败 ≥ `failure_threshold` → 进入 OPEN 状态（快速失败）
- 经过 `open_duration` → 进入 HALF_OPEN 状态（允许探测请求）
- 探测成功 ≥ `success_threshold` → 恢复 CLOSED 状态
- 探测失败 → 回到 OPEN 状态
