// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"text/template"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/cache"
	"github.com/michaelwang123/mountainKing/internal/config"
	appctx "github.com/michaelwang123/mountainKing/internal/context"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/internal/sanitize"
)

// TemplateEngine is the core component of the SQL template query engine.
// It orchestrates template loading, parameter validation, rendering, security
// checks, pagination wrapping, cache integration, and concurrency control.
type TemplateEngine struct {
	registry         *TemplateRegistry
	config           config.SQLTemplatesConfig
	graphqlCfg       config.GraphQLConfig
	datasourceName   string
	executor         RawExecutor
	cacheLayer       *cache.CacheLayer
	sanitizer        *sanitize.Sanitizer
	auditLogger      *audit.AuditLogger
	metrics          *TemplateMetrics
	tracer           trace.Tracer
	logger           *zap.Logger
	semaphore        chan struct{}
	reloadMu         sync.Mutex
	lastReloadAt     time.Time
	lastReloadResult *ReloadResult
	funcMap          template.FuncMap
}

// NewTemplateEngine creates and initialises a TemplateEngine.
//  1. Builds the custom FuncMap (safeString, quote, safeInt, etc.)
//  2. Initialises the semaphore (capacity = max_concurrent_queries)
//  3. Calls loadAll() to load all templates from disk
//  4. Initialises the registry with loaded templates
func NewTemplateEngine(cfg TemplateEngineConfig) (*TemplateEngine, error) {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	funcMap := buildFuncMap()

	concurrency := cfg.Config.MaxConcurrentQueries
	if concurrency <= 0 {
		concurrency = 10
	}

	te := &TemplateEngine{
		config:         cfg.Config,
		graphqlCfg:     cfg.GraphQLCfg,
		datasourceName: cfg.DatasourceName,
		executor:       cfg.Executor,
		cacheLayer:     cfg.CacheLayer,
		sanitizer:      cfg.Sanitizer,
		auditLogger:    cfg.AuditLogger,
		metrics:        cfg.Metrics,
		tracer:         cfg.Tracer,
		logger:         cfg.Logger,
		semaphore:      make(chan struct{}, concurrency),
		funcMap:        funcMap,
	}

	// Load all templates from disk.
	lr, err := loadAll(cfg.Config, funcMap, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("template engine loadAll: %w", err)
	}

	// Initialise registry with loaded templates.
	reg := NewTemplateRegistry()
	reg.Update(lr.Registered, lr.Hashes)
	te.registry = reg

	return te, nil
}

