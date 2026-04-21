# 需求文档

## 简介

本需求描述为 mountainKing GraphQL API 服务新增 SQL 模板查询引擎功能。该功能旨在替代现有 Java/OData 应用（application-api）中基于 FreeMarker 的 SQL 模板查询机制，使 mountainKing 能够支持复杂的多表 JOIN、CTE（公用表表达式）、窗口函数等高级 SQL 查询场景。

现有的 `starrocks` GraphQL 查询仅支持单表查询（SELECT + WHERE + ORDER BY + LIMIT），无法满足业务报表中涉及多表关联、条件分支、聚合计算等复杂查询需求。SQL 模板引擎通过预定义的 Go `text/template` 模板文件，将业务参数渲染为完整的 SQL 语句，经由 StarRocks 执行后返回动态 JSON 结果，从而在不修改代码的前提下支持任意复杂度的 SQL 查询。

**与现有功能的关系：** SQL 模板引擎作为 StarRocks 数据源的扩展查询方式，与现有单表 `starrocks` 查询并行工作，共享同一个 StarRocks 数据源连接池。现有 `starrocks` 查询功能不受影响。

**架构集成方式：** Template_Engine 作为独立组件，通过 `RawExecutor` 接口（仅包含 `ExecuteRaw` 方法）与 StarRocks Adapter 交互，实现接口隔离。Adapter 实现该接口，Template_Engine 仅持有 `RawExecutor` 引用而非完整的 `*starrocks.Adapter`，确保 Template_Engine 无法调用白名单查询等其他方法。`DataSource` 接口不做修改，Prometheus 适配器不受影响。

**并发保护：** Template_Engine 通过 `sql_templates.max_concurrent_queries`（默认 10）配置信号量限制模板查询并发数，防止长时间运行的复杂报表查询饿死共享连接池中的单表查询。

**超时模型：** 模板查询的端到端超时由 `server.request_timeout` 控制。在此总超时内，`render_timeout` 控制模板渲染阶段，`query_timeout` 控制 SQL 执行阶段，两者独立计时、互不包含。

```
┌─────────────────────────────────────────────────────────┐
│                    GraphQL Layer                         │
│                                                         │
│  starrocks(table, filters...)   templateQuery(name, params...)  │
│       │                                │                │
│  queryResolver.Starrocks()    queryResolver.TemplateQuery()     │
└───────┬────────────────────────────────┬────────────────┘
        │                                │
        │                      ┌─────────▼──────────┐
        │                      │   TemplateEngine    │
        │                      │  ┌───────────────┐  │
        │                      │  │ Registry      │  │
        │                      │  │ ParamValidator │  │
        │                      │  │ SQLSanitizer  │  │
        │                      │  │ Semaphore(10) │  │
        │                      │  └───────────────┘  │
        │                      └─────────┬──────────┘
        │                                │ RawExecutor interface
        │                                │ (rendered SQL)
        ▼                                ▼
┌────────────────────────────────────────────────────────┐
│                StarRocks Adapter                        │
│                                                         │
│  Execute(QueryRequest)      ExecuteRaw(sql, args...)    │
│  [DataSource interface]     [RawExecutor interface]     │
│       │                           │                     │
│       ▼                           ▼                     │
│  ┌────────────────────────────────────────┐             │
│  │         *sql.DB 连接池 (共享)           │             │
│  └────────────────────────────────────────┘             │
└─────────────────────────────────────────────────────────┘
```

## 术语表

- **Template_Engine**: API_Service 中负责加载、解析和渲染 SQL 模板的组件，通过 `RawExecutor` 接口与 StarRocks_Adapter 交互
- **Template_Registry**: Template_Engine 中负责管理模板注册信息（名称、文件路径、参数定义）的子组件
- **Template_File**: 使用 Go `text/template` 语法编写的 SQL 模板文件，文件扩展名为 `.sql.tmpl`，编码为 UTF-8
- **Template_Config**: 模板配置文件（YAML 格式），定义模板名称、文件路径、参数 Schema 和默认值
- **Template_Parameter**: 前端传递给模板的业务参数（如 `eerid`、`report_period_type`、`oneid`），用于控制模板渲染逻辑
- **Parameter_Schema**: 模板参数的类型定义，包含参数名称、数据类型、是否必填、默认值、校验规则和约束（max_length、max_items、enum）
- **Rendered_SQL**: Template_Engine 将 Template_Parameter 注入 Template_File 后生成的最终 SQL 语句
- **Template_Query**: 通过 GraphQL 暴露的模板查询入口，Client 通过 `templateName` 参数指定要执行的模板
- **Pagination_Wrapper**: Template_Engine 在 Rendered_SQL 外层包裹的分页 SQL 结构，实现 LIMIT/OFFSET 分页
- **API_Service**: 本项目的 GraphQL API 服务（沿用已有术语）
- **StarRocks_Adapter**: 负责与 StarRocks 数据库通信的适配器组件（沿用已有术语）
- **Query_Resolver**: 负责将 GraphQL 查询解析并路由到对应数据源的组件（沿用已有术语）
- **RawExecutor**: 接口类型，仅定义 `ExecuteRaw` 方法，用于 Template_Engine 与 StarRocks_Adapter 之间的接口隔离，确保模板引擎无法访问 Adapter 的其他方法
- **ExecuteRaw**: StarRocks_Adapter 实现 `RawExecutor` 接口的方法，接受原始 SQL 和参数，复用现有连接池执行查询，不经过 SQLQueryBuilder 和白名单校验
- **Shared_Template_Fragment**: 可被多个模板通过 `{{template "name" .}}` 引用的公共 SQL 片段文件，存放在 `sql_templates.shared_dir` 目录下，启动时自动加载

## 需求优先级与依赖关系

### 优先级定义

| 优先级 | 含义 | 需求列表 |
|--------|------|----------|
| P0 - 核心 | MVP 必须，模板查询能跑起来 | 需求 1, 2, 3, 4, 5 |
| P1 - 重要 | 生产可用，安全与查询控制 | 需求 6, 7, 8 |
| P2 - 增强 | 运维友好，可观测性与扩展性 | 需求 9, 10 |

### 依赖关系

```
需求 1 (模板文件加载) ← 需求 2 (模板渲染)
需求 2 (模板渲染) ← 需求 3 (GraphQL 集成)
需求 2 (模板渲染) ← 需求 4 (分页与字段选择)
需求 2 (模板渲染) ← 需求 6 (SQL 注入防护)
需求 3 (GraphQL 集成) ← 需求 5 (查询执行与结果返回)
需求 1 (模板文件加载) ← 需求 7 (参数校验)
需求 5 (查询执行) ← 需求 8 (缓存集成)
需求 5 (查询执行) ← 需求 9 (可观测性)
需求 1 (模板文件加载) ← 需求 10 (热加载)
```

> 箭头 `←` 表示右侧需求依赖左侧需求。建议按 P0 → P1 → P2 的顺序迭代开发。

## 需求

### 需求 1：SQL 模板文件加载与注册 `P0`

**用户故事：** 作为系统管理员，我希望通过配置文件和模板目录管理 SQL 模板，以便在不修改代码的前提下添加、修改和删除模板查询。

#### 验收标准

