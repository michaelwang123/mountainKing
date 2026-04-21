# 实现计划：SQL 模板查询引擎

## 概述

mountainKing GraphQL API 服务新增 SQL 模板查询引擎的增量实现计划。按 P0（核心）→ P1（安全与校验）→ P2（可观测性与热加载）优先级迭代，每个任务构建在前一个任务之上。复用现有 StarRocks Adapter 连接池、CacheLayer、AuditLogger、Sanitizer 等基础设施。

## Tasks

- [x] 1. 类型定义与配置扩展
  - [x] 1.1 定义 RawExecutor 接口和公共类型（internal/template/types.go）：RawExecutor 接口、TemplateQueryRequest、TemplateQueryResult、TemplateOrderByParam、TemplateEngineConfig、ReloadResult、TemplateLoadFailure、TemplateInfo、ParamSchemaInfo
    - _Requirements: 5.1_
    - _Design: 组件与接口 §1 (RawExecutor)_
  - [x] 1.2 扩展配置结构（internal/config/config.go）：新增 SQLTemplatesConfig（含 DatasourceName 字段）、TemplateConfig、TemplateParamConfig 结构体，在 Config 根结构体中嵌入 SQLTemplates 字段，在 setDefaults 中添加 sql_templates 默认值。同步更新 config.yaml 添加 sql_templates 配置段（含示例模板条目），方便后续任务本地开发测试
    - _Requirements: 1.2, 1.7_
    - _Design: 组件与接口 §2 (配置结构), 需求文档附录 A (配置示例)_
  - [x] 1.3 扩展配置校验（internal/config/validation.go）：新增 validateSQLTemplates 函数，校验 base_dir 非空、datasource_name 非空、render_timeout/max_rendered_sql_length/max_concurrent_queries 正值、模板名称格式和唯一性、参数类型合法性、pattern 正则语法
    - _Requirements: 1.9, 7.9_
    - _Design: 配置校验规则_
  - [x] 1.4 新增模板相关错误码（internal/errors/types.go）：ErrValidationTemplateNotFound、ErrValidationUnsafeSQL、ErrValidationMissingParameter、ErrValidationInvalidParameterType、ErrValidationInvalidParameterValue、ErrDatasourceTemplateQueryError、ErrInternalTemplateRenderError
    - _Requirements: 2.3, 2.4, 5.4, 6.6, 7.2, 7.4, 7.6_
    - _Design: 组件与接口 §3 (错误码定义)_
  - [x] 1.5 扩展 AuditLogger.LogEntry（internal/audit/audit.go）：新增 ExtraFields map[string]string 字段，修改 Log 方法输出 ExtraFields
    - _Requirements: 9.5_
    - _Design: 组件与接口 §13 (审计日志)_
  - [x] 1.6 编写配置校验单元测试（配置层校验 — 测试 validateSQLTemplates 函数）：覆盖 sql_templates 段的 enabled/disabled、datasource_name 必填、模板名称格式/重复、参数类型/pattern 校验。注意：P4/P5 在此任务中测试配置层校验逻辑，Task 7.3 中测试加载器层校验逻辑，两者测试层级不同
    - **Property 4: 模板名称格式校验（配置层）** — Validates: Requirements 1.9
    - **Property 5: 文件大小限制（配置层）** — Validates: Requirements 1.10
    - **Property 48: 正则预编译** — Validates: Requirements 7.9

- [x] 2. Checkpoint - 确保类型定义、配置扩展、错误码就绪
  - 验证：golangci-lint 通过 + 所有现有测试不受影响 + 新增配置校验测试通过

