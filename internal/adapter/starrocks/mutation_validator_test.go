// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"strings"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

func TestValidateMutationIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple name", "orders", false},
		{"valid with underscore prefix", "_table", false},
		{"valid with digits", "table_123", false},
		{"valid uppercase", "MyTable", false},
		{"valid underscore only", "_", false},
		{"empty string", "", true},
		{"starts with digit", "1table", true},
		{"contains hyphen", "my-table", true},
		{"contains space", "my table", true},
		{"contains dot", "db.table", true},
		{"contains special char", "table$", true},
		{"contains backtick", "table`name", true},
		{"contains semicolon", "table;drop", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMutationIdentifier(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMutationIdentifier(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestMutationValidator_ValidateInsertInput(t *testing.T) {
	v := NewMutationValidator(500, 1048576)

	tests := []struct {
		name    string
		table   string
		columns []string
		values  []any
		wantErr bool
	}{
		{
			name:    "valid single column insert",
			table:   "orders",
			columns: []string{"amount"},
			values:  []any{100},
			wantErr: false,
		},
		{
			name:    "valid multi-column insert",
			table:   "orders",
			columns: []string{"user_id", "amount", "status"},
			values:  []any{1, 100, "pending"},
			wantErr: false,
		},
		{
			name:    "empty values",
			table:   "orders",
			columns: []string{},
			values:  []any{},
			wantErr: true,
		},
		{
			name:    "invalid table name",
			table:   "1invalid",
			columns: []string{"col"},
			values:  []any{"val"},
			wantErr: true,
		},
		{
			name:    "invalid column name with hyphen",
			table:   "orders",
			columns: []string{"user-id"},
			values:  []any{1},
			wantErr: true,
		},
		{
			name:    "empty table name",
			table:   "",
			columns: []string{"col"},
			values:  []any{"val"},
			wantErr: true,
		},
		{
			name:    "table starting with digit",
			table:   "9table",
			columns: []string{"col"},
			values:  []any{"val"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateInsertInput(tt.table, tt.columns, tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInsertInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMutationValidator_ValidateUpdateInput(t *testing.T) {
	v := NewMutationValidator(500, 1048576)

	tests := []struct {
		name    string
		table   string
		setCols []string
		filters []datasource.FilterCondition
		wantErr bool
	}{
		{
			name:    "valid update",
			table:   "orders",
			setCols: []string{"status"},
			filters: []datasource.FilterCondition{
				{Field: "order_id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: false,
		},
		{
			name:    "empty set columns",
			table:   "orders",
			setCols: []string{},
			filters: []datasource.FilterCondition{
				{Field: "order_id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
		{
			name:    "empty filters",
			table:   "orders",
			setCols: []string{"status"},
			filters: []datasource.FilterCondition{},
			wantErr: true,
		},
		{
			name:    "invalid table name",
			table:   "1invalid",
			setCols: []string{"status"},
			filters: []datasource.FilterCondition{
				{Field: "order_id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
		{
			name:    "invalid set column name",
			table:   "orders",
			setCols: []string{"user-id"},
			filters: []datasource.FilterCondition{
				{Field: "order_id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
		{
			name:    "invalid filter field name",
			table:   "orders",
			setCols: []string{"status"},
			filters: []datasource.FilterCondition{
				{Field: "order.id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateUpdateInput(tt.table, tt.setCols, tt.filters)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpdateInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMutationValidator_ValidateDeleteInput(t *testing.T) {
	v := NewMutationValidator(500, 1048576)

	tests := []struct {
		name    string
		table   string
		filters []datasource.FilterCondition
		wantErr bool
	}{
		{
			name:  "valid delete with single filter",
			table: "orders",
			filters: []datasource.FilterCondition{
				{Field: "order_id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: false,
		},
		{
			name:  "valid delete with multiple filters",
			table: "orders",
			filters: []datasource.FilterCondition{
				{Field: "user_id", Operator: datasource.FilterOpEQ, Value: 42},
				{Field: "status", Operator: datasource.FilterOpEQ, Value: "cancelled"},
			},
			wantErr: false,
		},
		{
			name:    "empty filters not permitted",
			table:   "orders",
			filters: []datasource.FilterCondition{},
			wantErr: true,
		},
		{
			name:  "invalid table name",
			table: "1table",
			filters: []datasource.FilterCondition{
				{Field: "id", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
		{
			name:  "invalid filter field name",
			table: "orders",
			filters: []datasource.FilterCondition{
				{Field: "bad-field", Operator: datasource.FilterOpEQ, Value: 1},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateDeleteInput(tt.table, tt.filters)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDeleteInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMutationValidator_ValidateBatchInsertInput(t *testing.T) {
	v := NewMutationValidator(3, 1048576) // maxBatchSize=3 for testing

	tests := []struct {
		name    string
		table   string
		columns []string
		rows    [][]any
		wantErr bool
	}{
		{
			name:    "valid batch insert",
			table:   "orders",
			columns: []string{"user_id", "amount"},
			rows: [][]any{
				{1, 100},
				{2, 200},
			},
			wantErr: false,
		},
		{
			name:    "batch size exceeds limit",
			table:   "orders",
			columns: []string{"user_id"},
			rows: [][]any{
				{1}, {2}, {3}, {4},
			},
			wantErr: true,
		},
		{
			name:    "empty columns",
			table:   "orders",
			columns: []string{},
			rows:    [][]any{{1}},
			wantErr: true,
		},
		{
			name:    "row width mismatch",
			table:   "orders",
			columns: []string{"user_id", "amount"},
			rows: [][]any{
				{1, 100},
				{2}, // wrong width
			},
			wantErr: true,
		},
		{
			name:    "invalid table name",
			table:   "1table",
			columns: []string{"col"},
			rows:    [][]any{{1}},
			wantErr: true,
		},
		{
			name:    "invalid column name",
			table:   "orders",
			columns: []string{"good_col", "bad-col"},
			rows:    [][]any{{1, 2}},
			wantErr: true,
		},
		{
			name:    "exactly at batch limit",
			table:   "orders",
			columns: []string{"user_id"},
			rows:    [][]any{{1}, {2}, {3}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateBatchInsertInput(tt.table, tt.columns, tt.rows)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBatchInsertInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMutationValidator_ValidateSQLLength(t *testing.T) {
	v := NewMutationValidator(500, 100) // maxSQLLength=100 for testing

	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name:    "short SQL passes",
			sql:     "INSERT INTO `orders` (`amount`) VALUES (?)",
			wantErr: false,
		},
		{
			name:    "exactly at limit",
			sql:     strings.Repeat("x", 100),
			wantErr: false,
		},
		{
			name:    "exceeds limit by one",
			sql:     strings.Repeat("x", 101),
			wantErr: true,
		},
		{
			name:    "empty SQL passes",
			sql:     "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateSQLLength(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSQLLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
