// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package template implements the SQL template query engine for the mountainKing
// GraphQL API service. It loads, validates, renders, and executes parameterized
// SQL templates against a StarRocks data source via the RawExecutor interface.
package template

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/cache"
	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/sanitize"
)

// RawExecutor defines the raw SQL execution capability.
// TemplateEngine interacts with the StarRocks Adapter exclusively through
// this interface, achieving interface isolation — the template engine cannot
// access Execute, HealthCheck, or other DataSource methods.
type RawExecutor interface {
	// ExecuteRaw executes a raw SQL statement and returns the result.
	// It reuses the Adapter's *sql.DB connection pool and does not go
	// through SQLQueryBuilder or whitelist validation.
	//   query: the rendered SQL statement (already security-checked)
	//   args:  SQL parameters (for parameterised LIMIT/OFFSET values)
	ExecuteRaw(ctx context.Context, query string, args ...interface{}) (*datasource.QueryResult, error)
}

// TemplateQueryRequest represents a template query request from the resolver.
type TemplateQueryRequest struct {
	TemplateName string
	Parameters   map[string]interface{}
	Fields       []string
	First        *int
	Offset       *int
	OrderBy      []TemplateOrderByParam
	NeedCount    bool // set by the Resolver based on GraphQL field selection
	SkipCache    bool // true when extensions.cache=false
}

// TemplateQueryResult holds the result of a template query execution.
type TemplateQueryResult struct {
	Data       []map[string]interface{}
	TotalCount *int64 // -1 means count_enabled=false
	Warnings   []string
}

// TemplateOrderByParam represents a single sort criterion for template queries.
type TemplateOrderByParam struct {
	Field     string
	Direction string // "ASC" or "DESC"
}

// TemplateEngineConfig holds the dependencies required to create a TemplateEngine.
type TemplateEngineConfig struct {
	Config         config.SQLTemplatesConfig
	GraphQLCfg     config.GraphQLConfig
	DatasourceName string // StarRocks datasource name (from Adapter.Name(); used for metrics/audit/cache keys)
	Executor       RawExecutor
	CacheLayer     *cache.CacheLayer // nil = caching disabled
	Sanitizer      *sanitize.Sanitizer
	AuditLogger    *audit.AuditLogger
	Metrics        *TemplateMetrics // defined in metrics.go (Task 12.1)
	Tracer         trace.Tracer
	Logger         *zap.Logger
}

// ReloadResult summarises a template reload operation.
type ReloadResult struct {
	SuccessCount int
	Failures     []TemplateLoadFailure
	Duration     time.Duration
}

// TemplateLoadFailure describes a single template that failed to load.
type TemplateLoadFailure struct {
	Name  string
	Error string
}

// TemplateInfo exposes template metadata (used by the templateList query).
type TemplateInfo struct {
	Name         string
	Description  string
	CountEnabled bool
	Parameters   []ParamSchemaInfo
}

// ParamSchemaInfo exposes parameter schema metadata for a template.
type ParamSchemaInfo struct {
	Name         string
	Type         string
	Required     bool
	DefaultValue *string
}
