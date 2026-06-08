# 模块 12：高级主题与最佳实践

> 中间件链机制、配置热更新、批量查询、错误处理体系和扩展开发指南。

## 12.1 中间件链

mountainKing 使用 chi 的中间件链模式，请求按顺序经过每个中间件：

```
RequestID → BodyLimit → CORS → CSRF → Auth → AuthFailureLimiter → RateLimit → Compression
```

### 中间件职责

| 中间件 | 文件 | 职责 |
|--------|------|------|
| RequestID | chi 内置 | 生成唯一请求 ID |
| BodyLimit | `bodylimit.go` | 限制请求体大小（默认 1MB） |
| CORS | `cors.go` | 跨域资源共享 |
| CSRF | `csrf.go` | 生产模式禁用 GET 查询 |
| Auth | `auth_middleware.go` | JWT/API Key 认证 |
| AuthFailureLimiter | `auth_failure_limiter.go` | 暴力破解防护 |
| RateLimit | `ratelimit.go` | 令牌桶限流 |
| Compression | `compression.go` | gzip 响应压缩 |
| Concurrency | `concurrency.go` | 全局并发请求限制 |

### 公共端点豁免

认证和限流中间件对公共端点（`/health`、`/ready`、`/metrics`、`/playground`）自动豁免。

### 自定义中间件

基于 chi 的标准中间件签名：

```go
func MyMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 前置逻辑
        next.ServeHTTP(w, r)
        // 后置逻辑
    })
}
```

## 12.2 配置热更新

以下配置支持运行时热更新（无需重启服务）：

| 配置项 | 说明 |
|--------|------|
| `logging.level` | 日志级别 |
| `rate_limit.requests_per_window` | 限流阈值 |
| `rate_limit.window_size` | 限流窗口 |
| `cache.default_ttl` | 缓存默认 TTL |

### 实现机制

基于 Viper 的 `WatchConfig()` + fsnotify 文件监听：

1. 检测到 `config.yaml` 变更
2. 重新加载配置
3. 校验变更的配置项
4. 原子更新运行时参数

### 环境变量覆盖

环境变量始终优先于配置文件，格式：`GRAPHQL_` + 配置路径（`_` 分隔）。

```bash
export GRAPHQL_LOGGING_LEVEL=debug
export GRAPHQL_RATE_LIMIT_REQUESTS_PER_WINDOW=200
export GRAPHQL_CACHE_DEFAULT_TTL=120s
```

## 12.3 批量查询

客户端可在一个 HTTP POST 请求中发送多个 GraphQL 查询：

```json
[
  { "query": "{ starrocks(table: \"t1\", first: 10) { nodes { data } } }" },
  { "query": "{ starrocks(table: \"t2\", first: 10) { nodes { data } } }" },
  { "query": "{ prometheusInstant(query: \"up\") { metric { labels } value } }" }
]
```

响应为对应的结果数组：

```json
[
  { "data": { "starrocks": { ... } } },
  { "data": { "starrocks": { ... } } },
  { "data": { "prometheusInstant": { ... } } }
]
```

### 限制

```yaml
server:
  max_batch_queries: 10    # 单次批量最大查询数
```

### 限流计数

批量查询按实际查询数计数。3 个查询的批量请求消耗 3 个限流令牌。

## 12.4 错误处理体系

### 错误码分类

| 前缀 | 分类 | HTTP 状态码 |
|------|------|-------------|
| `AUTH_` | 认证/授权错误 | 401/403 |
| `VALIDATION_` | 输入校验错误 | 400 |
| `DATASOURCE_` | 数据源错误 | 502/504 |
| `INTERNAL_` | 内部错误 | 500 |

### 常见错误码

