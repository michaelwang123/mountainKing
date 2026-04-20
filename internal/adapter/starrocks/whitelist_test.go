// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

func TestParseAllowedTables_Valid(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{
				"orders": map[string]interface{}{
					"columns": []interface{}{"order_id", "user_id", "amount"},
				},
				"users": map[string]interface{}{
					"columns": []interface{}{"user_id", "username"},
				},
			},
		},
	}

	result, err := ParseAllowedTables(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(result))
	}

	ordersCols := result["orders"]
	if len(ordersCols) != 3 {
		t.Errorf("expected 3 columns for orders, got %d", len(ordersCols))
	}
	for _, col := range []string{"order_id", "user_id", "amount"} {
		if !ordersCols[col] {
			t.Errorf("expected column %q in orders", col)
		}
	}

	usersCols := result["users"]
	if len(usersCols) != 2 {
		t.Errorf("expected 2 columns for users, got %d", len(usersCols))
	}
}

func TestParseAllowedTables_MissingKey(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for missing allowed_tables")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidTable {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidTable, apiErr.Code)
	}
}

func TestParseAllowedTables_EmptyTables(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty allowed_tables")
	}
}

func TestParseAllowedTables_InvalidTableName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{
				"drop table;--": map[string]interface{}{
					"columns": []interface{}{"id"},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestParseAllowedTables_InvalidColumnName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{
				"orders": map[string]interface{}{
					"columns": []interface{}{"valid_col", "bad col!"},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid column name")
	}
}

func TestParseAllowedTables_MissingColumns(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{
				"orders": map[string]interface{}{},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for missing columns key")
	}
}

func TestParseAllowedTables_EmptyColumns(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": map[string]interface{}{
				"orders": map[string]interface{}{
					"columns": []interface{}{},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty columns")
	}
}

func TestParseAllowedTables_WrongType(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]interface{}{
			"allowed_tables": "not a map",
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}
