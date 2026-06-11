// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

func TestParseAllowedTables_Valid(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{
					"columns": []any{"event_id", "user_id", "event_type", "created_at"},
				},
				"metrics": map[string]any{
					"columns": []any{"timestamp", "metric_name", "value"},
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

	eventsCols := result["events"]
	if len(eventsCols) != 4 {
		t.Errorf("expected 4 columns for events, got %d", len(eventsCols))
	}
	for _, col := range []string{"event_id", "user_id", "event_type", "created_at"} {
		if !eventsCols[col] {
			t.Errorf("expected column %q in events", col)
		}
	}

	metricsCols := result["metrics"]
	if len(metricsCols) != 3 {
		t.Errorf("expected 3 columns for metrics, got %d", len(metricsCols))
	}
}

func TestParseAllowedTables_UnderscorePrefix(t *testing.T) {
	// ClickHouse allows identifiers starting with underscore
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"_internal_table": map[string]any{
					"columns": []any{"_id", "_timestamp", "value"},
				},
			},
		},
	}

	result, err := ParseAllowedTables(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cols := result["_internal_table"]
	if len(cols) != 3 {
		t.Errorf("expected 3 columns for _internal_table, got %d", len(cols))
	}
	if !cols["_id"] {
		t.Error("expected column _id in _internal_table")
	}
}

func TestParseAllowedTables_MissingKey(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{},
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
		Options: map[string]any{
			"allowed_tables": map[string]any{},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for empty allowed_tables")
	}
}

func TestParseAllowedTables_InvalidTableName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"drop table;--": map[string]any{
					"columns": []any{"id"},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestParseAllowedTables_DigitPrefixTableName(t *testing.T) {
	// ClickHouse does NOT allow identifiers starting with a digit
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"1table": map[string]any{
					"columns": []any{"id"},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for table name starting with digit")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidTable {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidTable, apiErr.Code)
	}
}

func TestParseAllowedTables_DigitPrefixColumnName(t *testing.T) {
	// ClickHouse does NOT allow identifiers starting with a digit
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{
					"columns": []any{"9col"},
				},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for column name starting with digit")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidField {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidField, apiErr.Code)
	}
}

func TestParseAllowedTables_InvalidColumnName(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{
					"columns": []any{"valid_col", "bad col!"},
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
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{},
			},
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for missing columns key")
	}
	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != apierrors.ErrValidationInvalidField {
		t.Errorf("expected code %s, got %s", apierrors.ErrValidationInvalidField, apiErr.Code)
	}
}

func TestParseAllowedTables_EmptyColumns(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{
					"columns": []any{},
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
		Options: map[string]any{
			"allowed_tables": "not a map",
		},
	}

	_, err := ParseAllowedTables(cfg)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestValidateIdentifier_Valid(t *testing.T) {
	valid := []string{"table_name", "col1", "_private", "A", "_", "abc123", "_abc_123"}
	for _, id := range valid {
		if err := ValidateIdentifier(id); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", id, err)
		}
	}
}

func TestValidateIdentifier_Invalid(t *testing.T) {
	invalid := []string{"", "1table", "9col", "drop;--", "has space", "has-dash", "table.name", "col@x"}
	for _, id := range invalid {
		if err := ValidateIdentifier(id); err == nil {
			t.Errorf("expected %q to be invalid, got nil error", id)
		}
	}
}
