# 需求文档：项目补强 (Project Hardening)

## 简介

根据项目评估结果（`official_document/project-improvement-checklist.md`），对 mountainKing GraphQL 多数据源 API 服务进行系统性补强。涵盖代码质量清理、工程基础设施完善、文档建设、可观测性运维增强、测试覆盖率提升以及安全测试补充。按 P0/P1/P2 三个优先级分批实施。

## 术语表

- **Build_System**: 项目构建系统，指 Makefile 及其定义的构建、测试、代码生成等目标
- **CI_Pipeline**: GitHub Actions 持续集成流水线（`.github/workflows/ci.yml`）
- **Code_Generator**: gqlgen 代码生成工具，通过 `//go:generate` 指令触发
- **Codebase**: mountainKing 项目源代码仓库
- **Dependabot**: GitHub 自动化依赖更新服务
- **Fuzz_Tester**: Go 原生模糊测试框架（`testing.F`）
- **Grafana_Dashboard**: Grafana 可视化监控面板 JSON 配置文件
- **Prometheus_Alert_Rules**: 基于 Prometheus 指标的告警规则文件
- **SQL_Template_Doc**: SQL 模板引擎专题文档（`official_document/sql-template-engine.md`）
- **Dev_Compose**: 轻量级本地开发用 Docker Compose 配置
- **Migration_Guide**: 从旧系统迁移到本服务的指南文档
- **Release_Workflow**: GitHub Actions 自动化发布流水线
- **Load_Test**: 基于 k6 或类似工具的负载测试脚本，用于验证性能 SLA

## 需求优先级与依赖关系

### 优先级定义

| 优先级 | 含义 | 需求列表 |
|--------|------|----------|
| P0 - 立即修复 | 代码质量和工程基础 | 需求 1, 2, 3, 4 |
| P1 - 短期改进 | 代码现代化、CI/CD、文档、测试 | 需求 5, 6, 7, 8, 9, 10, 11, 12, 13, 14 |
| P2 - 长期优化 | 运维增强、迁移支持 | 需求 15, 16, 17, 18 |

### 依赖关系

```
需求 2 (Makefile) ← 需求 3 (go:generate，Makefile generate 目标依赖它)
需求 9 (CHANGELOG) ← 需求 16 (Release workflow，从 CHANGELOG 提取 Release Notes)
需求 10 (Grafana Dashboard) ← 需求 11 (告警规则，复用相同指标名)
需求 10 (Grafana Dashboard) ← 需求 18 (Dev Compose 包含 Grafana)
```

> 箭头 `←` 表示右侧需求依赖左侧需求。同一优先级内无严格顺序依赖，可并行实施。

## 需求

### 需求 1：删除死代码 `P0`

**用户故事：** 作为开发者，我希望代码库中没有未使用的死代码，以便保持代码整洁并消除 lint 警告。

#### 验收标准

1. WHEN 补强完成后，THE Codebase SHALL 不包含 `internal/server/server.go` 中的 `placeholderHandler` 函数
2. WHEN 执行 `go vet ./...` 后，THE Codebase SHALL 不产生与未使用函数相关的警告

### 需求 2：创建 Makefile `P0`

**用户故事：** 作为开发者，我希望有一个标准化的 Makefile，以便通过简单命令完成构建、测试、代码生成等常见操作。

#### 验收标准

1. THE Build_System SHALL 提供 `build` 目标，执行 `go build -o bin/server ./cmd/server/` 编译项目
2. THE Build_System SHALL 提供 `test` 目标，执行带竞态检测和覆盖率报告的测试（`go test -race -coverprofile=coverage.out ./...`）
3. THE Build_System SHALL 提供 `lint` 目标，执行 `golangci-lint run ./...`
4. THE Build_System SHALL 提供 `vet` 目标，执行 `go vet ./...`
5. THE Build_System SHALL 提供 `generate` 目标，执行 `go generate ./...` 触发 gqlgen 代码生成
6. THE Build_System SHALL 提供 `docker` 目标，使用 `deploy/Dockerfile` 构建 Docker 镜像
7. THE Build_System SHALL 提供 `run` 目标，执行 `go run cmd/server/main.go` 本地运行服务
8. THE Build_System SHALL 提供 `fuzz` 目标，运行 fuzz 测试（默认 30 秒）
9. THE Build_System SHALL 提供 `clean` 目标，清理 `bin/`、`coverage.out` 等构建产物
10. THE Build_System SHALL 提供 `help` 目标（默认目标），列出所有可用目标及其说明
11. THE Build_System SHALL 提供 `coverage` 目标，生成覆盖率报告并输出总覆盖率百分比

