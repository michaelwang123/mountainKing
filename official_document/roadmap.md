# MountainKing 技术路线图

> 最后更新：2026-06-11 | 当前版本：v0.1.0

---

## 当前状态

项目已具备生产级 GraphQL 数据网关的完整能力：

- ✅ 双数据源适配器（StarRocks OLAP + Prometheus 时序）
- ✅ SQL 模板查询引擎（热加载、安全函数、分页、缓存）
- ✅ CRUD Mutations（INSERT/UPDATE/DELETE/BatchInsert）
- ✅ AnyValue 标量类型（任意 JSON 值直传，替代 JSON 包裹模式）
- ✅ 完整安全栈（JWT/APIKey、RBAC、CSRF、暴力破解防护）
- ✅ 完整可观测性（Prometheus 指标、OpenTelemetry 追踪、结构化日志）
- ✅ 弹性设计（熔断器、重试、限流、优雅关闭）
- ✅ 96+ 属性测试 + Fuzz 测试
- ✅ Docker 镜像自动发布（GHCR 多架构 + 语义化版本标签 + 健康验证）
- ✅ K8s 部署就绪（Deployment/Service/ConfigMap/HPA）
- ✅ 5 分钟开发体验（make dev + Mock 数据源 + GraphiQL）
- ✅ GitHub Pages 文档站点
- ✅ 12 章系统课程 + 完整官方文档

---

## 🔴 P0 — 扩大用户覆盖面（预计 1-2 周）

### 1. ClickHouse 数据源适配器

- **理由**：ClickHouse 用户基数远超 StarRocks，两者同为列式 OLAP 引擎，适配器逻辑高度复用
- **投入**：小（复用 StarRocks 查询构建器模式，只需替换 SQL 方言差异）
- **产出**：用户群至少翻倍
- **关键点**：ClickHouse 使用 HTTP 接口或原生 TCP 协议，需选择驱动
- **状态**：🔄 进行中（spec 已创建）

### 2. `mountainking init` 配置向导 CLI

- **理由**：产品路线图 P2 项，新用户面对 322 行 config.yaml 的最大痛点
- **投入**：中（交互式 CLI + 模板生成）
- **产出**：首次使用转化率显著提升
- **关键点**：使用 cobra/bubbletea 实现交互式命令行

### ~~3. Docker 镜像自动发布到 GHCR~~ ✅ 已完成

- 多架构支持（linux/amd64 + linux/arm64）
- 语义化版本标签（v1.2.3, v1.2, v1, latest）
- dev/nightly 工作流（main 分支自动构建 dev/sha-* 标签）
- 发布后自动健康检查验证

---

## 🟡 P1 — 差异化竞争力（预计 2-4 周）

### 4. GraphQL Subscriptions（WebSocket/SSE）

- **理由**：当前只支持 Query/Mutation，缺少实时数据推送。Prometheus 告警场景刚需
- **投入**：中大（WebSocket handler、subscription resolver、连接管理）
- **产出**：覆盖实时仪表盘/告警场景
- **关键点**：gqlgen 原生支持 WebSocket transport，需添加连接鉴权

### 5. Automatic Persisted Queries (APQ)

- **理由**：减少客户端请求体积，提升安全性（生产环境可禁止任意 query）
- **投入**：中（SHA-256 hash 查表 + 缓存层集成）
- **产出**：性能优化 + 生产安全加固
- **关键点**：复用现有 cache 层，新增 apq:{hash} 前缀

### 6. 通用 SQL 适配器（PostgreSQL/MySQL）

- **理由**：让非 OLAP 用户也能接入，覆盖最广泛的数据库用户群
- **投入**：中（复用 SQL 查询构建器，增加方言处理）
- **产出**：从"OLAP 专用工具"升级为"通用数据网关"
- **关键点**：方言差异（标识符引号、分页语法、类型映射）

### 7. 多租户支持

- **理由**：企业客户刚需，当前单配置/单认证模型无法满足 SaaS 场景
- **投入**：大（配置隔离 + 请求路由 + 资源配额）
- **产出**：进入 Enterprise 市场
- **关键点**：JWT tenant claim → 配置隔离 → 数据源路由

---

## 🟢 P2 — 生态完善（按需开发）

### 8. Helm Chart

- 生产 K8s 部署标准做法，当前只有裸 YAML
- `helm install mountainking ./deploy/helm --set datasources.starrocks.host=...`

