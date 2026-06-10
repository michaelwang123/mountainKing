// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// testWritableConfig creates a fixed WritableTableValidator configuration for property tests.
// The config defines two writable tables with specific columns and operation restrictions.
func testWritableConfig() *WritableTableValidator {
	writable := map[string]*WritableTableConfig{
		"orders": {
			Columns:           map[string]bool{"user_id": true, "amount": true, "status": true},
			AllowedOperations: map[string]bool{"insert": true, "update": true},
		},
		"events": {
			Columns:           map[string]bool{"event_type": true, "payload": true, "created_at": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
	}

	// allowed_tables is the broader read whitelist — superset of writable
	allowed := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true, "created_at": true, "updated_at": true},
		"events": {"event_id": true, "event_type": true, "payload": true, "created_at": true},
		"users":  {"user_id": true, "username": true, "email": true, "role": true},
	}

	return NewWritableTableValidator(writable, allowed)
}

// genNonWhitelistedTable generates a random table name that is NOT in the writable_tables config.
// It produces valid identifiers that differ from "orders" and "events".
func genNonWhitelistedTable(t *rapid.T, label string) string {
	// Generate a valid identifier that is not in our whitelist
	name := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{2,20}`).Draw(t, label)
	// Ensure it's not one of the whitelisted tables
	for name == "orders" || name == "events" {
		name = name + "_x"
	}
	return name
}

// genNonWhitelistedColumn generates a random column name that is NOT in the writable columns
// for the given table. It produces valid identifiers that are not in the writable whitelist.
func genNonWhitelistedColumn(t *rapid.T, label string, writableCols map[string]bool) string {
	name := rapid.StringMatching(`[a-zA-Z_][a-zA-Z0-9_]{2,20}`).Draw(t, label)
	// Ensure it's not one of the whitelisted columns
	for writableCols[name] {
		name = name + "_z"
	}
	return name
}

// genDisallowedOperation generates an operation that is NOT allowed for the given table.
func genDisallowedOperation(t *rapid.T, label string, allowedOps map[string]bool) string {
	allOps := []string{"insert", "update", "delete"}
	disallowed := make([]string, 0)
	for _, op := range allOps {
		if !allowedOps[op] {
			disallowed = append(disallowed, op)
		}
	}
	if len(disallowed) == 0 {
		// All ops allowed — skip this case
		return ""
	}
	idx := rapid.IntRange(0, len(disallowed)-1).Draw(t, label)
	return disallowed[idx]
}

// TestProperty2_TableWhitelistEnforcement validates Property 2:
// For any table name NOT present in writable_tables, WritableTableValidator.ValidateTable
// SHALL return a validation error.
//
// **Validates: Requirements 3.1, 9.5**
func TestProperty2_TableWhitelistEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := testWritableConfig()

		// Generate a table name that is NOT in writable_tables
		table := genNonWhitelistedTable(t, "nonWhitelistedTable")

		err := validator.ValidateTable(table)

		// Property: non-whitelisted table MUST produce an error
		if err == nil {
			t.Fatalf("expected error for non-whitelisted table %q, got nil", table)
		}

		// Verify error message mentions the table name
		if !strings.Contains(err.Error(), table) {
			t.Fatalf("error should reference the table name %q, got: %v", table, err)
		}
	})
}

// TestProperty2_TableWhitelistEnforcement_Positive validates that whitelisted tables pass.
// This ensures the property is not vacuously true by testing the positive case.
func TestProperty2_TableWhitelistEnforcement_Positive(t *testing.T) {
	validator := testWritableConfig()

	whitelistedTables := []string{"orders", "events"}
	for _, table := range whitelistedTables {
		err := validator.ValidateTable(table)
		if err != nil {
			t.Fatalf("expected nil for whitelisted table %q, got: %v", table, err)
		}
	}
}

// TestProperty3_ColumnWhitelistEnforcement validates Property 3:
// For any column name NOT present in writable_tables[table].Columns for the target table,
// WritableTableValidator.ValidateWriteColumns SHALL return a validation error.
//
// **Validates: Requirements 3.2, 3.7**
func TestProperty3_ColumnWhitelistEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := testWritableConfig()

		// Pick a whitelisted table
		tables := []string{"orders", "events"}
		tableIdx := rapid.IntRange(0, len(tables)-1).Draw(t, "tableIdx")
		table := tables[tableIdx]

		// Get writable columns for this table
		writableCols := map[string]map[string]bool{
			"orders": {"user_id": true, "amount": true, "status": true},
			"events": {"event_type": true, "payload": true, "created_at": true},
		}[table]

		// Generate a non-whitelisted column
		badCol := genNonWhitelistedColumn(t, "nonWhitelistedCol", writableCols)

		// Mix with some valid columns to test that any single bad column triggers failure
		numValidCols := rapid.IntRange(0, 2).Draw(t, "numValidCols")
		columns := make([]string, 0, numValidCols+1)
		validCols := make([]string, 0)
		for col := range writableCols {
			validCols = append(validCols, col)
		}
		for i := 0; i < numValidCols && i < len(validCols); i++ {
			columns = append(columns, validCols[i])
		}
		// Insert the bad column at a random position
		insertPos := rapid.IntRange(0, len(columns)).Draw(t, "insertPos")
		columns = append(columns[:insertPos], append([]string{badCol}, columns[insertPos:]...)...)

		err := validator.ValidateWriteColumns(table, columns)

		// Property: non-whitelisted column MUST produce an error
		if err == nil {
			t.Fatalf("expected error for non-whitelisted column %q in table %q, got nil", badCol, table)
		}

		// Verify error references the bad column
		if !strings.Contains(err.Error(), badCol) {
			t.Fatalf("error should reference the column name %q, got: %v", badCol, err)
		}
	})
}

// TestProperty3_ColumnWhitelistEnforcement_Positive validates that whitelisted columns pass.
func TestProperty3_ColumnWhitelistEnforcement_Positive(t *testing.T) {
	validator := testWritableConfig()

	// All valid columns for orders
	err := validator.ValidateWriteColumns("orders", []string{"user_id", "amount", "status"})
	if err != nil {
		t.Fatalf("expected nil for whitelisted columns, got: %v", err)
	}

	// All valid columns for events
	err = validator.ValidateWriteColumns("events", []string{"event_type", "payload"})
	if err != nil {
		t.Fatalf("expected nil for whitelisted columns, got: %v", err)
	}
}

// TestProperty5_AllowedOperationsEnforcement validates Property 5:
// For any operation type NOT in a table's allowed_operations, WritableTableValidator.ValidateOperation
// SHALL return a MUTATION_OPERATION_NOT_SUPPORTED error.
//
// **Validates: Requirements 3.6, 3.7**
func TestProperty5_AllowedOperationsEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := testWritableConfig()

		// Test tables with restricted operations:
		// "orders" allows [insert, update] — "delete" is disallowed
		// "events" allows [insert] — "update" and "delete" are disallowed
		type tableOps struct {
			table      string
			allowedOps map[string]bool
		}
		restrictedTables := []tableOps{
			{"orders", map[string]bool{"insert": true, "update": true}},
			{"events", map[string]bool{"insert": true}},
		}

		tblIdx := rapid.IntRange(0, len(restrictedTables)-1).Draw(t, "tableIdx")
		tbl := restrictedTables[tblIdx]

		// Generate a disallowed operation
		disallowedOp := genDisallowedOperation(t, "disallowedOp", tbl.allowedOps)
		if disallowedOp == "" {
			// All operations are allowed for this table — skip
			return
		}

		err := validator.ValidateOperation(tbl.table, disallowedOp)

		// Property: disallowed operation MUST produce an error
		if err == nil {
			t.Fatalf("expected error for disallowed operation %q on table %q, got nil",
				disallowedOp, tbl.table)
		}

		// Verify error message mentions the operation
		if !strings.Contains(err.Error(), disallowedOp) {
			t.Fatalf("error should reference the operation %q, got: %v", disallowedOp, err)
		}

		// Verify correct error code (MUTATION_OPERATION_NOT_SUPPORTED)
		if !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("error should indicate operation is not allowed, got: %v", err)
		}
	})
}

// TestProperty5_AllowedOperationsEnforcement_Positive validates that allowed operations pass.
func TestProperty5_AllowedOperationsEnforcement_Positive(t *testing.T) {
	validator := testWritableConfig()

	// orders allows insert and update
	for _, op := range []string{"insert", "update"} {
		err := validator.ValidateOperation("orders", op)
		if err != nil {
			t.Fatalf("expected nil for allowed operation %q on orders, got: %v", op, err)
		}
	}

	// events allows insert only
	err := validator.ValidateOperation("events", "insert")
	if err != nil {
		t.Fatalf("expected nil for allowed operation 'insert' on events, got: %v", err)
	}
}

// TestProperty18_FilterColumnScope validates Property 18:
// Filter fields are validated against allowed_tables (broader read whitelist),
// NOT against writable_tables[table].Columns. This allows filtering on read-only columns
// (e.g., WHERE order_id = ? even though order_id is not writable).
//
// **Validates: Requirements 3.2 (implicit — filter fields are read access)**
func TestProperty18_FilterColumnScope(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		validator := testWritableConfig()

		// The allowed_tables for "orders" include: order_id, user_id, amount, status, created_at, updated_at
		// The writable_tables for "orders" only include: user_id, amount, status
		// Filter fields should be validated against allowed_tables (the broader set)

		// Generate a column that IS in allowed_tables BUT NOT in writable_tables
		readOnlyColumns := []string{"order_id", "created_at", "updated_at"}
		colIdx := rapid.IntRange(0, len(readOnlyColumns)-1).Draw(t, "readOnlyColIdx")
		readOnlyCol := readOnlyColumns[colIdx]

		// ValidateFilterColumns should SUCCEED for read-only columns (validated against allowed_tables)
		err := validator.ValidateFilterColumns("orders", []string{readOnlyCol})
		if err != nil {
			t.Fatalf("expected nil for filter on allowed (read-only) column %q, got: %v",
				readOnlyCol, err)
		}

		// Generate a column that is NOT in allowed_tables at all
		notAllowedCol := genNonWhitelistedColumn(t, "notAllowedCol",
			map[string]bool{"order_id": true, "user_id": true, "amount": true, "status": true, "created_at": true, "updated_at": true})

		// ValidateFilterColumns should FAIL for columns not in allowed_tables
		err = validator.ValidateFilterColumns("orders", []string{notAllowedCol})
		if err == nil {
			t.Fatalf("expected error for filter on non-allowed column %q, got nil", notAllowedCol)
		}
	})
}

// TestProperty18_FilterColumnScope_WritableVsAllowed demonstrates that filter validation
// uses allowed_tables scope, not writable_tables scope.
func TestProperty18_FilterColumnScope_WritableVsAllowed(t *testing.T) {
	validator := testWritableConfig()

	// "users" table is in allowed_tables but NOT in writable_tables.
	// Filtering on "users" should work for ValidateFilterColumns.
	err := validator.ValidateFilterColumns("users", []string{"user_id", "username", "email"})
	if err != nil {
		t.Fatalf("expected nil for filter on allowed (non-writable) table 'users', got: %v", err)
	}

	// But ValidateTable and ValidateWriteColumns should reject "users" since it's not writable
	err = validator.ValidateTable("users")
	if err == nil {
		t.Fatal("expected error for non-writable table 'users' in ValidateTable, got nil")
	}
}
