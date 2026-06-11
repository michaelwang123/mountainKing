// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestScanRows_Basic(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	columns := []string{"id", "name", "value"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(1, "alice", 3.14).
			AddRow(2, "bob", 2.71))

	rows, err := db.Query("SELECT id, name, value FROM test")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		t.Fatalf("scanRows() returned error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}

	if result[0]["name"] != "alice" {
		t.Errorf("expected name=alice, got %v", result[0]["name"])
	}
	if result[1]["name"] != "bob" {
		t.Errorf("expected name=bob, got %v", result[1]["name"])
	}
}

func TestScanRows_ByteToString(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	columns := []string{"data"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow([]byte("hello bytes")))

	rows, err := db.Query("SELECT data FROM test")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		t.Fatalf("scanRows() returned error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}

	val, ok := result[0]["data"].(string)
	if !ok {
		t.Fatalf("expected string type, got %T", result[0]["data"])
	}
	if val != "hello bytes" {
		t.Errorf("expected 'hello bytes', got %q", val)
	}
}

func TestScanRows_TimeToRFC3339(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	ts := time.Date(2025, 6, 15, 10, 30, 0, 123456789, time.UTC)
	columns := []string{"created_at"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(ts))

	rows, err := db.Query("SELECT created_at FROM test")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		t.Fatalf("scanRows() returned error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}

	val, ok := result[0]["created_at"].(string)
	if !ok {
		t.Fatalf("expected string type for time.Time, got %T", result[0]["created_at"])
	}

	expected := ts.Format(time.RFC3339Nano)
	if val != expected {
		t.Errorf("expected %q, got %q", expected, val)
	}
}

func TestScanRows_NilTimePointer(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	columns := []string{"deleted_at"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(nil))

	rows, err := db.Query("SELECT deleted_at FROM test")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		t.Fatalf("scanRows() returned error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}

	if result[0]["deleted_at"] != nil {
		t.Errorf("expected nil for null time pointer, got %v", result[0]["deleted_at"])
	}
}

func TestScanRows_EmptyResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	columns := []string{"id", "name"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns))

	rows, err := db.Query("SELECT id, name FROM test")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	result, err := scanRows(rows)
	if err != nil {
		t.Fatalf("scanRows() returned error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty slice for empty result, got %v", result)
	}
	if result == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
}

func TestConvertValue_Byte(t *testing.T) {
	input := []byte("test data")
	result := convertValue(input)

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "test data" {
		t.Errorf("expected 'test data', got %q", s)
	}
}

func TestConvertValue_Time(t *testing.T) {
	ts := time.Date(2025, 1, 15, 8, 30, 45, 0, time.UTC)
	result := convertValue(ts)

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	expected := "2025-01-15T08:30:45Z"
	if s != expected {
		t.Errorf("expected %q, got %q", expected, s)
	}
}

func TestConvertValue_TimeWithNano(t *testing.T) {
	ts := time.Date(2025, 1, 15, 8, 30, 45, 123456789, time.UTC)
	result := convertValue(ts)

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	expected := "2025-01-15T08:30:45.123456789Z"
	if s != expected {
		t.Errorf("expected %q, got %q", expected, s)
	}
}

func TestConvertValue_TimePointerNil(t *testing.T) {
	var tp *time.Time
	result := convertValue(tp)

	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestConvertValue_TimePointerNonNil(t *testing.T) {
	ts := time.Date(2025, 3, 20, 14, 0, 0, 0, time.UTC)
	result := convertValue(&ts)

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}

	expected := "2025-03-20T14:00:00Z"
	if s != expected {
		t.Errorf("expected %q, got %q", expected, s)
	}
}

func TestConvertValue_Passthrough(t *testing.T) {
	tests := []struct {
		name  string
		input any
	}{
		{"int", 42},
		{"float64", 3.14},
		{"string", "hello"},
		{"bool", true},
		{"nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertValue(tt.input)
			if result != tt.input {
				t.Errorf("expected %v, got %v", tt.input, result)
			}
		})
	}
}
