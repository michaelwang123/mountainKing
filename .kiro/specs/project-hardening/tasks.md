# 实施计划：项目补强 (Project Hardening)

## 概述

按 P0 → P1 → P2 优先级分批实施 18 项补强需求。每个优先级批次之间设置检查点。任务涵盖死代码清理、构建系统、代码生成、文档建设、代码现代化、CI/CD 增强、可观测性运维、测试覆盖率提升、安全测试补充以及开发环境优化。所有代码使用 Go 语言实现。

## Tasks

- [x] 1. P0 — 代码质量与工程基础
  - [x] 1.1 删除死代码 `placeholderHandler`
    - 从 `internal/server/server.go` 中删除 `placeholderHandler` 函数（约第 183-190 行）
    - 确认无其他代码引用该函数
    - 运行 `go vet ./...` 确认无未使用函数警告
    - _Requirements: 1.1, 1.2_

  - [x] 1.2 创建 Makefile
    - 在项目根目录创建 `Makefile`，包含设计文档第 3 节定义的完整结构
    - 目标列表：`build`、`test`、`lint`、`vet`、`generate`、`docker`、`run`、`fuzz`、`clean`、`help`（默认）、`coverage`
    - `build` 执行 `go build -o bin/server ./cmd/server/`
    - `test` 执行 `go test -race -coverprofile=coverage.out ./...`
    - `lint` 执行 `golangci-lint run ./...`
    - `vet` 执行 `go vet ./...`
    - `generate` 执行 `go generate ./...`
    - `docker` 使用 `deploy/Dockerfile` 构建镜像
    - `run` 执行 `go run cmd/server/main.go`
    - `fuzz` 运行 fuzz 测试（默认 30s，逐个运行 FuzzSafeString → FuzzSanitizeSQL → FuzzSanitize）
    - `clean` 清理 `bin/` 和 `coverage.out`
    - `coverage` 依赖 `test`，输出总覆盖率百分比
    - `help` 使用 grep+awk 列出所有目标及说明
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 2.10, 2.11_

  - [x] 1.3 添加 go:generate 指令
    - 在 `internal/graphql/resolver/resolver.go` 的 `package resolver` 声明之后添加 `//go:generate go run github.com/99designs/gqlgen generate` 指令
    - _Requirements: 3.1, 3.2_

  - [x] 1.4 编写 SQL 模板引擎独立文档
    - 创建 `official_document/sql-template-engine.md`
    - 按设计文档第 6 节大纲编写，包含 11 个章节：功能概述、配置参考、模板语法、安全函数速查表（12 个函数）、最佳实践、端到端查询示例、错误处理（8 个错误码）、热加载、缓存策略、可观测性
    - 参考现有代码 `internal/template/` 目录中的实现细节
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10, 4.11_

- [ ] 2. P0 检查点
  - 确保 `go build ./...` 和 `go vet ./...` 通过，Makefile 各目标可正常执行。如有问题请向用户确认。

