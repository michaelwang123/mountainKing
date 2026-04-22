# Template Engine Review Fixes — Bugfix Design

## Overview

SQL 模板查询引擎代码审查发现 7 个实现缺陷（1 Critical、3 High、3 Medium）。本设计文档定义每个缺陷的 Bug Condition、修复方案、以及验证策略。修复范围涉及 4 个文件：`cmd/server/main.go`、`internal/template/sanitizer.go`、`internal/template/engine.go`、`internal/template/renderer.go`（+ `registry.go` 排序）。所有修复均为局部变更，不改变公共 API 签名。

## Glossary

- **Bug_Condition (C)**: 触发缺陷的输入条件集合
- **Property (P)**: 在 Bug Condition 下的期望正确行为
- **Preservation**: 修复不得改变的既有行为
- **TemplateWatcher**: `internal/template/watcher.go` 中的 fsnotify 文件监听器，负责检测模板文件变更并触发 Reload
- **sanitizeSQL**: `internal/template/sanitizer.go` 中的词法扫描器，检测 SQL 注入并移除注释
- **ClearByDatasource**: `internal/cache/layer.go` 中按 datasource 前缀清除缓存的方法，使用 `cache:{datasource}:` 前缀
- **RenderGoroutineLeaks**: `internal/template/metrics.go` 中定义的 Prometheus Gauge，追踪渲染超时导致的 goroutine 泄漏
- **loadResult**: `internal/template/loader.go` 中 `loadAll` 的返回结构，包含 `Registered`（成功模板 map）、`Hashes`（SHA-256 map）、`Failures`（失败列表）

## Bug Details

### Bug Condition

本次修复涵盖 7 个独立的 Bug Condition，每个对应一个代码审查发现的缺陷：

**C1 — TemplateWatcher 未集成到 main.go：**
当 `sql_templates.enabled=true` 且 TemplateEngine 创建成功时，`cmd/server/main.go` 从未创建 `TemplateWatcher`，fsnotify 文件监听功能完全缺失。

**C2 — sanitizer 反斜杠处理错误：**
当 `stateInSingleQuote` 遇到 `\\` 后跟 `'` 时，扫描器错误地将位置 1 的 `\'` 视为转义引号，导致字符串边界判断错误。

**C3 — Reload 缓存清除范围过大：**
当 Reload 检测到模板 hash 变化时，调用 `ClearByDatasource(ctx, "")` 清除所有缓存，而非仅清除变更模板的缓存。

**C4 — RenderGoroutineLeaks 指标未使用：**
当渲染超时时，`render` 函数从未递增 `RenderGoroutineLeaks` Gauge。

**C5 — Reload SuccessCount 包含错误隔离模板：**
`SuccessCount = len(lr.Registered)` 在错误隔离合并之后计算，包含了从旧 Registry 复制的失败模板旧版本。

**C6 — ListTemplates 返回顺序不确定：**
`GetAll()` 遍历 Go map，迭代顺序不确定，导致分页结果不稳定。

**C7 — 渲染超时 goroutine 泄漏无追踪：**
`render` 是独立包级函数，无法访问 `TemplateMetrics`，无法在超时时 Inc() 或在泄漏 goroutine 完成时 Dec()。

