// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

//go:generate go run github.com/99designs/gqlgen generate

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

import (
	"context"
	"sync/atomic"

	"github.com/michaelwang123/mountainKing/internal/adapter/starrocks"
	"github.com/michaelwang123/mountainKing/internal/audit"
	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/observability"
	"github.com/michaelwang123/mountainKing/internal/ratelimit"
	"github.com/michaelwang123/mountainKing/internal/template"
	"go.uber.org/zap"
)

// CacheClearer is the interface needed by the mutation resolver for cache
// clearing. It allows clearing result cache entries by datasource prefix
// or clearing all result cache entries. The implementation must only clear
// result cache (cache: prefix) and not APQ cache (apq: prefix).
type CacheClearer interface {
	// ClearByDatasource clears cached results for the specified datasource.
	ClearByDatasource(ctx context.Context, datasource string) error
	// ClearAll clears all cached results.
	ClearAll(ctx context.Context) error
}

// Resolver is the root resolver struct that holds all dependencies
// required by the GraphQL resolvers.
type Resolver struct {
	// DSManager provides access to data source lifecycle and query execution.
	DSManager *datasource.DataSourceManager
	// GraphQLConfig holds GraphQL engine settings such as max_result_rows.
	GraphQLConfig config.GraphQLConfig
	// CacheClearer provides cache clearing capability for the clearCache
	// mutation. It can be nil when caching is disabled.
	CacheClearer CacheClearer
	// TemplateEngine provides SQL template query capability.
	// nil means the feature is disabled (sql_templates.enabled=false).
	TemplateEngine *template.TemplateEngine

	// --- Mutation fields ---

	// MutationSQLBuilder constructs parameterized SQL for write operations.
	MutationSQLBuilder *starrocks.MutationSQLBuilder
	// MutationValidator validates mutation inputs before SQL construction.
	MutationValidator *starrocks.MutationValidator
	// WritableTableValidator enforces table/column whitelist and operation restrictions.
	WritableTableValidator *starrocks.WritableTableValidator
	// MutationConfig holds the current mutation configuration, atomically swapped on hot-reload.
	MutationConfig *atomic.Pointer[config.MutationsConfig]
	// MutationRateLimiter is the mutation-specific rate limiter instance (resolver level).
	MutationRateLimiter ratelimit.RateLimiter
	// AuditLogger writes audit entries for mutation operations.
	AuditLogger *audit.AuditLogger
	// MetricsCollector registers and exposes mutation metrics.
	MetricsCollector *observability.MetricsCollector
	// Logger provides structured logging for non-critical warnings (e.g., cache clear failures).
	// May be nil if not wired; cache clear warnings will be silently dropped in that case.
	Logger *zap.Logger
}