| 错误码 | 说明 |
|--------|------|
| `AUTH_MISSING` | 缺少认证凭据 |
| `AUTH_TOKEN_EXPIRED` | Token 已过期 |
| `AUTH_TOKEN_INVALID` | Token 无效 |
| `AUTH_INSUFFICIENT_PERMISSION` | 权限不足 |
| `VALIDATION_INVALID_TABLE` | 表名不在白名单 |
| `VALIDATION_INVALID_COLUMN` | 列名不在白名单 |
| `VALIDATION_TEMPLATE_NOT_FOUND` | 模板不存在 |
| `VALIDATION_MISSING_PARAMETER` | 必填参数缺失 |
| `VALIDATION_INVALID_PARAMETER_TYPE` | 参数类型错误 |
| `VALIDATION_INVALID_PARAMETER_VALUE` | 参数值不合法 |
| `VALIDATION_UNSAFE_SQL` | SQL 安全检查失败 |
| `VALIDATION_DEPTH_EXCEEDED` | 查询深度超限 |
| `DATASOURCE_UNAVAILABLE` | 数据源不可用 |
| `DATASOURCE_TIMEOUT` | 查询超时 |
| `DATASOURCE_CIRCUIT_OPEN` | 熔断器打开 |
| `INTERNAL_TEMPLATE_RENDER_ERROR` | 模板渲染失败 |

### 错误响应格式

```json
{
  "errors": [
    {
      "message": "table \"unknown_table\" is not in the allowed list",
      "extensions": {
        "code": "VALIDATION_INVALID_TABLE",
        "classification": "VALIDATION"
      }
    }
  ],
  "data": null
}
```

### 部分失败

跨数据源查询时，一个数据源失败不影响其他数据源的结果：

```json
{
  "data": {
    "starrocks": { "nodes": [...] },
    "prometheusInstant": null
  },
  "errors": [
    {
      "message": "datasource 'monitoring' is unavailable",
      "path": ["prometheusInstant"],
      "extensions": { "code": "DATASOURCE_UNAVAILABLE" }
    }
  ]
}
```

## 12.5 测试策略

### 单元测试

```bash
make test    # go test -race -coverprofile=coverage.out ./...
```

### 属性测试

项目使用 `pgregory.net/rapid` 框架进行属性测试，覆盖：
- 安全函数（safeString、safeInt 等）对任意输入的正确性
- 缓存 Key 生成的确定性
- 配置校验的完备性
- 熔断器状态转换的正确性

```go
func TestProperty_SafeString(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        input := rapid.String().Draw(t, "input")
        result, err := safeString(input)
        // 验证：结果不包含未转义的单引号
        // 验证：结果不包含 NULL 字节
    })
}
```

### Fuzz 测试

```bash
make fuzz    # 运行 Go 原生 fuzz 测试（默认 30 秒）
```

## 12.6 扩展开发指南

### 添加新数据源

1. 创建 `internal/adapter/<name>/` 目录
2. 实现 `DataSource` 接口
3. 编写 `.graphql` Schema
4. 注册工厂函数
5. 添加配置项
6. 运行 `make generate`

### 添加新中间件

1. 在 `internal/middleware/` 创建文件
2. 实现 `func(http.Handler) http.Handler` 签名
3. 在 `cmd/server/main.go` 中注册到中间件链

### 添加新模板函数

1. 在 `internal/template/funcmap.go` 中实现函数
2. 在 `buildFuncMap()` 中注册
3. 编写单元测试和属性测试
4. 更新文档

### 添加新 GraphQL Mutation

1. 在 `internal/graphql/schema/mutation.graphql` 中定义
2. 运行 `make generate`
3. 在 `internal/graphql/resolver/mutation.resolvers.go` 中实现
4. 添加授权检查（需要 `mutation` 操作权限）

## 12.7 项目规范

- 所有公开函数和类型必须有 GoDoc 注释
- 错误处理使用 `internal/errors` 包的统一错误码
- 日志使用 zap 的结构化字段（不要用 `fmt.Sprintf`）
- 配置项使用 snake_case 命名
- 测试文件与源文件同目录，命名 `*_test.go`
- 属性测试文件命名 `*_property_test.go`

---

返回：[课程目录](index.md)