**Formal Specification:**
```
FUNCTION isBugCondition(input)
  INPUT: input of type {issueID: string, context: SystemState}
  OUTPUT: boolean

  SWITCH input.issueID:
    CASE "C1":
      RETURN input.context.sqlTemplatesEnabled == true
             AND input.context.templateEngineCreated == true
             AND input.context.templateWatcherCreated == false

    CASE "C2":
      RETURN input.context.scannerState == stateInSingleQuote
             AND input.context.currentChar == '\\'
             AND input.context.nextChar == '\\'
             // Scanner sees \\ but only checks \' first, missing the escaped backslash

    CASE "C3":
      RETURN input.context.reloadTriggered == true
             AND input.context.templateHashChanged == true
             AND input.context.clearByDatasourceArg == ""

    CASE "C4":
      RETURN input.context.renderTimedOut == true
             AND input.context.renderGoroutineLeaksIncremented == false

    CASE "C5":
      RETURN input.context.reloadHasFailures == true
             AND input.context.successCountIncludesIsolated == true

    CASE "C6":
      RETURN len(input.context.registeredTemplates) > 1
             AND input.context.listTemplatesResultSorted == false

    CASE "C7":
      RETURN input.context.renderTimedOut == true
             AND input.context.renderFunctionHasMetricsAccess == false
END FUNCTION
```

### Examples

- **C1**: 服务启动时 `sql_templates.enabled=true`，TemplateEngine 创建成功，但修改 `./templates/` 下的 `.sql.tmpl` 文件后不会自动触发 Reload
- **C2**: SQL `SELECT * FROM t WHERE name = 'path\\' OR 1=1 --'`，sanitizer 认为 `\\'` 中的 `\'` 是转义引号，字符串未关闭，`OR 1=1 --` 被视为字符串内容而非注入
- **C3**: 修改一个模板后触发 Reload，所有 StarRocks 单表查询和 Prometheus 查询的缓存全部被清空
- **C4**: 模板渲染超时后，Prometheus 中 `graphql_template_render_goroutine_leaks` 始终为 0
- **C5**: 10 个模板中 2 个加载失败，Reload 返回 `successCount=10`（包含 2 个保留的旧版本），实际新加载成功只有 8 个
- **C6**: 连续两次调用 `templateList(first: 2)` 返回不同的模板对，因为 map 迭代顺序不确定
- **C7**: 渲染超时后泄漏的 goroutine 最终完成，但无法 Dec() 指标，因为 `render` 函数无法访问 metrics

## Expected Behavior

### Preservation Requirements

**Unchanged Behaviors:**
- 当 `sql_templates.enabled=false` 时，不创建 TemplateEngine 或 TemplateWatcher（3.1）
- 标准单引号字符串中 `''`（doubled quote）和简单 `\'`（无前置反斜杠）的处理逻辑不变（3.2）
- 无 hash 变化的模板 Reload 不触发缓存清除（3.3）
- 渲染成功时不影响 `RenderGoroutineLeaks` 指标（3.4）
- 所有模板加载成功时 `SuccessCount` 等于总模板数（3.5）
- 无分页参数时 `ListTemplates` 返回所有模板（3.6）
- 渲染成功时无 goroutine 泄漏追踪开销（3.7）

**Scope:**
所有不涉及上述 7 个 Bug Condition 的输入路径应完全不受修复影响。包括：
- 鼠标/API 触发的 `reloadTemplates` Mutation 功能
- `templateQuery` 的完整执行流程（lookup → validate → render → paginate → cache → execute）
- 所有非模板相关的缓存操作（StarRocks 单表查询、Prometheus 查询）
- 所有其他 GraphQL 查询和 Mutation

## Hypothesized Root Cause

Based on the code review findings, the root causes are:

1. **C1 — 集成遗漏**: `cmd/server/main.go` 在创建 TemplateEngine 后直接进入 resolver/server 初始化，遗漏了 `NewTemplateWatcher` 调用。`watcher.go` 代码完整但未被 main.go 使用。

2. **C2 — 检查顺序错误**: `sanitizer.go` `stateInSingleQuote` 中先检查 `\'` 后处理默认字符，但缺少对 `\\` 的独立检查。当输入为 `\\'` 时，位置 1 的 `\` 后跟 `'` 被错误匹配为 `\'`。

3. **C3 — 参数错误**: `engine.go` Reload 中调用 `ClearByDatasource(ctx, "")` 传入空字符串。`ClearByDatasource` 使用 `cache:{datasource}:` 前缀，空字符串导致前缀为 `cache::` 或清除所有条目。正确参数应为 `"template:"+name`。

