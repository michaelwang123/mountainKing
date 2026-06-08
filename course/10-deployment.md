# 模块 10：部署与运维

> Docker 构建、Kubernetes 部署、CI/CD 流水线和监控告警配置。

## 10.1 Docker 构建

### 多阶段构建

`deploy/Dockerfile` 使用多阶段构建，最终镜像基于 distroless：

```dockerfile
# 阶段 1：编译
FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/server/

# 阶段 2：运行（distroless，非 root）
FROM gcr.io/distroless/static-debian12
COPY --from=builder /server /server
COPY config.yaml /config.yaml
COPY templates/ /templates/
USER nonroot:nonroot
ENTRYPOINT ["/server"]
```

优势：
- 最终镜像极小（~20MB）
- 无 shell、无包管理器，减少攻击面
- 非 root 用户运行

### 构建命令

```bash
# 构建
docker build -f deploy/Dockerfile -t mountainking:latest .

# 运行
docker run -p 8080:8080 \
  -v /path/to/config.yaml:/config.yaml \
  -v /path/to/templates:/templates \
  mountainking:latest
```

## 10.2 Docker Compose

### 开发环境

`deploy/docker-compose.dev.yaml` 提供轻量级本地开发环境：

```bash
docker compose -f deploy/docker-compose.dev.yaml up -d
```

包含：API 服务 + Prometheus + Grafana（预配置仪表盘）

### 完整环境

`deploy/docker-compose.yaml` 包含所有组件：

```bash
docker compose -f deploy/docker-compose.yaml up -d
```

包含：API 服务 + StarRocks + Prometheus + Grafana + Redis + Jaeger

## 10.3 Kubernetes 部署

### 资源清单

```
deploy/k8s/
├── configmap.yaml     # 配置文件
├── deployment.yaml    # Deployment（含探针配置）
├── service.yaml       # Service
└── hpa.yaml           # 水平自动扩缩
```

### Deployment 关键配置

```yaml
spec:
  containers:
    - name: mountainking
      image: mountainking:latest
      ports:
        - containerPort: 8080
      # 存活探针
      livenessProbe:
        httpGet:
          path: /health
          port: 8080
        initialDelaySeconds: 5
        periodSeconds: 10
      # 就绪探针
      readinessProbe:
        httpGet:
          path: /ready
          port: 8080
        initialDelaySeconds: 5
        periodSeconds: 5
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
        limits:
          cpu: 500m
          memory: 512Mi
```

### HPA 自动扩缩

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

### 配置注入

通过 ConfigMap 挂载 `config.yaml`，敏感信息通过 Secret 注入环境变量：

```yaml
env:
  - name: GRAPHQL_AUTH_JWT_SECRET
    valueFrom:
      secretKeyRef:
        name: mountainking-secrets
        key: jwt-secret
  - name: SR_DB_PASSWORD
    valueFrom:
      secretKeyRef:
        name: mountainking-secrets
        key: starrocks-password
```

## 10.4 CI/CD

### GitHub Actions CI

`.github/workflows/ci.yml` 包含：
- 代码检查（golangci-lint）
- 单元测试（race detection + 覆盖率）
- 安全扫描（gosec）
- Docker 镜像构建

### Release 流水线

`.github/workflows/release.yml`：
- 基于 Git tag 触发
- 从 CHANGELOG.md 提取 Release Notes
- 构建并推送 Docker 镜像
- 创建 GitHub Release

## 10.5 监控告警

### Prometheus 抓取配置

```yaml
# deploy/prometheus.yml
scrape_configs:
  - job_name: mountainking
    static_configs:
      - targets: ['mountainking:8080']
    metrics_path: /metrics
    scrape_interval: 15s
```

### 告警规则

`deploy/prometheus-alerts.yml` 包含关键告警：

| 告警 | 条件 | 严重级别 |
|------|------|----------|
| 高错误率 | 5xx 比例 > 5% | critical |
| 高延迟 | P99 > 2s | warning |
| 熔断器打开 | 任何数据源熔断 | critical |
| 数据源不可用 | 健康检查失败 | critical |
| 缓存命中率低 | < 50% | warning |

### Grafana 仪表盘

`deploy/grafana/dashboard.json` 预配置面板：
- 请求速率和延迟分布
- 数据源查询性能
- 缓存命中率趋势
- 熔断器状态
- 模板查询指标
- 资源使用（CPU/内存）

## 10.6 生产部署检查清单

| 检查项 | 说明 |
|--------|------|
| 认证已启用 | `auth.method` ≠ `none` |
| Schema 自省已禁用 | `introspection_enabled: false` |
| GET 查询已禁用 | `allow_get_queries: false` |
| 脱敏已启用 | `sanitization.enabled: true` |
| 审计日志已启用 | `logging.audit.enabled: true` |
| 健康检查已配置 | K8s liveness/readiness probes |
| 资源限制已设置 | CPU/内存 requests 和 limits |
| 密钥通过 Secret 注入 | 不在 ConfigMap 中存储敏感信息 |
| 监控告警已配置 | Prometheus + Grafana + 告警规则 |
| 链路追踪已启用 | `tracing.enabled: true` |

---

下一模块：[性能调优指南](11-performance.md)