// Execute runs the full template query flow:
//
//	lookup → validate params → render → paginate → semaphore → cache/execute → count → result
func (te *TemplateEngine) Execute(ctx context.Context, req *TemplateQueryRequest) (*TemplateQueryResult, error) {
	start := time.Now()
	dsName := te.datasourceName

	// 0. Audit log (defer ensures both success and failure are recorded).
	var executeErr error
	defer func() {
		if te.auditLogger != nil {
			te.auditLogger.Log(audit.LogEntry{
				Principal:  extractPrincipal(ctx),
				Time:       time.Now(),
				Operation:  "query",
				Datasource: dsName,
				Success:    executeErr == nil,
				ExtraFields: map[string]string{
					"template_name": req.TemplateName,
				},
			})
		}
	}()

	// 1. Lookup template in registry.
	tmpl, ok := te.registry.Get(req.TemplateName)
	if !ok {
		te.recordQueryError(req.TemplateName)
		executeErr = apierrors.ValidationError(
			apierrors.ErrValidationTemplateNotFound,
			fmt.Sprintf("template %q not found", req.TemplateName),
		)
		return nil, executeErr
	}

	// 2. Validate parameters.
	validatedParams, err := validateParams(req.Parameters, tmpl.ParamSchemas)
	if err != nil {
		te.recordQueryError(req.TemplateName)
		executeErr = err
		return nil, executeErr
	}

	// 3. Create tracing span.
	var span trace.Span
	if te.tracer != nil {
		ctx, span = te.tracer.Start(ctx, "Template Query "+req.TemplateName,
			trace.WithAttributes(
				attribute.String("template.name", req.TemplateName),
				attribute.String("db.system", "starrocks"),
			),
		)
		defer span.End()
	}

	// 4. Render template (with render_timeout).
	renderStart := time.Now()
	renderedSQL, err := te.render(ctx, tmpl, validatedParams, te.config.RenderTimeout, te.config.MaxRenderedSQLLen)
	renderDuration := time.Since(renderStart)
	te.observeRenderDuration(req.TemplateName, renderDuration)
	if err != nil {
		if span != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		te.recordQueryError(req.TemplateName)
		executeErr = err
		return nil, executeErr
	}

	// 5. Set rendered SQL on span (sanitised).
	if span != nil && te.sanitizer != nil {
		span.SetAttributes(attribute.String("db.statement", te.sanitizer.Sanitize(renderedSQL)))
	}

	// 6. Log rendered SQL (sanitised, debug level).
	if te.sanitizer != nil {
		te.logger.Debug("rendered SQL",
			zap.String("template_name", req.TemplateName),
			zap.String("sql", te.sanitizer.Sanitize(renderedSQL)),
		)
	}

	// 7. Build pagination wrapper SQL.
	wrappedSQL, args, err := wrapWithPagination(
		renderedSQL, req.Fields, req.OrderBy, req.First, req.Offset, te.graphqlCfg.MaxResultRows,
	)
	if err != nil {
		te.recordQueryError(req.TemplateName)
		executeErr = err
		return nil, executeErr
	}

	// 8. Acquire semaphore (with context timeout awareness).
	semWaitStart := time.Now()
	select {
	case te.semaphore <- struct{}{}:
		defer func() { <-te.semaphore }()
	case <-ctx.Done():
		te.recordQueryError(req.TemplateName)
		executeErr = apierrors.DatasourceError(
			apierrors.ErrDatasourceTimeout,
			"template query timed out waiting for semaphore",
		)
		return nil, executeErr
	}
	te.observeSemaphoreWait(req.TemplateName, time.Since(semWaitStart))

	// 9. Execute query (with cache if applicable).
	var data []map[string]any
	if shouldCache(tmpl, req.SkipCache) {
		cacheKey := generateCacheKey(req.TemplateName, validatedParams, req.Fields, req.First, req.Offset, req.OrderBy)
		var loaderCalled bool
		data, loaderCalled, err = executeWithCache(ctx, te.cacheLayer, te.datasourceNameForCache(tmpl), cacheKey, func() ([]map[string]any, error) {
			result, execErr := te.executor.ExecuteRaw(ctx, wrappedSQL, args...)
			if execErr != nil {
				return nil, execErr
			}
			return result.Data, nil
		})
		te.recordCacheResult(req.TemplateName, loaderCalled)
	} else {
		result, execErr := te.executor.ExecuteRaw(ctx, wrappedSQL, args...)
		if execErr != nil {
			err = execErr
		} else {
			data = result.Data
		}
	}
	if err != nil {
		if span != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		te.recordQueryError(req.TemplateName)
		executeErr = err
		return nil, executeErr
	}

	// 10. Execute totalCount query (if needed).
	var totalCount *int64
	var warnings []string
	if req.NeedCount {
		if tmpl.CountEnabled {
			countSQL := wrapWithCount(renderedSQL)
			tc, tcErr := te.executeCountQuery(ctx, countSQL, tmpl, req.TemplateName, validatedParams, req.SkipCache)
			if tcErr != nil {
				if span != nil {
					span.SetStatus(codes.Error, tcErr.Error())
				}
				te.recordQueryError(req.TemplateName)
				executeErr = tcErr
				return nil, executeErr
			}
			totalCount = &tc
		} else {
			minusOne := int64(-1)
			totalCount = &minusOne
			warnings = append(warnings, fmt.Sprintf(
				"totalCount disabled for template %q, returning -1", req.TemplateName))
		}
	}

	// 11. Record metrics and log.
	queryDuration := time.Since(start)
	te.observeQueryDuration(req.TemplateName, dsName, queryDuration)
	te.recordQuerySuccess(req.TemplateName)

	te.logger.Info("template query executed",
		zap.String("template_name", req.TemplateName),
		zap.Duration("render_duration", renderDuration),
		zap.Duration("query_duration", queryDuration),
		zap.Int("result_rows", len(data)),
	)

	return &TemplateQueryResult{
		Data:       data,
		TotalCount: totalCount,
		Warnings:   warnings,
	}, nil
}