- [x] 3. P1 — 代码现代化与环境配置
  - [x] 3.1 全局替换 `interface{}` 为 `any`
    - 在所有手写 `.go` 源文件中将 `interface{}` 替换为 `any`
    - 排除自动生成目录 `internal/graphql/generated/`
    - 涉及文件包括但不限于：`internal/server/server.go`、`internal/server/server_test.go`、`internal/server/timeout_property_test.go`、`internal/config/config.go`、`internal/adapter/starrocks/query_builder.go`、`internal/adapter/prometheus/query_builder_test.go`、`internal/template/*.go`、`internal/observability/logging_property_test.go`、`internal/audit/audit_property_test.go`、`internal/datasource/interface.go` 等
    - 替换后运行 `go build ./...` 确认编译通过
    - 替换后运行 `go test ./...` 确认所有测试通过
    - _Requirements: 5.1, 5.2, 5.3_

  - [ ]* 3.2 编写 Property 1 属性测试：interface{} 不存在于手写源文件
    - **Property 1: interface{} 不存在于手写源文件**
    - 使用 `pgregory.net/rapid` 在项目测试中验证：遍历所有非 `internal/graphql/generated/` 目录的 `.go` 文件，确认不包含 `interface{}` 字符串
    - 测试文件：`internal/server/hardening_property_test.go`（或合适位置）
    - **Validates: Requirements 5.1**

  - [x] 3.3 创建 `.env.example` 文件
    - 在项目根目录创建 `.env.example`
    - 基于 `internal/config/config.go` 中 `LoadConfig` 函数列出所有 `GRAPHQL_` 前缀环境变量
    - 按功能分组（服务器、数据源、Redis、认证、日志、可观测性、SQL 模板等）
    - 每个变量附带 `#` 注释说明用途和默认值
    - 包含开发模式最小配置：`GRAPHQL_SERVER_MODE=development`、`GRAPHQL_AUTH_METHOD=none`、`GRAPHQL_GRAPHQL_INTROSPECTION_ENABLED=true`
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [ ]* 3.4 编写 Property 2 属性测试：.env.example 格式一致性
    - **Property 2: .env.example 格式一致性**
    - 使用 `pgregory.net/rapid` 验证：`.env.example` 中每个非空非注释行的变量名以 `GRAPHQL_` 开头，且该行之前存在至少一行 `#` 注释
    - **Validates: Requirements 6.2, 6.3**

- [x] 4. P1 — CI/CD 与依赖管理
  - [x] 4.1 添加 Dependabot 配置
    - 创建 `.github/dependabot.yml`
    - 配置 `gomod` 和 `github-actions` 两个生态系统
    - 设置每周检查频率（`interval: weekly`）
    - _Requirements: 7.1, 7.2, 7.3, 7.4_

  - [x] 4.2 CI 添加安全扫描 job
    - 在 `.github/workflows/ci.yml` 中添加独立的 `security` job
    - 按设计文档第 5 节的 YAML 结构实现：安装 gosec、运行扫描、上传报告为 artifact
    - 使用 `|| true` 确保不阻塞其他 job
    - 与 lint/test 并行运行（不在 `needs` 中依赖其他 job）
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

  - [x] 4.3 更新 CHANGELOG
    - 在 `CHANGELOG.md` 的 `[Unreleased]` → `### Added` 部分追加 SQL 模板引擎相关条目
    - 包含：模板引擎核心功能（加载注册、渲染、参数校验、分页包装、缓存集成、热加载）
    - 包含：安全功能（12 个安全/工具函数、7 状态词法扫描器、多语句注入检测）
    - 包含：可观测性（Prometheus 指标、OpenTelemetry Span、审计日志）
    - 包含：GraphQL 集成（templateQuery、templateList、reloadTemplates）
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5_

  - [x] 4.4 更新文档索引
    - 在 `official_document/README.md` 的文档目录表格中添加 `sql-template-engine.md` 和 `migration-guide.md`（P2 完成后）的链接
    - _Requirements: 4.1, 17.1_

- [x] 5. P1 — 可观测性运维
  - [x] 5.1 创建 Grafana Dashboard JSON
    - 创建 `deploy/grafana/dashboard.json`
    - 按设计文档第 7 节定义的 7 个 Row 布局：请求概览、数据源、缓存、错误、SQL 模板引擎、安全、系统
    - 数据源类型设为 Prometheus，使用 `$datasource` 变量支持多实例切换
    - 所有 PromQL 查询表达式使用项目实际暴露的指标名称
    - 面板类型：timeseries（延迟/速率）、stat（当前值）、gauge（百分比）
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8_

  - [ ]* 5.2 编写 Property 3 属性测试：Grafana Dashboard 指标名称有效性
    - **Property 3: Grafana Dashboard 指标名称有效性**
    - 使用 `pgregory.net/rapid` 验证：Dashboard JSON 中所有面板的 PromQL 表达式引用的指标名称属于项目已注册的 Prometheus 指标集合
    - 解析 JSON 文件，提取所有 `expr` 字段中的指标名称，与已知指标列表比对
    - **Validates: Requirements 10.8**

  - [x] 5.3 创建 Prometheus 告警规则
    - 创建 `deploy/prometheus-alerts.yml`
    - 按设计文档第 8 节定义的 6 条告警规则：HighP99Latency、HighP99LatencyMixed、HighErrorRate、DatasourceUnavailable、CircuitBreakerOpen、TemplateSemaphoreSaturated
    - 使用标准 Prometheus alerting rules 格式（`groups[].rules[]`）
    - 所有 `expr` 使用项目实际暴露的指标名称
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7_

  - [ ]* 5.4 编写 Property 4 属性测试：Prometheus 告警规则指标名称有效性
    - **Property 4: Prometheus 告警规则指标名称有效性**
    - 使用 `pgregory.net/rapid` 验证：告警规则 YAML 中所有 `expr` 表达式引用的指标名称属于项目已注册的 Prometheus 指标集合
    - **Validates: Requirements 11.7**

