# 模块 11：性能调优指南

> 连接池优化、DataLoader 机制、查询优化策略、缓存调优和负载测试。

## 11.1 性能目标

| 场景 | P95 延迟 | P99 延迟 |
|------|----------|----------|
| 单数据源查询 | ≤ 200ms | ≤ 500ms |
| 跨数据源混合查询 | ≤ 500ms | ≤ 1s |
| 模板查询（复杂报表） | ≤ 1s | ≤ 2s |
| 缓存命中 | ≤ 5ms | ≤ 10ms |
| 模板渲染 | ≤ 20ms | ≤ 50ms |

## 11.2 连接池优化

### StarRocks 连接池

```yaml
datasources:
  - name: analytics_db
    options:
      pool_size: 20                # 最大连接数
      connection_timeout: 5s       # 建立连接超时
      query_timeout: 30s           # 查询超时
      pool_acquire_timeout: 5s     # 获取连接超时
```

调优建议：
- `pool_size` = 预期并发查询数 × 1.5（留余量给重试和模板查询）
- 模板查询的信号量（`max_concurrent_queries`）应小于 `pool_size`，为单表查询留出连接
- 监控连接池使用率，接近上限时考虑扩容

### 连接池与信号量的关系

```
pool_size = 20
├── 单表查询：最多 20 个并发
└── 模板查询：最多 max_concurrent_queries (10) 个并发
    └── 剩余 10 个连接保证单表查询不被饿死
```

## 11.3 DataLoader 批量加载

DataLoader 是解决 GraphQL N+1 查询问题的关键组件。

### 问题场景

```graphql
{
  starrocks(table: "orders", first: 100) {
    nodes { data }  # 100 行数据
  }
  # 如果每行触发一个子查询 → 100 次数据库调用（N+1）
}
```

### 解决方案

DataLoader 在单个请求周期内收集所有 resolver 的数据加载请求，批量合并为一次数据库调用：

- **Per-Request 实例**：每个 HTTP 请求创建独立的 DataLoader，请求结束后销毁
- **批量窗口**：在短时间窗口内收集请求，然后批量执行
- **结果缓存**：同一请求内相同 key 的加载只执行一次

## 11.4 查询优化

### 字段选择

始终传 `fields` 参数，避免 `SELECT *`：

```graphql
# 好：只查需要的列
starrocks(table: "base_vss", fields: ["eerid", "vehicle_speed", "engine_engine_speed"], first: 100)

# 差：查所有列（base_vss 有 250+ 列）
starrocks(table: "base_vss", first: 100)
```

### 按需请求 totalCount

不需要总数时不要请求 `totalCount`，可跳过 COUNT 查询：

```graphql
# 好：不需要总数
{ starrocks(...) { nodes { data } pageInfo { hasNextPage } } }

# 差：每次都查总数
{ starrocks(...) { nodes { data } totalCount } }
```

### 合理的分页大小

```yaml
graphql:
  max_result_rows: 10000    # 硬上限
```

建议每页 20-100 条，避免一次拉取过多数据。

### 模板查询优化

- 模板 SQL 内部不要加 ORDER BY（外层分页包装器会处理）
- 使用 CTE 替代子查询，StarRocks 对 CTE 有更好的优化
- 复杂报表设置较长的 `cache_ttl`，减少重复计算

## 11.5 缓存调优

### 命中率优化

目标：缓存命中率 > 80%

```yaml
cache:
  default_ttl: 60s
  per_datasource:
    analytics_db:
      ttl: 300s          # OLAP 数据变化慢，长 TTL
    monitoring:
      ttl: 30s           # 指标实时性要求高
```

### 内存缓存容量

```yaml
cache:
  memory:
    max_entries: 10000
    max_memory_size: 256MB
```

监控 LRU 淘汰频率，如果频繁淘汰说明容量不足。

### 模板缓存策略

```yaml
sql_templates:
  templates:
    - name: fleet_report
      cache_enabled: true
      cache_ttl: 300s      # 报表数据 5 分钟缓存
    - name: realtime_status
      cache_enabled: false  # 实时状态不缓存
```

## 11.6 负载测试

项目提供 k6 负载测试脚本（`tests/load/k6-graphql.js`）：

```bash
# 安装 k6
# https://k6.io/docs/getting-started/installation/

# 运行负载测试
k6 run tests/load/k6-graphql.js
```

### 关键指标

| 指标 | 说明 | 目标 |
|------|------|------|
| `http_req_duration` | 请求延迟 | P95 < 200ms |
| `http_req_failed` | 失败率 | < 1% |
| `http_reqs` | 吞吐量 | > 1000 RPS |
| `vus` | 并发用户数 | 根据场景设定 |

### 压测场景建议

1. **基准测试**：10 VU，持续 1 分钟，确认基线性能
2. **阶梯加压**：10 → 50 → 100 → 200 VU，观察延迟变化
3. **峰值测试**：突发 500 VU，验证限流和熔断是否正常工作
4. **持久测试**：50 VU，持续 30 分钟，检查内存泄漏和连接池稳定性

## 11.7 性能监控

通过 Grafana 仪表盘持续监控：

- 请求延迟分布（P50/P95/P99）
- 数据源查询耗时
- 缓存命中率趋势
- 连接池使用率
- 模板渲染耗时
- 信号量等待耗时

设置告警：P99 延迟 > 2s 或缓存命中率 < 50% 时触发。

## 11.8 调优检查清单

| 检查项 | 操作 |
|--------|------|
| 字段选择 | 所有查询都传 `fields` 参数 |
| 分页大小 | 每页 20-100 条 |
| 缓存命中率 | > 80%，否则调整 TTL |
| 连接池 | 使用率 < 80%，否则扩容 |
| 信号量 | 等待时间 P99 < 100ms |
| 模板渲染 | P99 < 50ms |
| 内存使用 | 缓存不超过可用内存的 50% |

---

下一模块：[高级主题与最佳实践](12-advanced-topics.md)
