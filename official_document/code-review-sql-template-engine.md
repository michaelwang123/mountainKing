# SQL 模板查询引擎代码审查报告

## 审查范围

对照 `.kiro/specs/sql-template-engine/` 中的 requirements.md、design.md 和 tasks.md，全面审查以下代码：

- `internal/template/` 全部源文件（12 个源文件 + 13 个测试文件）
- `internal/graphql/resolver/template.resolvers.go` 及相关测试
- `internal/adapter/starrocks/adapter.go`（ExecuteRaw 方法）
- `cmd/server/main.go`（模板引擎集成部分）
- `internal/config/config.go`、`internal/config/validation.go`（配置结构与校验）
- `internal/errors/errors.go`（错误码）
- `internal/audit/audit.go`（ExtraFields 扩展）

## 审查维度

1. 实现完整性（对照 tasks.md）
2. 设计一致性（对照 design.md）
3. 需求覆盖（对照 requirements.md 验收标准）
4. 编译和类型安全
5. 安全性（SQL 注入防护）
6. 并发安全
7. 错误处理
8. 资源泄漏
9. 测试充分性（69 个属性测试）

## 审查结果

### 编译和类型安全

所有 14 个源文件通过 `go build ./...` 和 `go vet` 检查，零错误零警告。

---

## Critical 级别问题

### C1: main.go 缺少 TemplateWatcher 创建和启动

| 项目 | 详情 |
|------|------|
| 文件 | `cmd/server/main.go` ~行 200 |
| 对应任务 | Task 16.1 步骤 7 |
| 对应需求 | 需求 10.6（fsnotify 文件监听自动触发模板重新加载） |
| 影响 | 模板文件变更后不会自动触发重新加载，只能通过 `reloadTemplates` Mutation 手动触发 |

tasks.md Task 16.1 步骤 7 要求 `NewTemplateWatcher().Start()`，但 `cmd/server/main.go` 中创建 TemplateEngine 后完全没有创建 TemplateWatcher。需求 10.6 的 fsnotify 文件监听功能未集成到服务中。Task 16.2 要求的优雅关闭中 `TemplateWatcher.Stop()` 也因此缺失。

---

## High 级别问题

### H1: sanitizer.go 反斜杠序列处理有安全隐患

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/sanitizer.go` 行 ~85-93（stateInSingleQuote） |
| 对应需求 | 需求 6.6（词法扫描器） |
| 影响 | 如果模板作者绕过安全函数直接拼接 SQL，sanitizer 作为最后防线可能被绕过 |

当前代码在 `stateInSingleQuote` 中先检查 `\'` 再处理默认字符，但未处理 `\\`（转义反斜杠）。对于输入 `\\'`（两个反斜杠 + 单引号），正确的 SQL 语义是：`\\` = 转义反斜杠（字面值 `\`），`'` = 关闭字符串。但扫描器在位置 1 看到 `\` 后面跟 `'`，错误地将其视为 `\'`（转义引号），导致扫描器认为字符串未关闭。

实际影响：通过 `safeString` 函数处理的参数不会触发此问题（因为 safeString 先转义反斜杠再转义引号）。但 sanitizer 作为"纵深防御"的最后一层，应正确处理所有合法 SQL。

### H2: Reload 缓存清除逻辑错误 — 清除全部缓存而非仅变更模板

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/engine.go` 行 ~350（Reload 方法） |
| 对应需求 | 需求 10.7（仅对 hash 变化的模板清除缓存） |
| 影响 | 每次 Reload 会清除所有数据源的缓存（包括 StarRocks 单表查询和 Prometheus 查询），而非仅清除变更模板的缓存 |

当前代码调用 `te.cacheLayer.ClearByDatasource(ctx, "")` 传入空字符串。设计文档要求 `Cache.DeleteByPrefix("cache:template:{template_name}")`，但 CacheLayer 没有 DeleteByPrefix 方法。当前实现会导致每次模板热加载时所有缓存被清空，影响其他查询的缓存命中率。

### H3: RenderGoroutineLeaks 指标已定义但从未使用

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/metrics.go` 行 17, `internal/template/renderer.go` |
| 对应需求 | 设计文档 D7（Goroutine 泄漏风险缓解策略） |
| 影响 | 无法通过 Prometheus 监控渲染超时导致的 goroutine 泄漏 |

