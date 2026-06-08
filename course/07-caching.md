# 模块 07：缓存策略与实践

> 深入理解缓存层架构、三重防护机制和 TTL 策略。

## 7.1 缓存层架构

```
查询请求
    │
    ▼
CacheLayer.GetOrLoad(key, datasource, loader)
    │
    ├── 1. 检查缓存 → 命中 → 返回（gob 反序列化）
    │
    ├── 2. 未命中 → singleflight 去重
    │       │
    │       └── 只有一个 goroutine 执行 loader
    │
    ├── 3. 空结果 → 缓存 emptyMarker（短 TTL）
    │
    └── 4. 有结果 → gob 序列化 → 缓存（TTL + jitter）
```

## 7.2 缓存后端

### 内存缓存（默认）

基于 `hashicorp/golang-lru/v2`，适合单实例部署：

```yaml
cache:
  enabled: true
  backend: memory
  memory:
    max_entries: 10000
    max_memory_size: 256MB    # 双重限制：条目数 + 内存大小
```

### Redis 缓存

适合多实例部署，缓存共享：

```yaml
cache:
  enabled: true
  backend: redis
  redis:
    addr: redis:6379
    password: "${GRAPHQL_REDIS_PASSWORD}"
    db: 1
```

## 7.3 三重防护

### 缓存穿透防护

查询不存在的数据时，缓存中没有命中，每次都穿透到数据源。

**解决方案**：空结果缓存。当 loader 返回空数据时，缓存一个 `emptyMarker` 标记，使用较短的 TTL（默认 30s）。

```yaml
cache:
  empty_result_ttl: 30s
```

### 缓存雪崩防护

大量缓存条目同时过期，导致请求同时穿透到数据源。

**解决方案**：TTL 抖动。在基础 TTL 上添加 ±N% 的随机偏移：

```yaml
cache:
  ttl_jitter_percent: 10    # ±10% 随机抖动
```

例如 TTL=60s，实际 TTL 在 54s~66s 之间随机分布。

### 缓存击穿防护

热点 key 过期瞬间，大量并发请求同时穿透到数据源。

**解决方案**：singleflight。使用 `golang.org/x/sync/singleflight` 确保同一 key 只有一个 goroutine 执行 loader，其他请求等待并共享结果。

## 7.4 TTL 策略

### 全局默认 TTL

```yaml
cache:
  default_ttl: 60s
```

### 按数据源 TTL

```yaml
cache:
  per_datasource:
    analytics_db:
      ttl: 300s       # StarRocks 数据变化慢，TTL 长
    monitoring:
      ttl: 30s        # Prometheus 指标实时性要求高
```

### 模板级 TTL

```yaml
sql_templates:
  templates:
    - name: fleet_report
      cache_ttl: 300s    # 覆盖数据源默认 TTL
```

优先级：模板级 TTL > 数据源 TTL > 全局默认 TTL

## 7.5 缓存 Key 生成

缓存 Key 格式：`cache:{datasource}:{xxhash}`

xxhash 输入包含：
- 数据源名称
- 表名或模板名
- 查询参数（过滤条件、字段列表）
- 分页参数（first、offset）
- 排序参数

`totalCount` 使用独立的缓存 Key（不含分页参数），因为总数不随翻页变化。

## 7.6 缓存管理

### 清除缓存

通过 GraphQL Mutation：

```graphql
# 清除特定数据源缓存
mutation {
  clearCache(datasource: "analytics_db")
}

# 清除所有缓存
mutation {
  clearCache
}
```

### 绕过缓存

客户端可通过 GraphQL extensions 绕过缓存：

```json
{
  "query": "{ starrocks(...) { ... } }",
  "extensions": { "cache": false }
}
```

### 模板缓存自动清除

模板文件变更时（热加载），引擎通过 SHA-256 hash 比较检测变更，仅清除变更模板的缓存条目。

## 7.7 缓存监控

相关 Prometheus 指标：

| 指标 | 说明 |
|------|------|
| `graphql_cache_hits_total` | 缓存命中计数 |
| `graphql_cache_misses_total` | 缓存未命中计数 |
| `graphql_template_cache_hits_total` | 模板缓存命中/未命中 |

缓存命中率 = hits / (hits + misses)。生产环境建议命中率 > 80%。

## 7.8 最佳实践

1. **合理设置 TTL**：数据变化频率低的用长 TTL，实时性要求高的用短 TTL
2. **启用 jitter**：防止缓存雪崩，建议 10%
3. **监控命中率**：命中率过低说明 TTL 太短或查询模式分散
4. **按需禁用**：对实时性要求极高的查询，在模板配置中设 `cache_enabled: false`
5. **内存限制**：内存缓存设置合理的 `max_entries` 和 `max_memory_size`，防止 OOM

---

下一模块：[可观测性体系](08-observability.md)