1. THE Template_Engine SHALL 在 API_Service 启动时从配置文件指定的目录中加载所有 Template_File
2. THE Template_Engine SHALL 支持通过 YAML 配置文件（`config.yaml` 中的 `sql_templates` 段）定义模板注册信息，每个模板条目包含名称（name）、文件路径（file）、描述（description）和参数 Schema（parameters）
3. WHEN Template_Engine 加载 Template_File 时，THE Template_Engine SHALL 使用 Go `text/template` 包解析模板语法，验证模板文件的语法正确性
4. IF Template_File 包含语法错误，THEN THE Template_Engine SHALL 记录错误日志（包含文件路径和错误详情）并跳过该模板的注册，API_Service 继续启动
5. IF 配置文件中定义的 Template_File 路径指向不存在的文件，THEN THE Template_Engine SHALL 记录错误日志并跳过该模板的注册
6. THE Template_Registry SHALL 确保每个模板名称全局唯一，IF 配置文件中存在重复的模板名称，THEN THE Template_Engine SHALL 在启动时记录错误日志并拒绝注册重复的模板
7. THE Template_Engine SHALL 支持配置模板目录的基础路径（`sql_templates.base_dir`），Template_File 的路径相对于该基础路径解析。base_dir 本身不递归扫描，模板路径可包含子目录（如 `fleet/report.sql.tmpl`）
8. WHEN API_Service 启动完成后，THE Template_Registry SHALL 在日志中输出所有已成功注册的模板名称列表
9. THE Template_Engine SHALL 限制模板名称仅包含 `[a-zA-Z0-9_-]` 字符，长度为 1-64 字符，IF 模板名称不符合规范，THEN 记录错误日志并跳过该模板的注册
10. THE Template_Engine SHALL 限制单个 Template_File 的文件大小不超过 1MB，IF 文件超过限制，THEN 记录错误日志并跳过该模板的注册
11. THE Template_Engine SHALL 验证模板文件为有效的 UTF-8 编码，IF 文件包含无效 UTF-8 字节序列，THEN 记录错误日志并跳过该模板的注册
12. THE Template_Engine SHALL 支持配置共享模板片段目录（`sql_templates.shared_dir`，默认为 `base_dir` 下的 `_shared` 子目录）。启动时 SHALL 自动加载该目录下所有 `.sql.tmpl` 文件作为可引用的模板片段（通过 `{{template "name" .}}` 调用），共享片段无需在 `templates` 列表中显式注册。IF 共享片段存在语法错误，THEN 记录错误日志并跳过该片段。IF 共享片段中 `{{define "name"}}` 的名称与已注册模板名称冲突，THEN 记录警告日志，已注册模板优先，共享片段中的同名定义被忽略

### 需求 2：SQL 模板渲染 `P0`

**用户故事：** 作为客户端开发者，我希望通过传递业务参数来渲染 SQL 模板，以便获取基于参数动态生成的复杂查询结果。

#### 验收标准

1. WHEN Client 发送包含模板名称和参数的 GraphQL 查询时，THE Template_Engine SHALL 从 Template_Registry 中查找对应的 Template_File 并使用 Go `text/template` 渲染引擎将参数注入模板
2. THE Template_Engine SHALL 将 Template_Parameter 作为 `.Params` 对象传递给模板渲染上下文，模板中通过 `{{.Params.eerid}}` 语法访问参数值
3. IF Client 请求的模板名称在 Template_Registry 中不存在，THEN THE Template_Engine SHALL 返回 VALIDATION_TEMPLATE_NOT_FOUND 错误
4. IF 模板渲染过程中发生错误（如引用了未定义的参数），THEN THE Template_Engine SHALL 返回 INTERNAL_TEMPLATE_RENDER_ERROR 错误，错误消息包含渲染失败的具体原因
5. THE Template_Engine SHALL 支持 Go `text/template` 的内置函数（如 `eq`、`ne`、`lt`、`gt`、`and`、`or`、`not`、`len`、`index`），用于模板中的条件分支和逻辑判断
6. THE Template_Engine SHALL 注册自定义模板函数 `join`（将字符串切片连接为逗号分隔的字符串）和 `quote`（调用 `safeString`（需求 6.1）进行 SQL 转义后，再用单引号包裹返回，即 `'escaped_value'`）。注意：`safeString` 只做转义不加引号，`quote` = 转义 + 包裹引号。**推荐用法：** 模板中字符串值统一使用 `{{.Params.x | quote}}`（更简洁、不易遗漏引号），`safeString` 仅用于需要手动控制引号的特殊场景（如 LIKE 模式拼接）
7. THE Template_Engine SHALL 支持模板继承（`{{template "name" .}}`）和模板片段（`{{define "name"}}...{{end}}`），允许多个模板共享公共 SQL 片段（如通用的 CTE 定义）
8. THE Rendered_SQL SHALL 为有效的 SQL 语句，THE Template_Engine SHALL 在渲染完成后对 Rendered_SQL 执行 trim 操作并验证结果不为空字符串（纯空白字符视为空）
9. THE Template_Engine SHALL 在 `sql_templates.render_timeout`（默认 5s）内完成模板渲染，WHEN 渲染超时时，THE Template_Engine SHALL 返回 INTERNAL_TEMPLATE_RENDER_ERROR 错误
10. THE Rendered_SQL 的长度 SHALL 不超过 `sql_templates.max_rendered_sql_length`（默认 65536 字节 / 64KB），IF 超过限制，THEN 返回 VALIDATION_UNSAFE_SQL 错误
11. THE Template_Engine SHALL 注册以下额外的自定义模板函数，用于模板中的数据处理：`default`（当值为零值时返回指定的默认值，如 `{{.Params.limit | default 100}}`）、`upper`（将字符串转为大写）、`lower`（将字符串转为小写）、`trimSpace`（去除字符串首尾空白字符）

### 需求 3：GraphQL Schema 集成 `P0`

**用户故事：** 作为客户端开发者，我希望每个 SQL 模板作为独立的 GraphQL Query 字段暴露，以便通过标准 GraphQL 查询语法调用模板查询。

#### 验收标准