- [x] 3. SQL 安全函数（FuncMap）
  - [x] 3.1 实现安全函数（internal/template/funcmap.go）：buildFuncMap 返回 template.FuncMap，包含 safeString（NULL 字节移除 + 反斜杠转义 + 单引号转义）、quote（safeString + 单引号包裹）、safeInt、safeFloat（拒绝 NaN/±Inf）、safeIdentifier（[a-zA-Z0-9_.] 校验 + 反引号包裹 + 最多 2 段）、safeInList（空切片拒绝 + 逐元素转义）、safeLike（反斜杠/% /_ 转义，文档注明需配合 ESCAPE '\\'）
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.8_
    - _Design: 组件与接口 §9 (SQL 安全函数)_
  - [x] 3.2 实现工具函数：join、defaultFn、upper、lower、trimSpace
    - _Requirements: 2.6, 2.11_
  - [x] 3.3 编写安全函数单元测试和属性测试
    - **Property 28: safeString 转义正确性** — Validates: Requirements 6.1
    - **Property 29: safeString 反斜杠安全** — Validates: Requirements 6.1
    - **Property 30: safeInt 类型安全** — Validates: Requirements 6.2
    - **Property 31: safeFloat 类型安全** — Validates: Requirements 6.3（注意：需额外验证 NaN/±Inf 被拒绝，即 safeFloat 成功 ⟹ 结果为有限 float64）
    - **Property 32: safeIdentifier 字符安全** — Validates: Requirements 6.4
    - **Property 33: safeIdentifier 反引号包裹** — Validates: Requirements 6.4
    - **Property 34: safeIdentifier 段数限制** — Validates: Requirements 6.4
    - **Property 35: safeInList 元素独立转义** — Validates: Requirements 6.5
    - **Property 36: safeInList 空数组拒绝** — Validates: Requirements 6.5
    - **Property 37: safeLike 通配符转义** — Validates: Requirements 6.8

- [x] 4. SQL 安全检查器（Sanitizer）
  - [x] 4.1 实现词法扫描器（internal/template/sanitizer.go）：sanitizeSQL 函数，7 状态状态机（NORMAL、IN_SINGLE_QUOTE、IN_DOUBLE_QUOTE、IN_BACKTICK、IN_LINE_COMMENT、IN_BLOCK_COMMENT、IN_HINT），检测字符串/标识符外分号、移除非 Hint 注释、检测未闭合引号
    - _Requirements: 6.6_
    - _Design: 组件与接口 §10 (SQL 安全检查器)_
  - [x] 4.2 编写安全检查器单元测试和属性测试
    - **Property 38: 多语句检测** — Validates: Requirements 6.6
    - **Property 39: SQL 注释检测** — Validates: Requirements 6.6
    - **Property 40: SQL Hint 保留** — Validates: Requirements 6.6
    - **Property 67: 双引号标识符安全** — Validates: Requirements 6.6
    - **Property 68: 反引号标识符安全** — Validates: Requirements 6.6
    - **Property 69: 未闭合引号检测** — Validates: Requirements 6.6

- [x] 5. 参数校验器（Validator）
  - [x] 5.1 实现参数校验（internal/template/validator.go）：validateParams 函数，必填检查、默认值填充（类型化）、类型转换（JSON float64→int64 等）、枚举约束、max_length/max_items 约束、pattern 正则约束
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9_
    - _Design: 组件与接口 §8 (参数校验器)_
  - [x] 5.2 编写参数校验器单元测试和属性测试
    - **Property 41: 必填参数检查** — Validates: Requirements 7.2
    - **Property 42: 类型匹配检查** — Validates: Requirements 7.4
    - **Property 43: 默认值填充** — Validates: Requirements 7.5
    - **Property 44: 枚举约束** — Validates: Requirements 7.6
    - **Property 45: 字符串长度约束** — Validates: Requirements 7.7
    - **Property 46: 数组大小约束** — Validates: Requirements 7.8
    - **Property 47: 正则约束** — Validates: Requirements 7.9

- [x] 6. Checkpoint - 确保安全层就绪
  - 验证：golangci-lint 通过 + FuncMap/Sanitizer/Validator 所有属性测试通过

- [x] 7. 模板注册表与加载器
  - [x] 7.1 实现模板注册表（internal/template/registry.go）：TemplateRegistry 结构体（RWMutex 保护），Get/GetAll/Update/GetHash 方法，RegisteredTemplate 和 ParamSchema 类型
    - _Requirements: 1.1, 1.6_
    - _Design: 组件与接口 §5 (TemplateRegistry)_
  - [x] 7.2 实现模板加载器（internal/template/loader.go）：loadAll 函数，共享片段加载（shared_dir）、模板名称校验（^[a-zA-Z0-9_-]{1,64}$）、文件大小限制（1MB）、UTF-8 校验、text/template 解析（Option("missingkey=error")）、参数 Schema 预编译（正则、默认值类型化）、SHA-256 hash 计算
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.7, 1.8, 1.9, 1.10, 1.11, 1.12_
    - _Design: 组件与接口 §6 (模板加载器)_
  - [x] 7.3 编写注册表和加载器单元测试和属性测试（加载器层校验 — 测试 loadAll 函数）。注意：P4/P5 在此任务中测试加载器层校验逻辑（文件实际加载时的校验），Task 1.6 中测试配置层校验逻辑（YAML 配置解析时的校验），两者测试层级不同
    - **Property 1: 模板名称唯一性** — Validates: Requirements 1.6
    - **Property 2: 模板语法有效性** — Validates: Requirements 1.3
    - **Property 3: 无效模板不影响启动** — Validates: Requirements 1.4
    - **Property 4: 模板名称格式校验（加载器层）** — Validates: Requirements 1.9
    - **Property 5: 文件大小限制（加载器层）** — Validates: Requirements 1.10
    - **Property 6: UTF-8 编码校验** — Validates: Requirements 1.11
    - **Property 7: 共享片段加载** — Validates: Requirements 1.12

