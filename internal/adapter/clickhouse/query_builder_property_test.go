// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// propWhitelist defines the allowed tables and columns used across all property tests.
var propWhitelist = map[string]map[string]bool{
	"events": {"event_id": true, "user_id": true, "event_type": true, "created_at": true},
}

// propTableNames returns the list of table names from the property test whitelist.
func propTableNames() []string {
	names := make([]string, 0, len(propWhitelist))
	for k := range propWhitelist {
		names = append(names, k)
	}
	return names
}

// propColumnsFor returns the list of column names for a given table in the property test whitelist.
func propColumnsFor(table string) []string {
	cols := make([]string, 0, len(propWhitelist[table]))
	for c := range propWhitelist[table] {
		cols = append(cols, c)
	}
	return cols
}

// propGenTable draws a random table name from the property test whitelist.
func propGenTable(t *rapid.T) string {
	names := propTableNames()
	return names[rapid.IntRange(0, len(names)-1).Draw(t, "tableIdx")]
}

// propGenFields draws a non-empty subset of columns for the given table.
func propGenFields(t *rapid.T, table string) []string {
	cols := propColumnsFor(table)
	n := rapid.IntRange(1, len(cols)).Draw(t, "numFields")
	perm := rapid.SliceOfN(rapid.IntRange(0, len(cols)-1), n, n).Draw(t, "fieldPerm")
	seen := map[int]bool{}
	var result []string
	for _, idx := range perm {
		if !seen[idx] {
			seen[idx] = true
			result = append(result, cols[idx])
		}
	}
	if len(result) == 0 {
		result = append(result, cols[0])
	}
	return result
}

// propGenSimpleOperator draws a filter operator that uses a single ? placeholder.
func propGenSimpleOperator(t *rapid.T) datasource.FilterOperator {
	ops := []datasource.FilterOperator{
		datasource.FilterOpEQ,
		datasource.FilterOpNEQ,
		datasource.FilterOpGT,
		datasource.FilterOpGTE,
		datasource.FilterOpLT,
		datasource.FilterOpLTE,
		datasource.FilterOpLIKE,
	}
	return ops[rapid.IntRange(0, len(ops)-1).Draw(t, "opIdx")]
}

// propGenSortDirection draws ASC or DESC.
func propGenSortDirection(t *rapid.T) datasource.SortDirection {
	if rapid.Bool().Draw(t, "sortDir") {
		return datasource.SortASC
	}
	return datasource.SortDESC
}

// TestPropertyParameterizationSafety validates that for any valid QueryRequest
// (whitelisted table/columns, valid filters), the number of `?` in the generated SQL
// equals len(params), and no user-provided filter values appear as literals in the SQL.
//
// **Validates: Requirements 15.1, 15.2, 15.3, 17.5**
func TestPropertyParameterizationSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(propWhitelist)
		table := propGenTable(t)
		fields := propGenFields(t, table)
		cols := propColumnsFor(table)

		// Generate filters (0-3 simple filters).
		// Use a "USR_" prefix to ensure generated values are distinguishable from SQL syntax.
		numFilters := rapid.IntRange(0, 3).Draw(t, "numFilters")
		var filters []datasource.FilterCondition
		var filterValues []string
		for i := 0; i < numFilters; i++ {
			field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "filterFieldIdx")]
			value := "USR_" + rapid.String().Draw(t, "filterValue")
			filters = append(filters, datasource.FilterCondition{
				Field:    field,
				Operator: propGenSimpleOperator(t),
				Value:    value,
			})
			filterValues = append(filterValues, value)
		}

		// Generate orderBy (0-2 clauses).
		numOrder := rapid.IntRange(0, 2).Draw(t, "numOrder")
		var orderBy []datasource.OrderByClause
		for i := 0; i < numOrder; i++ {
			field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "orderFieldIdx")]
			orderBy = append(orderBy, datasource.OrderByClause{
				Field:     field,
				Direction: propGenSortDirection(t),
			})
		}

		// Generate pagination (optional).
		var pagination *datasource.PaginationParams
		if rapid.Bool().Draw(t, "hasPagination") {
			limit := rapid.IntRange(1, 1000).Draw(t, "limit")
			offset := rapid.IntRange(0, 10000).Draw(t, "offset")
			pagination = &datasource.PaginationParams{
				Limit:  &limit,
				Offset: &offset,
			}
		}

		req := datasource.QueryRequest{
			Fields:     fields,
			Filters:    filters,
			OrderBy:    orderBy,
			Pagination: pagination,
		}

		sql, params, err := builder.Build(req, table)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		// Property: number of ? placeholders == len(params).
		placeholderCount := strings.Count(sql, "?")
		if placeholderCount != len(params) {
			t.Fatalf("placeholder count %d != len(params) %d in SQL: %s", placeholderCount, len(params), sql)
		}

		// Property: no user-provided filter values appear as literals in the SQL.
		// Values are prefixed with "USR_" so they cannot be confused with SQL syntax.
		for _, val := range filterValues {
			if strings.Contains(sql, val) {
				t.Fatalf("SQL contains user value literal %q: %s", val, sql)
			}
		}
	})
}

