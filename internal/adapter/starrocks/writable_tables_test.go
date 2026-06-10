// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

func TestParseWritableTables_Valid(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns":            []any{"user_id", "amount", "status"},
					"allowed_operations": []any{"insert", "update"},
				},
				"events": map[string]any{
					"columns":            []any{"event_type", "payload"},
					"allowed_operations": []any{"insert"},
				},
			},
		},
	}

	result, err := ParseWritableTables(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(result))
	}

	// Check orders table
	orders := result["orders"]
	if orders == nil {
		t.Fatal("expected orders table in result")
	}
	if len(orders.Columns) != 3 {
		t.Errorf("expected 3 columns for orders, got %d", len(orders.Columns))
	}
	for _, col := range []string{"user_id", "amount", "status"} {
		if !orders.Columns[col] {
			t.Errorf("expected column %q in orders", col)
		}
	}
	if !orders.AllowedOperations["insert"] || !orders.AllowedOperations["update"] {
		t.Errorf("expected insert and update operations for orders")
	}
	if orders.AllowedOperations["delete"] {
		t.Errorf("expected delete to NOT be allowed for orders")
	}

	// Check events table
	events := result["events"]
	if events == nil {
		t.Fatal("expected events table in result")
	}
	if len(events.Columns) != 2 {
		t.Errorf("expected 2 columns for events, got %d", len(events.Columns))
	}
	if !events.AllowedOperations["insert"] {
		t.Errorf("expected insert operation for events")
	}
	if events.AllowedOperations["update"] || events.AllowedOperations["delete"] {
		t.Errorf("expected only insert for events, got update or delete")
	}
}

func TestParseWritableTables_DefaultOperations(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{"user_id", "amount"},
				},
			},
		},
	}

	result, err := ParseWritableTables(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	orders := result["orders"]
	if orders == nil {
		t.Fatal("expected orders table in result")
	}

	// Default should be all three operations
	for _, op := range []string{"insert", "update", "delete"} {
		if !orders.AllowedOperations[op] {
			t.Errorf("expected default operation %q for orders", op)
		}
	}
}

func TestParseWritableTables_MissingKey(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for missing writable_tables")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidTable {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidTable, apiErr.Code)
	}
}

func TestParseWritableTables_EmptyTables(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty writable_tables")
	}
}

func TestParseWritableTables_InvalidTableName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"drop table;--": map[string]any{
					"columns": []any{"id"},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestParseWritableTables_InvalidColumnName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{"valid_col", "bad col!"},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid column name")
	}
}

func TestParseWritableTables_MissingColumns(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for missing columns key")
	}
}

func TestParseWritableTables_EmptyColumns(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty columns")
	}
}

func TestParseWritableTables_WrongType(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": "not a map",
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestParseWritableTables_InvalidOperation(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns":            []any{"user_id"},
					"allowed_operations": []any{"insert", "truncate"},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
}

func TestParseWritableTables_EmptyOperations(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns":            []any{"user_id"},
					"allowed_operations": []any{},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty allowed_operations")
	}
}

func TestParseWritableTables_NonStringColumn(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns": []any{"user_id", 42},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for non-string column name")
	}
}

func TestParseWritableTables_NonStringOperation(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"writable_tables": map[string]any{
				"orders": map[string]any{
					"columns":            []any{"user_id"},
					"allowed_operations": []any{123},
				},
			},
		},
	}

	_, err := ParseWritableTables(cfg)
	if err == nil {
		t.Fatal("expected error for non-string operation")
	}
}

func TestValidateWritableSubset_Valid(t *testing.T) {
	writable := map[string]*WritableTableConfig{
		"orders": {
			Columns:           map[string]bool{"user_id": true, "amount": true},
			AllowedOperations: map[string]bool{"insert": true, "update": true},
		},
	}

	allowed := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true},
		"users":  {"user_id": true, "username": true},
	}

	err := ValidateWritableSubset(writable, allowed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateWritableSubset_TableNotInAllowed(t *testing.T) {
	writable := map[string]*WritableTableConfig{
		"secret_table": {
			Columns:           map[string]bool{"col1": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
	}

	allowed := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true},
	}

	err := ValidateWritableSubset(writable, allowed)
	if err == nil {
		t.Fatal("expected error when writable table is not in allowed_tables")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidTable {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidTable, apiErr.Code)
	}
}

func TestValidateWritableSubset_ColumnNotInAllowed(t *testing.T) {
	writable := map[string]*WritableTableConfig{
		"orders": {
			Columns:           map[string]bool{"user_id": true, "secret_col": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
	}

	allowed := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true},
	}

	err := ValidateWritableSubset(writable, allowed)
	if err == nil {
		t.Fatal("expected error when writable column is not in allowed_tables")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidField {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidField, apiErr.Code)
	}
}

func TestValidateWritableSubset_EmptyWritable(t *testing.T) {
	writable := map[string]*WritableTableConfig{}
	allowed := map[string]map[string]bool{
		"orders": {"order_id": true},
	}

	err := ValidateWritableSubset(writable, allowed)
	if err != nil {
		t.Fatalf("unexpected error for empty writable: %v", err)
	}
}

func TestValidateWritableSubset_MultipleTablesPartialMismatch(t *testing.T) {
	writable := map[string]*WritableTableConfig{
		"orders": {
			Columns:           map[string]bool{"user_id": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
		"events": {
			Columns:           map[string]bool{"missing_col": true},
			AllowedOperations: map[string]bool{"insert": true},
		},
	}

	allowed := map[string]map[string]bool{
		"orders": {"user_id": true, "amount": true},
		"events": {"event_id": true, "event_type": true},
	}

	err := ValidateWritableSubset(writable, allowed)
	if err == nil {
		t.Fatal("expected error when one writable table has a column not in allowed_tables")
	}
}