- [x] 8. 模板渲染器
  - [x] 8.1 实现模板渲染（internal/template/renderer.go）：render 函数，构建 renderContext{Params}，render_timeout 超时控制（goroutine + select），Trim + 非空验证，长度检查（max_rendered_sql_length），调用 sanitizeSQL 安全检查
    - _Requirements: 2.1, 2.2, 2.4, 2.5, 2.7, 2.8, 2.9, 2.10_
    - _Design: 组件与接口 §7 (模板渲染器)_
  - [x] 8.2 编写渲染器单元测试和属性测试
    - **Property 8: 渲染结果非空** — Validates: Requirements 2.8
    - **Property 9: 渲染结果长度限制** — Validates: Requirements 2.10
    - **Property 10: 渲染超时保护** — Validates: Requirements 2.9
    - **Property 11: 不存在模板返回错误** — Validates: Requirements 2.3
    - **Property 12: 渲染确定性** — Validates: Requirements 2.1

- [ ] 9. 分页包装器
  - [-] 9.1 实现分页包装（internal/template/pagination.go）：wrapWithPagination（over-fetch first+1 策略、safeIdentifier 校验 fields/orderBy、参数化 LIMIT/OFFSET、__tq_wrapper__ 别名）、wrapWithCount（SELECT COUNT(*) AS total_count、__tq_cnt__ 别名）
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7_
    - _Design: 组件与接口 §11 (分页包装器), D14 (over-fetch)_
  - [~] 9.2 编写分页包装器单元测试和属性测试
    - **Property 17: LIMIT 参数化** — Validates: Requirements 4.7
    - **Property 18: OFFSET 参数化** — Validates: Requirements 4.7
    - **Property 19: 默认 LIMIT 强制** — Validates: Requirements 4.6
    - **Property 20: 字段选择安全性** — Validates: Requirements 4.3
    - **Property 21: OrderBy 字段安全性** — Validates: Requirements 4.4
    - **Property 22: totalCount 独立性** — Validates: Requirements 4.5

- [ ] 10. Checkpoint - 确保模板引擎核心组件就绪
  - 验证：golangci-lint 通过 + Registry/Loader/Renderer/Pagination 所有测试通过

- [ ] 11. 缓存集成
  - [~] 11.1 实现缓存 key 生成与缓存集成（internal/template/cache.go）：generateCacheKey（canonical_string 确定性构建）、generateCountCacheKey（仅 templateName+params）、executeWithCache（JSON 序列化 + loaderCalled 标志 + singleflight 语义文档）、executeCount（validatedParams 参数来源一致性）、shouldCache 辅助方法
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_
    - _Design: 组件与接口 §12 (缓存集成), D3, D4, D8, D11_
  - [~] 11.2 编写缓存集成单元测试和属性测试
    - **Property 49: 缓存 key 确定性** — Validates: Requirements 8.2
    - **Property 50: 缓存 key 区分性** — Validates: Requirements 8.2
    - **Property 51: 缓存 key 含 fields** — Validates: Requirements 8.2
    - **Property 52: 模板级 TTL 覆盖** — Validates: Requirements 8.3
    - **Property 53: 缓存禁用** — Validates: Requirements 8.4
    - **Property 54: 客户端缓存绕过** — Validates: Requirements 8.5
    - **Property 55: totalCount 独立缓存** — Validates: Requirements 8.6