### 需求 3：添加 go:generate 指令 `P0`

**用户故事：** 作为开发者，我希望 gqlgen 代码生成有标准化入口，以便通过 `go generate ./...` 统一触发。

#### 验收标准

1. THE Code_Generator SHALL 在 `internal/graphql/resolver/resolver.go` 文件中包含 `//go:generate go run github.com/99designs/gqlgen generate` 指令
2. WHEN 执行 `go generate ./...` 时，THE Code_Generator SHALL 成功生成 GraphQL resolver 代码

### 需求 4：编写 SQL 模板引擎独立文档 `P0`

**用户故事：** 作为开发者，我希望有一份完整的 SQL 模板引擎专题文档，以便快速了解模板编写方法、安全函数和最佳实践。

#### 验收标准

1. THE SQL_Template_Doc SHALL 存放于 `official_document/sql-template-engine.md`
2. THE SQL_Template_Doc SHALL 包含功能概述章节，说明模板引擎的定位、与现有 starrocks 查询的关系、架构集成方式
3. THE SQL_Template_Doc SHALL 包含配置参考章节，说明 `sql_templates` 配置段的所有配置项（enabled、datasource_name、base_dir、shared_dir、render_timeout、max_rendered_sql_length、max_concurrent_queries、templates 列表及参数 Schema）
4. THE SQL_Template_Doc SHALL 包含模板语法说明章节，涵盖变量绑定（`{{.Params.xxx}}`）、条件逻辑（`{{if}}`/`{{else}}`）、循环构造（`{{range}}`）、模板继承（`{{template}}`/`{{define}}`）
5. THE SQL_Template_Doc SHALL 包含安全函数速查表，列出所有 12 个自定义模板函数（safeString、quote、safeInt、safeFloat、safeIdentifier、safeInList、safeLike、join、default、upper、lower、trimSpace）及其用途、示例和输出
6. THE SQL_Template_Doc SHALL 包含最佳实践章节，说明 SQL 注入防护策略、模板组织方式（共享片段）、分页注意事项（避免外层 ORDER BY）、并发控制（信号量）
7. THE SQL_Template_Doc SHALL 包含完整的端到端查询示例，展示从模板文件定义 → config.yaml 配置 → GraphQL 调用 → 响应结果的完整流程
8. THE SQL_Template_Doc SHALL 包含错误处理章节，列出所有模板相关错误码（VALIDATION_TEMPLATE_NOT_FOUND、INTERNAL_TEMPLATE_RENDER_ERROR、VALIDATION_UNSAFE_SQL、VALIDATION_MISSING_PARAMETER、VALIDATION_INVALID_PARAMETER_TYPE、VALIDATION_INVALID_PARAMETER_VALUE、VALIDATION_INVALID_FIELD、DATASOURCE_TEMPLATE_QUERY_ERROR）及其触发条件和处理建议
9. THE SQL_Template_Doc SHALL 包含热加载章节，说明 fsnotify 自动重载和 reloadTemplates Mutation 手动重载的使用方式
10. THE SQL_Template_Doc SHALL 包含缓存策略章节，说明模板级缓存 TTL、缓存禁用、totalCount 独立缓存、缓存 Key 生成规则
11. THE SQL_Template_Doc SHALL 包含可观测性章节，说明模板相关的 Prometheus 指标和 OpenTelemetry Span

### 需求 5：interface{} 全局替换为 any `P1`

