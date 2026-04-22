# 负载测试 — mountainKing GraphQL API

本目录包含基于 [k6](https://k6.io/) 的负载测试脚本，用于验证 GraphQL API 的性能 SLA。

## 前置条件

### 安装 k6

```bash
# macOS
brew install k6

# Linux (Debian/Ubuntu)
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D68
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
  | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6

# Docker
docker pull grafana/k6
```

## 运行测试

确保 mountainKing 服务已在 `http://localhost:8080` 运行。

```bash
# 使用默认配置运行所有场景
k6 run tests/load/k6-graphql.js

# 指定服务地址
k6 run --env BASE_URL=http://your-host:8080 tests/load/k6-graphql.js

# 携带认证 token
k6 run --env AUTH_TOKEN=your-jwt-token tests/load/k6-graphql.js

# Docker 方式运行
docker run --rm -i --network host grafana/k6 run - < tests/load/k6-graphql.js
```

## 测试场景

| 场景 | 描述 | VU 数 | 持续时间 | P95 阈值 | P99 阈值 |
|------|------|-------|---------|---------|---------|
| `single_datasource` | 单数据源 StarRocks 查询 | 10 | 30s | ≤200ms | ≤500ms |
| `mixed_query` | 跨数据源并行查询（StarRocks + Prometheus） | 20 | 30s | ≤500ms | ≤1s |
| `template_query` | SQL 模板引擎查询 | 10 | 30s | — | — |

场景按顺序执行，总运行时间约 100 秒。

## 结果解读

运行结束后 k6 会输出汇总报告，关注以下指标：

- **http_req_duration**: 请求延迟分布（avg、min、med、max、p90、p95、p99）
- **http_reqs**: 总请求数和每秒请求数（RPS）
- **checks**: 断言通过率（status 200、无 GraphQL errors）
- **thresholds**: 阈值检查结果（✓ 通过 / ✗ 失败）

如果任何 threshold 失败，k6 退出码为非零，可用于 CI 集成判断。

## 自定义

修改 `k6-graphql.js` 中的 `options.scenarios` 调整 VU 数和持续时间。修改 GraphQL 查询以匹配实际业务场景。
