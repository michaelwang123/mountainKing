# 数据源适配�?

## 概述

本服务采用适配器模式接入数据源。所有适配器实现统一�?`DataSource` 接口，通过 `AdapterRegistry` 注册，由 `DataSourceManager` 统一管理生命周期�?

当前内置两个适配器：
- **StarRocks** �?OLAP 分析型数据库（通过 MySQL 协议�?
- **Prometheus** �?时序数据库（通过 HTTP API�?

## StarRocks 适配�?

### 连接方式

通过 MySQL 协议连接 StarRocks FE 节点，使�?`database/sql` + `go-sql-driver/mysql` 驱动�?

### SQL 查询构建

GraphQL 查询参数自动转换�?SQL�?

| GraphQL 参数 | SQL 映射 |
|-------------|---------|
| `fields` | `SELECT` 子句（字段选择优化�?|
| `filters` | `WHERE` 子句 |
| `orderBy` | `ORDER BY` 子句 |
| `first`/`after`/`offset`/`limit` | `LIMIT`/`OFFSET` 子句 |
| `totalCount` 字段 | 额外 `COUNT(*)` 查询 |

所�?SQL 使用参数化查询（`?` 占位符），标识符使用反引号包裹�?

### 白名单校�?

StarRocks 适配器要求配�?`allowed_tables` 白名单（必填）。查询时校验�?
- 表名必须在白名单�?
- 字段名必须在对应表的 `columns` 列表�?
- 标识符格式：仅允�?`[a-zA-Z0-9_]`

### 类型映射

| StarRocks 类型 | GraphQL 类型 |
|---------------|-------------|
| INT, BIGINT | Int |
| FLOAT, DOUBLE | Float |
| VARCHAR, STRING | String |
| BOOLEAN | Boolean |
| DECIMAL | String（保留精度） |
| DATETIME, DATE | DateTime（自定义标量�?|
| JSON | JSON（自定义标量�?|
| 其他 | String（兜底，记录警告日志�?|

## Prometheus 适配�?

### 连接方式

通过 Prometheus HTTP API（`/api/v1/query` �?`/api/v1/query_range`）查询。连接验证使�?`GET /api/v1/status/buildinfo`�?

### 查询模式

| 模式 | GraphQL 入口 | Prometheus API |
|------|-------------|---------------|
| 即时查询 | `prometheusInstant` | `/api/v1/query` |
| 范围查询 | `prometheusRange` | `/api/v1/query_range` |

### 参数转换

| GraphQL 参数 | PromQL 映射 |
|-------------|------------|
| `query` | PromQL 表达�?|
| `time` | 即时查询时间�?|
| `startTime`/`endTime` | 范围查询时间范围 |
| `step` | 范围查询步长 |
| `filters` | 标签匹配器（Label Matcher�?|

### 类型映射

| Prometheus 类型 | GraphQL 类型 |
|----------------|-------------|
| scalar | Float |
| string | String |
| vector | PrometheusVector |
| matrix | PrometheusMatrix |

特殊值处理：`NaN` �?`+Inf`/`-Inf` 转换�?GraphQL `null`，并�?`extensions.warnings` 中记录�?

### 数据点限�?

当返回数据量超过 `max_data_points` 配置时，返回错误提示，建议缩小时间范围或增大 step 值�?

## 扩展新数据源

添加新数据源适配器的步骤�?

### 1. 实现 DataSource 接口

�?`internal/adapter/{adapter_name}/` 目录下创建适配器：

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

�?`internal/graphql/schema/myadapter.graphql` 中定义数据类型�?

### 4. 注册适配�?

�?`cmd/server/main.go` 中注册：

```go
registry.Register("myadapter", myadapter.Factory(logger.Logger))
```

### 5. 添加配置

�?`config.yaml` 中添加数据源条目�?

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

由于 gqlgen 采用编译时代码生成，Schema 合并发生在编译时。添加新适配器需要重新执�?`go generate` 和编译，不支持运行时热加载�?

## 连接管理

### 连接�?

每个数据源维护独立的连接池，可配置：
- `pool_size` �?连接池大�?
- `connection_timeout` �?连接超时
- `query_timeout` �?查询超时
- `pool_acquire_timeout` �?连接获取超时（默�?5s�?

### 重连策略

数据源不可用时自动执行指数退避重连：
- 初始间隔：`reconnect_interval`（默�?5s�?
- 最大间隔：`max_reconnect_interval`（默�?60s�?
- 退避公式：`min(reconnect_interval × 2^(attempt-1), max_reconnect_interval)`

### 熔断�?

每个数据源独立的熔断器保护：
- 连续失败 �?`failure_threshold` �?进入 OPEN 状态（快速失败）
- 经过 `open_duration` �?进入 HALF_OPEN 状态（允许探测请求�?
- 探测成功 �?`success_threshold` �?恢复 CLOSED 状�?
- 探测失败 �?回到 OPEN 状�?