**用户故事：** 作为开发者，我希望代码库使用 Go 1.18+ 的 `any` 类型别名替代 `interface{}`，以便代码风格现代化且一致。

#### 验收标准

1. WHEN 补强完成后，THE Codebase SHALL 将所有手写源文件中的 `interface{}` 替换为 `any`（不含自动生成文件 `internal/graphql/generated/` 目录）
2. WHEN 替换完成后，THE Codebase SHALL 通过 `go build ./...` 编译且无错误
3. WHEN 替换完成后，THE Codebase SHALL 通过 `go test ./...` 且所有测试通过

### 需求 6：创建 .env.example 文件 `P1`

**用户故事：** 作为开发者，我希望有一个环境变量示例文件，以便快速了解本地开发所需的环境变量配置。

#### 验收标准

1. THE Codebase SHALL 在项目根目录包含 `.env.example` 文件
2. THE `.env.example` SHALL 列出所有支持的 `GRAPHQL_` 前缀环境变量及其默认值或示例值
3. THE `.env.example` SHALL 包含注释说明每个环境变量的用途
4. THE `.env.example` SHALL 覆盖服务器配置、数据源连接、Redis、认证、日志、可观测性等关键配置项
5. THE `.env.example` SHALL 包含开发模式快速启动所需的最小配置（GRAPHQL_SERVER_MODE=development、GRAPHQL_AUTH_METHOD=none、GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true）

### 需求 7：添加 Dependabot 配置 `P1`

**用户故事：** 作为项目维护者，我希望有自动化依赖更新机制，以便及时发现并更新有安全漏洞的依赖。

#### 验收标准

1. THE Codebase SHALL 在 `.github/dependabot.yml` 包含 Dependabot 配置文件
2. THE Dependabot SHALL 配置 Go modules（`gomod`）生态系统的自动更新
3. THE Dependabot SHALL 配置 GitHub Actions 的自动更新
4. THE Dependabot SHALL 设置每周检查频率

### 需求 8：CI 添加安全扫描 `P1`

**用户故事：** 作为项目维护者，我希望 CI 流水线包含静态安全分析，以便在代码合并前发现潜在安全问题。

#### 验收标准

1. THE CI_Pipeline SHALL 包含独立的安全扫描 job（`security`）
2. THE CI_Pipeline SHALL 集成 gosec 静态安全分析工具
3. WHEN 安全扫描发现问题时，THE CI_Pipeline SHALL 将扫描结果上传为构建产物（artifact）
4. THE CI_Pipeline SHALL 在 lint 和 test 之外独立运行安全扫描，不阻塞其他 job

### 需求 9：更新 CHANGELOG `P1`

**用户故事：** 作为项目维护者，我希望 CHANGELOG 完整记录 SQL 模板引擎功能，以便用户了解版本变更内容。

#### 验收标准

1. THE CHANGELOG SHALL 在 `[Unreleased]` 部分新增 SQL 模板引擎相关功能条目
2. THE CHANGELOG SHALL 包含模板引擎核心功能描述（模板加载与注册、Go text/template 渲染、参数校验、分页包装、缓存集成、热加载）
3. THE CHANGELOG SHALL 包含模板引擎安全功能描述（12 个安全/工具函数、7 状态词法扫描器、多语句注入检测）
4. THE CHANGELOG SHALL 包含模板引擎可观测性功能描述（Prometheus 指标、OpenTelemetry Span、审计日志）
5. THE CHANGELOG SHALL 包含模板引擎 GraphQL 集成描述（templateQuery、templateList、reloadTemplates）

### 需求 10：创建 Grafana Dashboard JSON `P1`

**用户故事：** 作为运维人员，我希望有预置的 Grafana 监控面板配置，以便快速部署服务监控。

#### 验收标准