4. **C4 + C7 — 架构限制**: `render` 是包级函数，无法访问 `TemplateEngine.metrics`。超时分支无法调用 `metrics.RenderGoroutineLeaks.Inc()`，泄漏 goroutine 完成时也无法 Dec()。

5. **C5 — 计算时机错误**: `SuccessCount = len(lr.Registered)` 在错误隔离合并（将旧版本复制到 `lr.Registered`）之后执行，导致计数包含非新加载的模板。

6. **C6 — Go map 迭代特性**: `registry.go` `GetAll()` 遍历 `map[string]*RegisteredTemplate`，Go 语言规范不保证 map 迭代顺序。

## Correctness Properties

Property 1: Bug Condition — TemplateWatcher Integration (C1)

_For any_ system state where `sql_templates.enabled` is true and TemplateEngine is successfully created, the fixed `main.go` SHALL create a `TemplateWatcher` via `NewTemplateWatcher`, call `Start()`, and include `Stop()` in the graceful shutdown sequence before closing TemplateEngine.

**Validates: Requirements 2.1**

Property 2: Bug Condition — Sanitizer Backslash Handling (C2)

_For any_ SQL string containing `\\` (escaped backslash) inside a single-quoted literal, the fixed `sanitizeSQL` SHALL correctly parse `\\` as a literal backslash (staying in string state) and treat the following `'` as a closing quote (returning to NORMAL state), maintaining correct string boundary tracking.

**Validates: Requirements 2.2**

Property 3: Bug Condition — Reload Cache Clearing Scope (C3)

_For any_ Reload operation where template hash changes are detected, the fixed `Reload` method SHALL call `ClearByDatasource(ctx, "template:"+name)` for each changed template, clearing only that template's cache entries.

**Validates: Requirements 2.3**

Property 4: Bug Condition — RenderGoroutineLeaks Metric Tracking (C4 + C7)

_For any_ template render that times out, the fixed `render` method (now a `TemplateEngine` method) SHALL increment `RenderGoroutineLeaks` gauge on timeout, and the leaked goroutine wrapper SHALL decrement the gauge when the goroutine eventually completes.

**Validates: Requirements 2.4, 2.7**

Property 5: Bug Condition — Reload SuccessCount Accuracy (C5)

_For any_ Reload operation with template load failures, the fixed `Reload` method SHALL calculate `SuccessCount` as the number of freshly loaded templates (before error isolation merge), excluding templates retained from the previous registry.

**Validates: Requirements 2.5**

Property 6: Bug Condition — ListTemplates Deterministic Ordering (C6)

_For any_ call to `ListTemplates` or `GetAll`, the fixed code SHALL return templates sorted by name in ascending lexicographic order, ensuring stable pagination results.

**Validates: Requirements 2.6**

Property 7: Preservation — Non-Bug-Condition Behavior

_For any_ input where none of the 7 bug conditions hold, the fixed code SHALL produce exactly the same behavior as the original code, preserving all existing functionality including template query execution, non-template cache operations, authentication, rate limiting, and all other system behaviors.

**Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**

## Fix Implementation

### Changes Required

Assuming our root cause analysis is correct:

**File**: `cmd/server/main.go`

**Fix C1 — Add TemplateWatcher creation and lifecycle management**

1. **Create TemplateWatcher after TemplateEngine init** (~line 200, after `templateEngine` creation):
   - Determine watch directories: `[]string{cfg.SQLTemplates.BaseDir}` (add `cfg.SQLTemplates.SharedDir` if non-empty and different from BaseDir)
   - Call `template.NewTemplateWatcher(templateEngine, watchDirs, logger.Logger)`
   - Call `watcher.Start()`
   - Log watcher initialization

2. **Add Stop() to graceful shutdown** (before `templateEngine.Close()`):
   - Call `watcher.Stop()` before closing TemplateEngine
   - Log any stop errors

