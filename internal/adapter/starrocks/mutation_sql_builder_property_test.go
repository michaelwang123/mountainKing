// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// genMutationIdentifier generates a valid SQL identifier matching ^[a-zA-Z_][a-zA-Z0-9_]*$.
func genMutationIdentifier(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{0,15}`).Draw(t, label)
}

// genMutationValue generates a random value that may include SQL injection attempts.
func genMutationValue(t *rapid.T, label string) any {
	valueType := rapid.IntRange(0, 5).Draw(t, label+"_type")
	switch valueType {
	case 0:
		return rapid.String().Draw(t, label+"_str")
	case 1:
		return rapid.Int().Draw(t, label+"_int")
	case 2:
		return rapid.Float64().Draw(t, label+"_float")
	case 3:
		// SQL injection attempt strings
		injections := []string{
			"'; DROP TABLE users; --",
			"1 OR 1=1",
			"' UNION SELECT * FROM passwords --",
			"Robert'); DROP TABLE Students;--",
			"1; DELETE FROM orders WHERE 1=1",
			"' OR '1'='1",
			"admin'--",
			"1' AND (SELECT COUNT(*) FROM sysobjects) > 0 --",
		}
		idx := rapid.IntRange(0, len(injections)-1).Draw(t, label+"_injIdx")
		return injections[idx]
	case 4:
		return rapid.Bool().Draw(t, label+"_bool")
	default:
		return nil
	}
}

// genFilterOperator generates a random filter operator for mutation WHERE clauses.
func genFilterOperator(t *rapid.T, label string) datasource.FilterOperator {
	ops := []datasource.FilterOperator{
		datasource.FilterOpEQ,
		datasource.FilterOpNEQ,
		datasource.FilterOpGT,
		datasource.FilterOpGTE,
		datasource.FilterOpLT,
		datasource.FilterOpLTE,
		datasource.FilterOpLIKE,
		datasource.FilterOpIN,
		datasource.FilterOpNOT_IN,
		datasource.FilterOpIS_NULL,
		datasource.FilterOpIS_NOT_NULL,
	}
	return ops[rapid.IntRange(0, len(ops)-1).Draw(t, label)]
}

// genFilterConditions generates a slice of filter conditions with valid identifiers.
func genFilterConditions(t *rapid.T, numFilters int) []datasource.FilterCondition {
	filters := make([]datasource.FilterCondition, numFilters)
	for i := range numFilters {
		op := genFilterOperator(t, fmt.Sprintf("filterOp_%d", i))
		field := genMutationIdentifier(t, fmt.Sprintf("filterField_%d", i))

		var value any
		switch op {
		case datasource.FilterOpIS_NULL, datasource.FilterOpIS_NOT_NULL:
			value = nil
		case datasource.FilterOpIN, datasource.FilterOpNOT_IN:
			// Generate a slice of values for IN/NOT_IN
			n := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("inSize_%d", i))
			vals := make([]any, n)
			for j := range n {
				vals[j] = genMutationValue(t, fmt.Sprintf("inVal_%d_%d", i, j))
			}
			value = vals
		default:
			value = genMutationValue(t, fmt.Sprintf("filterVal_%d", i))
		}

		filters[i] = datasource.FilterCondition{
			Field:    field,
			Operator: op,
			Value:    value,
		}
	}
	return filters
}

// expectedParamCount computes the expected number of ? placeholders for a set of filters.
func expectedParamCount(filters []datasource.FilterCondition) int {
	count := 0
	for _, f := range filters {
		switch f.Operator {
		case datasource.FilterOpIS_NULL, datasource.FilterOpIS_NOT_NULL:
			// No placeholder
		case datasource.FilterOpIN, datasource.FilterOpNOT_IN:
			if vals, ok := f.Value.([]any); ok {
				count += len(vals)
			} else {
				count++ // fallback single placeholder
			}
		default:
			count++
		}
	}
	return count
}

// stripBacktickQuotedIdentifiers removes all backtick-quoted identifiers from the SQL text
// so that value-leak detection doesn't produce false positives when a generated value
// coincidentally matches part of a quoted identifier (e.g., value "A`" matching in `A`).
func stripBacktickQuotedIdentifiers(sql string) string {
	var result strings.Builder
	i := 0
	for i < len(sql) {
		if sql[i] == '`' {
			// Skip everything until the closing backtick
			j := i + 1
			for j < len(sql) && sql[j] != '`' {
				j++
			}
			if j < len(sql) {
				j++ // skip closing backtick
			}
			i = j
		} else {
			result.WriteByte(sql[i])
			i++
		}
	}
	return result.String()
}

// valueAppearsInSQL checks if any user-provided value appears literally in the SQL text.
// Only checks string values since numeric/bool values could coincidentally match SQL keywords.
// Backtick-quoted identifiers are stripped from the SQL before checking, because identifiers
// are structural elements (table/column names) and not user-provided values leaking through.
func valueAppearsInSQL(sql string, value any) bool {
	switch v := value.(type) {
	case string:
		if v == "" {
			return false // empty string is not meaningful to check
		}
		// Skip very short strings that could match SQL structural elements
		if len(v) < 2 {
			return false
		}
		// Strip backtick-quoted identifiers to avoid false positives where a value
		// like "A`" coincidentally matches a fragment of the quoted identifier `A`
		stripped := stripBacktickQuotedIdentifiers(sql)
		return strings.Contains(stripped, v)
	case []any:
		for _, item := range v {
			if valueAppearsInSQL(sql, item) {
				return true
			}
		}
	}
	return false
}

// TestProperty1_ParameterizationSafety_BuildInsert validates Property 1 for BuildInsert:
// For any valid insert input, count of ? in SQL == len(params), and no user value
// appears literally in the SQL text.
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6**
func TestProperty1_ParameterizationSafety_BuildInsert(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := &MutationSQLBuilder{}

		// Generate valid table name and columns
		table := genMutationIdentifier(t, "table")
		numCols := rapid.IntRange(1, 8).Draw(t, "numCols")
		columns := make([]string, numCols)
		for i := range numCols {
			columns[i] = genMutationIdentifier(t, fmt.Sprintf("col_%d", i))
		}

		// Generate values (same count as columns)
		values := make([]any, numCols)
		for i := range numCols {
			values[i] = genMutationValue(t, fmt.Sprintf("val_%d", i))
		}

		result := builder.BuildInsert(table, columns, values)

		// Property 1a: count of ? in SQL == len(params)
		placeholderCount := strings.Count(result.SQL, "?")
		if placeholderCount != len(result.Params) {
			t.Fatalf("placeholder count mismatch: SQL has %d '?' but params has %d items.\nSQL: %s",
				placeholderCount, len(result.Params), result.SQL)
		}

		// Property 1b: placeholder count == number of values
		if placeholderCount != numCols {
			t.Fatalf("expected %d placeholders for %d columns, got %d.\nSQL: %s",
				numCols, numCols, placeholderCount, result.SQL)
		}

		// Property 1c: no user-provided value appears literally in SQL text
		for i, val := range values {
			if valueAppearsInSQL(result.SQL, val) {
				t.Fatalf("user value at index %d (%v) appears literally in SQL text.\nSQL: %s",
					i, val, result.SQL)
			}
		}
	})
}