// ListTemplates returns metadata for all registered templates with optional pagination.
func (te *TemplateEngine) ListTemplates(first *int, offset *int) []*TemplateInfo {
	all := te.registry.GetAll()

	// Convert to TemplateInfo slice.
	infos := make([]*TemplateInfo, 0, len(all))
	for _, t := range all {
		params := make([]ParamSchemaInfo, 0, len(t.ParamSchemas))
		for _, p := range t.ParamSchemas {
			info := ParamSchemaInfo{
				Name:     p.Name,
				Type:     p.Type,
				Required: p.Required,
			}
			if p.Default != nil {
				s := fmt.Sprintf("%v", p.Default)
				info.DefaultValue = &s
			}
			params = append(params, info)
		}
		infos = append(infos, &TemplateInfo{
			Name:         t.Name,
			Description:  t.Description,
			CountEnabled: t.CountEnabled,
			Parameters:   params,
		})
	}

	// Apply pagination (offset then first).
	if offset != nil && *offset > 0 {
		if *offset >= len(infos) {
			return []*TemplateInfo{}
		}
		infos = infos[*offset:]
	}
	if first != nil && *first >= 0 && *first < len(infos) {
		infos = infos[:*first]
	}

	return infos
}

// DatasourceName returns the associated StarRocks datasource name.
func (te *TemplateEngine) DatasourceName() string {
	return te.datasourceName
}

// Close releases resources. Currently a no-op (watcher is separate).
func (te *TemplateEngine) Close() error {
	return nil
}

// Reload reloads all template files from disk. Used by both the
// reloadTemplates Mutation and fsnotify watcher.
//
// Error isolation: failed templates retain their old version from the
// previous registry. Only templates whose SHA-256 hash changed have
// their cache entries cleared.
func (te *TemplateEngine) Reload(ctx context.Context, fromMutation bool) (*ReloadResult, error) {
	te.reloadMu.Lock()
	defer te.reloadMu.Unlock()

	// Mutation-triggered reload has a 10s cooldown: if less than 10s since
	// last successful reload, return the cached result immediately.
	// fsnotify-triggered reload (fromMutation=false) always proceeds.
	if fromMutation {
		if time.Since(te.lastReloadAt) < 10*time.Second && te.lastReloadResult != nil {
			return te.lastReloadResult, nil
		}
	}

	startTime := time.Now()

	// Load all templates from disk.
	lr, err := loadAll(te.config, te.funcMap, te.logger)
	if err != nil {
		return nil, fmt.Errorf("reload loadAll: %w", err)
	}

	// Save the count of freshly loaded templates BEFORE error isolation merge.
	freshSuccessCount := len(lr.Registered)

	// Error isolation: for templates that failed to load, copy old version.
	oldAll := te.registry.GetAll()
	oldMap := make(map[string]*RegisteredTemplate, len(oldAll))
	for _, t := range oldAll {
		oldMap[t.Name] = t
	}

	for _, f := range lr.Failures {
		if old, ok := oldMap[f.Name]; ok {
			lr.Registered[f.Name] = old
			if h, hOk := te.registry.GetHash(f.Name); hOk {
				lr.Hashes[f.Name] = h
			}
			te.logger.Warn("template reload failed, retaining old version",
				zap.String("name", f.Name),
				zap.String("error", f.Error),
			)
		}
	}

	// Determine which templates changed (hash comparison).
	if te.cacheLayer != nil {
		for name, newHash := range lr.Hashes {
			oldHash, exists := te.registry.GetHash(name)
			if !exists || oldHash != newHash {
				_ = te.cacheLayer.ClearByDatasource(ctx, "template:"+name)
				te.logger.Info("clearing cache for changed template",
					zap.String("name", name),
				)
			}
		}
	}

	// Atomically replace registry.
	te.registry.Update(lr.Registered, lr.Hashes)

	duration := time.Since(startTime)
	result := &ReloadResult{
		SuccessCount: freshSuccessCount,
		Failures:     lr.Failures,
		Duration:     duration,
	}

	te.lastReloadAt = time.Now()
	te.lastReloadResult = result

	te.logger.Info("template reload complete",
		zap.Int("success_count", result.SuccessCount),
		zap.Int("failure_count", len(result.Failures)),
		zap.Duration("duration", duration),
	)

	return result, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// executeCountQuery executes a COUNT(*) query with optional cache integration.
func (te *TemplateEngine) executeCountQuery(
	ctx context.Context,
	countSQL string,
	tmpl *RegisteredTemplate,
	templateName string,
	validatedParams map[string]any,
	skipCache bool,
) (int64, error) {
	loader := func() (int64, error) {
		result, err := te.executor.ExecuteRaw(ctx, countSQL)
		if err != nil {
			return 0, err
		}
		return extractCount(result.Data), nil
	}

	if shouldCache(tmpl, skipCache) && te.cacheLayer != nil {
		countKey := generateCountCacheKey(templateName, validatedParams)
		return executeCount(ctx, te.cacheLayer, te.datasourceNameForCache(tmpl), countKey, loader)
	}

	return loader()
}

// extractCount extracts the count value from a COUNT(*) query result.
func extractCount(data []map[string]any) int64 {
	if len(data) == 0 {
		return 0
	}
	row := data[0]
	// The count column is aliased as "total_count" by wrapWithCount.
	if v, ok := row["total_count"]; ok {
		return toInt64(v)
	}
	// Fallback: try the first column value.
	for _, v := range row {
		return toInt64(v)
	}
	return 0
}

// toInt64 converts various numeric types to int64.
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	case string:
		// Try parsing as int.
		var n int64
		fmt.Sscanf(val, "%d", &n)
		return n
	default:
		return 0
	}
}

