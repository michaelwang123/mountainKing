// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"fmt"
	"strings"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// wrapWithPagination wraps the rendered SQL with pagination, field selection,
// and ORDER BY. LIMIT and OFFSET are parameterised via ? placeholders.
//
// Over-fetch strategy: actual LIMIT = first+1 (or maxResultRows+1 when first
// is nil) so the Resolver can determine hasNextPage accurately.
func wrapWithPagination(
	renderedSQL string,
	fields []string,
	orderBy []TemplateOrderByParam,
	first *int,
	offset *int,
	maxResultRows int,
) (string, []interface{}, error) {

	// Determine LIMIT (over-fetch by 1).
	limit := maxResultRows + 1
	if first != nil {
		limit = *first + 1
	}

	// Determine OFFSET.
	off := 0
	if offset != nil {
		off = *offset
	}

	// Build field selection.
	fieldSelection := "*"
	if len(fields) > 0 {
		safeParts := make([]string, 0, len(fields))
		for _, f := range fields {
			safe, err := safeIdentifier(f)
			if err != nil {
				return "", nil, apierrors.ValidationError(
					apierrors.ErrValidationInvalidField,
					fmt.Sprintf("invalid field name %q: %v", f, err),
				)
			}
			safeParts = append(safeParts, safe)
		}
		fieldSelection = strings.Join(safeParts, ", ")
	}

	// Build ORDER BY clause.
	var orderByClause string
	if len(orderBy) > 0 {
		obParts := make([]string, 0, len(orderBy))
		for _, ob := range orderBy {
			safe, err := safeIdentifier(ob.Field)
			if err != nil {
				return "", nil, apierrors.ValidationError(
					apierrors.ErrValidationInvalidField,
					fmt.Sprintf("invalid orderBy field %q: %v", ob.Field, err),
				)
			}
			dir := "ASC"
			if strings.EqualFold(ob.Direction, "DESC") {
				dir = "DESC"
			}
			obParts = append(obParts, safe+" "+dir)
		}
		orderByClause = " ORDER BY " + strings.Join(obParts, ", ")
	}

	sql := fmt.Sprintf(
		"SELECT %s FROM (%s) AS __tq_wrapper__%s LIMIT ? OFFSET ?",
		fieldSelection, renderedSQL, orderByClause,
	)

	args := []interface{}{limit, off}
	return sql, args, nil
}

// wrapWithCount wraps the rendered SQL with a COUNT(*) query.
func wrapWithCount(renderedSQL string) string {
	return fmt.Sprintf("SELECT COUNT(*) AS total_count FROM (%s) AS __tq_cnt__", renderedSQL)
}