- [ ] 12. TemplateEngine 核心与 RawExecutor 实现
  - [~] 12.1 实现 TemplateEngine 主体：
    - engine.go：NewTemplateEngine（FuncMap 构建 + 信号量初始化 + loadAll）、Execute 完整流程（defer 审计日志 + executeErr 追踪 + 信号量等待计时 + 缓存命中/未命中指标）、ListTemplates、DatasourceName、Close
    - metrics.go（结构体定义部分）：TemplateMetrics 结构体声明（QueryDuration、QueriesTotal、RenderDuration、SemaphoreWait、CacheHitsTotal、RenderGoroutineLeaks 共 6 个字段）。注意：此处仅定义结构体，NewTemplateMetrics 注册函数在 Task 15.1 中实现。Execute 中的指标调用依赖此结构体，因此必须在 Task 12 中创建
    - _Requirements: 1.1, 2.1, 4.9, 5.1, 5.2, 5.5, 5.6_
    - _Design: 组件与接口 §4 (TemplateEngine), Execute 伪代码, §18 (并发控制), §13 (TemplateMetrics 结构体)_
  - [~] 12.2 实现 StarRocks Adapter ExecuteRaw 方法（internal/adapter/starrocks/adapter.go）：复用 *sql.DB 和 scanRows，不经过 SQLQueryBuilder 和白名单
    - _Requirements: 5.1, 5.2, 5.3_
    - _Design: 组件与接口 §1 (StarRocks Adapter 实现)_
  - [~] 12.3 编写 TemplateEngine 单元测试（使用 MockRawExecutor）
    - **Property 23: 并发限制** — Validates: Requirements 4.9
    - **Property 24: 结果截断 + 警告** — Validates: Requirements 5.6
    - **Property 25: 查询超时保护** — Validates: Requirements 5.5
    - **Property 27: 接口隔离** — Validates: Requirements 5.1
  - [~] 12.4 实现 Reload 方法（internal/template/engine.go）：reloadMu 互斥锁保护、10s Mutation 冷却时间（fsnotify 不受冷却限制）、错误隔离合并（失败模板从旧 Registry 复制旧版本）、hash 比较仅清除变更模板缓存、lastReloadResult 记录
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.7, 10.9_
    - _Design: 组件与接口 §4 (Reload 方法), D5 (热加载原子性), D9 (不重新读取 config.yaml)_

- [ ] 13. Checkpoint - 确保 TemplateEngine 端到端可用
  - 验证：golangci-lint 通过 + TemplateEngine.Execute 通过 MockRawExecutor 端到端测试 + 缓存命中/未命中行为正确

- [ ] 14. GraphQL Schema 与 Resolver
  - [~] 14.1 定义 GraphQL Schema（internal/graphql/schema/template.graphql）：TemplateOrderBy input、TemplateQueryConnection type、TemplateInfo/TemplateParameterInfo type、ReloadTemplatesResult/TemplateLoadFailure type、extend Query（templateQuery/templateList）、extend Mutation（reloadTemplates）。注意：gqlgen.yml 已使用 `*.graphql` 通配符，无需更新 schema 路径
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8_
    - _Design: 组件与接口 §15 (GraphQL Schema)_
  - [~] 14.2 运行 gqlgen 代码生成：go generate ./... 生成 generated.go 和 models_gen.go
    - _Design: §15 gqlgen 代码生成说明_
  - [~] 14.3 修改 Resolver 结构体（internal/graphql/resolver/resolver.go）：新增 TemplateEngine *template.TemplateEngine 字段（nil 表示功能禁用），此为跨文件变更，影响 NewResolver 调用方
    - _Design: 组件与接口 §16 (Resolver 依赖注入)_
  - [~] 14.4 实现 Resolver（internal/graphql/resolver/template.resolvers.go）：TemplateQuery（over-fetch 截断 + max_result_rows 截断 + warnings）、TemplateList、ReloadTemplates
    - _Requirements: 3.1, 3.4, 3.8, 3.9_
    - _Design: 组件与接口 §16 (Resolver 实现)_
  - [~] 14.5 实现 Resolver 辅助函数：convertJSONToMap、convertOrderBy、skipCacheRequested、extractPrincipal、fieldRequested（graphql.CollectFieldsCtx）、buildTemplateQueryConnection（hasNextPage over-fetch 逻辑）、convertReloadResult、setExtensionWarnings
    - _Design: 辅助函数定义_
  - [~] 14.6 编写 Resolver 单元测试和属性测试
    - **Property 13: templateList 完整性** — Validates: Requirements 3.4
    - **Property 14: templateList 参数一致性** — Validates: Requirements 3.5
    - **Property 15: countEnabled 一致性** — Validates: Requirements 3.5
    - **Property 16: 功能禁用行为** — Validates: Requirements 3.9

