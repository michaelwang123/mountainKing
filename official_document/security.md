# 安全指南

## 认证

服务支持两种认证方式，通过 `auth.method` 配置选择。

### JWT 认证

从 `Authorization: Bearer <token>` 头提取 JWT Token，验证：
- 签名算法（支持 HS256、RS256、ES256）
- 过期时间（`exp` claim）
- 签发者（`iss` claim）

生产环境推荐使用非对称签名（RS256/ES256），API 服务仅需配置公钥即可验证 Token，私钥留在认证服务端。

```yaml
auth:
  method: jwt
  jwt:
    algorithm: RS256
    public_key_file: /etc/secrets/jwt-public.pem
    issuer: my-auth-service
```

### API Key 认证

从 `X-API-Key` 头提取 API Key，使用 bcrypt 哈希比对（constant-time，防止 timing attack）。

每个 API Key 关联独立的权限范围：
- `datasources` — 允许访问的数据源列表
- `operations` — 允许的操作类型（`query`, `mutation`）

支持 API Key 轮换：为同一客户端同时配置新旧两个 Key，旧 Key 设置过期时间。

```yaml
auth:
  method: apikey
  apikey:
    keys:
      - id: client-a
        key: "${GRAPHQL_APIKEY_CLIENT_A}"  # bcrypt 哈希值
        permissions:
          datasources: [analytics_db, monitoring]
          operations: [query]
      - id: client-a-old
        key: "${GRAPHQL_APIKEY_CLIENT_A_OLD}"
        expires_at: "2026-06-01T00:00:00Z"
        permissions:
          datasources: [analytics_db]
          operations: [query]
```

API Key 存储要求：配置文件中的 `key` 字段必须存储 bcrypt 哈希值（非明文）。使用以下命令生成：

```bash
htpasswd -nbBC 10 "" "raw-api-key" | cut -d: -f2
```

### 公共端点

以下端点豁免认证和限流检查：
- `/health` — 存活检查
- `/ready` — 就绪检查
- `/metrics` — Prometheus 指标
- `/playground` — GraphQL Playground（仅开发模式）

## 授权

认证通过后，授权检查验证认证主体对目标数据源和操作类型的权限：
- 数据源权限：检查 `AuthIdentity.Datasources` 是否包含目标数据源
- 操作权限：检查 `AuthIdentity.Operations` 是否包含操作类型（`query` 或 `mutation`）

权限不足返回 HTTP 403 和 `AUTH_INSUFFICIENT_PERMISSION` 错误码。

## 请求限流

采用令牌桶算法，支持两种模式：

### 本地限流（默认）

每个实例独立限流。N 个实例 × `requests_per_window` = 全局最大请求量。

### 分布式限流

基于 Redis + Lua 脚本的原子令牌桶操作，所有实例共享全局限流总量。Redis 不可用时自动降级为本地模式。

### 限流 Key 优先级

1. API Key ID（`apikey:{id}`）
2. JWT sub claim（`jwt:{sub}`）
3. 客户端 IP（`ip:{addr}`）

### 限流响应头

所有非公共端点请求的响应均包含：
- `X-RateLimit-Limit` — 限流上限
- `X-RateLimit-Remaining` — 剩余可用请求数
- `X-RateLimit-Reset` — 限流重置时间（Unix 时间戳）

超限返回 HTTP 429。

批量查询按实际查询数（而非 HTTP 请求数）计数。

## 暴力破解防护

独立于正常限流，专门针对认证失败场景：
- 同一 IP 在 `window` 内认证失败超过 `threshold` 次，封禁 `ban_duration`
- 支持可信代理 IP 提取（`trusted_proxies` + `X-Forwarded-For`，取最右侧非信任 IP）

## 输入校验与注入防护

### SQL 注入防护（StarRocks）

- 所有 SQL 查询使用参数化查询（`?` 占位符）
- 表名和字段名通过白名单校验（`allowed_tables` 配置必填）
- 标识符使用反引号包裹
- 标识符格式校验：仅允许 `[a-zA-Z0-9_]`

### PromQL 注入防护（Prometheus）

- 标签值输入校验，拒绝包含 PromQL 特殊字符（`}`, `{`, `|`, `~`, `"`）的输入
- 查询表达式校验（子查询嵌套深度、高开销操作检查）

## CSRF 防护

- 生产模式下 GET 查询端点默认禁用（`allow_get_queries: false`）
- POST 请求要求 `Content-Type: application/json`，浏览器简单表单提交无法触发

## 敏感信息脱敏

日志和 Trace Span 中的 `db.statement` 属性自动脱敏：

```yaml
sanitization:
  enabled: true
  rules:
    - pattern: "'[^']*'"        # SQL 字符串字面量 → '***'
      replacement: "'***'"
    - pattern: "\\b\\d{4,}\\b"  # 4位以上数值 → ***
      replacement: "***"
```

## 审计日志

独立于应用日志，记录：
- 认证主体标识（JWT sub 或 API Key ID）
- 操作时间
- 操作类型
- 目标数据源
- 请求结果（成功/失败）

## TLS/HTTPS

本服务不直接处理 TLS 终止。TLS 由前置负载均衡器（Nginx Ingress、AWS ALB、Envoy）负责。服务仅监听 HTTP 端口。如需服务间 mTLS（如 Istio Service Mesh），由 Sidecar Proxy 透明处理。

## Introspection 控制

生产环境建议禁用 GraphQL Introspection 查询，防止 Schema 信息泄露：

```yaml
graphql:
  introspection_enabled: false
```