`RenderGoroutineLeaks` Gauge 在 metrics.go 中定义并注册到 Prometheus，但 renderer.go 中渲染超时后从未调用 `Inc()` 来追踪泄漏的 goroutine。设计文档 D7 明确提到需要此指标来监控泄漏。

---

## Medium 级别问题

### M1: Reload 的 SuccessCount 包含错误隔离保留的旧版本模板

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/engine.go` 行 ~370 |
| 对应需求 | 需求 10.5（返回成功加载的模板数量） |
| 影响 | `reloadTemplates` Mutation 返回的 successCount 可能误导用户 |

`result.SuccessCount = len(lr.Registered)` 在错误隔离合并之后计算，此时 `lr.Registered` 已包含从旧 Registry 复制的失败模板旧版本。这意味着 SuccessCount 会把"保留旧版本"也算作"成功"。

### M2: ListTemplates 返回顺序不确定

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/engine.go` ListTemplates, `internal/template/registry.go` GetAll |
| 对应需求 | 需求 3.4（templateList 返回模板元信息列表） |
| 影响 | `templateList` 查询每次返回的模板顺序可能不同，对客户端分页不友好 |

`GetAll()` 遍历 `map[string]*RegisteredTemplate`，Go map 迭代顺序不确定。

### M3: renderer.go 超时后 goroutine 泄漏无追踪

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/renderer.go` 行 ~50 |
| 对应需求 | 设计文档 D7 |
| 影响 | 渲染超时后泄漏的 goroutine 无法被监控 |

渲染超时后，执行 `template.Execute()` 的 goroutine 会继续运行直到完成。当前 render 函数是独立函数，无法访问 metrics，因此无法在超时分支中递增 `RenderGoroutineLeaks` 指标。

---

## Low 级别问题

### L1: 设计文档签名偏差 — 函数 vs 方法

| 项目 | 详情 |
|------|------|
| 文件 | 多个文件 |
| 影响 | 不影响正确性，但与设计文档不完全一致 |

design.md 定义 `loadAll`、`render`、`validateParams`、`wrapWithPagination` 为 TemplateEngine 的方法，但实现为包级函数。功能等价。

### L2: P26 权限测试覆盖不足

| 项目 | 详情 |
|------|------|
| 文件 | `internal/graphql/resolver/template_integration_test.go` |
| 对应需求 | 需求 5.7, 10.8 |
| 影响 | 权限检查的验收标准未被直接验证 |

P26（权限检查）仅测试了 nil TemplateEngine 的行为，未测试实际的 auth middleware 拒绝无权限请求的场景。这与设计决策 D10 一致（授权由 middleware 处理），但 requirements 5.7 和 10.8 的验收标准未被直接验证。

### L3: watcher.go 的 debounceTimer 在 Stop 后可能触发

| 项目 | 详情 |
|------|------|
| 文件 | `internal/template/watcher.go` 行 ~80 |
| 影响 | 不会导致 panic，但会产生无意义的日志 |

`Stop()` 关闭 done channel 后，如果 debounceTimer 已经 fired 但 callback 还没执行，`triggerReload()` 会在 engine 已关闭后尝试 Reload。由于 Reload 有 mutex 保护且是幂等的，不会导致 panic。

---

## 测试充分性总结

69 个属性测试中：
- 67 个有对应的测试实现且测试逻辑正确
- P11（不存在模板返回错误）在 engine 层测试而非 renderer 层，合理
- P26（权限检查）仅测试 nil engine 场景，覆盖较弱
- 所有属性测试文件均存在且与 tasks.md 映射表一致

## 需求覆盖总结

| 需求 | 覆盖状态 | 备注 |
|------|---------|------|
| 需求 1（模板加载） | ✅ 完整 | |
| 需求 2（模板渲染） | ✅ 完整 | |
| 需求 3（GraphQL 集成） | ✅ 完整 | |
| 需求 4（分页与字段选择） | ✅ 完整 | |
| 需求 5（查询执行） | ⚠️ 基本完整 | 5.7 权限检查依赖 middleware，未直接验证 |
| 需求 6（SQL 注入防护） | ⚠️ 有安全隐患 | sanitizer 反斜杠处理有 bug（H1） |
| 需求 7（参数校验） | ✅ 完整 | |
| 需求 8（缓存集成） | ✅ 完整 | |
| 需求 9（可观测性） | ⚠️ 基本完整 | RenderGoroutineLeaks 指标未使用（H3） |
| 需求 10（热加载） | ❌ 不完整 | fsnotify 未集成到 main.go（C1），缓存清除逻辑错误（H2） |
