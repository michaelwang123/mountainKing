// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package starrocks implements the StarRocks data source adapter, which connects
// to StarRocks via the MySQL protocol and translates GraphQL query parameters
// into parameterized SQL statements.
package starrocks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// identifierRe matches valid SQL identifiers: letters, digits, and underscores.
var identifierRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateIdentifier checks that name contains only allowed characters [a-zA-Z0-9_].
func ValidateIdentifier(name string) error {
	if name == "" {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField, "identifier must not be empty")
	}
	if !identifierRe.MatchString(name) {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			fmt.Sprintf("identifier %q contains invalid characters", name))
	}
	return nil
}

// SQLQueryBuilder converts GraphQL query parameters to parameterized SQL.
type SQLQueryBuilder struct {
	allowedTables map[string]map[string]bool // table → allowed columns
}

// NewSQLQueryBuilder creates a new SQLQueryBuilder with the given whitelist.
func NewSQLQueryBuilder(allowedTables map[string]map[string]bool) *SQLQueryBuilder {
	return &SQLQueryBuilder{allowedTables: allowedTables}
}

// Build constructs a SELECT query with parameterized values.
// It validates table/field names against the whitelist, wraps identifiers in
// backticks, and uses ? placeholders for filter values.
func (b *SQLQueryBuilder) Build(req datasource.QueryRequest, table string) (string, []any, error) {
	if err := b.validateTable(table); err != nil {
		return "", nil, err
	}

	allowedCols := b.allowedTables[table]

	// Build SELECT clause.
	selectClause, err := b.buildSelectClause(req.Fields, allowedCols)
	if err != nil {
		return "", nil, err
	}

	// Build WHERE clause.
	whereClause, params, err := b.buildWhereClause(req.Filters, allowedCols)
	if err != nil {
		return "", nil, err
	}

	// Build ORDER BY clause.
	orderByClause, err := b.buildOrderByClause(req.OrderBy, allowedCols)
	if err != nil {
		return "", nil, err
	}

	// Build LIMIT/OFFSET clause.
	limitClause, limitParams := b.buildLimitClause(req.Pagination)
	params = append(params, limitParams...)

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectClause)
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdentifier(table))

	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	}
	if orderByClause != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderByClause)
	}
	if limitClause != "" {
		sb.WriteString(" ")
		sb.WriteString(limitClause)
	}

	return sb.String(), params, nil
}

// BuildCount constructs a COUNT(*) query with the same filters as Build.
func (b *SQLQueryBuilder) BuildCount(req datasource.QueryRequest, table string) (string, []any, error) {
	if err := b.validateTable(table); err != nil {
		return "", nil, err
	}

	allowedCols := b.allowedTables[table]

	whereClause, params, err := b.buildWhereClause(req.Filters, allowedCols)
	if err != nil {
		return "", nil, err
	}

	var sb strings.Builder
	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(quoteIdentifier(table))

	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	}

	return sb.String(), params, nil
}

// validateTable checks that the table name is in the whitelist and is a valid identifier.
func (b *SQLQueryBuilder) validateTable(table string) error {
	if err := ValidateIdentifier(table); err != nil {
		return err
	}
	if _, ok := b.allowedTables[table]; !ok {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("table %q is not in the allowed whitelist", table))
	}
	return nil
}

// buildSelectClause returns the column list for SELECT. If fields is empty, returns "*".
func (b *SQLQueryBuilder) buildSelectClause(fields []string, allowedCols map[string]bool) (string, error) {
	if len(fields) == 0 {
		return "*", nil
	}
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if err := ValidateIdentifier(f); err != nil {
			return "", err
		}
		if !allowedCols[f] {
			return "", apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("field %q is not in the allowed columns", f))
		}
		parts = append(parts, quoteIdentifier(f))
	}
	return strings.Join(parts, ", "), nil
}