- [x] 6. P1 — 安全测试与覆盖率提升
  - [x] 6.1 为安全关键函数添加 Fuzz Testing
    - 创建 `internal/template/funcmap_fuzz_test.go`：`FuzzSafeString` 函数
      - 种子语料库：空字符串、单引号、反斜杠、NULL 字节、SQL 注入 payload（`'; DROP TABLE --`、`\x00`、`' OR '1'='1`）
      - 验证：不 panic；输出不含未转义单引号（不存在奇数个连续 `'`）；不含 NULL 字节
      - 此 fuzz 测试同时覆盖 Property 5（不 panic）和 Property 6（输出安全不变量）
    - 创建 `internal/template/sanitizer_fuzz_test.go`：`FuzzSanitizeSQL` 函数
      - 验证：不 panic；返回 error 或有效结果
    - 创建 `internal/sanitize/sanitize_fuzz_test.go`：`FuzzSanitize` 函数
      - 使用 `DefaultRules` 初始化 Sanitizer
      - 验证：不 panic
    - _Requirements: 12.1, 12.2, 12.3, 12.4, 12.5_

  - [x] 6.2 补充 StarRocks Adapter 单元测试
    - 添加 go-sqlmock 依赖：`go get github.com/DATA-DOG/go-sqlmock`
    - 创建 `internal/adapter/starrocks/adapter_test.go`
    - 使用 `DATA-DOG/go-sqlmock` mock `*sql.DB`
    - 测试用例：`TestConnect_Success`、`TestConnect_PingFail`、`TestExecute_Success`、`TestExecute_WithCount`、`TestHealthCheck_Success`、`TestHealthCheck_Fail`、`TestClose`
    - 通过同包内直接设置 `adapter.db` 字段注入 mock
    - 目标覆盖率 ≥ 60%
    - _Requirements: 13.1, 13.4_

  - [x] 6.3 补充 Prometheus Adapter 单元测试
    - 创建 `internal/adapter/prometheus/adapter_test.go`
    - 使用 `net/http/httptest.Server` mock Prometheus HTTP API
    - 测试用例：`TestConnect_Success`、`TestConnect_Fail`、`TestExecute_InstantQuery`、`TestExecute_RangeQuery`、`TestExecute_Error`、`TestHealthCheck_Success`、`TestHealthCheck_Fail`、`TestClose`
    - 目标覆盖率 ≥ 60%
    - _Requirements: 13.2, 13.5_

  - [x] 6.4 补充 DataSourceManager 单元测试
    - 创建 `internal/datasource/manager_test.go`
    - 使用现有 `internal/datasource/mock.go` 中的 `MockDataSource`
    - 测试用例：`TestInit_PartialFailure`、`TestInit_DisabledSkipped`、`TestGet_Found`、`TestGet_NotFound`、`TestGet_Unavailable`、`TestCloseAll`
    - 目标覆盖率 ≥ 65%
    - _Requirements: 13.3, 13.6_

  - [x] 6.5 清理 .gitkeep 文件
    - 删除以下非空目录中的 `.gitkeep` 文件：
      - `internal/adapter/prometheus/.gitkeep`
      - `internal/adapter/starrocks/.gitkeep`
      - `internal/audit/.gitkeep`
      - `internal/cache/.gitkeep`
      - `internal/context/.gitkeep`
      - `internal/datasource/.gitkeep`
      - `internal/graphql/dataloader/.gitkeep`
      - `internal/graphql/generated/.gitkeep`
      - `internal/graphql/resolver/.gitkeep`
      - `internal/graphql/scalar/.gitkeep`
      - `internal/graphql/schema/.gitkeep`
      - `internal/health/.gitkeep`
      - `internal/middleware/.gitkeep`
      - `internal/sanitize/.gitkeep`
      - `pkg/retry/.gitkeep`
    - _Requirements: 14.1, 14.2_

  - [ ]* 6.6 编写 Property 7 属性测试：非空目录不含 .gitkeep
    - **Property 7: 非空目录不含 .gitkeep**
    - 使用 `pgregory.net/rapid` 验证：遍历项目所有目录，如果目录包含除 `.gitkeep` 以外的文件，则该目录不应包含 `.gitkeep`
    - **Validates: Requirements 14.1, 14.2**