### 9. Admin Web UI

- 数据源状态、SQL 模板管理、指标查看的可视化界面
- 基于 React + Tailwind，复用 /metrics 和 /health 端点数据

### 10. `/debug` 开发调试页面

- 仅 development mode 可用
- 展示：连接池状态、熔断器状态、缓存命中率、限流计数器实时数值

### 11. `mountainking doctor` 自检命令

- 检查 Go 版本、外部依赖连通性、配置有效性
- 输出诊断报告，快速定位环境问题

### 12. Schema Federation / 网关模式

- 与其他 GraphQL 服务组合的能力
- 适合微服务架构下的 GraphQL 统一入口

### 13. SDK 自动生成

- TypeScript/Python/Java 客户端 SDK
- 基于 GraphQL schema 自动生成类型安全的客户端代码

### 14. Plugin 系统

- 允许第三方贡献数据源适配器和中间件
- Go plugin 或 gRPC-based 插件协议

---

## 🔵 P3 — 长期愿景

### 数据源生态扩展

| 适配器 | 类型 | 复杂度 | 用户需求 |
|--------|------|--------|----------|
| ClickHouse | OLAP | 低 | 高 |
| PostgreSQL | 关系型 | 中 | 高 |
| MySQL | 关系型 | 中 | 高 |
| Elasticsearch | 搜索 | 中 | 中 |
| InfluxDB | 时序 | 中 | 中 |
| MongoDB | 文档 | 高 | 中 |
| S3/Parquet | 数据湖 | 高 | 低 |

### 企业特性

| 特性 | 说明 |
|------|------|
| OAuth2/OIDC | 企业 SSO 集成 |
| 行级安全 | 基于角色的数据行过滤 |
| Query Allowlisting | 生产环境只允许注册过的 query |
| 审计日志流式导出 | Kafka/Loki/CloudWatch |
| 请求签名 | mTLS 或 HMAC |
| API 版本管理 | Schema deprecation 工作流 |

### 高级 GraphQL 特性

| 特性 | 说明 |
|------|------|
| @defer/@stream | 增量结果交付 |
| 查询成本分析 | 智能复杂度评分 |
| 自定义指令 | @cache、@auth、@deprecated |
| 响应压缩优化 | Brotli 支持 |

### 运维工具链

| 工具 | 说明 |
|------|------|
| Terraform Provider | Infrastructure as Code |
| 金丝雀部署 | 基于指标的自动回滚 |
| 配置漂移检测 | 运行时 vs 文件配置对比告警 |
| CI 负载测试自动化 | k6 集成到 GitHub Actions |

### 测试完善

| 方向 | 说明 |
|------|------|
| 真实数据库集成测试 | docker-compose + testcontainers |
| E2E 浏览器测试 | Playwright（文档站点） |
| 混沌工程 | 故障注入测试 |
| Mutation Testing | 测试有效性度量 |
| 契约测试 | Consumer-driven contracts |

### 社区生态

| 方向 | 说明 |
|------|------|
| 示例应用 | 全栈 demo 项目 |
| VS Code 扩展 | SQL 模板智能提示 |
| 贡献者指南 | First-timer-friendly issues |
| 文档国际化 | English primary docs |
| GitHub Discussions | 社区问答 |

---

## 技术债清理

| 项目 | 优先级 | 状态 |
|------|--------|------|
| Branch protection rules | Medium | 待手动设置 |
| .dockerignore 优化 | Low | ✅ 已完成 |
| Playwright E2E 测试 | Low | 待需要时添加 |
| Lighthouse CI | Low | 待需要时添加 |
| Docker 镜像发布验证 | Low | ✅ 已完成 |
| Checkpoint tasks 标记 | Low | 仅标记问题 |

---

## 推荐实施顺序

**快速扩大用户（2 周）**：~~Docker 镜像发布~~ ✅ → ClickHouse 适配器 → init CLI

**企业级产品化（1 月）**：多租户 → OAuth2/OIDC → Helm Chart

**技术深度差异化（1 月）**：Subscriptions → APQ → 通用 SQL 适配器

---

## 核心理念

> 从"StarRocks 专用 GraphQL 网关"进化为"任意数据源的零代码 GraphQL API 平台"。
> 每个新适配器带来一批新用户，每个企业特性带来一个新客户。