1. THE API_Service SHALL 为所有已注册的模板提供一个统一的 GraphQL Query 字段 `templateQuery`，Client 通过 `templateName` 参数指定要执行的模板
2. THE `templateQuery` 字段 SHALL 接受以下参数：`templateName`（String!, 模板名称）、`parameters`（JSON, 业务参数键值对，使用现有的 `JSON` 标量类型）、`fields`（[String!], 需要返回的字段列表）、`first`（Int, 返回行数限制）、`offset`（Int, 偏移量）、`orderBy`（[TemplateOrderBy!], 排序条件）
3. THE `templateQuery` 字段 SHALL 返回 `TemplateQueryConnection` 类型，包含 `nodes`（[JSON!]!, 结果行数组）、`pageInfo`（PageInfo!）和 `totalCount`（Int!）。注意：模板查询不使用 Relay cursor 分页（因为模板 SQL 结果无稳定游标），故省略 `edges`，仅使用 `nodes` + offset 分页
4. THE API_Service SHALL 提供 `templateList` GraphQL Query 字段，返回所有已注册模板的元信息列表，每个条目包含模板名称（name）、描述（description）和参数 Schema（parameters）
5. THE `templateList` 查询 SHALL 返回 `[TemplateInfo!]!` 类型，`TemplateInfo` 包含 `name`（String!）、`description`（String!）、`countEnabled`（Boolean!，是否支持 totalCount）和 `parameters`（[TemplateParameterInfo!]!）字段，`TemplateParameterInfo` 包含 `name`（String!）、`type`（String!）、`required`（Boolean!）和 `defaultValue`（String）字段。`countEnabled` 字段让 Client 在请求前即可知道某模板是否支持 totalCount，避免请求后才发现返回 -1
6. THE Schema 定义 SHALL 放置在 `internal/graphql/schema/template.graphql` 文件中，遵循现有的模块化 Schema 结构
7. THE `TemplateOrderBy` input 类型定义为 `input TemplateOrderBy { field: String!, direction: SortDirection! }`，复用现有的 `SortDirection` 枚举（`ASC` / `DESC`）
8. THE `templateList` 查询 SHALL 支持可选的分页参数 `first`（Int, 默认返回全部）和 `offset`（Int, 默认 0），WHEN 已注册模板数量较多时，Client 可通过分页控制返回数量。IF 未指定 `first`，THEN 返回所有已注册模板
9. WHEN 配置文件中 `sql_templates.enabled` 为 false 时，THE Template_Engine SHALL 不初始化（不加载模板、不注册信号量）。`templateQuery` 对任何模板名称 SHALL 返回 VALIDATION_TEMPLATE_NOT_FOUND 错误，`templateList` SHALL 返回空数组 `[]`，`reloadTemplates` Mutation SHALL 返回 successCount=0 的空结果。GraphQL Schema 中的类型定义不受影响（编译时生成）

### 需求 4：分页与字段选择 `P0`

**用户故事：** 作为客户端开发者，我希望对模板查询结果进行分页和字段选择，以便控制返回的数据量和网络传输量。

#### 验收标准

1. THE Template_Engine SHALL 支持对 Rendered_SQL 的结果进行分页，通过在 Rendered_SQL 外层包裹 `SELECT ... FROM (Rendered_SQL) AS t LIMIT ? OFFSET ?` 实现 Pagination_Wrapper
2. WHEN Client 在 `templateQuery` 中指定 `first` 和 `offset` 参数时，THE Pagination_Wrapper SHALL 将这些参数转换为 SQL LIMIT 和 OFFSET 子句
3. WHEN Client 在 `templateQuery` 中指定 `fields` 参数时，THE Pagination_Wrapper SHALL 对每个字段名调用 `safeIdentifier`（需求 6.4）校验合法性后，在外层 SELECT 中仅选择指定的字段，而非 `SELECT *`。IF 任何字段名包含非法字符，THEN 返回 VALIDATION_INVALID_FIELD 错误
4. WHEN Client 在 `templateQuery` 中指定 `orderBy` 参数时，THE Pagination_Wrapper SHALL 对排序字段名调用 `safeIdentifier`（需求 6.4）校验后，在外层 SQL 中添加 ORDER BY 子句
5. WHEN Client 请求 `totalCount` 字段时，THE Template_Engine SHALL 执行一条额外的 `SELECT COUNT(*) FROM (Rendered_SQL) AS t` 查询以返回总记录数。THE Template_Engine SHALL 支持在模板配置中禁用 totalCount（`count_enabled: false`），用于复杂 CTE 查询避免双倍执行开销，WHEN 模板禁用 totalCount 且 Client 请求该字段时，SHALL 返回 -1 并在 `extensions.warnings` 中提示
6. IF Client 未指定 `first` 参数，THEN THE Template_Engine SHALL 使用配置的默认最大返回行数（`graphql.max_result_rows`，默认 10000）作为 LIMIT 值
7. THE Pagination_Wrapper SHALL 使用参数化查询（`?` 占位符）传递 LIMIT 和 OFFSET 值，防止 SQL 注入
8. 模板 SQL 应避免在最外层使用 `ORDER BY`（会被 Pagination_Wrapper 的 ORDER BY 覆盖），排序逻辑应由客户端通过 `orderBy` 参数控制。如果模板内部子查询需要排序（如窗口函数的 PARTITION BY ... ORDER BY），不受此限制
9. THE Template_Engine SHALL 通过信号量（`sql_templates.max_concurrent_queries`，默认 10）限制同时执行的模板查询数量，WHEN 并发模板查询数达到上限时，新的模板查询 SHALL 排队等待，等待时间计入 `query_timeout`。此机制防止长时间运行的复杂报表查询饿死共享连接池中的单表查询

### 需求 5：查询执行与结果返回 `P0`

**用户故事：** 作为客户端开发者，我希望模板查询的结果以 JSON 格式返回，以便前端灵活处理动态 Schema 的查询结果。

#### 验收标准

1. THE StarRocks_Adapter SHALL 实现 `RawExecutor` 接口，提供 `ExecuteRaw(ctx context.Context, query string, args ...interface{}) (*QueryResult, error)` 方法，该方法复用现有的 `*sql.DB` 连接池执行任意 SQL 语句并通过现有的 `scanRows` 函数转换结果。此方法不经过 `SQLQueryBuilder` 和白名单校验。`RawExecutor` 接口定义在 Template_Engine 包中，StarRocks_Adapter 实现该接口，Template_Engine 仅依赖 `RawExecutor` 接口而非具体的 Adapter 类型，实现接口隔离。`DataSource` 接口不做修改
2. WHEN StarRocks 返回查询结果时，THE Template_Engine SHALL 将每一行结果转换为 JSON 对象（key 为列名，value 为列值），与现有 `starrocks` 查询的 `StarRocksRow.data` 格式一致
3. THE Template_Engine SHALL 支持 StarRocks 返回的所有数据类型，复用 StarRocks_Adapter 中已有的类型映射规则（INT → Int, VARCHAR → String, DATETIME → DateTime 等）
4. IF StarRocks 执行 Rendered_SQL 时发生错误，THEN THE Template_Engine SHALL 返回 DATASOURCE_TEMPLATE_QUERY_ERROR 错误，错误消息包含 StarRocks 返回的错误信息（脱敏后）
5. THE Template_Engine SHALL 遵守现有的查询超时配置（`options.query_timeout`），WHEN 模板查询执行时间超过配置的超时时间时，THE Template_Engine SHALL 取消查询并返回 DATASOURCE_TIMEOUT 错误。注意：`render_timeout`（需求 2.9）控制模板渲染阶段，`query_timeout` 控制 SQL 执行阶段，两者独立计时、互不包含，总超时受 `server.request_timeout` 约束
6. THE Template_Engine SHALL 遵守现有的最大返回行数配置（`graphql.max_result_rows`），WHEN 查询结果超过配置上限时，THE Template_Engine SHALL 截断结果并在响应的 `extensions.warnings` 中包含截断提示
7. THE Template_Engine SHALL 复用现有的数据源权限检查，认证主体须具有 StarRocks 数据源（如 `analytics_db`）的 `query` 操作权限，否则返回 AUTH_INSUFFICIENT_PERMISSION 错误

### 需求 6：SQL 注入防护 `P1`

**用户故事：** 作为系统管理员，我希望模板引擎具备完善的 SQL 注入防护能力，以便防止恶意参数破坏 SQL 语句的安全性。