---

**File**: `internal/template/sanitizer.go`

**Fix C2 — Add `\\` handling before `\'` in stateInSingleQuote**

**Function**: `sanitizeSQL` — `stateInSingleQuote` case (~line 85)

**Specific Changes**:
1. Add a new check at the beginning of `stateInSingleQuote`: if `ch == '\\'` and next char is also `'\\'`, write both backslashes and advance by 2 (stay in string state)
2. The check order becomes: `\\` first → `\'` second → `''` third → closing `'` fourth → default

Current code:
```go
case stateInSingleQuote:
    if ch == '\\' && i+1 < n && sql[i+1] == '\'' {
        // Backslash-escaped quote \' ...
```

Fixed code:
```go
case stateInSingleQuote:
    if ch == '\\' && i+1 < n && sql[i+1] == '\\' {
        // Escaped backslash \\ — write both and stay in state
        out.WriteByte('\\')
        out.WriteByte('\\')
        i += 2
    } else if ch == '\\' && i+1 < n && sql[i+1] == '\'' {
        // Backslash-escaped quote \' ...
```

---

**File**: `internal/template/engine.go`

**Fix C3 — Correct ClearByDatasource argument in Reload**

**Function**: `Reload` (~line 350, cache clearing loop)

**Specific Changes**:
1. Replace `te.cacheLayer.ClearByDatasource(ctx, "")` with `te.cacheLayer.ClearByDatasource(ctx, "template:"+name)`
2. Remove the unused `prefix` variable and dead-code comment about `DeleteByPrefix`

---

**Fix C5 — Calculate SuccessCount before error isolation merge**

**Function**: `Reload` (~line 340-370)

**Specific Changes**:
1. Save `freshSuccessCount := len(lr.Registered)` immediately after `loadAll` returns, before the error isolation loop
2. Set `result.SuccessCount = freshSuccessCount` instead of `len(lr.Registered)`

---

**File**: `internal/template/engine.go` (ListTemplates) + `internal/template/registry.go` (GetAll)

**Fix C6 — Sort templates by name**

**Specific Changes**:
1. In `registry.go` `GetAll()`: after building the result slice, sort it by `Name` using `sort.Slice`
2. Add `"sort"` import to `registry.go`

---

**File**: `internal/template/renderer.go` → refactored as `TemplateEngine` method

**Fix C4 + C7 — Make render a TemplateEngine method with goroutine leak tracking**

**Specific Changes**:
1. Change `func render(ctx, tmpl, params, renderTimeout, maxRenderedSQLLen)` to `func (te *TemplateEngine) render(ctx, tmpl, params, renderTimeout, maxRenderedSQLLen)`
2. Update the call site in `engine.go` `Execute` from `render(ctx, ...)` to `te.render(ctx, ...)`
3. In the timeout branch (`<-timeoutCtx.Done()`):
   - Call `te.metrics.RenderGoroutineLeaks.Inc()` (nil-safe check)
   - Spawn a goroutine that reads from `ch` (waits for leaked goroutine to finish) then calls `te.metrics.RenderGoroutineLeaks.Dec()`
4. Return the timeout error as before

## Testing Strategy

### Validation Approach

The testing strategy follows a two-phase approach: first, surface counterexamples that demonstrate the bugs on unfixed code, then verify the fixes work correctly and preserve existing behavior.

### Exploratory Bug Condition Checking

**Goal**: Surface counterexamples that demonstrate the bugs BEFORE implementing the fixes. Confirm or refute the root cause analysis.

**Test Plan**: Write tests targeting each bug condition on the UNFIXED code to observe failures.