1. THE Grafana_Dashboard SHALL 存放于 `deploy/grafana/dashboard.json`
2. THE Grafana_Dashboard SHALL 包含请求概览行（Row）：请求速率（QPS）、延迟分位数（P50/P95/P99）、并发请求数（in-flight）
3. THE Grafana_Dashboard SHALL 包含数据源行：数据源查询延迟、连接池状态（active/idle/waiting）
4. THE Grafana_Dashboard SHALL 包含缓存行：缓存命中率（hits / (hits + misses)）、命中/未命中计数
5. THE Grafana_Dashboard SHALL 包含错误行：错误率（按 error_type 分组）、错误总数趋势
6. THE Grafana_Dashboard SHALL 包含 SQL 模板引擎行：模板查询延迟、模板渲染延迟、信号量等待时间、模板缓存命中率
7. THE Grafana_Dashboard SHALL 包含安全行：限流触发率（429 响应）、认证失败率
8. THE Grafana_Dashboard SHALL 使用 Prometheus 作为数据源，查询表达式与项目实际暴露的指标名称一致（如 `graphql_request_duration_seconds`、`graphql_template_query_duration_seconds` 等）

### 需求 11：创建 Prometheus 告警规则 `P1`

**用户故事：** 作为运维人员，我希望有预置的告警规则，以便在服务异常时及时收到通知。

#### 验收标准

1. THE Prometheus_Alert_Rules SHALL 存放于 `deploy/prometheus-alerts.yml`
2. THE Prometheus_Alert_Rules SHALL 包含 P99 延迟超标告警（单数据源 >500ms、混合查询 >1s）
3. THE Prometheus_Alert_Rules SHALL 包含错误率飙升告警（5 分钟内错误率 >5%）
4. THE Prometheus_Alert_Rules SHALL 包含数据源不可用告警（健康检查失败）
5. THE Prometheus_Alert_Rules SHALL 包含熔断器打开告警
6. THE Prometheus_Alert_Rules SHALL 包含模板查询信号量饱和告警（等待时间 P99 >1s）
7. THE Prometheus_Alert_Rules SHALL 使用与项目实际暴露的 Prometheus 指标名称一致的查询表达式

### 需求 12：为安全关键函数添加 Fuzz Testing `P1`

**用户故事：** 作为开发者，我希望安全关键函数有模糊测试覆盖，以便发现边界情况和潜在安全漏洞。

#### 验收标准

1. THE Fuzz_Tester SHALL 为 `internal/template/funcmap.go` 中的 `safeString` 函数提供 fuzz 测试
2. THE Fuzz_Tester SHALL 为 `internal/template/sanitizer.go` 中的 `sanitizeSQL` 函数提供 fuzz 测试
3. THE Fuzz_Tester SHALL 为 `internal/sanitize/sanitize.go` 中的敏感信息脱敏函数提供 fuzz 测试
4. WHEN fuzz 测试输入任意字节序列时，THE Fuzz_Tester SHALL 验证被测函数不会 panic
5. WHEN fuzz 测试 `safeString` 函数时，THE Fuzz_Tester SHALL 验证输出不包含未转义的单引号（即不存在奇数个连续单引号）且不包含 NULL 字节（`\x00`）

### 需求 13：补充低覆盖率包的测试 `P1`

**用户故事：** 作为开发者，我希望低覆盖率的适配器包有更充分的测试，以便提高代码可靠性。

#### 验收标准

1. THE Codebase SHALL 为 `internal/adapter/starrocks/adapter.go` 补充基于 mock `*sql.DB` 的单元测试，覆盖 Connect、Execute、HealthCheck、Close 方法
2. THE Codebase SHALL 为 `internal/adapter/prometheus/adapter.go` 补充基于 `httptest.Server` 的单元测试，覆盖 Connect、Execute（instant/range）、HealthCheck、Close 方法
3. THE Codebase SHALL 为 `internal/datasource/manager.go` 补充使用 MockDataSource 的单元测试，覆盖 Init（部分失败）、Get、CloseAll 方法
4. WHEN 补充测试完成后，`internal/adapter/starrocks` 包覆盖率 SHALL ≥ 60%
5. WHEN 补充测试完成后，`internal/adapter/prometheus` 包覆盖率 SHALL ≥ 60%
6. WHEN 补充测试完成后，`internal/datasource` 包覆盖率 SHALL ≥ 65%