// TestProperty1_ParameterizationSafety_BuildUpdate validates Property 1 for BuildUpdate:
// For any valid update input, count of ? in SQL == len(params), and no user value
// appears literally in the SQL text.
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6**
func TestProperty1_ParameterizationSafety_BuildUpdate(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := &MutationSQLBuilder{}

		// Generate valid table name
		table := genMutationIdentifier(t, "table")

		// Generate SET columns and values
		numSetCols := rapid.IntRange(1, 6).Draw(t, "numSetCols")
		setCols := make([]string, numSetCols)
		setVals := make([]any, numSetCols)
		for i := range numSetCols {
			setCols[i] = genMutationIdentifier(t, fmt.Sprintf("setCol_%d", i))
			setVals[i] = genMutationValue(t, fmt.Sprintf("setVal_%d", i))
		}

		// Generate filter conditions (at least 1)
		numFilters := rapid.IntRange(1, 4).Draw(t, "numFilters")
		filters := genFilterConditions(t, numFilters)

		result := builder.BuildUpdate(table, setCols, setVals, filters)

		// Property 1a: count of ? in SQL == len(params)
		placeholderCount := strings.Count(result.SQL, "?")
		if placeholderCount != len(result.Params) {
			t.Fatalf("placeholder count mismatch: SQL has %d '?' but params has %d items.\nSQL: %s\nParams: %v",
				placeholderCount, len(result.Params), result.SQL, result.Params)
		}

		// Property 1b: placeholder count == SET values + filter params
		expectedCount := numSetCols + expectedParamCount(filters)
		if placeholderCount != expectedCount {
			t.Fatalf("expected %d placeholders (SET:%d + filters:%d), got %d.\nSQL: %s",
				expectedCount, numSetCols, expectedParamCount(filters), placeholderCount, result.SQL)
		}

		// Property 1c: no user-provided SET value appears literally in SQL text
		for i, val := range setVals {
			if valueAppearsInSQL(result.SQL, val) {
				t.Fatalf("SET value at index %d (%v) appears literally in SQL text.\nSQL: %s",
					i, val, result.SQL)
			}
		}

		// Property 1d: no user-provided filter value appears literally in SQL text
		for i, f := range filters {
			if f.Operator == datasource.FilterOpIS_NULL || f.Operator == datasource.FilterOpIS_NOT_NULL {
				continue
			}
			if valueAppearsInSQL(result.SQL, f.Value) {
				t.Fatalf("filter value at index %d (%v) appears literally in SQL text.\nSQL: %s",
					i, f.Value, result.SQL)
			}
		}
	})
}