// buildWhereClause converts filter conditions to a SQL WHERE clause with ? placeholders.
func (b *SQLQueryBuilder) buildWhereClause(filters []datasource.FilterCondition, allowedCols map[string]bool) (string, []any, error) {
	if len(filters) == 0 {
		return "", nil, nil
	}

	clauses := make([]string, 0, len(filters))
	var params []any

	for _, f := range filters {
		if err := ValidateIdentifier(f.Field); err != nil {
			return "", nil, err
		}
		if !allowedCols[f.Field] {
			return "", nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("filter field %q is not in the allowed columns", f.Field))
		}

		col := quoteIdentifier(f.Field)

		switch f.Operator {
		case datasource.FilterOpEQ:
			clauses = append(clauses, col+" = ?")
			params = append(params, f.Value)
		case datasource.FilterOpNEQ:
			clauses = append(clauses, col+" != ?")
			params = append(params, f.Value)
		case datasource.FilterOpGT:
			clauses = append(clauses, col+" > ?")
			params = append(params, f.Value)
		case datasource.FilterOpGTE:
			clauses = append(clauses, col+" >= ?")
			params = append(params, f.Value)
		case datasource.FilterOpLT:
			clauses = append(clauses, col+" < ?")
			params = append(params, f.Value)
		case datasource.FilterOpLTE:
			clauses = append(clauses, col+" <= ?")
			params = append(params, f.Value)
		case datasource.FilterOpLIKE:
			clauses = append(clauses, col+" LIKE ?")
			params = append(params, f.Value)
		case datasource.FilterOpIN:
			inClause, inParams, err := buildINClause(col, f.Value, false)
			if err != nil {
				return "", nil, err
			}
			clauses = append(clauses, inClause)
			params = append(params, inParams...)
		case datasource.FilterOpNOT_IN:
			inClause, inParams, err := buildINClause(col, f.Value, true)
			if err != nil {
				return "", nil, err
			}
			clauses = append(clauses, inClause)
			params = append(params, inParams...)
		case datasource.FilterOpIS_NULL:
			clauses = append(clauses, col+" IS NULL")
		case datasource.FilterOpIS_NOT_NULL:
			clauses = append(clauses, col+" IS NOT NULL")
		default:
			return "", nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("unsupported filter operator %d", f.Operator))
		}
	}

	return strings.Join(clauses, " AND "), params, nil
}

// buildINClause builds an IN or NOT IN clause from a slice value.
func buildINClause(col string, value any, negate bool) (string, []any, error) {
	values, ok := value.([]any)
	if !ok {
		return "", nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"IN/NOT IN operator requires a slice value")
	}
	if len(values) == 0 {
		return "", nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"IN/NOT IN operator requires at least one value")
	}

	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = "?"
	}

	op := "IN"
	if negate {
		op = "NOT IN"
	}
	clause := fmt.Sprintf("%s %s (%s)", col, op, strings.Join(placeholders, ", "))
	return clause, values, nil
}

// buildOrderByClause converts OrderBy clauses to SQL ORDER BY.
func (b *SQLQueryBuilder) buildOrderByClause(orderBy []datasource.OrderByClause, allowedCols map[string]bool) (string, error) {
	if len(orderBy) == 0 {
		return "", nil
	}

	parts := make([]string, 0, len(orderBy))
	for _, o := range orderBy {
		if err := ValidateIdentifier(o.Field); err != nil {
			return "", err
		}
		if !allowedCols[o.Field] {
			return "", apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("order by field %q is not in the allowed columns", o.Field))
		}

		dir := "ASC"
		if o.Direction == datasource.SortDESC {
			dir = "DESC"
		}
		parts = append(parts, quoteIdentifier(o.Field)+" "+dir)
	}
	return strings.Join(parts, ", "), nil
}

// buildLimitClause builds LIMIT/OFFSET from pagination params.
// It supports both Relay-style (First/After) and traditional (Limit/Offset) pagination.
func (b *SQLQueryBuilder) buildLimitClause(p *datasource.PaginationParams) (string, []any) {
	if p == nil {
		return "", nil
	}

	var parts []string

	// Determine limit: First takes precedence over Limit.
	var limit *int
	if p.First != nil {
		limit = p.First
	} else if p.Limit != nil {
		limit = p.Limit
	}

	// Determine offset: After (decoded cursor) takes precedence over Offset.
	var offset *int
	if p.After != nil {
		decoded := decodeCursor(*p.After)
		if decoded >= 0 {
			offset = &decoded
		}
	} else if p.Offset != nil {
		offset = p.Offset
	}

	// StarRocks does not support parameterized LIMIT/OFFSET (? placeholders).
	// Inline the integer values directly — safe because they are int types.
	if limit != nil {
		parts = append(parts, fmt.Sprintf("LIMIT %d", *limit))
	}
	if offset != nil {
		parts = append(parts, fmt.Sprintf("OFFSET %d", *offset))
	}

	return strings.Join(parts, " "), nil
}

// quoteIdentifier wraps an identifier in backticks.
func quoteIdentifier(name string) string {
	return "`" + name + "`"
}

// decodeCursor decodes a cursor string to an integer offset.
// Cursors are simple numeric strings representing the offset.
// Returns -1 if the cursor cannot be decoded.
func decodeCursor(cursor string) int {
	var offset int
	_, err := fmt.Sscanf(cursor, "%d", &offset)
	if err != nil || offset < 0 {
		return -1
	}
	return offset
}