- [ ] 15. 可观测性集成
  - [~] 15.1 实现 Prometheus 指标注册函数（internal/template/metrics.go 追加）：NewTemplateMetrics 函数（接收 *prometheus.Registry 和 customLabels，创建并注册 6 个指标到 Registry，返回 *TemplateMetrics）。注意：TemplateMetrics 结构体已在 Task 12.1 中定义，此处仅添加注册函数
    - _Requirements: 9.1, 9.2, 9.6_
    - _Design: 组件与接口 §13 (Prometheus 指标注册)_
  - [~] 15.2 集成 OpenTelemetry Tracing：在 Execute 中创建 "Template Query {name}" 子 Span，设置 template.name/db.system/db.statement 属性
    - _Requirements: 9.3_
    - _Design: 组件与接口 §13 (OpenTelemetry Tracing)_
  - [~] 15.3 集成结构化日志：模板查询执行日志（Info 级别）、渲染后 SQL 日志（Debug 级别，脱敏后）
    - _Requirements: 9.4, 6.7_
    - _Design: 组件与接口 §13 (结构化日志)_
  - [~] 15.4 编写可观测性属性测试
    - **Property 56: 查询延迟指标记录** — Validates: Requirements 9.1
    - **Property 57: 查询计数指标记录** — Validates: Requirements 9.2
    - **Property 58: 渲染延迟指标记录** — Validates: Requirements 9.6
    - **Property 59: Tracing Span 创建** — Validates: Requirements 9.3
    - **Property 60: 审计日志记录** — Validates: Requirements 9.5

- [ ] 16. 服务初始化集成
  - [~] 16.1 修改 main.go 初始化流程，按以下 8 步顺序集成（与设计文档 §17 对齐）：
    1. LoadConfig() — 解析 config.yaml
    2. NewMetricsCollector() — 创建指标收集器
    3. 合并模板 TTL 到 CacheLayerConfig — 遍历 cfg.SQLTemplates.Templates，将 cache_ttl 以 "template:{name}" 键合并到 TTLConfig map（★ 必须在 NewCacheLayer 之前）
    4. NewCacheLayer() — 创建缓存层（TTLConfig 已包含模板 TTL）
    5. DataSourceManager.Init() — 初始化数据源（含 StarRocks Adapter）
    6. NewTemplateEngine() — 创建模板引擎（使用 cfg.SQLTemplates.DatasourceName 查找 Adapter，类型断言为 *starrocks.Adapter，传入 RawExecutor 接口）
    7. NewTemplateWatcher().Start() — 启动文件监听（base_dir + shared_dir）
    8. NewResolver() — 创建 GraphQL Resolver（注入 TemplateEngine，nil 表示禁用）
    - _Requirements: 1.1, 3.9_
    - _Design: 组件与接口 §17 (服务初始化集成), D8 (缓存 TTL 预注册)_
  - [~] 16.2 集成优雅关闭：在现有关闭流程中插入 TemplateWatcher.Stop() 和 TemplateEngine.Close()（在 TracingProvider.Shutdown 之前、DataSourceManager.CloseAll 之前）
    - _Design: 优雅关闭集成_
  - [~] 16.3 新增 MetricsCollector.CustomLabels() 公开 getter 方法
    - _Design: §17 步骤 6-8 注释_

- [ ] 17. Checkpoint - 确保 GraphQL 端到端集成完成
  - 验证：golangci-lint 通过 + gqlgen 代码生成成功 + templateQuery/templateList/reloadTemplates GraphQL 端到端测试通过 + 指标/Tracing/审计日志正常记录

- [ ] 18. 模板热加载
  - [~] 18.1 实现文件监听器（internal/template/watcher.go）：TemplateWatcher 结构体，NewTemplateWatcher（filepath.WalkDir 递归添加子目录）、Start（500ms 防抖 + fsnotify.Create 动态添加新子目录）、Stop
    - _Requirements: 10.6_
    - _Design: 组件与接口 §14 (热加载)_
  - [~] 18.2 编写热加载单元测试和属性测试
    - **Property 61: 热加载原子性** — Validates: Requirements 10.3
    - **Property 62: 错误隔离** — Validates: Requirements 10.4
    - **Property 63: 缓存清除（仅变更）** — Validates: Requirements 10.7
    - **Property 64: 并发安全** — Validates: Requirements 10.9
    - **Property 65: 权限检查** — Validates: Requirements 10.8
    - **Property 66: 模板 hash 追踪** — Validates: Requirements 10.7

