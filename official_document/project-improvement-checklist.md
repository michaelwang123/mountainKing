# 项目补强项清单

> 评估日期：2026-04-22
> 评估范围：graphql-multi-datasource-api + sql-template-engine 两个 spec 的完整实现

## 评估概要

- 编译：✅ 通过（`go build ./...` + `go vet ./...`）
- 测试：✅ 全部通过（修复 server_test.go 3 个过时测试后）
- 两个 spec 所有实现任务均已完成

---

## A. 代码质量（快速修复）

| # | 问题 | 影响 | 优先级 |
|---|------|------|--------|
| A1 | `server.go` 中 `placeholderHandler` 函数未使用（死代码） | lint 警告 | P0 |
| A2 | 50+ 处 `interface{}` 应替换为 `any`（Go 1.18+ 风格） | 代码现代化 | P1 |
| A3 | 10+ 个 `.gitkeep` 文件残留在已有内容的目录中 | 仓库整洁度 | P2 |

## B. 测试覆盖率

| 包 | 当前覆盖率 | 目标 | 建议 |
|---|-----------|------|------|
| adapter/prometheus | 37.1% | ≥70% | 需要 mock HTTP server 测试 adapter.go |
| adapter/starrocks | 38.7% | ≥70% | 需要 mock sql.DB 测试 adapter.go |
| datasource | 50.0% | ≥70% | manager.go 的连接管理逻辑需补测 |
| config | 88.6% ✅ | — | 测试耗时 263s，property test 需优化 |

高覆盖率包（无需改进）：

| 包 | 覆盖率 |
|---|--------|
| health | 100.0% |
| sanitize | 100.0% |
| pkg/retry | 96.6% |
| middleware | 91.6% |
| graphql/dataloader | 91.5% |
| graphql/scalar | 90.5% |
| observability | 88.1% |
| audit | 87.9% |
| cache | 84.1% |
| server | 80.6% |
| graphql/resolver | 77.6% |
| ratelimit | 75.3% |
| template | 75.2% |

## C. 工程基础设施

| # | 缺失项 | 说明 | 优先级 |
|---|--------|------|--------|
| C1 | Makefile | 缺少 `make build/test/lint/generate/docker` 等常用目标 | P0 |
| C2 | `//go:generate` 指令 | gqlgen 代码生成没有标准化入口，应在 resolver.go 中添加 | P0 |
| C3 | `.env.example` | 本地开发所需环境变量没有示例文件 | P1 |
| C4 | pre-commit hooks | 没有提交前自动 lint/test 机制 | P1 |
| C5 | dependabot/renovate | 没有自动化依赖更新配置 | P1 |
| C6 | CI 安全扫描 | 没有 gosec/trivy 等 SAST 工具集成 | P1 |
| C7 | Release workflow | CI 只有 lint/test/build，没有 tag/release 自动化流程 | P2 |
| C8 | 覆盖率 badge | README 没有展示覆盖率状态 | P2 |

## D. 文档建设

| # | 缺失项 | 说明 | 优先级 |
|---|--------|------|--------|
| D1 | SQL 模板引擎独立文档 | 模板引擎信息散落在 security/troubleshooting/observability 中，缺少 `official_document/sql-template-engine.md` 专题文档（模板编写指南、安全函数速查、最佳实践） | P0 |
| D2 | CHANGELOG 不完整 | 只有 `[Unreleased]`，未记录 SQL 模板引擎功能，没有版本号 | P1 |
| D3 | Grafana Dashboard JSON | 需求提到 Grafana 监控但没有预置 dashboard 文件 | P1 |
| D4 | Prometheus 告警规则 | 只有 scrape 配置，没有基于指标的告警规则（如 P99 延迟超标、错误率飙升） | P1 |
| D5 | 迁移指南 | 从 Java/OData application-api 迁移到本服务的指南缺失 | P2 |
| D6 | API 版本策略 | 没有文档说明 GraphQL Schema 的版本演进策略 | P2 |

## E. 部署与运维

| # | 缺失项 | 说明 | 优先级 |
|---|--------|------|--------|
| E1 | Helm Chart | 只有原始 K8s YAML，没有参数化的 Helm chart | P1 |
| E2 | 开发用 docker-compose | 现有 compose 是集成测试用的（StarRocks FE/BE 很重），缺少轻量级开发环境 | P2 |
| E3 | Grafana provisioning | 没有 datasource/dashboard 自动配置文件 | P2 |

## F. 测试补强

| # | 缺失项 | 说明 | 优先级 |
|---|--------|------|--------|
| F1 | 负载测试脚本 | 需求定义了 P95/P99 延迟 SLA，但没有 k6/vegeta 等负载测试脚本 | P1 |
| F2 | Fuzz testing | `safeString`、`sanitizeSQL` 等安全关键函数适合用 Go 原生 fuzzing 补充 | P1 |
| F3 | 端到端冒烟测试 | 没有一键验证完整链路的脚本 | P2 |

---

## 建议实施顺序

### 第一批（P0 — 立即修复）
- A1: 删除 `placeholderHandler` 死代码
- C1: 创建 Makefile
- C2: 添加 `//go:generate` 指令
- D1: 编写 SQL 模板引擎独立文档

### 第二批（P1 — 短期改进）
- A2: `interface{}` → `any` 全局替换
- B: 补充 adapter/datasource 测试覆盖率
- C3-C6: 工程基础设施完善
- D2-D4: 文档补充
- E1: Helm Chart
- F1-F2: 负载测试和 Fuzz 测试

### 第三批（P2 — 长期优化）
- A3: 清理 .gitkeep
- C7-C8: Release workflow 和覆盖率 badge
- D5-D6: 迁移指南和 API 版本策略
- E2-E3: 开发环境和 Grafana provisioning
- F3: 端到端冒烟测试
