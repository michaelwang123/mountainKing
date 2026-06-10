// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"fmt"
	"time"

	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
)

// mutationExecPlan encapsulates the execution plan for a mutation operation.
type mutationExecPlan struct {
	operation string // "insert", "update", "delete", "insertBatch"
	table     string
	// cfg is the mutation config snapshot. If nil, executeMutation reads it atomically.
	// Passing cfg avoids a double atomic read when the caller already loaded it.
	cfg *config.MutationsConfig
	// validate performs input validation and whitelist checks.
	// Called after auth succeeds. Return error to reject with validation_failed audit.
	validate func() error
	// buildAndExecute builds SQL, validates length, and executes via the writable datasource.
	// Returns affected rows and the SQL statement length (for tracing).
	// The ctx passed contains the tracing span.
	buildAndExecute func(ctx context.Context, wds datasource.WritableDataSource) (affected int64, sqlLen int, err error)
}

// executeMutation orchestrates the full mutation lifecycle: config check, rate limit,
// auth, validation, execution with tracing, metrics, audit, and cache invalidation.
func (r *mutationResolver) executeMutation(ctx context.Context, plan mutationExecPlan) (*generated.MutationResult, error) {
	// 1. Atomic config read (use pre-loaded cfg if provided).
	cfg := plan.cfg
	if cfg == nil {
		cfg = r.MutationConfig.Load()
	}

	// 2. Feature gate.
	if !cfg.Enabled {
		return nil, &gqlerror.Error{
			Message:    "mutations feature is disabled",
			Extensions: map[string]any{"code": apierrors.ErrMutationFeatureDisabled},
		}
	}

	// 3. Rate limit.
	if err := r.checkMutationRateLimit(ctx); err != nil {
		return nil, err
	}

	// 4. Authorization.
	if err := r.checkMutationAuth(ctx, cfg.DatasourceName); err != nil {
		r.logMutationAudit(ctx, plan.operation, plan.table, cfg.DatasourceName, false, "authorization_denied", 0)
		return nil, err
	}

	// 5. Input validation + whitelist checks.
	if err := plan.validate(); err != nil {
		r.logMutationAudit(ctx, plan.operation, plan.table, cfg.DatasourceName, false, "validation_failed", 0)
		return nil, err
	}

	// 6. Get writable datasource OUTSIDE the tracing span (accurate metrics).
	wds, err := r.DSManager.GetWritable(cfg.DatasourceName)
	if err != nil {
		r.logMutationAudit(ctx, plan.operation, plan.table, cfg.DatasourceName, false, "execution_failed", 0)
		return nil, err
	}

	// 7. Execute with tracing + metrics.
	start := time.Now()
	var sqlLen int
	affected, execErr := r.traceMutation(ctx, plan.operation, plan.table, func(ctx context.Context) (int64, error) {
		a, sl, e := plan.buildAndExecute(ctx, wds)
		sqlLen = sl
		return a, e
	})

	// 8. Record metrics (always, even on error).
	r.recordMutationMetrics(plan.operation, cfg.DatasourceName, plan.table, start, execErr)

	if execErr != nil {
		r.logMutationAudit(ctx, plan.operation, plan.table, cfg.DatasourceName, false, "execution_failed", 0)
		return nil, execErr
	}

	// 9. Warning check.
	warning := r.checkAffectedRowsWarning(affected, cfg)

	// 10. Audit success (include SQL length for diagnostics).
	r.logMutationAudit(ctx, plan.operation, plan.table, cfg.DatasourceName, true, "", affected)

	// 11. Cache invalidation.
	r.invalidateCacheAfterMutation(ctx, plan.operation, cfg.DatasourceName, plan.table)

	// 12. Log SQL statement size for observability (useful for batch insert diagnostics).
	if r.Logger != nil && sqlLen > 0 {
		r.Logger.Debug("mutation executed",
			zap.String("operation", plan.operation),
			zap.String("table", plan.table),
			zap.Int("sql_bytes", sqlLen),
			zap.Int64("affected_rows", affected),
		)
	}

	// 13. Return result.
	return &generated.MutationResult{
		Success:      true,
		AffectedRows: int(affected),
		Warning:      warning,
	}, nil
}

// recordMutationMetrics records duration and counter metrics for a mutation operation.
func (r *mutationResolver) recordMutationMetrics(operation, ds, table string, start time.Time, err error) {
	if r.MetricsCollector == nil {
		return
	}
	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
	}
	r.MetricsCollector.MutationDuration.WithLabelValues(operation, ds, table, status).Observe(duration)
	r.MetricsCollector.MutationsTotal.WithLabelValues(operation, ds, table, status).Inc()
}

// checkAffectedRowsWarning returns a warning string if affected rows exceed the configured threshold.
func (r *mutationResolver) checkAffectedRowsWarning(affected int64, cfg *config.MutationsConfig) *string {
	if int(affected) > cfg.MaxAffectedRows {
		w := fmt.Sprintf("affected rows (%d) exceeded threshold (%d)", affected, cfg.MaxAffectedRows)
		return &w
	}
	return nil
}

// invalidateCacheAfterMutation clears the cache for the affected datasource after a successful mutation.
func (r *mutationResolver) invalidateCacheAfterMutation(ctx context.Context, operation, ds, table string) {
	if r.CacheClearer == nil {
		return
	}
	if cacheErr := r.CacheClearer.ClearByDatasource(ctx, ds); cacheErr != nil {
		if r.Logger != nil {
			r.Logger.Warn("cache invalidation failed after successful mutation",
				zap.String("operation", operation),
				zap.String("datasource", ds),
				zap.String("table", table),
				zap.Error(cacheErr),
			)
		}
	}
}

// convertMutationFilters converts a slice of GraphQL MutationFilterInput into datasource FilterConditions.
func convertMutationFilters(filters []*generated.MutationFilterInput) ([]datasource.FilterCondition, error) {
	result := make([]datasource.FilterCondition, 0, len(filters))
	for _, f := range filters {
		fc, err := convertMutationFilter(f)
		if err != nil {
			return nil, &gqlerror.Error{
				Message:    fmt.Sprintf("invalid filter: %s", err.Error()),
				Extensions: map[string]any{"code": apierrors.ErrValidationInvalidField},
			}
		}
		result = append(result, fc)
	}
	return result, nil
}
