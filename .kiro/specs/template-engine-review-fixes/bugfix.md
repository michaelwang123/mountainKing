# Bugfix Requirements Document

## Introduction

SQL 模板查询引擎代码审查发现 7 个实现缺陷，涵盖 1 个 Critical、3 个 High、3 个 Medium 级别问题。这些问题包括：服务集成缺失（TemplateWatcher 未创建）、SQL sanitizer 安全漏洞（反斜杠序列处理错误）、缓存清除逻辑错误（清除全部缓存而非变更模板缓存）、监控指标未使用（RenderGoroutineLeaks）、Reload 成功计数不准确、ListTemplates 返回顺序不确定、以及渲染超时 goroutine 泄漏无追踪。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN `sql_templates.enabled` is true and the TemplateEngine is created in `cmd/server/main.go` THEN the system never creates a `TemplateWatcher` via `NewTemplateWatcher()` and never calls `Start()`, so fsnotify file watching (requirement 10.6) is not integrated into the service, and `TemplateWatcher.Stop()` is missing from the graceful shutdown sequence

1.2 WHEN the SQL sanitizer (`sanitizer.go` `stateInSingleQuote`) encounters the input `\\'` (two backslashes followed by a single quote) THEN the system incorrectly treats the `\'` at position 1 as an escaped quote (staying in string state), because `\\` (escaped backslash) is not handled before `\'` in the scanner, causing the scanner to believe the string is still open when it should be closed

1.3 WHEN `engine.go` `Reload` detects a template with a changed hash and attempts to clear its cache THEN the system calls `te.cacheLayer.ClearByDatasource(ctx, "")` with an empty string, which clears ALL cache entries (including StarRocks single-table queries and Prometheus queries) instead of only the changed template's cache entries

1.4 WHEN a template render times out in `renderer.go` (the `timeoutCtx.Done()` branch is taken) THEN the system never increments the `RenderGoroutineLeaks` Gauge metric defined in `metrics.go`, making it impossible to monitor goroutine leaks via Prometheus

1.5 WHEN `engine.go` `Reload` calculates `result.SuccessCount = len(lr.Registered)` THEN the system includes error-isolated templates (old versions retained from the previous registry merged into `lr.Registered`) in the count, causing `reloadTemplates` Mutation to report a misleadingly high success count

1.6 WHEN `engine.go` `ListTemplates` calls `te.registry.GetAll()` to retrieve all registered templates THEN the system returns templates in non-deterministic order because `GetAll()` iterates over a Go `map[string]*RegisteredTemplate`, making pagination results unstable across calls

1.7 WHEN a template render times out in `renderer.go` and the leaked goroutine (running `template.Execute()`) eventually completes THEN the system has no mechanism to track or decrement the goroutine leak count because the `render` function is a standalone package-level function with no access to `TemplateMetrics`

### Expected Behavior (Correct)

2.1 WHEN `sql_templates.enabled` is true and the TemplateEngine is created in `cmd/server/main.go` THEN the system SHALL create a `TemplateWatcher` via `NewTemplateWatcher(templateEngine, watchDirs, logger)`, call `Start()` to begin fsnotify file watching, and call `TemplateWatcher.Stop()` during the graceful shutdown sequence before closing the TemplateEngine

2.2 WHEN the SQL sanitizer (`sanitizer.go` `stateInSingleQuote`) encounters a backslash character THEN the system SHALL first check for `\\` (escaped backslash — write both backslashes and stay in string state) before checking for `\'` (escaped quote), so that input `\\'` is correctly parsed as: `\\` = literal backslash, `'` = closing quote returning to NORMAL state

2.3 WHEN `engine.go` `Reload` detects a template with a changed hash and needs to clear its cache THEN the system SHALL call `te.cacheLayer.ClearByDatasource(ctx, "template:"+name)` to clear only the changed template's cache entries, leaving other datasource caches (StarRocks single-table, Prometheus) intact

2.4 WHEN a template render times out in `renderer.go` THEN the system SHALL increment the `RenderGoroutineLeaks` Gauge metric to track the leaked goroutine, enabling Prometheus-based monitoring of goroutine leaks as specified in design doc D7

2.5 WHEN `engine.go` `Reload` calculates `SuccessCount` THEN the system SHALL count only templates that were freshly loaded successfully from disk (excluding error-isolated templates retained from the previous registry), so that `reloadTemplates` Mutation returns an accurate success count

2.6 WHEN `engine.go` `ListTemplates` returns template metadata THEN the system SHALL sort the templates by name in ascending lexicographic order before applying pagination, ensuring stable and deterministic pagination results

2.7 WHEN a template render times out in `renderer.go` THEN the system SHALL have access to `TemplateMetrics` (either by making `render` a method on `TemplateEngine` or by passing metrics as a parameter), SHALL increment `RenderGoroutineLeaks` on timeout, and SHALL decrement it when the leaked goroutine eventually completes

### Unchanged Behavior (Regression Prevention)

3.1 WHEN `sql_templates.enabled` is false THEN the system SHALL CONTINUE TO skip TemplateEngine initialization entirely, and `templateQuery` SHALL CONTINUE TO return `VALIDATION_TEMPLATE_NOT_FOUND` for any template name

3.2 WHEN the SQL sanitizer encounters standard single-quoted strings with `''` (doubled quote) escaping or simple `\'` (backslash-escaped quote without preceding backslash) THEN the system SHALL CONTINUE TO correctly track string boundaries and detect multi-statement injection

3.3 WHEN no templates have changed hash during Reload THEN the system SHALL CONTINUE TO skip cache clearing entirely, preserving all existing cache entries

3.4 WHEN a template renders successfully within the timeout THEN the system SHALL CONTINUE TO return the rendered SQL without affecting the `RenderGoroutineLeaks` metric

3.5 WHEN all templates load successfully during Reload (no failures) THEN the system SHALL CONTINUE TO report `SuccessCount` equal to the total number of registered templates

3.6 WHEN `ListTemplates` is called without pagination parameters THEN the system SHALL CONTINUE TO return all registered templates (now in sorted order)

3.7 WHEN a template renders successfully within the timeout THEN the system SHALL CONTINUE TO return the result through the channel without any goroutine leak tracking overhead