- [x] 7. P1 检查点
  - 确保所有测试通过（`go test ./...`），CI 配置语法正确，文档内容完整。如有问题请向用户确认。

- [x] 8. P2 — 负载测试与发布流程
  - [x] 8.1 添加负载测试脚本
    - 创建 `tests/load/k6-graphql.js`：k6 负载测试主脚本
    - 包含 3 个场景：`single_datasource`（P95≤200ms、P99≤500ms）、`mixed_query`（P95≤500ms、P99≤1s）、`template_query`
    - 使用 `scenarios` 配置独立 VU 数和持续时间
    - 创建 `tests/load/README.md`：使用说明（安装 k6、运行命令、结果解读）
    - _Requirements: 15.1, 15.2, 15.3, 15.4, 15.5, 15.6_

  - [x] 8.2 添加 GitHub Release Workflow
    - 创建 `.github/workflows/release.yml`
    - 触发条件：推送 `v*` 模式的 Git tag
    - 步骤：Go 交叉编译 Linux amd64/arm64 二进制、从 CHANGELOG.md 提取版本变更说明、构建推送 Docker 镜像到 GHCR、创建 GitHub Release 附带编译产物
    - CHANGELOG 提取失败时使用 "Release vX.Y.Z" 作为默认 Release Notes
    - _Requirements: 16.1, 16.2, 16.3, 16.4, 16.5, 16.6_

  - [x] 8.3 编写迁移指南
    - 创建 `official_document/migration-guide.md`
    - 包含：从 Java/OData application-api 到 mountainKing GraphQL API 的迁移步骤
    - 包含：API 映射对照表（OData 端点 → GraphQL 查询，FreeMarker 模板 → Go text/template）
    - 包含：配置迁移说明（旧系统配置项 → config.yaml 对应项）
    - 包含：常见问题和注意事项
    - _Requirements: 17.1, 17.2, 17.3, 17.4, 17.5_

  - [x] 8.4 创建轻量级开发用 Docker Compose
    - 创建 `deploy/docker-compose.dev.yaml`
    - 包含：Redis (redis:7-alpine, 端口 6379)、Prometheus (prom/prometheus, 端口 9090, 挂载 `deploy/prometheus.yml`)、Grafana (grafana/grafana-oss, 端口 3000, 预配置 Prometheus 数据源, 加载 `deploy/grafana/dashboard.json`)
    - 不包含 StarRocks FE/BE
    - 文件顶部包含使用说明注释
    - _Requirements: 18.1, 18.2, 18.3, 18.4, 18.5, 18.6_

- [x] 9. 最终检查点
  - 确保所有测试通过，所有新增文件存在且内容完整，`go build ./...` 和 `go vet ./...` 无错误。如有问题请向用户确认。

## Notes

- 标记 `*` 的任务为可选，可跳过以加速 MVP 交付
- 每个任务引用具体需求编号以确保可追溯性
- 检查点确保增量验证
- Property 测试验证通用正确性属性（使用 rapid 库）
- Fuzz 测试使用 Go 原生 `testing.F`（Property 5、6 通过 fuzz 测试覆盖）
- 适配器测试使用 go-sqlmock（StarRocks）和 httptest.Server（Prometheus）