#### 验收标准

1. THE Template_Engine SHALL 提供 `safeString` 自定义模板函数，该函数对字符串参数执行 SQL 转义处理（转义单引号 `'` 为 `''`，转义反斜杠 `\` 为 `\\`，移除 NULL 字节 `\0`），仅做转义不加引号。注意：反斜杠转义是必要的，因为 StarRocks 默认启用反斜杠转义（与 MySQL 一致），未转义的反斜杠可能导致后续引号被吞噬从而引发注入
2. THE Template_Engine SHALL 提供 `safeInt` 自定义模板函数，该函数验证参数为有效整数并返回整数字符串表示，IF 参数不是有效整数，THEN 返回模板渲染错误
3. THE Template_Engine SHALL 提供 `safeFloat` 自定义模板函数，该函数验证参数为有效浮点数并返回浮点数字符串表示，IF 参数不是有效浮点数，THEN 返回模板渲染错误
4. THE Template_Engine SHALL 提供 `safeIdentifier` 自定义模板函数，该函数验证参数仅包含合法 SQL 标识符字符（`[a-zA-Z0-9_.]`）。遇到点号时 SHALL 按 `.` 拆分（最多 2 段，即 `table.column` 格式），分别校验每段长度为 1-64 字符并用反引号包裹（如 `a.b` → `` `a`.`b` ``，`abc` → `` `abc` ``）。IF 参数包含非法字符、段数超过 2、或任一段为空/超过 64 字符，THEN 返回模板渲染错误
5. THE Template_Engine SHALL 提供 `safeInList` 自定义模板函数，该函数接受字符串切片参数，对每个元素执行 SQL 转义后生成 `'val1','val2','val3'` 格式的 IN 子句值列表。IF 传入的切片为空（长度为 0），THEN `safeInList` SHALL 返回模板渲染错误（因为 `IN ()` 在 StarRocks 中是无效 SQL），模板作者应在调用前使用 `{{if .Params.ids}}` 判断非空
6. THE Template_Engine SHALL 在模板渲染完成后对 Rendered_SQL 执行基础安全检查：使用简单词法扫描器（状态机追踪单引号边界，处理转义引号 `''` 和反斜杠转义 `\'`）检测未在字符串字面量内的分号 `;`。扫描器同时 SHALL 检测未在字符串字面量内的 SQL 单行注释（`--`）和块注释（`/* ... */`），但 SHALL 保留 StarRocks Optimizer Hint（以 `/*+` 开头的块注释，如 `/*+ SET_VAR(...) */`）。对于非 Hint 的注释，IF 检测到，THEN 将其从 Rendered_SQL 中移除后再执行后续检查。扫描器实现为线性扫描（O(n)），不需要完整的 SQL 解析器。IF 检测到多条 SQL 语句，THEN 返回 VALIDATION_UNSAFE_SQL 错误
7. THE Template_Engine SHALL 在日志中记录每次模板渲染生成的 Rendered_SQL（脱敏后），用于安全审计。脱敏处理 SHALL 复用现有 `sanitization.rules` 配置中的脱敏规则（参见主需求文档需求 13.13），无需为模板引擎定义独立的脱敏规则
8. THE Template_Engine SHALL 提供 `safeLike` 自定义模板函数，该函数对字符串参数中的 LIKE 通配符进行转义（`%` → `\%`、`_` → `\_`、`\` → `\\`），用于安全构建 LIKE 模式

### 需求 7：模板参数校验 `P1`

**用户故事：** 作为客户端开发者，我希望模板引擎在渲染前校验参数的类型和完整性，以便在查询执行前获得清晰的参数错误提示。

#### 验收标准

1. THE Template_Engine SHALL 在模板渲染前根据 Parameter_Schema 校验 Client 提供的 Template_Parameter
2. WHEN Parameter_Schema 中定义的必填参数（`required: true`）未在 Client 请求中提供时，THE Template_Engine SHALL 返回 VALIDATION_MISSING_PARAMETER 错误，错误消息包含缺失的参数名称
3. THE Template_Engine SHALL 校验参数值的数据类型是否与 Parameter_Schema 中定义的类型匹配，支持的类型包括 `string`、`int`、`float`、`boolean`、`string[]`（字符串数组）
4. IF 参数值的数据类型与 Parameter_Schema 不匹配，THEN THE Template_Engine SHALL 返回 VALIDATION_INVALID_PARAMETER_TYPE 错误
5. WHEN Parameter_Schema 中定义了参数的默认值（`default`）且 Client 未提供该参数时，THE Template_Engine SHALL 使用默认值填充该参数
6. THE Template_Engine SHALL 支持在 Parameter_Schema 中定义参数的枚举约束（`enum`），IF 参数值不在枚举列表中，THEN THE Template_Engine SHALL 返回 VALIDATION_INVALID_PARAMETER_VALUE 错误
7. THE Template_Engine SHALL 支持在 Parameter_Schema 中定义 `string` 类型参数的最大长度约束（`max_length`，默认 1024），IF 参数值长度超过限制，THEN 返回 VALIDATION_INVALID_PARAMETER_VALUE 错误
8. THE Template_Engine SHALL 支持在 Parameter_Schema 中定义 `string[]` 类型参数的最大元素数量约束（`max_items`，默认 1000），IF 数组元素数量超过限制，THEN 返回 VALIDATION_INVALID_PARAMETER_VALUE 错误
9. THE Template_Engine SHALL 支持在 Parameter_Schema 中定义 `string` 类型参数的正则表达式约束（`pattern`），IF 参数值不匹配指定的正则表达式，THEN 返回 VALIDATION_INVALID_PARAMETER_VALUE 错误。正则表达式使用 Go `regexp` 包语法（RE2 引擎，天然防止 ReDoS 攻击，保证线性时间复杂度匹配），用于日期格式（如 `^\d{4}-\d{2}-\d{2}$`）、编码格式等常见校验场景。THE Template_Engine SHALL 在启动时预编译所有 pattern 正则表达式，IF 正则语法无效，THEN 记录错误日志并跳过该模板的注册

### 需求 8：缓存集成 `P1`

**用户故事：** 作为系统管理员，我希望模板查询结果能够利用现有的缓存机制，以便减少重复查询对 StarRocks 的负载。

#### 验收标准

1. THE Template_Engine SHALL 复用现有的 Cache_Layer 组件对模板查询结果进行缓存
2. THE Cache_Layer SHALL 基于模板名称、排序后的参数值、字段选择和分页参数的组合生成缓存 key，格式为 `cache:template:{template_name}:{xxhash64(sorted_params + sorted_fields + first + offset + orderBy)}`，不使用 Rendered_SQL 作为 key 输入（避免模板空白差异导致缓存未命中）。注意：`fields` 参数必须包含在 key 计算中，否则不同字段选择会错误命中同一缓存
3. THE Template_Engine SHALL 支持在模板配置中为每个模板独立设置缓存 TTL（`cache_ttl`），IF 未配置，THEN 使用数据源级别的默认 TTL
4. THE Template_Engine SHALL 支持在模板配置中禁用特定模板的缓存（`cache_enabled: false`），用于实时性要求高的查询
5. WHEN Client 在 GraphQL 请求的扩展参数中设置 `extensions.cache` 为 false 时，THE Template_Engine SHALL 跳过缓存直接执行模板查询（复用现有行为）
6. WHEN Client 请求 `totalCount` 字段时，totalCount 查询 SHALL 使用独立缓存 key（在数据查询 key 后追加 `:count` 后缀），与数据查询分别缓存，TTL 相同

### 需求 9：可观测性集成 `P2`

**用户故事：** 作为系统管理员，我希望模板查询具备完善的可观测性，以便监控模板查询的性能和使用情况。

#### 验收标准

1. THE Template_Engine SHALL 注册名为 `graphql_template_query_duration_seconds` 的 Histogram 指标，记录每次模板查询的处理延迟，标签包含 `template_name`（模板名称）和 `datasource`（数据源名称）。注意：模板查询同时计入现有的 `graphql_request_duration_seconds`（HTTP 层），两者维度不同
2. THE Template_Engine SHALL 注册名为 `graphql_template_queries_total` 的 Counter 指标，记录模板查询总数，标签包含 `template_name`（模板名称）和 `status`（success 或 error）
3. WHEN Template_Engine 执行模板查询时，THE Template_Engine SHALL 在当前 Resolver Span 下创建一个子 Span，Span 名称格式为 `Template Query {template_name}`，并设置以下属性：`template.name`（模板名称）、`db.system`（值为 starrocks）、`db.statement`（Rendered_SQL，脱敏后）
4. THE Template_Engine SHALL 在结构化日志中记录每次模板查询的关键信息：模板名称、参数摘要（脱敏后）、渲染耗时、执行耗时和结果行数
5. THE Template_Engine SHALL 在审计日志中记录模板查询操作，`AuditLogger.LogEntry` 新增 `TemplateName` 字段，记录所执行的模板名称
6. THE Template_Engine SHALL 注册名为 `graphql_template_render_duration_seconds` 的 Histogram 指标，记录模板渲染耗时（独立于查询执行耗时），标签包含 `template_name`

### 需求 10：模板热加载 `P2`

**用户故事：** 作为系统管理员，我希望在不重启服务的情况下更新 SQL 模板，以便快速响应业务查询需求的变化。

#### 验收标准

1. THE Template_Engine SHALL 支持通过 GraphQL Mutation 操作 `reloadTemplates` 触发模板重新加载
2. WHEN `reloadTemplates` Mutation 被调用时，THE Template_Engine SHALL 重新读取配置文件和模板目录，更新 Template_Registry 中的模板注册信息
3. THE Template_Engine SHALL 使用读写锁（`sync.RWMutex`）保护 Template_Registry，确保模板重新加载期间正在执行的查询不受影响
4. IF 重新加载过程中某个模板文件存在语法错误，THEN THE Template_Engine SHALL 保留该模板的旧版本并在响应中返回加载失败的模板列表
5. THE `reloadTemplates` Mutation SHALL 返回重新加载的结果摘要，包含成功加载的模板数量、失败的模板列表和总耗时
6. THE Template_Engine SHALL 同时支持 fsnotify 文件监听自动触发模板重新加载（复用现有 HotReloader 的目录监听模式，监听 `sql_templates.base_dir` 和 `sql_templates.shared_dir` 目录变化，500ms 防抖），与 `reloadTemplates` Mutation 手动触发并行工作。fsnotify 触发的重新加载与 Mutation 触发共享同一个互斥锁（需求 10.9），确保不会并发执行。自动重新加载的结果通过日志记录（成功/失败模板列表）
7. WHEN 模板重新加载成功后，THE Template_Engine SHALL 通过比较模板文件内容的 SHA-256 hash 判断模板是否变更，仅对 hash 发生变化的模板清除缓存条目（调用 `Cache.DeleteByPrefix("cache:template:{template_name}")`），未变更的模板保留缓存。THE Template_Engine SHALL 在内存中维护每个模板的当前 hash 值用于比较
8. THE `reloadTemplates` Mutation SHALL 要求认证主体具有 "mutation" 操作权限（与 `clearCache` Mutation 一致），否则返回 AUTH_INSUFFICIENT_PERMISSION 错误
9. THE Template_Engine SHALL 对 `reloadTemplates` 操作和 fsnotify 触发的自动重新加载使用同一个互斥锁（`sync.Mutex`），防止并发触发多次重新加载导致竞态条件。该互斥锁独立于 Template_Registry 的读写锁（需求 10.3），仅保护重新加载操作本身

## 错误码汇总

| 错误码 | HTTP 状态码 | 触发条件 | 所属需求 |
|--------|-----------|---------|---------|
| VALIDATION_TEMPLATE_NOT_FOUND | 400 | 请求的模板名称在 Template_Registry 中不存在 | 需求 2.3 |
| INTERNAL_TEMPLATE_RENDER_ERROR | 500 | 模板渲染失败（语法错误、未定义参数、渲染超时） | 需求 2.4, 2.9 |
| VALIDATION_UNSAFE_SQL | 400 | 渲染结果包含多条 SQL 语句或超过最大长度限制 | 需求 6.6, 2.10 |
| VALIDATION_MISSING_PARAMETER | 400 | 必填参数缺失 | 需求 7.2 |
| VALIDATION_INVALID_PARAMETER_TYPE | 400 | 参数值的数据类型与 Schema 定义不匹配 | 需求 7.4 |
| VALIDATION_INVALID_PARAMETER_VALUE | 400 | 参数值不在枚举范围内、超过长度限制或超过数组元素数量限制 | 需求 7.6, 7.7, 7.8 |
| VALIDATION_INVALID_FIELD | 400 | 分页包装器中的字段名包含非法字符 | 需求 4.3 |
| DATASOURCE_TEMPLATE_QUERY_ERROR | 200 | StarRocks 执行模板 SQL 失败（在 GraphQL errors 数组中返回） | 需求 5.4 |
| DATASOURCE_TIMEOUT | 200 | 模板查询执行超时（复用现有错误码） | 需求 5.5 |
| AUTH_INSUFFICIENT_PERMISSION | 403 | 认证主体缺少数据源 query 权限或 mutation 权限 | 需求 5.7, 10.8 |

## 附录 A：配置示例

```yaml
# config.yaml 新增段
sql_templates:
  enabled: true
  base_dir: ./templates                    # 模板文件基础目录
  shared_dir: ./templates/_shared          # 共享模板片段目录（默认 base_dir/_shared）
  render_timeout: 5s                       # 模板渲染超时
  max_rendered_sql_length: 65536           # 渲染结果最大字节数 (64KB)
  max_concurrent_queries: 10               # 模板查询最大并发数（防止饿死连接池）
  # 注意：YAML 单引号字符串内不解释转义字符，正则中的 \d 无需写成 \\d
  templates:
    - name: fleet_report                   # 模板名称，仅允许 [a-zA-Z0-9_-]
      file: fleet/fleet_report.sql.tmpl    # 相对于 base_dir 的路径
      description: 车队综合报表
      cache_enabled: true                  # 是否启用缓存（默认 true）
      cache_ttl: 300s                      # 缓存 TTL（不设置则使用数据源默认 TTL）
      count_enabled: true                  # 是否支持 totalCount（默认 true）
      parameters:
        - name: eerid
          type: string
          required: true
          max_length: 64                   # 字符串最大长度（默认 1024）
        - name: period
          type: string
          required: false
          default: monthly
          enum: [daily, weekly, monthly]   # 枚举约束
        - name: vehicle_ids
          type: "string[]"
          required: false
          max_items: 500                   # 数组最大元素数（默认 1000）

    - name: driver_score
      file: driver/driver_score.sql.tmpl
      description: 驾驶员评分报表
      cache_enabled: false                 # 实时性要求高，禁用缓存
      count_enabled: false                 # 复杂 CTE 查询，禁用 totalCount 避免双倍开销
      parameters:
        - name: driver_id
          type: int
          required: true
        - name: start_date
          type: string
          required: true
          max_length: 10
          pattern: '^\d{4}-\d{2}-\d{2}$'  # 正则校验：日期格式 YYYY-MM-DD
        - name: end_date
          type: string
          required: true
          max_length: 10
          pattern: '^\d{4}-\d{2}-\d{2}$'  # 正则校验：日期格式 YYYY-MM-DD