// datasourceNameForCache returns the datasource name used for cache TTL lookup.
// If the template has a custom cache_ttl, use "template:{name}" to match the
// pre-registered TTL in CacheLayerConfig.TTLConfig.
func (te *TemplateEngine) datasourceNameForCache(tmpl *RegisteredTemplate) string {
	if tmpl.CacheTTL != nil {
		return "template:" + tmpl.Name
	}
	return te.datasourceName
}

// extractPrincipal extracts the authenticated principal from the request context.
func extractPrincipal(ctx context.Context) string {
	if identity, ok := ctx.Value(appctx.CtxKeyAuthIdentity).(*middleware.AuthIdentity); ok && identity != nil {
		return identity.Subject
	}
	return "anonymous"
}

// ---------------------------------------------------------------------------
// Metrics helpers (nil-safe)
// ---------------------------------------------------------------------------

func (te *TemplateEngine) recordQueryError(templateName string) {
	if te.metrics != nil && te.metrics.QueriesTotal != nil {
		te.metrics.QueriesTotal.WithLabelValues(templateName, "error").Inc()
	}
}

func (te *TemplateEngine) recordQuerySuccess(templateName string) {
	if te.metrics != nil && te.metrics.QueriesTotal != nil {
		te.metrics.QueriesTotal.WithLabelValues(templateName, "success").Inc()
	}
}

func (te *TemplateEngine) observeRenderDuration(templateName string, d time.Duration) {
	if te.metrics != nil && te.metrics.RenderDuration != nil {
		te.metrics.RenderDuration.WithLabelValues(templateName).Observe(d.Seconds())
	}
}

func (te *TemplateEngine) observeQueryDuration(templateName, datasource string, d time.Duration) {
	if te.metrics != nil && te.metrics.QueryDuration != nil {
		te.metrics.QueryDuration.WithLabelValues(templateName, datasource).Observe(d.Seconds())
	}
}

func (te *TemplateEngine) observeSemaphoreWait(templateName string, d time.Duration) {
	if te.metrics != nil && te.metrics.SemaphoreWait != nil {
		te.metrics.SemaphoreWait.WithLabelValues(templateName).Observe(d.Seconds())
	}
}

func (te *TemplateEngine) recordCacheResult(templateName string, loaderCalled bool) {
	if te.metrics != nil && te.metrics.CacheHitsTotal != nil {
		if loaderCalled {
			te.metrics.CacheHitsTotal.WithLabelValues(templateName, "miss").Inc()
		} else {
			te.metrics.CacheHitsTotal.WithLabelValues(templateName, "hit").Inc()
		}
	}
}
