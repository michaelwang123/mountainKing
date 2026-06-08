# 模块 09：弹性设计模式

> 理解熔断器、限流、重试、重连和优雅关闭等弹性机制的设计与配置。

## 9.1 弹性机制总览

```
请求 → 限流 → 熔断器检查 → 执行查询 → 成功/失败
                  │              │
                  │         失败 → 重试（指数退避）
                  │              │
                  │         连续失败 → 熔断器打开
                  │              │
                  │         连接断开 → 后台重连（指数退避）
                  │
             熔断器打开 → 快速失败（不执行查询）
```

## 9.2 熔断器

三状态模型：CLOSED → OPEN → HALF_OPEN → CLOSED

```
CLOSED（正常）
    │ 连续失败达到阈值
    ▼
OPEN（熔断，快速失败）
    │ 等待 open_duration
    ▼
HALF_OPEN（探测）
    │ 探测成功达到阈值 → CLOSED
    │ 探测失败 → OPEN
```

### 配置

```yaml
circuit_breaker:
  failure_threshold: 5        # 连续失败次数触发熔断
  open_duration: 30s          # 熔断持续时间
  half_open_max_requests: 1   # HALF_OPEN 状态允许的探测请求数
  success_threshold: 2        # HALF_OPEN 中连续成功次数恢复正常
```

### 工作原理

- **CLOSED**：请求正常通过。每次成功重置失败计数，每次失败递增计数。连续失败达到 `failure_threshold` 时转为 OPEN。
- **OPEN**：所有请求快速失败（不执行实际查询）。等待 `open_duration` 后转为 HALF_OPEN。
- **HALF_OPEN**：允许最多 `half_open_max_requests` 个探测请求。连续成功达到 `success_threshold` 时恢复 CLOSED；任何失败立即回到 OPEN。

所有状态检查和转换在同一把锁内完成，线程安全。

## 9.3 指数退避重试

```yaml
retry:
  max_retries: 3
  retry_interval: 100ms
  backoff: exponential
```

重试策略：
- 仅对瞬时错误重试（网络超时、连接重置等）
- 业务错误（SQL 语法错误、权限不足等）立即返回，不重试
- 退避间隔：100ms → 200ms → 400ms（指数增长）

错误分类器（`pkg/retry/classifier.go`）判断错误是否可重试。

## 9.4 后台指数退避重连

数据源连接断开后，DataSource Manager 在后台自动重连：

```yaml
datasources:
  - name: analytics_db
    options:
      reconnect_interval: 5s        # 初始重连间隔
      max_reconnect_interval: 60s   # 最大重连间隔
```

重连间隔：5s → 10s → 20s → 40s → 60s（上限）

重连期间，该数据源的查询通过熔断器快速失败，不会阻塞请求。

## 9.5 请求限流

### 令牌桶算法

以固定速率向桶中添加令牌，请求消耗令牌，桶空时拒绝请求。允许突发流量同时控制平均速率。

```yaml
rate_limit:
  mode: local                    # local | distributed
  requests_per_window: 100       # 窗口内最大请求数
  window_size: 60s               # 时间窗口
```

### 本地限流

基于 `golang.org/x/time/rate`，适合单实例部署。按客户端维度（认证主体或 IP）独立限流。

### 分布式限流

基于 Redis，适合多实例部署，所有实例共享限流计数：

```yaml
rate_limit:
  mode: distributed
  redis:
    addr: redis:6379
    password: "${GRAPHQL_REDIS_PASSWORD}"
```

### 自动降级

Redis 不可用时自动降级为本地限流，Redis 恢复后自动切回分布式模式。

### 批量查询限流

批量查询按实际查询数（而非 HTTP 请求数）计数。一个包含 5 个查询的批量请求消耗 5 个令牌。

## 9.6 信号量并发控制

SQL 模板引擎使用信号量限制并发查询数：

```yaml
sql_templates:
  max_concurrent_queries: 10
```

防止复杂报表查询（可能运行数秒）饿死共享连接池中的快速单表查询。

信号量等待时间通过 `graphql_template_semaphore_wait_seconds` 指标监控。

## 9.7 优雅关闭

收到 SIGTERM/SIGINT 后，按序执行关闭：

```
1. 停止接受新连接（http.Server.Shutdown）
2. 等待进行中的请求完成（最多 max_wait_time）
3. 关闭 TracingProvider（5s 超时）
4. 刷新 Metrics
5. 关闭所有数据源连接（DataSourceManager.CloseAll）
6. 同步日志缓冲区（Logger.Sync）
```

```yaml
shutdown:
  max_wait_time: 30s
```

每个步骤都有独立的超时控制，确保关闭过程不会无限阻塞。

## 9.8 超时模型

```
┌─────────────────────────────────────────────┐
│          server.request_timeout (30s)        │
│                                             │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐ │
│  │ 中间件    │  │ Resolver │  │ DataSource│ │
│  │ 处理      │  │ 执行     │  │ 查询      │ │
│  └──────────┘  └──────────┘  └───────────┘ │
│                                             │
│  模板查询额外超时：                          │
│  ├── render_timeout (5s) — 模板渲染          │
│  └── query_timeout (30s) — SQL 执行          │
└─────────────────────────────────────────────┘
```

`request_timeout` 是总超时，`render_timeout` 和 `query_timeout` 在总超时内独立计时。

## 9.9 弹性配置建议

| 场景 | 建议配置 |
|------|----------|
| 高可用 | 熔断阈值 3-5，重试 2-3 次 |
| 低延迟 | 短超时（10-15s），少重试（1-2 次） |
| 高吞吐 | 大连接池，高限流阈值 |
| 复杂报表 | 长超时（60s），信号量限制并发 |

---

下一模块：[部署与运维](10-deployment.md)