- [ ] 19. 集成测试
  - [~] 19.1 编写端到端集成测试（GraphQL 请求 → Resolver → TemplateEngine → MockRawExecutor → 响应验证），具体测试用例：
    - TestIntegration_TemplateQuery_NormalFlow — 正常查询返回 nodes + pageInfo + totalCount
    - TestIntegration_TemplateQuery_ParamValidationFailure — 必填参数缺失/类型不匹配返回对应错误码
    - TestIntegration_TemplateQuery_CacheHitMiss — 相同参数第二次请求命中缓存，extensions.cache=false 绕过缓存
    - TestIntegration_TemplateQuery_TotalCountDisabled — count_enabled=false 时 totalCount 返回 -1 + warnings
    - TestIntegration_TemplateQuery_OverFetchHasNextPage — first=10 时实际查询 11 行，返回 10 行 + hasNextPage=true
    - TestIntegration_TemplateQuery_FeatureDisabled — sql_templates.enabled=false 时返回 VALIDATION_TEMPLATE_NOT_FOUND
    - TestIntegration_TemplateList_Complete — templateList 返回所有已注册模板元信息
    - _Requirements: 3.1, 3.9, 4.5, 5.2, 8.1_
  - [~] 19.2 编写热加载集成测试，具体测试用例：
    - TestIntegration_HotReload_FileChange — 修改模板文件 → Reload → 验证新版本生效
    - TestIntegration_HotReload_CacheClear — hash 变化的模板缓存被清除，未变化的保留
    - TestIntegration_HotReload_ErrorIsolation — 失败模板保留旧版本，其他模板正常更新
    - TestIntegration_HotReload_MutationCooldown — 10s 内重复 Mutation 返回上次结果
    - _Requirements: 10.1, 10.4, 10.7_
  - [~] 19.3 编写权限集成测试：无 query 权限返回 AUTH_INSUFFICIENT_PERMISSION、无 mutation 权限 reloadTemplates 被拒绝
    - _Requirements: 5.7, 10.8_
    - **Property 26: 权限检查** — Validates: Requirements 5.7

- [ ] 20. 文档与示例模板
  - [~] 20.1 创建示例模板文件：templates/_shared/time_filter.sql.tmpl、templates/fleet/fleet_report.sql.tmpl、templates/driver/driver_score.sql.tmpl
    - _Design: 需求文档附录 C (SQL 模板文件示例)_
  - [~] 20.2 验证 config.yaml 中 sql_templates 配置段完整性（Task 1.2 中已添加基础配置，此处补充完整的生产示例注释和说明）
    - _Design: 需求文档附录 A (配置示例)_

- [ ] 21. 最终验收
  - 验证：golangci-lint 通过 + 所有单元测试通过 + 所有属性测试通过（P1-P69）+ 集成测试通过 + 示例模板可正常执行 + 性能 SLA 验证（渲染 p99 ≤ 50ms、缓存命中 p99 ≤ 10ms）

## 属性测试文件映射汇总

验收时核对：每个属性测试文件覆盖的属性编号与需求文档附录 G 一致。

| 测试文件 | 覆盖属性 | 对应任务 |
|---------|---------|---------|
| `internal/template/funcmap_property_test.go` | P28-P37 | Task 3.3 |
| `internal/template/sanitizer_property_test.go` | P38-P40, P67-P69 | Task 4.2 |
| `internal/template/validator_property_test.go` | P41-P48 | Task 5.2 |
| `internal/template/registry_property_test.go` | P1-P7 | Task 7.3 |
| `internal/template/renderer_property_test.go` | P8-P12 | Task 8.2 |
| `internal/template/pagination_property_test.go` | P17-P22 | Task 9.2 |
| `internal/template/cache_property_test.go` | P49-P55 | Task 11.2 |
| `internal/template/engine_property_test.go` | P23-P25, P27 | Task 12.3 |
| `internal/graphql/resolver/template_property_test.go` | P13-P16 | Task 14.6 |
| `internal/template/metrics_property_test.go` | P56-P60 | Task 15.4 |
| `internal/template/watcher_property_test.go` | P61-P66 | Task 18.2 |
| `internal/graphql/resolver/template_integration_test.go` | P26 | Task 19.3 |
| `internal/config/validation_test.go`（扩展） | P4(配置层), P5(配置层), P48 | Task 1.6 |
