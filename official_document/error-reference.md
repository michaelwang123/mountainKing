# 错误码参考

## 概述

所有错误码遵循 `{CATEGORY}_{ERROR_NAME}` 命名规范。错误信息通过 GraphQL 响应的 `errors[].extensions` 返回，包含 `code`（错误码）和 `classification`（错误分类）字段。

```json
{
  "errors": [{
    "message": "human-readable error description",
    "path": ["fieldName"],
    "extensions": {
      "code": "DATASOURCE_TIMEOUT",
      "classification": "DATASOURCE"
    }
  }]
}
```

## AUTH — 认证授权错误

HTTP 状态码：401 或 403

| 错误码 | HTTP | 说明 | 触发条件 |
|--------|------|------|---------|
| `AUTH_MISSING` | 401 | 缺少认证凭据 | 请求未携带 Authorization 或 X-API-Key 头 |
| `AUTH_TOKEN_EXPIRED` | 401 | JWT Token 已过期 | Token 的 exp claim 早于当前时间 |
| `AUTH_TOKEN_INVALID` | 401 | JWT Token 无效 | 签名验证失败、格式错误或签发者不匹配 |
| `AUTH_APIKEY_INVALID` | 401 | API Key 无效 | Key 不在已注册列表中 |
| `AUTH_APIKEY_EXPIRED` | 401 | API Key 已过期 | Key 的 expires_at 早于当前时间 |
| `AUTH_INSUFFICIENT_PERMISSION` | 403 | 权限不足 | 认证主体无权访问目标数据源或操作类型 |
| `AUTH_BRUTE_FORCE_BLOCKED` | 429 | 暴力破解封禁 | 同一 IP 认证失败次数超过阈值 |

## VALIDATION — 请求验证错误

HTTP 状态码：400

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `VALIDATION_SYNTAX_ERROR` | GraphQL 语法错误 | 查询文本不符合 GraphQL 语法规范 |
| `VALIDATION_COMPLEXITY_EXCEEDED` | 查询复杂度超限 | 查询复杂度超过 `max_query_complexity` |
| `VALIDATION_DEPTH_EXCEEDED` | 查询深度超限 | 查询嵌套深度超过 `max_query_depth` |
| `VALIDATION_BATCH_EXCEEDED` | 批量查询数超限 | 批量查询数超过 `max_batch_queries` |
| `VALIDATION_TABLE_NOT_ALLOWED` | 表名不在白名单 | StarRocks 查询的表名不在 `allowed_tables` 中 |
| `VALIDATION_FIELD_NOT_ALLOWED` | 字段名不在白名单 | StarRocks 查询的字段名不在对应表的 columns 中 |
| `VALIDATION_INVALID_IDENTIFIER` | 标识符格式非法 | 标识符包含非 `[a-zA-Z0-9_]` 字符 |
| `VALIDATION_PROMQL_INJECTION` | PromQL 注入检测 | 标签值包含 PromQL 特殊字符 |
| `VALIDATION_INVALID_FILTER` | 过滤条件无效 | 过滤条件格式或值不合法 |
| `VALIDATION_TEMPLATE_NOT_FOUND` | 模板不存在 | 请求的模板名称未在 Template Registry 中注册 |
| `VALIDATION_UNSAFE_SQL` | 渲染 SQL 不安全 | 渲染结果包含多条 SQL 语句（分号检测）或超过最大长度限制 |
| `VALIDATION_MISSING_PARAMETER` | 必填参数缺失 | 模板必填参数未在请求中提供 |
| `VALIDATION_INVALID_PARAMETER_TYPE` | 参数类型不匹配 | 参数值的数据类型与 Schema 定义不匹配 |
| `VALIDATION_INVALID_PARAMETER_VALUE` | 参数值约束违反 | 参数值不在枚举范围内、超过长度限制、超过数组元素数量限制或不匹配正则约束 |

## DATASOURCE — 数据源错误

HTTP 状态码：200（GraphQL 层面的部分错误）

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `DATASOURCE_TIMEOUT` | 数据源查询超时 | 查询耗时超过 `query_timeout` |
| `DATASOURCE_UNAVAILABLE` | 数据源不可用 | 数据源连接断开或熔断器处于 OPEN 状态 |
| `DATASOURCE_CONNECTION_EXHAUSTED` | 连接池耗尽 | 连接获取等待超过 `pool_acquire_timeout` |
| `DATASOURCE_QUERY_ERROR` | 数据源查询错误 | SQL 语法错误、PromQL 语法错误等业务错误 |
| `DATASOURCE_MAX_DATAPOINTS` | 数据点超限 | Prometheus 返回数据量超过 `max_data_points` |
| `DATASOURCE_TEMPLATE_QUERY_ERROR` | 模板查询执行错误 | StarRocks 执行模板 SQL 失败 |

## RATELIMIT — 限流错误

HTTP 状态码：429

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `RATELIMIT_EXCEEDED` | 请求频率超限 | 令牌桶中令牌不足 |

限流响应头：
- `X-RateLimit-Limit` — 限流上限
- `X-RateLimit-Remaining` — 剩余可用请求数
- `X-RateLimit-Reset` — 限流重置时间（Unix 时间戳）

## MUTATION — Mutation 操作错误

Mutation（CUD 写操作）相关错误。这些错误通过 GraphQL `errors[].extensions.code` 返回。

