# 性能调优

## 性能目标

| 场景 | P95 延迟 | P99 延迟 |
|------|---------|---------|
| 单数据源简单查询 | ≤ 200ms | ≤ 500ms |
| 跨数据源混合查询 | ≤ 500ms | ≤ 1000ms |

以上延迟不含数据源自身查询耗时。

并发能力：支持至少 1000 个并发 GraphQL 查询连接。

## 缓存策略

### 缓存层架构

Cache Layer 位于 DataLoader 之后、DataSource Manager 之前：

```
Resolver → DataLoader（批量合并） → Cache Layer → DataSource Manager
```

DataLoader 先将同一数据源的多个 resolver 请求批量合并，合并后的每个请求独立经过 Cache Layer 查询缓存。

### 缓存 Key 生成

格式：`cache:{datasource}:{xxhash64(normalized_query + sorted_variables)}`

- 使用 xxhash64（非密码学高性能哈希）
- 查询文本在哈希前进行规范化（去除空格、注释，统一大小写）
- 数据源前缀支持按数据源清除缓存

### 缓存防护

| 防护类型 | 机制 | 配置 |
|---------|------|------|
| 穿透防护 | 空结果缓存短 TTL | `empty_result_ttl: 30s` |
| 雪崩防护 | TTL 随机抖动 | `ttl_jitter_percent: 10` |
| 击穿防护 | Singleflight | 同一 key 并发回源只执行一次 |

### 缓存后端选择

| 后端 | 适用场景 | 特点 |
|------|---------|------|
| memory | 单实例或低延迟要求 | LRU 淘汰，max_entries + max_memory_size 双重限制 |
| redis | 多实例共享缓存 | 跨实例共享，SCAN + DEL 按前缀删除 |

### 缓存序列化

使用 `encoding/gob` 格式，比 JSON 快 2-5 倍。反序列化失败时自动删除损坏条目并回源查询。

### 每数据源 TTL

```yaml
cache:
  default_ttl: 60s
  per_datasource:
    analytics_db:
      ttl: 300s      # OLAP 数据变化慢，TTL 较长
    monitoring:
      ttl: 30s        # 监控数据实时性要求高，TTL 较短
```

## DataLoader

### 批量合并

DataLoader 将同一请求中针对同一数据源的多个 resolver 调用批量合并：
- 批量窗口：1ms（可配置）
- 最大批量大小：100（可配置）

### Per-Request 隔离

DataLoader 实例必须是 per-request 的，禁止跨请求共享，避免数据泄漏。

## 连接池调优

### StarRocks

```yaml
options:
  pool_size: 20              # 根据并发查询量调整
  connection_timeout: 5s
  query_timeout: 30s
  pool_acquire_timeout: 5s   # 连接获取超时
```

### 连接池大小规划

```
单 Pod 连接数 = pool_size
总连接数 = pool_size × Pod 数量（HPA max_replicas）
```

确保数据源能承受最大 Pod 数量下的总连接数。

## 熔断器调优

```yaml
circuit_breaker:
  failure_threshold: 5       # 连续失败 5 次后熔断
  open_duration: 30s         # 熔断 30s 后进入半开状态
  half_open_max_requests: 1  # 半开状态允许 1 个探测请求
  success_threshold: 2       # 连续成功 2 次后恢复
```

熔断器防止级联故障：数据源不可用时快速失败，避免连接池耗尽。

## 重试策略

```yaml
retry:
  max_retries: 3
  retry_interval: 100ms
  backoff: exponential       # 100ms → 200ms → 400ms
```

仅对瞬时错误（连接超时、网络中断）重试，业务错误（SQL 语法错误）立即返回。

## 超时控制

双层超时机制：

1. **请求级超时**（`request_timeout: 30s`）— 整个 HTTP 请求的总超时
2. **查询级超时**（`query_timeout`）— 单个数据源查询超时

查询级超时取 `min(query_timeout, 请求剩余时间)`，确保不超过请求总超时。

## 查询保护

| 保护机制 | 配置 | 说明 |
|---------|------|------|
| 复杂度限制 | `max_query_complexity: 100` | 拒绝超复杂查询 |
| 深度限制 | `max_query_depth: 10` | 拒绝过深嵌套 |
| 结果截断 | `max_result_rows: 10000` | 截断超大结果集 |
| 请求体限制 | `max_request_body_size: 1MB` | 拒绝超大请求 |
| 批量限制 | `max_batch_queries: 10` | 限制批量查询数 |

## 响应压缩

```yaml
compression:
  enabled: true
  min_size: 1KB
```

当响应体超过 `min_size` 且客户端支持 gzip 时自动压缩，减少网络传输量。

## 基准测试

项目包含 Go benchmark 测试（`internal/server/benchmark_test.go`），覆盖：
- 单数据源简单查询延迟
- 跨数据源混合查询延迟
- 并发查询吞吐量
- 缓存命中/未命中场景对比

运行基准测试：

```bash
go test -bench=. -benchmem ./internal/server/
```

## APQ（Automatic Persisted Queries）

启用后显著减少网络传输量：
- 首次请求：发送完整查询文本 + SHA256 哈希
- 后续请求：仅发送哈希值

```yaml
graphql:
  apq_enabled: true
```

APQ 缓存复用 Cache Layer 后端配置。多实例部署推荐使用 Redis 后端避免冷启动。