```


## 附录 B：GraphQL 查询示例

### 模板查询

```graphql
# 查询车队报表，指定参数、字段选择、分页和排序
query {
  templateQuery(
    templateName: "fleet_report"
    parameters: { eerid: "EER001", period: "monthly", vehicle_ids: ["V001", "V002"] }
    fields: ["vehicle_id", "plate_number", "event_count"]
    first: 20
    offset: 0
    orderBy: [{ field: "event_count", direction: DESC }]
  ) {
    nodes
    pageInfo {
      hasNextPage
      hasPreviousPage
    }
    totalCount
  }
}
```

### 模板列表查询

```graphql
# 查询所有可用模板及其参数定义
query {
  templateList {
    name
    description
    countEnabled
    parameters {
      name
      type
      required
      defaultValue
    }
  }
}
```

### 模板热加载

```graphql
# 手动触发模板重新加载（需要 mutation 权限）
mutation {
  reloadTemplates {
    successCount
    failures {
      name
      error
    }
    duration
  }
}
```

## 附录 C：SQL 模板文件示例

### 基础模板：车队综合报表（`fleet/fleet_report.sql.tmpl`）

```sql
{{/* 车队综合报表 - 支持条件分支、CTE、多表 JOIN */}}
{{/* 推荐使用 quote 函数处理字符串值（自动转义+加引号） */}}
WITH base_events AS (
  SELECT vehicle_id, event_type, event_time, duration_seconds
  FROM vehicle_events
  WHERE eerid = {{.Params.eerid | quote}}
  {{if eq .Params.period "daily"}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 1 DAY)
  {{else if eq .Params.period "weekly"}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 7 DAY)
  {{else}}
    AND event_time >= DATE_SUB(NOW(), INTERVAL 30 DAY)
  {{end}}
  {{if .Params.vehicle_ids}}
    AND vehicle_id IN ({{.Params.vehicle_ids | safeInList}})
  {{end}}
)
SELECT
  b.vehicle_id,
  v.plate_number,
  COUNT(*) AS event_count,
  SUM(b.duration_seconds) AS total_duration,
  AVG(b.duration_seconds) AS avg_duration
