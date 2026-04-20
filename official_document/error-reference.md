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

## DATASOURCE — 数据源错误

HTTP 状态码：200（GraphQL 层面的部分错误）

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `DATASOURCE_TIMEOUT` | 数据源查询超时 | 查询耗时超过 `query_timeout` |
| `DATASOURCE_UNAVAILABLE` | 数据源不可用 | 数据源连接断开或熔断器处于 OPEN 状态 |
| `DATASOURCE_CONNECTION_EXHAUSTED` | 连接池耗尽 | 连接获取等待超过 `pool_acquire_timeout` |
| `DATASOURCE_QUERY_ERROR` | 数据源查询错误 | SQL 语法错误、PromQL 语法错误等业务错误 |
| `DATASOURCE_MAX_DATAPOINTS` | 数据点超限 | Prometheus 返回数据量超过 `max_data_points` |

## RATELIMIT — 限流错误

HTTP 状态码：429

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `RATELIMIT_EXCEEDED` | 请求频率超限 | 令牌桶中令牌不足 |

限流响应头：
- `X-RateLimit-Limit` — 限流上限
- `X-RateLimit-Remaining` — 剩余可用请求数
- `X-RateLimit-Reset` — 限流重置时间（Unix 时间戳）

## INTERNAL — 内部错误

HTTP 状态码：500

| 错误码 | 说明 | 触发条件 |
|--------|------|---------|
| `INTERNAL_UNEXPECTED` | 未预期的内部错误 | 未捕获的异常或编程错误 |
| `INTERNAL_CACHE_ERROR` | 缓存操作错误 | 缓存后端读写失败（不影响查询，降级为直接查询数据源） |

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
