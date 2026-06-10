// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// genWritableIdentifier generates a valid SQL identifier matching ^[a-zA-Z_][a-zA-Z0-9_]*$.
func genWritableIdentifier(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{1,12}`).Draw(t, label)
}

// genAllowedTablesMap generates a random allowed_tables map with 1-5 tables, each having 2-6 columns.
func genAllowedTablesMap(t *rapid.T) map[string]map[string]bool {
	numTables := rapid.IntRange(1, 5).Draw(t, "numAllowedTables")
	allowed := make(map[string]map[string]bool, numTables)

	for i := 0; i < numTables; i++ {
		tableName := genWritableIdentifier(t, fmt.Sprintf("allowedTable_%d", i))
		numCols := rapid.IntRange(2, 6).Draw(t, fmt.Sprintf("numAllowedCols_%d", i))
		cols := make(map[string]bool, numCols)
		for j := 0; j < numCols; j++ {
			colName := genWritableIdentifier(t, fmt.Sprintf("allowedCol_%d_%d", i, j))
			cols[colName] = true
		}
		allowed[tableName] = cols
	}

	return allowed
}

// genWritableSubsetOf generates a writable_tables map that IS a proper subset of allowedTables.
// It picks a random subset of tables and a random subset of their columns.
func genWritableSubsetOf(t *rapid.T, allowed map[string]map[string]bool) map[string]*WritableTableConfig {
	// Collect table names
	tables := make([]string, 0, len(allowed))
	for tbl := range allowed {
		tables = append(tables, tbl)
	}

	// Pick at least 1 table, up to all tables
	numTables := rapid.IntRange(1, len(tables)).Draw(t, "numWritableTables")

	writable := make(map[string]*WritableTableConfig, numTables)
	used := make(map[int]bool)

	for i := 0; i < numTables; i++ {
		// Pick a table index that hasn't been used
		idx := rapid.IntRange(0, len(tables)-1).Draw(t, fmt.Sprintf("writableTableIdx_%d", i))
		for used[idx] {
			idx = (idx + 1) % len(tables)
		}
		used[idx] = true

		tbl := tables[idx]
		allowedCols := allowed[tbl]

		// Collect columns for this table
		cols := make([]string, 0, len(allowedCols))
		for c := range allowedCols {
			cols = append(cols, c)
		}

		// Pick a subset of columns (at least 1)
		numCols := rapid.IntRange(1, len(cols)).Draw(t, fmt.Sprintf("numWritableCols_%d", i))
		colUsed := make(map[int]bool)
		writableCols := make(map[string]bool, numCols)

		for j := 0; j < numCols; j++ {
			cIdx := rapid.IntRange(0, len(cols)-1).Draw(t, fmt.Sprintf("writableColIdx_%d_%d", i, j))
			for colUsed[cIdx] {
				cIdx = (cIdx + 1) % len(cols)
			}
			colUsed[cIdx] = true
			writableCols[cols[cIdx]] = true
		}

		writable[tbl] = &WritableTableConfig{
			Columns:           writableCols,
			AllowedOperations: map[string]bool{"insert": true, "update": true, "delete": true},
		}
	}

	return writable
}

// TestProperty6_WritableSubset_TableNotInAllowed validates that when a writable table
// is NOT present in allowed_tables, ValidateWritableSubset returns an error.
//
// **Validates: Requirements 3.5**
func TestProperty6_WritableSubset_TableNotInAllowed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a valid allowed_tables map
		allowed := genAllowedTablesMap(t)

		// Generate a writable_tables map that starts as a valid subset
		writable := genWritableSubsetOf(t, allowed)

		// Add a table that is NOT in allowed_tables
		extraTable := genWritableIdentifier(t, "extraTable") + "_notallowed"
		// Ensure it doesn't accidentally collide with any allowed table
		for allowed[extraTable] != nil {
			extraTable = extraTable + "x"
		}

		writable[extraTable] = &WritableTableConfig{
			Columns:           map[string]bool{"col_a": true},
			AllowedOperations: map[string]bool{"insert": true},
		}

		// Validate — should return an error because extraTable is not in allowed_tables
		err := ValidateWritableSubset(writable, allowed)
		if err == nil {
			t.Fatalf("expected error for writable table %q not in allowed_tables, got nil", extraTable)
		}

		// Error should mention the table is not present in allowed_tables
		errMsg := err.Error()
		if !strings.Contains(errMsg, "not present in allowed_tables") {
			t.Fatalf("expected error to mention 'not present in allowed_tables', got: %v", err)
		}
	})
}

// TestProperty6_WritableSubset_ColumnNotInAllowed validates that when a writable column
// is NOT present in the corresponding table's allowed_tables columns, ValidateWritableSubset
// returns an error.
//
// **Validates: Requirements 3.5**
func TestProperty6_WritableSubset_ColumnNotInAllowed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a valid allowed_tables map with at least one table
		allowed := genAllowedTablesMap(t)

		// Pick one table from allowed to use as writable
		tables := make([]string, 0, len(allowed))
		for tbl := range allowed {
			tables = append(tables, tbl)
		}
		chosenIdx := rapid.IntRange(0, len(tables)-1).Draw(t, "chosenTableIdx")
		chosenTable := tables[chosenIdx]

		// Create a writable config with at least one valid column from allowed
		allowedCols := allowed[chosenTable]
		validCols := make(map[string]bool)
		for c := range allowedCols {
			validCols[c] = true
			break // just take one valid column
		}

		// Add an extra column that is NOT in allowed_tables for this table
		extraCol := genWritableIdentifier(t, "extraCol") + "_notallowed"
		// Ensure it doesn't accidentally collide
		for allowedCols[extraCol] {
			extraCol = extraCol + "x"
		}
		validCols[extraCol] = true

		writable := map[string]*WritableTableConfig{
			chosenTable: {
				Columns:           validCols,
				AllowedOperations: map[string]bool{"insert": true},
			},
		}

		// Validate — should return an error because extraCol is not in allowed_tables
		err := ValidateWritableSubset(writable, allowed)
		if err == nil {
			t.Fatalf("expected error for writable column %q in table %q not in allowed_tables, got nil",
				extraCol, chosenTable)
		}

		// Error should mention the column is not present in allowed_tables
		errMsg := err.Error()
		if !strings.Contains(errMsg, "not present in allowed_tables") {
			t.Fatalf("expected error to mention 'not present in allowed_tables', got: %v", err)
		}
	})
}

// TestProperty6_WritableSubset_ValidSubsetReturnsNil validates that when writable_tables
// IS a proper subset of allowed_tables (all tables and columns exist in allowed),
// ValidateWritableSubset returns nil.
//
// **Validates: Requirements 3.5**
func TestProperty6_WritableSubset_ValidSubsetReturnsNil(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a valid allowed_tables map
		allowed := genAllowedTablesMap(t)

		// Generate a writable_tables map that is a proper subset
		writable := genWritableSubsetOf(t, allowed)

		// Validate — should return nil because writable is a valid subset
		err := ValidateWritableSubset(writable, allowed)
		if err != nil {
			t.Fatalf("expected nil error for valid writable subset, got: %v\nallowed: %v\nwritable tables: %v",
				err, allowed, formatWritable(writable))
		}
	})
}

// formatWritable is a test helper to format writable config for error messages.
func formatWritable(writable map[string]*WritableTableConfig) string {
	var sb strings.Builder
	for tbl, cfg := range writable {
		sb.WriteString(fmt.Sprintf("%s: columns=%v ", tbl, cfg.Columns))
	}
	return sb.String()
}
