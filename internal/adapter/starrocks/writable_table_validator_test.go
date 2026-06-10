// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"testing"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

func newTestValidator() *WritableTableValidator {
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
	allowed := map[string]map[string]bool{
		"orders": {"order_id": true, "user_id": true, "amount": true, "status": true, "created_at": true},
		"events": {"event_id": true, "event_type": true, "payload": true, "created_at": true},
		"users":  {"user_id": true, "username": true, "email": true},
	}
	return NewWritableTableValidator(writable, allowed)
}

func TestWritableTableValidator_ValidateTable(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name    string
		table   string
		wantErr bool
		errCode string
	}{
		{name: "valid writable table", table: "orders", wantErr: false},
		{name: "valid writable table events", table: "events", wantErr: false},
		{name: "table not in writable list", table: "users", wantErr: true, errCode: apierrors.ErrValidationInvalidTable},
		{name: "non-existent table", table: "nonexistent", wantErr: true, errCode: apierrors.ErrValidationInvalidTable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateTable(tt.table)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				apiErr, ok := err.(*apierrors.APIError)
				if !ok {
					t.Fatalf("expected *APIError, got %T", err)
				}
				if apiErr.Code != tt.errCode {
					t.Errorf("expected code %q, got %q", tt.errCode, apiErr.Code)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWritableTableValidator_ValidateOperation(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name      string
		table     string
		operation string
		wantErr   bool
		errCode   string
	}{
		{name: "insert allowed on orders", table: "orders", operation: "insert", wantErr: false},
		{name: "update allowed on orders", table: "orders", operation: "update", wantErr: false},
		{name: "delete not allowed on orders", table: "orders", operation: "delete", wantErr: true, errCode: apierrors.ErrMutationOperationNotSupported},
		{name: "insert allowed on events", table: "events", operation: "insert", wantErr: false},
		{name: "update not allowed on events", table: "events", operation: "update", wantErr: true, errCode: apierrors.ErrMutationOperationNotSupported},
		{name: "delete not allowed on events", table: "events", operation: "delete", wantErr: true, errCode: apierrors.ErrMutationOperationNotSupported},
		{name: "non-existent table", table: "nonexistent", operation: "insert", wantErr: true, errCode: apierrors.ErrValidationInvalidTable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateOperation(tt.table, tt.operation)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				apiErr, ok := err.(*apierrors.APIError)
				if !ok {
					t.Fatalf("expected *APIError, got %T", err)
				}
				if apiErr.Code != tt.errCode {
					t.Errorf("expected code %q, got %q", tt.errCode, apiErr.Code)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWritableTableValidator_ValidateWriteColumns(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name    string
		table   string
		columns []string
		wantErr bool
		errCode string
	}{
		{name: "all valid columns", table: "orders", columns: []string{"user_id", "amount"}, wantErr: false},
		{name: "single valid column", table: "orders", columns: []string{"status"}, wantErr: false},
		{name: "column not in writable list", table: "orders", columns: []string{"order_id"}, wantErr: true, errCode: apierrors.ErrValidationInvalidField},
		{name: "mix of valid and invalid", table: "orders", columns: []string{"user_id", "order_id"}, wantErr: true, errCode: apierrors.ErrValidationInvalidField},
		{name: "non-existent table", table: "nonexistent", columns: []string{"col"}, wantErr: true, errCode: apierrors.ErrValidationInvalidTable},
		{name: "empty columns succeeds", table: "orders", columns: []string{}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateWriteColumns(tt.table, tt.columns)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				apiErr, ok := err.(*apierrors.APIError)
				if !ok {
					t.Fatalf("expected *APIError, got %T", err)
				}
				if apiErr.Code != tt.errCode {
					t.Errorf("expected code %q, got %q", tt.errCode, apiErr.Code)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWritableTableValidator_ValidateFilterColumns(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name    string
		table   string
		fields  []string
		wantErr bool
		errCode string
	}{
		{name: "filter on readable column", table: "orders", fields: []string{"order_id"}, wantErr: false},
		{name: "filter on writable column", table: "orders", fields: []string{"user_id"}, wantErr: false},
		{name: "multiple valid filter fields", table: "orders", fields: []string{"order_id", "status", "created_at"}, wantErr: false},
		{name: "filter field not in allowed_tables", table: "orders", fields: []string{"nonexistent_col"}, wantErr: true, errCode: apierrors.ErrValidationInvalidField},
		{name: "table in allowed but not writable", table: "users", fields: []string{"user_id"}, wantErr: false},
		{name: "non-existent table in allowed_tables", table: "nonexistent", fields: []string{"col"}, wantErr: true, errCode: apierrors.ErrValidationInvalidTable},
		{name: "empty fields succeeds", table: "orders", fields: []string{}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateFilterColumns(tt.table, tt.fields)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				apiErr, ok := err.(*apierrors.APIError)
				if !ok {
					t.Fatalf("expected *APIError, got %T", err)
				}
				if apiErr.Code != tt.errCode {
					t.Errorf("expected code %q, got %q", tt.errCode, apiErr.Code)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}
