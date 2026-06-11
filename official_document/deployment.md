# 部署指南

## Docker 镜像

### 拉取官方镜像

项目通过 GitHub Actions 自动发布多架构镜像（linux/amd64 + linux/arm64）到 GHCR：

```bash
# 拉取最新稳定版
docker pull ghcr.io/michaelwang123/mountainking:latest

# 拉取指定版本
docker pull ghcr.io/michaelwang123/mountainking:v1.2.3

# 拉取开发版（main 分支最新）
docker pull ghcr.io/michaelwang123/mountainking:dev
```

镜像标签策略：
- `vMAJOR.MINOR.PATCH` — 精确版本（如 `v1.2.3`）
- `vMAJOR.MINOR` — minor 版本追踪（如 `v1.2`）
- `vMAJOR` — major 版本追踪（如 `v1`）
- `latest` — 最新稳定版（不包含预发布版本）
- `dev` — main 分支最新构建
- `sha-<7char>` — 精确 commit 构建

### 本地构建

使用多阶段 Dockerfile，最终镜像基于 `distroless` 基础镜像：

```bash
docker build -f deploy/Dockerfile \
  --build-arg VERSION=v1.0.0 \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t graphql-api:v1.0.0 .
```

镜像特性：
- 多阶段构建，最终镜像体积最小化
- 基于 `gcr.io/distroless/static-debian12:nonroot`
- 以非 root 用户运行（UID 65534）
- 通过 `--build-arg` 注入版本号和构建时间，在 `/health` 端点响应中可见
- 内置 HEALTHCHECK（使用 `-health` flag 探测 /health 端点）
- 包含 SQL 模板目录 `/templates/`

### 运行

```bash
docker run -p 8080:8080 \
  -v /path/to/config.yaml:/config.yaml \
  -e GRAPHQL_STARROCKS_PASSWORD=secret \
  ghcr.io/michaelwang123/mountainking:latest
```

### 健康检查

镜像内置 HEALTHCHECK 指令，使用二进制自身的 `-health` flag 探测：

```dockerfile
HEALTHCHECK --interval=10s --timeout=3s --retries=3 --start-period=5s \
  CMD ["/graphql-api", "-health"]
```

自定义端口时需设置环境变量 `GRAPHQL_SERVER_PORT`，否则健康检查默认探测 8080 端口。

## Docker Compose

`deploy/docker-compose.yaml` 提供完整的集成测试环境：

```bash
cd deploy
docker compose up -d
```

包含的服务：
- **starrocks-fe** — StarRocks Frontend（端口 8030, 9030）
- **starrocks-be** — StarRocks Backend
- **prometheus** — Prometheus 时序数据库（端口 9090）
- **redis** — Redis（端口 6379）
- **graphql-api** — 本服务（端口 8080）

## Kubernetes 部署

### 资源清单

项目提供以下 Kubernetes 资源清单（`deploy/k8s/` 目录）：

| 文件 | 资源类型 | 说明 |
|------|---------|------|
| `deployment.yaml` | Deployment | 应用部署，含探针和资源限制 |
| `service.yaml` | Service | ClusterIP 服务暴露 |
| `configmap.yaml` | ConfigMap | 配置文件和资源参数 |
| `hpa.yaml` | HorizontalPodAutoscaler | 基于自定义指标的自动扩缩容 |

### 部署

```bash
kubectl apply -f deploy/k8s/
```

### 探针配置

| 探针 | 路径 | 说明 |
|------|------|------|
| startupProbe | `/health` | 启动探针，`failureThreshold: 30, periodSeconds: 2`（最长等待 60s） |
| livenessProbe | `/health` | 存活探针，`periodSeconds: 10, failureThreshold: 3` |
| readinessProbe | `/ready` | 就绪探针，`periodSeconds: 5, failureThreshold: 3` |

启动探针（startupProbe）确保服务在建立多个数据源连接期间不会被 kubelet 误杀。

### 资源配置

默认资源配置通过 ConfigMap 管理：

| 资源 | 请求 | 限制 |
|------|------|------|
| CPU | 100m | 500m |
| 内存 | 128Mi | 512Mi |

### HPA 自动扩缩容

基于 `graphql_requests_in_flight` 自定义 Prometheus 指标：

- 最小副本数：2
- 最大副本数：10
- 目标值：每 Pod 平均 50 个并发请求
- 扩容稳定窗口：30s
- 缩容稳定窗口：300s

需要部署 Prometheus Adapter 将自定义指标暴露为 Kubernetes custom metrics API。

### 连接池与 Pod 数量

HPA 扩缩容时需注意数据源连接总数：
- 每个 Pod 的连接池大小 × Pod 数量 = 数据源总连接数
- 确保数据源能承受最大 Pod 数量下的总连接数

### Prometheus 指标抓取

Deployment 模板包含 Prometheus 抓取注解：

```yaml
annotations:
  prometheus.io/scrape: "true"
  prometheus.io/port: "8080"
  prometheus.io/path: "/metrics"
```

### 配置管理

通过 ConfigMap 挂载配置文件：

```yaml
volumes:
  - name: config
    configMap:
      name: graphql-api-config
volumeMounts:
  - name: config
    mountPath: /config.yaml
    subPath: config.yaml
```

敏感信息（密码、密钥）通过环境变量注入，建议使用 Kubernetes Secret。

## CI/CD 流水线

项目提供 GitHub Actions CI/CD 配置（`.github/workflows/ci.yml`）：

### 流水线阶段

1. **Lint** — golangci-lint 代码检查
2. **Test** — 单元测试 + 覆盖率报告
3. **Build & Push** — Docker 镜像构建和推送到 GHCR

### 触发条件

- `push` 到 `main` 分支
- Pull Request 到 `main` 分支

### 镜像标签策略

- `sha` — Git commit SHA
- `ref` — 分支名
- `semver` — 语义化版本号（tag 触发时）

### 使用

```bash
# 手动触发（需要 GitHub Actions 权限）
gh workflow run ci.yml

# 查看运行状态
gh run list --workflow=ci.yml
```

## TLS/HTTPS

本服务不直接处理 TLS 终止。推荐架构：

```
客户端 → [TLS] → 负载均衡器 → [HTTP] → API Service
```

TLS 终止选项：
- Nginx Ingress Controller
- AWS ALB / GCP Cloud Load Balancer
- Envoy Proxy
- Istio Service Mesh（mTLS）

## 生产环境检查清单

- [ ] `server.mode` 设为 `production`
- [ ] `graphql.introspection_enabled` 设为 `false`
- [ ] `allow_get_queries` 设为 `false`
- [ ] 配置 JWT 或 API Key 认证
- [ ] 配置 `trusted_proxies`（如使用反向代理）
- [ ] 启用审计日志
- [ ] 启用敏感信息脱敏
- [ ] 配置 Prometheus 指标抓取
- [ ] 配置 OpenTelemetry 链路追踪
- [ ] 配置合理的限流参数
- [ ] 配置缓存（内存或 Redis）
- [ ] 配置 Kubernetes 资源限制
- [ ] 配置 HPA 自动扩缩容
- [ ] 确保 StarRocks `allowed_tables` 白名单已配置
- [ ] 如启用 SQL 模板引擎，确保 `sql_templates.datasource_name` 指向有效的 StarRocks 数据源
- [ ] 如启用 SQL 模板引擎，确保模板文件目录（`sql_templates.base_dir`）已挂载到容器中
- [ ] 如启用 SQL 模板引擎，确保模板文件权限正确（只读即可）