### 需求 14：清理 .gitkeep 文件 `P1`

**用户故事：** 作为开发者，我希望已有内容的目录中不再保留 `.gitkeep` 占位文件，以便保持仓库整洁。

#### 验收标准

1. WHEN 目录中已包含其他文件时，THE Codebase SHALL 删除该目录中的 `.gitkeep` 文件
2. WHEN 补强完成后，THE Codebase SHALL 不包含任何位于非空目录中的 `.gitkeep` 文件

### 需求 15：添加负载测试脚本 `P2`

**用户故事：** 作为开发者，我希望有负载测试脚本验证性能 SLA，以便在发布前确认服务满足延迟和吞吐量目标。

#### 验收标准

1. THE Load_Test SHALL 存放于 `tests/load/` 目录
2. THE Load_Test SHALL 包含单数据源查询场景（验证 P95 ≤200ms、P99 ≤500ms）
3. THE Load_Test SHALL 包含跨数据源混合查询场景（验证 P95 ≤500ms、P99 ≤1s）
4. THE Load_Test SHALL 包含模板查询场景
5. THE Load_Test SHALL 包含使用说明（README），说明如何运行和解读结果
6. THE Load_Test SHALL 使用 k6、vegeta 或 Go 原生 benchmark 工具实现

### 需求 16：添加 GitHub Release Workflow `P2`

**用户故事：** 作为项目维护者，我希望有自动化发布流程，以便在打 tag 时自动创建 GitHub Release。

#### 验收标准

1. THE Release_Workflow SHALL 存放于 `.github/workflows/release.yml`
2. WHEN 推送符合 `v*` 模式的 Git tag 时，THE Release_Workflow SHALL 自动触发
3. THE Release_Workflow SHALL 使用 Go 编译生成 Linux amd64 和 arm64 二进制文件
4. THE Release_Workflow SHALL 构建并推送 Docker 镜像到 GHCR（GitHub Container Registry）
5. THE Release_Workflow SHALL 创建 GitHub Release 并附带编译产物和 Docker 镜像标签
6. THE Release_Workflow SHALL 自动从 CHANGELOG.md 提取对应版本的变更说明作为 Release Notes

### 需求 17：编写迁移指南 `P2`

**用户故事：** 作为开发者，我希望有一份从旧系统迁移到本服务的指南，以便平滑过渡。

#### 验收标准

1. THE Migration_Guide SHALL 存放于 `official_document/migration-guide.md`
2. THE Migration_Guide SHALL 说明从 Java/OData application-api 到 mountainKing GraphQL API 的迁移步骤
3. THE Migration_Guide SHALL 包含 API 映射对照表（OData 端点 → GraphQL 查询，FreeMarker 模板 → Go text/template）
4. THE Migration_Guide SHALL 包含配置迁移说明（旧系统配置项 → config.yaml 对应项）
5. THE Migration_Guide SHALL 包含常见问题和注意事项

### 需求 18：创建轻量级开发用 Docker Compose `P2`

**用户故事：** 作为开发者，我希望有一个轻量级的本地开发环境配置，以便快速启动依赖服务而无需完整的 StarRocks 集群。

#### 验收标准

1. THE Dev_Compose SHALL 存放于 `deploy/docker-compose.dev.yaml`
2. THE Dev_Compose SHALL 包含 Redis 服务（供分布式缓存和限流使用）
3. THE Dev_Compose SHALL 包含 Prometheus 服务（供指标抓取和查询测试使用）
4. THE Dev_Compose SHALL 包含 Grafana 服务（预配置 Prometheus 数据源，加载需求 10 的 dashboard）
5. THE Dev_Compose SHALL 不包含 StarRocks FE/BE 等重量级服务
6. THE Dev_Compose SHALL 包含使用说明注释，说明如何启动、连接各服务以及与 mountainKing 服务配合使用