// TestProperty1_ParameterizationSafety_BuildDelete validates Property 1 for BuildDelete:
// For any valid delete input, count of ? in SQL == len(params), and no user value
// appears literally in the SQL text.
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6**
func TestProperty1_ParameterizationSafety_BuildDelete(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := &MutationSQLBuilder{}

		// Generate valid table name
		table := genMutationIdentifier(t, "table")

		// Generate filter conditions (at least 1)
		numFilters := rapid.IntRange(1, 5).Draw(t, "numFilters")
		filters := genFilterConditions(t, numFilters)

		result := builder.BuildDelete(table, filters)

		// Property 1a: count of ? in SQL == len(params)
		placeholderCount := strings.Count(result.SQL, "?")
		if placeholderCount != len(result.Params) {
			t.Fatalf("placeholder count mismatch: SQL has %d '?' but params has %d items.\nSQL: %s\nParams: %v",
				placeholderCount, len(result.Params), result.SQL, result.Params)
		}

		// Property 1b: placeholder count == expected filter params
		expectedCount := expectedParamCount(filters)
		if placeholderCount != expectedCount {
			t.Fatalf("expected %d placeholders for filters, got %d.\nSQL: %s",
				expectedCount, placeholderCount, result.SQL)
		}

		// Property 1c: no user-provided filter value appears literally in SQL text
		for i, f := range filters {
			if f.Operator == datasource.FilterOpIS_NULL || f.Operator == datasource.FilterOpIS_NOT_NULL {
				continue
			}
			if valueAppearsInSQL(result.SQL, f.Value) {
				t.Fatalf("filter value at index %d (%v) appears literally in SQL text.\nSQL: %s",
					i, f.Value, result.SQL)
			}
		}
	})
}

// TestProperty1_ParameterizationSafety_BuildBatchInsert validates Property 1 for BuildBatchInsert:
// For any valid batch insert input, count of ? in SQL == len(params), and no user value
// appears literally in the SQL text.
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4, 2.5, 2.6**
func TestProperty1_ParameterizationSafety_BuildBatchInsert(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		builder := &MutationSQLBuilder{}

		// Generate valid table name and columns
		table := genMutationIdentifier(t, "table")
		numCols := rapid.IntRange(1, 6).Draw(t, "numCols")
		columns := make([]string, numCols)
		for i := range numCols {
			columns[i] = genMutationIdentifier(t, fmt.Sprintf("col_%d", i))
		}

		// Generate rows (1-10 rows, each with numCols values)
		numRows := rapid.IntRange(1, 10).Draw(t, "numRows")
		rows := make([][]any, numRows)
		for i := range numRows {
			row := make([]any, numCols)
			for j := range numCols {
				row[j] = genMutationValue(t, fmt.Sprintf("row_%d_val_%d", i, j))
			}
			rows[i] = row
		}

		result := builder.BuildBatchInsert(table, columns, rows)

		// Property 1a: count of ? in SQL == len(params)
		placeholderCount := strings.Count(result.SQL, "?")
		if placeholderCount != len(result.Params) {
			t.Fatalf("placeholder count mismatch: SQL has %d '?' but params has %d items.\nSQL: %s",
				placeholderCount, len(result.Params), result.SQL)
		}

		// Property 1b: placeholder count == numCols * numRows
		expectedCount := numCols * numRows
		if placeholderCount != expectedCount {
			t.Fatalf("expected %d placeholders (%d cols × %d rows), got %d.\nSQL: %s",
				expectedCount, numCols, numRows, placeholderCount, result.SQL)
		}

		// Property 1c: no user-provided value appears literally in SQL text
		for i, row := range rows {
			for j, val := range row {
				if valueAppearsInSQL(result.SQL, val) {
					t.Fatalf("batch value at row[%d][%d] (%v) appears literally in SQL text.\nSQL: %s",
						i, j, val, result.SQL)
				}
			}
		}
	})
}