FROM base_events b
JOIN vehicles v ON b.vehicle_id = v.vehicle_id
GROUP BY b.vehicle_id, v.plate_number
```

### 共享模板片段示例

```sql
{{/* common/time_filter.sql.tmpl - 可被多个模板引用 */}}
{{define "time_filter"}}
  {{if eq .Params.period "daily"}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 1 DAY)
  {{else if eq .Params.period "weekly"}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 7 DAY)
  {{else}}
    AND {{.Column}} >= DATE_SUB(NOW(), INTERVAL 30 DAY)
  {{end}}
{{end}}
```

### 自定义模板函数速查表

| 函数 | 用途 | 示例 | 输出 |
|------|------|------|------|
| `safeString` | SQL 字符串转义（不加引号） | `{{.Params.name \| safeString}}` | `O''Brien` （同时转义 `\` → `\\`） |
| `quote` | SQL 字符串转义 + 包裹引号 | `{{.Params.name \| quote}}` | `'O''Brien'` |
| `safeInt` | 验证并输出整数 | `{{.Params.limit \| safeInt}}` | `100` |
| `safeFloat` | 验证并输出浮点数 | `{{.Params.threshold \| safeFloat}}` | `3.14` |
| `safeIdentifier` | SQL 标识符校验 + 反引号包裹 | `{{.Params.col \| safeIdentifier}}` | `` `column_name` `` |
| `safeInList` | 字符串数组 → IN 子句值 | `{{.Params.ids \| safeInList}}` | `'a','b','c'` |
| `safeLike` | LIKE 通配符转义 | `{{.Params.keyword \| safeLike}}` | `100\%` |
| `join` | 字符串数组 → 逗号分隔 | `{{.Params.cols \| join}}` | `a,b,c` |
| `default` | 零值时返回默认值 | `{{.Params.limit \| default 100}}` | `100`（当 limit 未传时） |
| `upper` | 字符串转大写 | `{{.Params.status \| upper}}` | `ACTIVE` |
| `lower` | 字符串转小写 | `{{.Params.status \| lower}}` | `active` |
| `trimSpace` | 去除首尾空白 | `{{.Params.name \| trimSpace}}` | `hello` |

## 附录 D：GraphQL Schema 定义参考

```graphql
# internal/graphql/schema/template.graphql

"""模板查询排序条件"""
input TemplateOrderBy {
  field: String!
  direction: SortDirection!
}

"""模板查询结果（使用 offset 分页，不使用 Relay cursor）"""
type TemplateQueryConnection {
  nodes: [JSON!]!
  pageInfo: PageInfo!
  totalCount: Int!
}

"""模板元信息"""
type TemplateInfo {
  name: String!
  description: String!
  countEnabled: Boolean!
  parameters: [TemplateParameterInfo!]!
}

"""模板参数元信息"""
type TemplateParameterInfo {
  name: String!
  type: String!
  required: Boolean!
  defaultValue: String
}

"""模板重新加载结果"""
type ReloadTemplatesResult {
  successCount: Int!
  failures: [TemplateLoadFailure!]!
  duration: String!
}

"""模板加载失败详情"""
type TemplateLoadFailure {
  name: String!
  error: String!
}

extend type Query {
  """执行 SQL 模板查询"""
  templateQuery(
    templateName: String!
    parameters: JSON
    fields: [String!]
    first: Int
    offset: Int
    orderBy: [TemplateOrderBy!]
  ): TemplateQueryConnection!

  """列出所有已注册的模板"""
  templateList(first: Int, offset: Int): [TemplateInfo!]!
}