**Test Cases**:
1. **C1 — Watcher Integration Test**: Verify that `main.go` creates and starts a TemplateWatcher when `sql_templates.enabled=true` (will fail on unfixed code — no watcher created)
2. **C2 — Backslash Escape Test**: Call `sanitizeSQL("SELECT * FROM t WHERE x = 'a\\\\' OR 1=1 --'")` and verify the string boundary is correctly detected (will fail on unfixed code — scanner stays in string state)
3. **C3 — Cache Scope Test**: Trigger Reload with one changed template and verify only that template's cache is cleared (will fail on unfixed code — all cache cleared)
4. **C4/C7 — Goroutine Leak Metric Test**: Trigger a render timeout and verify `RenderGoroutineLeaks` is incremented (will fail on unfixed code — metric stays at 0)
5. **C5 — SuccessCount Test**: Reload with some template failures and verify `SuccessCount` excludes error-isolated templates (will fail on unfixed code — count too high)
6. **C6 — Ordering Test**: Call `ListTemplates` multiple times and verify consistent ordering (may fail on unfixed code — non-deterministic)

**Expected Counterexamples**:
- C2: `sanitizeSQL` returns cleaned SQL without error for `'a\\'` followed by injection payload, when it should detect the semicolon/injection outside the string
- C5: `SuccessCount` returns 10 when only 8 templates were freshly loaded

### Fix Checking

**Goal**: Verify that for all inputs where each bug condition holds, the fixed function produces the expected behavior.

**Pseudocode:**
```
FOR ALL input WHERE isBugCondition(input) DO
  result := fixedFunction(input)
  ASSERT expectedBehavior(result)
END FOR
```

### Preservation Checking

**Goal**: Verify that for all inputs where the bug conditions do NOT hold, the fixed functions produce the same results as the original functions.

**Pseudocode:**
```
FOR ALL input WHERE NOT isBugCondition(input) DO
  ASSERT originalFunction(input) = fixedFunction(input)
END FOR
```

**Testing Approach**: Property-based testing is recommended for preservation checking because:
- It generates many test cases automatically across the input domain
- It catches edge cases that manual unit tests might miss
- It provides strong guarantees that behavior is unchanged for all non-buggy inputs

**Test Plan**: Observe behavior on UNFIXED code first for non-bug inputs, then write property-based tests capturing that behavior.

**Test Cases**:
1. **Sanitizer Preservation**: For SQL strings without `\\` inside single quotes, verify `sanitizeSQL` produces identical output before and after fix
2. **Reload Preservation**: For Reload with no hash changes, verify no cache clearing occurs
3. **SuccessCount Preservation**: For Reload with zero failures, verify `SuccessCount == len(templates)`
4. **ListTemplates Preservation**: Verify all templates are still returned (just in sorted order)
5. **Render Preservation**: For renders that complete within timeout, verify identical results and no metric changes

### Unit Tests

- C1: Integration test verifying TemplateWatcher lifecycle in main.go flow
- C2: Test `sanitizeSQL` with `\\'` (escaped backslash + closing quote), `\'` (escaped quote), `''` (doubled quote), and mixed sequences
- C3: Test Reload cache clearing calls the correct datasource prefix per changed template
- C4/C7: Test render timeout increments gauge, and leaked goroutine completion decrements it
- C5: Test Reload SuccessCount with 0, partial, and full failures
- C6: Test GetAll returns sorted results; test ListTemplates pagination stability

### Property-Based Tests

- Generate random SQL strings with various escape sequences inside single quotes and verify sanitizer correctly tracks string boundaries
- Generate random template sets with random failure patterns and verify SuccessCount accuracy
- Generate random template registries and verify GetAll always returns sorted results
- Generate random render durations (some exceeding timeout) and verify goroutine leak gauge accuracy

### Integration Tests

- Full Reload flow: modify template file → watcher triggers Reload → only changed template cache cleared → SuccessCount accurate
- Full render flow: template render with timeout → goroutine leak tracked → leaked goroutine completes → gauge decremented
- Full ListTemplates flow: multiple paginated calls return consistent, sorted results
