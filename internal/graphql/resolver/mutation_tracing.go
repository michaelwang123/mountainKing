// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// traceMutation wraps a mutation execution in an OpenTelemetry span.
// It creates a span named "mutation.{operation}" with standard database attributes,
// records errors on the span, and sets the affected_rows attribute on success.
//
// The fn function receives the child context (with span) and returns affected rows
// and an optional error.
func (r *mutationResolver) traceMutation(ctx context.Context, operation, table string, fn func(ctx context.Context) (int64, error)) (int64, error) {
	tracer := otel.Tracer("mountainKing")
	ctx, span := tracer.Start(ctx, "mutation."+operation)
	defer span.End()

	span.SetAttributes(
		attribute.String("db.system", "starrocks"),
		attribute.String("db.operation", operation),
		attribute.String("db.table", table),
	)

	affected, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetAttributes(attribute.Int64("db.affected_rows", affected))
		span.SetStatus(codes.Ok, "")
	}

	return affected, err
}