extend type Mutation {
  """重新加载 SQL 模板（需要 mutation 权限）"""
  reloadTemplates: ReloadTemplatesResult!
}
```

## 附录 E：性能 SLA

| 指标 | 目标值 | 说明 |
|------|--------|------|
| 模板渲染延迟 (p99) | ≤ 50ms | 不含 SQL 执行时间，纯模板渲染 |
| 模板查询端到端延迟 (p95) | ≤ 500ms | 含渲染 + SQL 执行 + 结果转换，不含网络传输 |
| 模板查询端到端延迟 (p99) | ≤ 2s | 复杂报表查询的上限 |
| 缓存命中时延迟 (p99) | ≤ 10ms | 缓存命中时跳过渲染和 SQL 执行 |
| 缓存命中率（稳态） | ≥ 60% | 相同参数的重复查询应命中缓存 |
| 最大并发模板查询 | ≥ 50 | 系统能力上限，需调整 `max_concurrent_queries`（默认 10）和 `pool_size`（默认 20）配合达成。默认配置下信号量限制为 10 并发，保护连接池不被模板查询独占 |
| 模板热加载耗时 | ≤ 1s | 重新加载所有模板文件的总耗时 |
| 参数校验延迟 | ≤ 1ms | 单次参数校验（含类型检查、枚举、正则） |

> 以上目标基于 StarRocks 查询本身在合理范围内（≤ 30s query_timeout）的前提。实际延迟受 SQL 复杂度和数据量影响。

## 附录 F：安全威胁模型

| # | 威胁 | 攻击向量 | 防御措施 | 对应需求 |
|---|------|---------|---------|---------|
| T1 | SQL 注入 - 字符串参数 | 参数值包含 `'; DROP TABLE --` 或 `\' OR 1=1--` | `safeString` 转义单引号 + 转义反斜杠 + 移除 NULL 字节 | 需求 6.1 |
| T2 | SQL 注入 - 数值参数 | 参数值为 `1 OR 1=1` 伪装成整数 | `safeInt` / `safeFloat` 严格类型验证 | 需求 6.2, 6.3 |
| T3 | SQL 注入 - 标识符参数 | 参数值为 `` `; DROP TABLE users; --` `` | `safeIdentifier` 正则校验 `[a-zA-Z0-9_.]` | 需求 6.4 |
| T4 | SQL 注入 - IN 子句 | 数组元素包含 `') OR ('1'='1` | `safeInList` 对每个元素独立转义 | 需求 6.5 |
| T5 | SQL 注入 - LIKE 通配符 | 参数值包含 `%` 导致全表扫描 | `safeLike` 转义通配符 | 需求 6.8 |
| T6 | 多语句注入 | 渲染结果包含 `; INSERT INTO ...` | 词法扫描器检测字符串外分号 | 需求 6.6 |
| T7 | 注释注入 | 参数值包含 `--` 或 `/*` 注释掉后续 SQL | 词法扫描器检测并移除 SQL 注释 | 需求 6.6 |
| T8 | 参数溢出 - 长字符串 | 传入超长字符串导致内存/SQL 膨胀 | `max_length` 约束（默认 1024） | 需求 7.7 |
| T9 | 参数溢出 - 大数组 | 传入百万元素数组导致 IN 子句爆炸 | `max_items` 约束（默认 1000） | 需求 7.8 |
| T10 | 渲染结果膨胀 | 模板循环生成超大 SQL | `max_rendered_sql_length` 限制（64KB） | 需求 2.10 |
| T11 | 渲染 DoS | 恶意参数触发模板无限循环 | `render_timeout` 超时控制（5s） | 需求 2.9 |
| T12 | 未授权访问 | 无权限用户调用模板查询 | 数据源级别权限检查 | 需求 5.7 |
| T13 | 未授权管理操作 | 无权限用户调用 reloadTemplates | mutation 权限检查 | 需求 10.8 |
| T14 | 模板文件篡改 | 攻击者修改服务器上的 .sql.tmpl 文件 | 文件系统权限控制（运维层面）+ 热加载日志审计 | 需求 10.6 |
| T15 | 查询结果泄露 | 模板查询返回敏感数据 | 日志中 Rendered_SQL 脱敏 + 审计日志记录 | 需求 6.7, 9.5 |
| T16 | 连接池饿死 | 大量复杂模板查询占满连接池 | `max_concurrent_queries` 信号量限制并发 | 需求 4.9 |
| T17 | 反斜杠注入 | 参数值 `a\' OR 1=1--` 利用反斜杠吞噬引号 | `safeString` 转义反斜杠 `\` → `\\` | 需求 6.1 |

## 附录 G：正确性属性

以下属性用于指导 property-based testing（使用 `pgregory.net/rapid`），与现有项目的 96 条属性保持同等质量标准。

### 模板加载与注册（需求 1）

| # | 属性 | 描述 |
|---|------|------|
| P1 | 模板名称唯一性 | ∀ 已注册模板 t1, t2：t1.name ≠ t2.name |
| P2 | 模板语法有效性 | ∀ 已注册模板 t：`template.Parse(t.content)` 无错误 |
| P3 | 无效模板不影响启动 | ∀ 模板集合 S 含无效模板：API_Service 启动成功 ∧ 无效模板不在 Registry 中 |
| P4 | 模板名称格式校验 | ∀ 模板名称 n：n ∈ Registry ⟹ n 匹配 `^[a-zA-Z0-9_-]{1,64}$` |
| P5 | 文件大小限制 | ∀ 模板文件 f：f.size > 1MB ⟹ f 不在 Registry 中 |
| P6 | UTF-8 编码校验 | ∀ 已注册模板 t：t.content 为有效 UTF-8 编码 |
| P7 | 共享片段加载 | ∀ shared_dir 下的有效 .sql.tmpl 文件 f：f 可通过 `{{template}}` 引用 |

### 模板渲染（需求 2）

| # | 属性 | 描述 |
|---|------|------|
| P8 | 渲染结果非空 | ∀ 成功渲染的 SQL s：len(s) > 0 |
| P9 | 渲染结果长度限制 | ∀ 成功渲染的 SQL s：len(s) ≤ max_rendered_sql_length |
| P10 | 渲染超时保护 | ∀ 渲染操作 r：r.duration ≤ render_timeout ∨ r.error = INTERNAL_TEMPLATE_RENDER_ERROR |
| P11 | 不存在模板返回错误 | ∀ 模板名称 n ∉ Registry：query(n) 返回 VALIDATION_TEMPLATE_NOT_FOUND |
| P12 | 渲染确定性 | ∀ 模板 t, 参数 p：render(t, p) 的结果在多次调用间一致 |

### GraphQL 集成（需求 3）

| # | 属性 | 描述 |
|---|------|------|
| P13 | templateList 完整性 | templateList 返回的模板集合 = Registry 中所有已注册模板 |
| P14 | templateList 参数一致性 | ∀ 模板 t：templateList 中 t 的 parameters = 配置文件中 t 的 parameters |
| P15 | countEnabled 一致性 | ∀ 模板 t：templateList 中 t.countEnabled = 配置文件中 t.count_enabled |
| P16 | 功能禁用行为 | WHEN sql_templates.enabled=false：templateQuery 返回 VALIDATION_TEMPLATE_NOT_FOUND ∧ templateList 返回 [] |

### 分页与字段选择（需求 4）

| # | 属性 | 描述 |
|---|------|------|
| P17 | LIMIT 参数化 | ∀ 分页查询：生成的 SQL 中 LIMIT 值通过 `?` 占位符传递 |
| P18 | OFFSET 参数化 | ∀ 分页查询：生成的 SQL 中 OFFSET 值通过 `?` 占位符传递 |
| P19 | 默认 LIMIT 强制 | ∀ 未指定 first 的查询：实际 LIMIT = graphql.max_result_rows |
| P20 | 字段选择安全性 | ∀ fields 中的字段 f：f 通过 safeIdentifier 校验 |
| P21 | OrderBy 字段安全性 | ∀ orderBy 中的字段 f：f 通过 safeIdentifier 校验 |
| P22 | totalCount 独立性 | ∀ 请求 totalCount 的查询：totalCount 值 = COUNT(*) 结果，不受 LIMIT 影响 |
| P23 | 并发限制 | ∀ 并发模板查询数 > max_concurrent_queries：超出的查询排队等待 |

### 查询执行（需求 5）

| # | 属性 | 描述 |
|---|------|------|
| P24 | 结果截断 + 警告 | ∀ 结果行数 > max_result_rows：返回行数 = max_result_rows ∧ extensions.warnings 非空 |
| P25 | 查询超时保护 | ∀ 查询 q：q.duration > query_timeout ⟹ q.error = DATASOURCE_TIMEOUT |
| P26 | 权限检查 | ∀ 无 query 权限的请求：返回 AUTH_INSUFFICIENT_PERMISSION |
| P27 | 接口隔离 | Template_Engine 仅通过 RawExecutor 接口访问 Adapter，无法调用 Execute/HealthCheck 等方法 |

### SQL 注入防护（需求 6）

| # | 属性 | 描述 |
|---|------|------|
| P28 | safeString 转义正确性 | ∀ 字符串 s：safeString(s) 不包含未转义的单引号 ∧ 不包含未转义的反斜杠 ∧ 不包含 NULL 字节 |
| P29 | safeString 反斜杠安全 | ∀ 字符串 s 含 `\`：safeString(s) 中 `\` 被转义为 `\\` |
| P30 | safeInt 类型安全 | ∀ 输入 v：safeInt(v) 成功 ⟹ v 可解析为 int64 |
| P31 | safeFloat 类型安全 | ∀ 输入 v：safeFloat(v) 成功 ⟹ v 可解析为 float64 |
| P32 | safeIdentifier 字符安全 | ∀ 输入 v：safeIdentifier(v) 成功 ⟹ v 仅包含 `[a-zA-Z0-9_.]` |
| P33 | safeIdentifier 反引号包裹 | ∀ 输入 v = "a.b"：safeIdentifier(v) = `` `a`.`b` `` |
| P34 | safeIdentifier 段数限制 | ∀ 输入 v 含 2 个以上点号：safeIdentifier(v) 返回错误 |
| P35 | safeInList 元素独立转义 | ∀ 字符串数组 arr：safeInList(arr) 中每个元素独立经过 safeString 转义 |
| P36 | safeInList 空数组拒绝 | ∀ 空切片 arr（len=0）：safeInList(arr) 返回渲染错误 |
| P37 | safeLike 通配符转义 | ∀ 字符串 s 含 `%` 或 `_`：safeLike(s) 中这些字符被转义 |
| P38 | 多语句检测 | ∀ 含字符串外分号的 SQL：安全检查返回 VALIDATION_UNSAFE_SQL |
| P39 | SQL 注释检测 | ∀ 含字符串外 `--` 或 `/*` 的非 Hint 注释：注释被移除后再执行检查 |
| P40 | SQL Hint 保留 | ∀ 含 `/*+ ... */` 的 SQL：Optimizer Hint 不被移除 |

### 参数校验（需求 7）

| # | 属性 | 描述 |
|---|------|------|
| P41 | 必填参数检查 | ∀ required=true 的参数 p 未提供：返回 VALIDATION_MISSING_PARAMETER |
| P42 | 类型匹配检查 | ∀ 参数 p 类型不匹配 Schema：返回 VALIDATION_INVALID_PARAMETER_TYPE |
| P43 | 默认值填充 | ∀ 可选参数 p 未提供且有 default：渲染时 p = default |
| P44 | 枚举约束 | ∀ 参数 p 有 enum 约束且 p.value ∉ enum：返回 VALIDATION_INVALID_PARAMETER_VALUE |
| P45 | 字符串长度约束 | ∀ string 参数 p：len(p.value) > max_length ⟹ 返回错误 |
| P46 | 数组大小约束 | ∀ string[] 参数 p：len(p.value) > max_items ⟹ 返回错误 |
| P47 | 正则约束 | ∀ string 参数 p 有 pattern 约束：p.value 不匹配 pattern ⟹ 返回错误 |
| P48 | 正则预编译 | ∀ 模板配置中的 pattern：启动时预编译成功，否则模板不注册 |

### 缓存集成（需求 8）

| # | 属性 | 描述 |
|---|------|------|
| P49 | 缓存 key 确定性 | ∀ 相同的 (templateName, params, fields, pagination)：生成相同的缓存 key |
| P50 | 缓存 key 区分性 | ∀ 不同的 (templateName, params, fields, pagination)：生成不同的缓存 key |
| P51 | 缓存 key 含 fields | ∀ 不同 fields 的请求：生成不同的缓存 key（防止字段选择错误命中） |
| P52 | 模板级 TTL 覆盖 | ∀ 配置了 cache_ttl 的模板 t：缓存 TTL = t.cache_ttl |
| P53 | 缓存禁用 | ∀ cache_enabled=false 的模板 t：查询 t 不写入缓存 |
| P54 | 客户端缓存绕过 | ∀ extensions.cache=false 的请求：不读取缓存 |
| P55 | totalCount 独立缓存 | ∀ 请求 totalCount 的查询：count 缓存 key ≠ data 缓存 key |

### 可观测性（需求 9）

| # | 属性 | 描述 |
|---|------|------|
| P56 | 查询延迟指标记录 | ∀ 模板查询：graphql_template_query_duration_seconds 被记录 |
| P57 | 查询计数指标记录 | ∀ 模板查询：graphql_template_queries_total 递增 |
| P58 | 渲染延迟指标记录 | ∀ 模板渲染：graphql_template_render_duration_seconds 被记录 |
| P59 | Tracing Span 创建 | ∀ 模板查询：创建 `Template Query {name}` 子 Span |
| P60 | 审计日志记录 | ∀ 模板查询：审计日志包含 TemplateName 字段 |

### 热加载（需求 10）

| # | 属性 | 描述 |
|---|------|------|
| P61 | 热加载原子性 | ∀ 热加载期间的查询：使用旧版本或新版本模板，不会看到中间状态 |
| P62 | 错误隔离 | ∀ 热加载中语法错误的模板 t：t 保留旧版本，其他模板正常更新 |
| P63 | 缓存清除（仅变更） | ∀ 热加载中 hash 变化的模板 t：t 的缓存条目被清除；hash 未变的模板保留缓存 |
| P64 | 并发安全 | ∀ 并发的 reloadTemplates 调用（含 fsnotify 触发）：互斥锁保证串行执行 |
| P65 | 权限检查 | ∀ 无 mutation 权限的 reloadTemplates 调用：返回 AUTH_INSUFFICIENT_PERMISSION |
| P66 | 模板 hash 追踪 | ∀ 已注册模板 t：Template_Engine 维护 t 的 SHA-256 hash 用于变更检测 |
