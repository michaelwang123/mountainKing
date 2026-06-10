// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/michaelwang123/mountainKing/internal/audit"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/middleware"
)

// Sentinel errors for filter conversion — pre-allocated to avoid heap allocation per request.
var (
	errNilFilterInput     = errors.New("nil filter input")
	errINRequiresNonNull  = errors.New("IN/NOT_IN filter requires a non-null array value")
	errINRequiresNonEmpty = errors.New("IN/NOT_IN filter requires a non-empty array value")
)

// convertMutationFilter converts a GraphQL MutationFilterInput into a
// datasource.FilterCondition. For IN/NOT_IN operators, the value is
// type-asserted directly to []any. For IS_NULL/IS_NOT_NULL operators,
// the value is set to nil. For all other operators, the value is passed
// through directly (AnyValue scalar provides the correct Go type).
func convertMutationFilter(input *generated.MutationFilterInput) (datasource.FilterCondition, error) {
	if input == nil {
		return datasource.FilterCondition{}, errNilFilterInput
	}

	op := convertFilterOperator(input.Operator)

	var value any

	switch input.Operator {
	case generated.FilterOperatorIsNull, generated.FilterOperatorIsNotNull:
		// IS_NULL/IS_NOT_NULL do not use a value.
		value = nil

	case generated.FilterOperatorIn, generated.FilterOperatorNotIn:
		// IN/NOT_IN expect the value to be a JSON array → []any.
		// Note: if UseNumber() is configured in the JSON decoder, array elements may be
		// json.Number instead of float64. The AnyValue scalar accepts this, and the SQL
		// builder/driver should handle json.Number via its Stringer interface.
		if input.Value == nil {
			return datasource.FilterCondition{}, errINRequiresNonNull
		}
		arr, ok := (input.Value).([]any)
		if !ok {
			return datasource.FilterCondition{}, fmt.Errorf("IN/NOT_IN filter value must be a JSON array, got %T", input.Value)
		}
		if len(arr) == 0 {
			return datasource.FilterCondition{}, errINRequiresNonEmpty
		}
		value = arr

	default:
		// For all other operators, pass the value through directly.
		if input.Value == nil {
			return datasource.FilterCondition{}, fmt.Errorf("filter operator %s requires a non-null value", input.Operator)
		}
		value = input.Value
	}

	return datasource.FilterCondition{
		Field:    input.Field,
		Operator: op,
		Value:    value,
	}, nil
}

// checkMutationRateLimit checks the mutation-specific rate limit for the
// authenticated principal. It extracts the auth identity from context,
// constructs the rate limit key as "mutation:{identity}", and calls the
// MutationRateLimiter. On limiter error, it fails open (allows through).
func (r *mutationResolver) checkMutationRateLimit(ctx context.Context) error {
	if r.MutationRateLimiter == nil {
		return nil
	}

	identity := extractAuthIdentity(ctx)
	key := "mutation:" + extractRateLimitKeyFromIdentity(identity)

	result, err := r.MutationRateLimiter.Allow(ctx, key, 1)
	if err != nil {
		// Fail open: on limiter error, allow the request through.
		return nil
	}
	if !result.Allowed {
		return &gqlerror.Error{
			Message: "mutation rate limit exceeded",
			Extensions: map[string]any{
				"code": apierrors.ErrMutationRateLimitExceeded,
			},
		}
	}
	return nil
}

// checkMutationAuth verifies that the authenticated principal has the
// "mutation" operation permission and access to the target datasource.
// It extracts the auth identity from context and checks:
// 1. "mutation" is present in identity.Operations
// 2. targetDatasource is in identity.Datasources (or Datasources is nil/empty = unrestricted)
func (r *mutationResolver) checkMutationAuth(ctx context.Context, targetDatasource string) error {
	identity := extractAuthIdentity(ctx)
	if identity == nil {
		return &gqlerror.Error{
			Message: "authentication required for mutation operations",
			Extensions: map[string]any{
				"code": apierrors.ErrAuthMissing,
			},
		}
	}

	// Check "mutation" is in identity.Operations.
	if !slices.Contains(identity.Operations, "mutation") {
		return &gqlerror.Error{
			Message: "insufficient permissions: mutation operation not allowed",
			Extensions: map[string]any{
				"code": apierrors.ErrAuthInsufficientPermission,
			},
		}
	}

	// Check datasource access. Nil/empty Datasources means unrestricted.
	if len(identity.Datasources) > 0 {
		if !slices.Contains(identity.Datasources, targetDatasource) {
			return &gqlerror.Error{
				Message: fmt.Sprintf("insufficient permissions: no access to datasource %q", targetDatasource),
				Extensions: map[string]any{
					"code": apierrors.ErrAuthInsufficientPermission,
				},
			}
		}
	}

	return nil
}

// extractAuthIdentity retrieves the AuthIdentity from the request context.
// Returns nil if no identity is present.
func extractAuthIdentity(ctx context.Context) *middleware.AuthIdentity {
	identity, _ := ctx.Value(ctxkeys.CtxKeyAuthIdentity).(*middleware.AuthIdentity)
	return identity
}

// extractRateLimitKeyFromIdentity constructs a rate limit key from the auth identity.
// Priority: API Key ID > JWT sub > "anonymous".
func extractRateLimitKeyFromIdentity(identity *middleware.AuthIdentity) string {
	if identity == nil {
		return "anonymous"
	}
	if identity.Subject != "" {
		return identity.Subject
	}
	return "anonymous"
}

// logMutationAudit logs an audit entry for a mutation operation.
// It extracts the principal from context and records the operation details.
// If the AuditLogger is nil, this is a no-op.
//
// For success cases, pass reason="" and affectedRows with the actual count.
// For failure cases, pass the reason string (e.g., "authorization_denied", "validation_failed",
// "execution_failed") and affectedRows=0.
func (r *mutationResolver) logMutationAudit(ctx context.Context, operation, table, ds string, success bool, reason string, affectedRows int64) {
	if r.AuditLogger == nil {
		return
	}

	identity := extractAuthIdentity(ctx)
	principal := ""
	if identity != nil {
		principal = identity.Subject
	}

	extraFields := make(map[string]string, 3)
	extraFields["table"] = table
	if success {
		extraFields["affected_rows"] = fmt.Sprintf("%d", affectedRows)
	} else if reason != "" {
		extraFields["reason"] = reason
	}

	r.AuditLogger.Log(audit.LogEntry{
		Principal:   principal,
		Time:        time.Now(),
		Operation:   operation,
		Datasource:  ds,
		Success:     success,
		ExtraFields: extraFields,
	})
}
