# 模块 06：安全体系详解

> 全面理解 mountainKing 的认证、授权、输入校验和安全防护机制。

## 6.1 安全架构总览

```
请求进入
    │
    ▼
BodyLimit（请求体大小限制，防止 DoS）
    │
    ▼
CORS（跨域资源共享控制）
    │
    ▼
CSRF（跨站请求伪造防护，生产模式禁用 GET 查询）
    │
    ▼
Auth（认证：JWT / API Key / none）
    │
    ▼
AuthFailureLimiter（暴力破解防护）
    │
    ▼
RateLimit（令牌桶限流）
    │
    ▼
GraphQL Engine（复杂度/深度检查）
    │
    ▼
Resolver（授权检查 + 白名单校验）
```

## 6.2 认证方式

通过 `auth.method` 配置选择认证方式：`jwt`、`apikey` 或 `none`（仅开发环境）。

### JWT 认证

从 `Authorization: Bearer <token>` 头提取 JWT Token，验证签名、过期时间和签发者。

```yaml
auth:
  method: jwt
  jwt:
    algorithm: RS256                    # HS256 | RS256 | ES256
    public_key_file: /etc/secrets/jwt-public.pem
    issuer: my-auth-service
```

支持的算法：
- **HS256**：对称密钥，适合内部服务间通信
- **RS256**：RSA 非对称签名，生产推荐（API 服务仅需公钥）
- **ES256**：ECDSA 非对称签名，密钥更短、性能更好

### API Key 认证

从 `X-API-Key` 头提取 API Key，使用 bcrypt 哈希比对（constant-time，防止 timing attack）。

```yaml
auth:
  method: apikey
  apikey:
    keys:
      - id: client-a
        key: "${GRAPHQL_APIKEY_CLIENT_A}"    # bcrypt 哈希值
        permissions:
          datasources: [analytics_db]
          operations: [query]
```

生成 bcrypt 哈希：

```bash
htpasswd -nbBC 10 "" "your-raw-api-key" | cut -d: -f2
```

### API Key 轮换

为同一客户端同时配置新旧两个 Key，旧 Key 设置过期时间：

```yaml
keys:
  - id: client-a-new
    key: "${GRAPHQL_APIKEY_CLIENT_A_NEW}"
    permissions: { datasources: [analytics_db], operations: [query] }
  - id: client-a-old
    key: "${GRAPHQL_APIKEY_CLIENT_A_OLD}"
    expires_at: "2026-06-01T00:00:00Z"
    permissions: { datasources: [analytics_db], operations: [query] }
```

## 6.3 授权模型

认证通过后，授权检查验证两个维度：
- **数据源权限**：`AuthIdentity.Datasources` 是否包含目标数据源
- **操作权限**：`AuthIdentity.Operations` 是否包含操作类型（`query` 或 `mutation`）

权限不足返回 HTTP 403 和 `AUTH_INSUFFICIENT_PERMISSION` 错误码。

JWT 认证的权限通过 Token claims 传递；API Key 认证的权限在配置文件中定义。

## 6.4 公共端点

以下端点豁免认证和限流：
- `/health` — 存活检查
- `/ready` — 就绪检查
- `/metrics` — Prometheus 指标
- `/playground` — GraphQL Playground（仅开发模式）

## 6.5 暴力破解防护

```yaml
auth_failure:
  enabled: true
  threshold: 10       # 窗口内最大失败次数
  window: 5m          # 时间窗口
  ban_duration: 15m   # 封禁时长
```

按客户端 IP 追踪认证失败次数，超过阈值后临时封禁。支持 `trusted_proxies` 配置以正确提取真实客户端 IP。

## 6.6 CSRF 防护

生产模式下默认禁用 GET 查询（`allow_get_queries: false`），防止 CSRF 攻击。只允许 POST 请求携带 `Content-Type: application/json`。

## 6.7 输入校验

### GraphQL 层

- **查询复杂度限制**：`graphql.max_query_complexity`（默认 100）
- **查询深度限制**：`graphql.max_query_depth`（默认 10）
- **结果集截断**：`graphql.max_result_rows`（默认 10000）
- **请求体大小**：`server.max_request_body_size`（默认 1MB）
- **批量查询限制**：`server.max_batch_queries`（默认 10）

### StarRocks 层

- 表名/列名白名单校验
- 参数化查询（防止 SQL 注入）

### 模板引擎层

- 参数类型/必填/枚举/长度/正则校验
- 12 个安全模板函数
- 7 状态词法扫描器（多语句注入检测、SQL 注释移除）
- 渲染后 SQL 长度限制（`max_rendered_sql_length`）

## 6.8 敏感信息脱敏

```yaml
sanitization:
  enabled: true
  rules:
    - pattern: "'[^']*'"           # SQL 字符串字面量
      replacement: "'***'"
    - pattern: "\\b\\d{4,}\\b"     # 4位以上数字
      replacement: "***"
```

脱敏应用于：
- 日志输出
- OpenTelemetry Span 的 `db.statement` 属性
- 错误消息中的 SQL 片段

## 6.9 Schema 自省控制

```yaml
graphql:
  introspection_enabled: false    # 生产环境建议禁用
```

禁用后，客户端无法通过 `__schema` 和 `__type` 查询获取 Schema 信息，防止信息泄露。

## 6.10 安全检查清单

| 检查项 | 配置 | 生产建议 |
|--------|------|----------|
| 认证方式 | `auth.method` | `jwt`（RS256/ES256） |
| Schema 自省 | `graphql.introspection_enabled` | `false` |
| GET 查询 | `server.allow_get_queries` | `false` |
| 暴力破解防护 | `auth_failure.enabled` | `true` |
| 请求体限制 | `server.max_request_body_size` | `1MB` |
| 查询复杂度 | `graphql.max_query_complexity` | `100` |
| 查询深度 | `graphql.max_query_depth` | `10` |
| 脱敏 | `sanitization.enabled` | `true` |
| CORS | `cors.enabled` | 按需配置允许的域名 |

---

下一模块：[缓存策略与实践](07-caching.md)
