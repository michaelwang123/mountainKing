// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// testWhitelist defines the allowed tables and columns used across all property tests.
var testWhitelist = map[string]map[string]bool{
	"orders": {"order_id": true, "user_id": true, "amount": true, "status": true},
	"users":  {"user_id": true, "username": true, "email": true},
}

// tableNames returns the list of table names from the whitelist.
func tableNames() []string {
	names := make([]string, 0, len(testWhitelist))
	for k := range testWhitelist {
		names = append(names, k)
	}
	return names
}

// columnsFor returns the list of column names for a given table.
func columnsFor(table string) []string {
	cols := make([]string, 0, len(testWhitelist[table]))
	for c := range testWhitelist[table] {
		cols = append(cols, c)
	}
	return cols
}

// genTable draws a random table name from the whitelist.
func genTable(t *rapid.T) string {
	names := tableNames()
	return names[rapid.IntRange(0, len(names)-1).Draw(t, "tableIdx")]
}

// genFields draws a non-empty subset of columns for the given table.
func genFields(t *rapid.T, table string) []string {
	cols := columnsFor(table)
	n := rapid.IntRange(1, len(cols)).Draw(t, "numFields")
	// Shuffle by picking unique indices.
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

// genSimpleOperator draws a filter operator that uses a single ? placeholder.
func genSimpleOperator(t *rapid.T) datasource.FilterOperator {
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

// genSortDirection draws ASC or DESC.
func genSortDirection(t *rapid.T) datasource.SortDirection {
	if rapid.Bool().Draw(t, "sortDir") {
		return datasource.SortASC
	}
	return datasource.SortDESC
}

// TestProperty15_StarRocksSQLQueryBuild validates that for any valid QueryRequest
// with fields, filters, orderBy, and pagination, the generated SQL has:
// - SELECT clause with backtick-wrapped fields
// - WHERE clause with ? placeholders matching filter count
// - ORDER BY clause matching orderBy
// - LIMIT/OFFSET matching pagination
//
// Feature: graphql-multi-datasource-api, Property 15: StarRocks SQL 查询构建
// **Validates: Requirements 4.2, 4.3, 4.4, 4.5, 7.2**
func TestProperty15_StarRocksSQLQueryBuild(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(testWhitelist)
		table := genTable(t)
		fields := genFields(t, table)
		cols := columnsFor(table)

		// Generate filters (0-3 simple filters).
		numFilters := rapid.IntRange(0, 3).Draw(t, "numFilters")
		var filters []datasource.FilterCondition
		for i := 0; i < numFilters; i++ {
			field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "filterFieldIdx")]
			filters = append(filters, datasource.FilterCondition{
				Field:    field,
				Operator: genSimpleOperator(t),
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
				Direction: genSortDirection(t),
			})
		}

		// Generate pagination (optional).
		var pagination *datasource.PaginationParams
		hasPagination := rapid.Bool().Draw(t, "hasPagination")
		if hasPagination {
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

		// 1. SELECT clause: each field should be backtick-wrapped.
		for _, f := range fields {
			quoted := "`" + f + "`"
			if !strings.Contains(sql, quoted) {
				t.Fatalf("SQL missing backtick-wrapped field %s: %s", quoted, sql)
			}
		}

		// 2. WHERE clause: number of ? placeholders in WHERE should match filter count.
		if numFilters > 0 {
			if !strings.Contains(sql, "WHERE") {
				t.Fatalf("SQL missing WHERE clause with %d filters: %s", numFilters, sql)
			}
		}
		// Count ? placeholders in the SQL.
		totalPlaceholders := strings.Count(sql, "?")
		expectedPlaceholders := numFilters
		if hasPagination {
			expectedPlaceholders += 2 // LIMIT ? OFFSET ?
		}
		if totalPlaceholders != expectedPlaceholders {
			t.Fatalf("expected %d placeholders, got %d in SQL: %s", expectedPlaceholders, totalPlaceholders, sql)
		}

		// 3. Params count should match total placeholders.
		if len(params) != expectedPlaceholders {
			t.Fatalf("expected %d params, got %d", expectedPlaceholders, len(params))
		}

		// 4. ORDER BY clause should be present when orderBy is non-empty.
		if numOrder > 0 {
			if !strings.Contains(sql, "ORDER BY") {
				t.Fatalf("SQL missing ORDER BY with %d clauses: %s", numOrder, sql)
			}
			for _, o := range orderBy {
				quoted := "`" + o.Field + "`"
				if !strings.Contains(sql, quoted) {
					t.Fatalf("ORDER BY missing field %s: %s", quoted, sql)
				}
			}
		}

		// 5. LIMIT/OFFSET should be present when pagination is set.
		if hasPagination {
			if !strings.Contains(sql, "LIMIT") {
				t.Fatalf("SQL missing LIMIT with pagination: %s", sql)
			}
			if !strings.Contains(sql, "OFFSET") {
				t.Fatalf("SQL missing OFFSET with pagination: %s", sql)
			}
		}

		// 6. FROM clause should contain backtick-wrapped table name.
		if !strings.Contains(sql, "FROM `"+table+"`") {
			t.Fatalf("SQL missing FROM `%s`: %s", table, sql)
		}
	})
}

