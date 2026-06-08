# 模块 03：GraphQL Schema 深入解析

> 理解 mountainKing 的 Schema 设计、类型系统、分页模型和代码生成流程。

## 3.1 Schema 模块化设计

Schema 文件按数据源和功能拆分，存放在 `internal/graphql/schema/` 目录：

```
schema/
├── base.graphql        # 基础类型、枚举、Query/Mutation 根类型
├── starrocks.graphql   # StarRocks 数据源类型
├── prometheus.graphql  # Prometheus 数据源类型
├── template.graphql    # SQL 模板查询类型
└── mutation.graphql    # Mutation 操作定义
```

gqlgen 在代码生成阶段自动合并所有 `.graphql` 文件为完整 Schema。新增数据源只需添加对应的 `.graphql` 文件并实现 Resolver。

## 3.2 自定义标量类型

```graphql
scalar DateTime    # ISO 8601 格式，如 "2024-01-15T10:30:00Z"
scalar JSON        # 任意 JSON 值，用于动态字段
```

`JSON` 标量是 mountainKing 的核心设计选择——因为 StarRocks 是 OLAP 数据库，不同表结构不同，使用 JSON 作为动态字段容器，客户端通过 `fields` 参数指定需要的列。

## 3.3 Query 操作

### StarRocks 单表查询

```graphql
type Query {
  starrocks(
    table: String!              # 表名（必须在白名单中）
    fields: [String!]           # 选择的列（不传则 SELECT *）
    filters: [StarRocksFilter!] # 过滤条件（AND 关系）
    orderBy: [StarRocksOrderBy!]# 排序
    first: Int                  # 返回条数
    after: String               # Relay 游标
    offset: Int                 # 偏移量
    limit: Int                  # 限制条数
  ): StarRocksConnection!
}
```

### Prometheus 查询

```graphql
type Query {
  # 即时查询
  prometheusInstant(
    query: String!
    time: DateTime
    filters: [PrometheusLabelFilter!]
  ): PrometheusInstantResult!

  # 范围查询
  prometheusRange(
    query: String!
    startTime: DateTime!
    endTime: DateTime!
    step: String!
    filters: [PrometheusLabelFilter!]
  ): PrometheusRangeResult!
}
```

### SQL 模板查询

```graphql
extend type Query {
  templateQuery(
    templateName: String!       # 模板名称
    parameters: JSON            # 业务参数
    fields: [String!]           # 选择的列
    first: Int                  # 返回条数
    offset: Int                 # 偏移量
    orderBy: [TemplateOrderBy!] # 排序
  ): TemplateQueryConnection!

  templateList(first: Int, offset: Int): [TemplateInfo!]!
}
```

## 3.4 分页模型

mountainKing 同时支持两种分页方式：

### Relay 游标分页（推荐用于无限滚动）

```graphql
{
  starrocks(table: "nc_notification", first: 10) {
    edges {
      node { data }
      cursor
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}
```

下一页使用 `after: "上一页的 endCursor"`。

### Offset/Limit 分页（适合传统翻页）

```graphql
{
  starrocks(table: "nc_notification", first: 10, offset: 20) {
    nodes { data }
    pageInfo { hasNextPage hasPreviousPage }
    totalCount
  }
}
```

`offset = (页码 - 1) × 每页条数`

### 模板查询分页

模板查询仅支持 offset 分页（不使用 Relay cursor），因为复杂 SQL 的游标语义不明确：

```graphql
{
  templateQuery(
    templateName: "fleet_report"
    parameters: { eerid: "VIN001", period: "weekly" }
    first: 20
    offset: 0
  ) {
    nodes
    pageInfo { hasNextPage }
    totalCount
  }
}
```

## 3.5 过滤操作符

```graphql
enum FilterOperator {
  EQ          # 等于
  NEQ         # 不等于
  GT / GTE    # 大于 / 大于等于
  LT / LTE    # 小于 / 小于等于
  LIKE        # 模式匹配
  IN / NOT_IN # 集合包含 / 不包含
  IS_NULL / IS_NOT_NULL  # 空值检查
}
```

多个 filters 之间是 AND 关系。IN 操作符的值用逗号分隔：`value: "SMS,EMAIL"`。

## 3.6 Mutation 操作

mountainKing 的 Mutation 仅用于管理功能：

```graphql
type Mutation {
  clearCache(datasource: String): Boolean!     # 清除缓存
  reloadTemplates: ReloadTemplatesResult!       # 重载 SQL 模板
}
```

执行 Mutation 需要认证主体具有 `mutation` 操作权限。

## 3.7 代码生成

修改 `.graphql` 文件后需要重新生成代码：

```bash
go generate ./...
# 或
make generate
```

生成的文件：
- `internal/graphql/generated/generated.go` — GraphQL 执行引擎
- `internal/graphql/generated/models_gen.go` — Go 类型定义

gqlgen 配置在 `gqlgen.yml` 中，指定了 Schema 文件路径、Resolver 映射和模型绑定。

## 3.8 Schema 设计最佳实践

1. **字段选择优化**：始终传 `fields` 参数，避免 `SELECT *`，减少网络传输和数据库负载
2. **按需请求 totalCount**：不需要总数时不要请求 `totalCount` 字段，可跳过 COUNT 查询
3. **合理设置 first**：配合 `graphql.max_result_rows`（默认 10000）使用，避免一次拉取过多数据
4. **使用 GraphQL 变量**：程序化调用时使用变量而非字符串拼接，更安全也更清晰

---

下一模块：[数据源适配器详解](04-datasource-adapters.md)