| 错误码 | 说明 | 可能原因 | 解决方法 |
|--------|------|----------|----------|
| `MUTATION_FEATURE_DISABLED` | Mutation 功能未启用 | 配置文件中 `mutations.enabled` 设为 `false` 或未配置 | 在配置文件中设置 `mutations.enabled: true`（支持热更新，无需重启） |
| `AUTH_INSUFFICIENT_PERMISSION` | 缺少 mutation 操作权限 | JWT 的 operations claim 不包含 `"mutation"`，或 API Key 的 operations 列表未配置 `mutation` | 在 JWT payload 的 operations 字段中添加 `"mutation"`；或在 API Key 配置中添加 `mutation` 到 operations 列表 |
| `MUTATION_OPERATION_NOT_SUPPORTED` | 表不支持请求的操作类型 | `writable_tables` 中该表的 `allowed_operations` 列表未包含请求的操作（insert/update/delete） | 在数据源配置的 `writable_tables` 中为目标表添加所需操作到 `allowed_operations` |
| `VALIDATION_BATCH_LIMIT_EXCEEDED` | 批量插入大小超限 | `insertBatchStarrocks` 的 rows 数量超过 `mutations.max_batch_size` 配置值（默认 500） | 减少单次批量插入的行数，或调整 `mutations.max_batch_size` 配置 |
| `VALIDATION_PAYLOAD_TOO_LARGE` | 生成的 SQL 语句过长 | Mutation 构建的 SQL 语句长度超过 `mutations.max_sql_length` 配置值（默认 1048576 字节） | 减少单次操作的数据量，或调整 `mutations.max_sql_length` 配置 |
| `MUTATION_RATELIMIT_EXCEEDED` | Mutation 专用限流触发 | 写操作频率超过 `mutations.rate_limit` 配置的 `requests_per_window`（默认 20 次/60s） | 降低 Mutation 请求频率，或调整 `mutations.rate_limit.requests_per_window` 和 `window_size` 配置 |
| `MUTATION_LIMIT_EXCEEDED` | 影响行数超过阈值（警告） | Mutation 执行后实际影响的行数超过 `mutations.max_affected_rows`（默认 1000） | 此为警告信息，操作已执行成功。可通过添加更精确的 filter 条件缩小影响范围，或调整 `mutations.max_affected_rows` 配置 |

> **注意**：`AUTH_INSUFFICIENT_PERMISSION` 同时适用于数据源访问权限和 Mutation 操作权限场景，详见 [AUTH 错误码](#auth--认证授权错误) 章节。`VALIDATION_BATCH_LIMIT_EXCEEDED` 和 `VALIDATION_PAYLOAD_TOO_LARGE` 也适用于非 Mutation 场景（批量查询数超限、请求体过大），此处为其在 Mutation 上下文中的触发条件。

### Mutation 错误处理建议

```
if error.extensions.code == "MUTATION_FEATURE_DISABLED":
    # 检查服务配置，确认 mutations.enabled: true
    check_config("mutations.enabled")
elif error.extensions.code == "AUTH_INSUFFICIENT_PERMISSION":
    # 确认 JWT/API Key 包含 mutation 权限
    ensure_operations_claim_includes("mutation")
elif error.extensions.code == "MUTATION_OPERATION_NOT_SUPPORTED":
    # 检查 writable_tables 配置中该表的 allowed_operations
    check_writable_tables_config(table_name)
elif error.extensions.code == "MUTATION_RATELIMIT_EXCEEDED":
    # 等待限流窗口重置后重试
    wait_and_retry(rate_limit_window)
elif error.extensions.code == "VALIDATION_BATCH_LIMIT_EXCEEDED":
    # 将批量数据拆分为更小的批次
    split_batch(rows, max_batch_size)
```

## INTERNAL — 内部错误

HTTP 状态码：500

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `INTERNAL_UNEXPECTED` | 未预期的内部错误 | 未捕获的异常或编程错误 |
| `INTERNAL_CACHE_ERROR` | 缓存操作错误 | 缓存后端读写失败（不影响查询，降级为直接查询数据源） |
| `INTERNAL_TEMPLATE_RENDER_ERROR` | 模板渲染失败 | 模板渲染过程中发生错误（语法错误、未定义参数、渲染超时） |

## HTTP 状态码映射

| HTTP 状态码 | 场景 |
|------------|------|
| 200 | 查询成功（即使部分数据源失败，GraphQL 层面仍返回 200） |
| 400 | 请求体格式错误、GraphQL 语法错误、批量查询超限 |
| 401 | 认证失败（缺少凭据、Token 过期/无效） |
| 403 | 授权失败（权限不足） |
| 413 | 请求体超过 `max_request_body_size` |
| 429 | 限流或暴力破解封禁 |
| 503 | 健康检查失败（`/health` 或 `/ready`） |

## 客户端错误处理建议

```
if response.status == 401:
    refresh_token_or_reauth()
elif response.status == 429:
    wait_until(response.headers["X-RateLimit-Reset"])
    retry()
elif response.status == 200 and response.errors:
    for error in response.errors:
        if error.extensions.code.startswith("DATASOURCE_"):
            # 数据源错误，其他数据源结果可能仍然可用
            handle_partial_failure(error)
        elif error.extensions.code.startswith("VALIDATION_"):
            # 查询本身有问题，需要修改查询
            fix_query(error)
```