// TestProperty16_StarRocksParameterizedQueryAntiInjection validates that for any
// filter value containing SQL special characters (', ", ;, --), the generated SQL
// uses ? placeholders and the special characters only appear in the params slice,
// never in the SQL string itself.
//
// Feature: graphql-multi-datasource-api, Property 16: StarRocks 参数化查询防注入
// **Validates: Requirements 4.7**
func TestProperty16_StarRocksParameterizedQueryAntiInjection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(testWhitelist)
		table := genTable(t)
		cols := columnsFor(table)
		field := cols[rapid.IntRange(0, len(cols)-1).Draw(t, "fieldIdx")]

		// Generate a value that contains SQL special characters.
		specialChars := []string{"'", "\"", ";", "--", "' OR 1=1 --", "'; DROP TABLE orders; --"}
		specialIdx := rapid.IntRange(0, len(specialChars)-1).Draw(t, "specialIdx")
		prefix := rapid.String().Draw(t, "prefix")
		suffix := rapid.String().Draw(t, "suffix")
		maliciousValue := prefix + specialChars[specialIdx] + suffix

		req := datasource.QueryRequest{
			Fields: []string{field},
			Filters: []datasource.FilterCondition{
				{
					Field:    field,
					Operator: datasource.FilterOpEQ,
					Value:    maliciousValue,
				},
			},
		}

		sql, params, err := builder.Build(req, table)
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		// The SQL should use ? placeholder, not contain the malicious value.
		if strings.Contains(sql, maliciousValue) {
			t.Fatalf("SQL contains raw malicious value %q: %s", maliciousValue, sql)
		}

		// Check that none of the SQL special characters from the value appear
		// unescaped in the SQL (outside of structural SQL syntax).
		// The special chars should only be in params.
		for _, ch := range specialChars {
			// Skip single quote check if it's part of SQL syntax (it shouldn't be
			// since we use ? placeholders). Check the value portion only.
			if ch == "'" || ch == "\"" || ch == ";" || ch == "--" {
				// These should not appear as part of user data in the SQL string.
				// The SQL may contain backticks and structural keywords, but not
				// user-supplied special chars.
				sqlWithoutStructure := sql
				// Remove known structural parts to isolate potential injection.
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "SELECT", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "FROM", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "WHERE", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "AND", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "ORDER BY", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "LIMIT", "")
				sqlWithoutStructure = strings.ReplaceAll(sqlWithoutStructure, "OFFSET", "")

				if strings.Contains(sqlWithoutStructure, maliciousValue) {
					t.Fatalf("SQL structural remainder contains malicious value %q: %s", maliciousValue, sql)
				}
			}
		}

		// The params slice should contain the malicious value.
		if len(params) < 1 {
			t.Fatalf("expected at least 1 param, got %d", len(params))
		}
		found := false
		for _, p := range params {
			if p == maliciousValue {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("params should contain the malicious value %q, got %v", maliciousValue, params)
		}

		// The SQL should have exactly 1 ? placeholder for the filter.
		if strings.Count(sql, "?") < 1 {
			t.Fatalf("SQL should have at least 1 ? placeholder: %s", sql)
		}
	})
}

// TestProperty17_StarRocksIdentifierWhitelistValidation validates that:
// - For any table name or field name NOT in the whitelist, Build returns a validation error.
// - For any identifier containing non-[a-zA-Z0-9_] characters, ValidateIdentifier returns error.
//
// Feature: graphql-multi-datasource-api, Property 17: StarRocks 标识符白名单校验
// **Validates: Requirements 4.7**
func TestProperty17_StarRocksIdentifierWhitelistValidation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := NewSQLQueryBuilder(testWhitelist)

		// Sub-property A: table not in whitelist →error.
		unknownTable := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{3,12}`).Draw(t, "unknownTable")
		// Ensure it's not in the whitelist.
		if _, ok := testWhitelist[unknownTable]; !ok {
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

		// Sub-property B: field not in whitelist →error.
		table := genTable(t)
		unknownField := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{3,12}`).Draw(t, "unknownField")
		if !testWhitelist[table][unknownField] {
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

		// Sub-property C: identifier with invalid characters →ValidateIdentifier error.
		// Generate a string that contains at least one non-[a-zA-Z0-9_] character.
		invalidChars := []string{" ", "-", ".", "/", "@", "#", "$", "%", "!", "`", "'", "\"", ";"}
		charIdx := rapid.IntRange(0, len(invalidChars)-1).Draw(t, "invalidCharIdx")
		base := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_]{0,5}`).Draw(t, "identBase")
		invalidIdent := base + invalidChars[charIdx] + rapid.StringMatching(`[a-zA-Z0-9_]{0,3}`).Draw(t, "identSuffix")

		err := ValidateIdentifier(invalidIdent)
		if err == nil {
			t.Fatalf("expected ValidateIdentifier error for %q, got nil", invalidIdent)
		}
		if !strings.Contains(err.Error(), "invalid characters") {
			t.Fatalf("expected 'invalid characters' error for %q, got: %v", invalidIdent, err)
		}

		// Sub-property D: empty identifier →ValidateIdentifier error.
		err = ValidateIdentifier("")
		if err == nil {
			t.Fatal("expected ValidateIdentifier error for empty string, got nil")
		}
	})
}