// TestPropertyWhitelistEnforcement validates that for any random table name NOT in
// the whitelist, Build() always returns an error. For any random column name NOT in
// the allowed columns, Build() also returns an error.
//
// **Validates: Requirements 15.2, 17.6**
func TestPropertyWhitelistEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(propWhitelist)

		// Sub-property A: table not in whitelist → error.
		unknownTable := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{3,12}`).Draw(t, "unknownTable")
		if _, ok := propWhitelist[unknownTable]; !ok {
			req := datasource.QueryRequest{Fields: []string{}}
			_, _, err := builder.Build(req, unknownTable)
			if err == nil {
				t.Fatalf("expected error for unknown table %q, got nil", unknownTable)
			}
			if !strings.Contains(err.Error(), "not in the allowed whitelist") &&
				!strings.Contains(err.Error(), "VALIDATION_INVALID_TABLE") {
				t.Fatalf("expected whitelist validation error for table %q, got: %v", unknownTable, err)
			}
		}

		// Sub-property B: field not in whitelist → error.
		table := propGenTable(t)
		unknownField := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{3,12}`).Draw(t, "unknownField")
		if !propWhitelist[table][unknownField] {
			req := datasource.QueryRequest{Fields: []string{unknownField}}
			_, _, err := builder.Build(req, table)
			if err == nil {
				t.Fatalf("expected error for unknown field %q in table %q, got nil", unknownField, table)
			}
			if !strings.Contains(err.Error(), "not in the allowed columns") &&
				!strings.Contains(err.Error(), "VALIDATION_INVALID_FIELD") {
				t.Fatalf("expected field validation error for %q, got: %v", unknownField, err)
			}
		}
	})
}

// TestPropertyIdentifierQuoting validates that for any successful Build() call,
// all table and column identifiers in the SQL output are surrounded by backticks.
//
// **Validates: Requirements 15.3, 17.5**
func TestPropertyIdentifierQuoting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(propWhitelist)
		table := propGenTable(t)
		fields := propGenFields(t, table)
		cols := propColumnsFor(table)

		// Generate filters (0-2 simple filters).
		numFilters := rapid.IntRange(0, 2).Draw(t, "numFilters")
		var filters []datasource.FilterCondition
		for i := 0; i < numFilters; i++ {
			field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "filterFieldIdx")]
			filters = append(filters, datasource.FilterCondition{
				Field:    field,
				Operator: propGenSimpleOperator(t),
				Value:    rapid.String().Draw(t, "filterValue"),
			})
		}

		// Generate orderBy (0-2 clauses).
		numOrder := rapid.IntRange(0, 2).Draw(t, "numOrder")
		var orderBy []datasource.OrderByClause
		for i := 0; i < numOrder; i++ {
			field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "orderFieldIdx")]
			orderBy = append(orderBy, datasource.OrderByClause{
				Field:     field,
				Direction: propGenSortDirection(t),
			})
		}

		req := datasource.QueryRequest{
			Fields:  fields,
			Filters: filters,
			OrderBy: orderBy,
		}

		sql, _, err := builder.Build(req, table)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		// Property: table name appears backtick-quoted in FROM clause.
		if !strings.Contains(sql, "FROM `"+table+"`") {
			t.Fatalf("SQL missing backtick-quoted table in FROM clause: %s", sql)
		}

		// Property: each selected field appears backtick-quoted.
		for _, f := range fields {
			quoted := "`" + f + "`"
			if !strings.Contains(sql, quoted) {
				t.Fatalf("SQL missing backtick-quoted field %s: %s", quoted, sql)
			}
		}

		// Property: each filter field appears backtick-quoted.
		for _, f := range filters {
			quoted := "`" + f.Field + "`"
			if !strings.Contains(sql, quoted) {
				t.Fatalf("SQL missing backtick-quoted filter field %s: %s", quoted, sql)
			}
		}

		// Property: each orderBy field appears backtick-quoted.
		for _, o := range orderBy {
			quoted := "`" + o.Field + "`"
			if !strings.Contains(sql, quoted) {
				t.Fatalf("SQL missing backtick-quoted order field %s: %s", quoted, sql)
			}
		}

		// Property: no unquoted identifier (table/column name) appears in SQL
		// outside of backtick delimiters.
		sqlStripped := sql
		// Remove all backtick-quoted identifiers.
		for strings.Contains(sqlStripped, "`") {
			start := strings.Index(sqlStripped, "`")
			end := strings.Index(sqlStripped[start+1:], "`")
			if end == -1 {
				break
			}
			sqlStripped = sqlStripped[:start] + sqlStripped[start+1+end+1:]
		}
		// After stripping, no raw table/column name should appear.
		if strings.Contains(sqlStripped, table) {
			t.Fatalf("SQL contains unquoted table name %q after stripping backticks: original=%s", table, sql)
		}
		for _, f := range fields {
			if strings.Contains(sqlStripped, f) {
				t.Fatalf("SQL contains unquoted field %q after stripping backticks: original=%s", f, sql)
			}
		}
	})
}
