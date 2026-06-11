// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// newTestAdapter creates an Adapter with minimal config for testing.
// The allowed tables whitelist includes "users" with columns "id" and "name".
func newTestAdapter(t *testing.T) (*Adapter, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	allowedTables := map[string]map[string]bool{
		"users": {"id": true, "name": true},
	}

	adapter := &Adapter{
		name: "test-clickhouse",
		config: datasource.DataSourceConfig{
			Name:    "test-clickhouse",
			Type:    "clickhouse",
			Enabled: true,
			Options: map[string]any{
				"pool_size": 5,
			},
		},
		queryBuilder: NewSQLQueryBuilder(allowedTables),
		typeMapper:   NewTypeMapper(zap.NewNop()),
		logger:       zap.NewNop(),
		db:           db,
		available:    true,
	}

	return adapter, mock
}

// --- NewAdapter tests ---

func TestNewAdapter_Success(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Name:    "test-ck",
		Type:    "clickhouse",
		Enabled: true,
		Options: map[string]any{
			"allowed_tables": map[string]any{
				"events": map[string]any{
					"columns": []any{"event_id", "user_id"},
				},
			},
		},
	}

	adapter, err := NewAdapter("test-ck", cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAdapter() returned error: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter() returned nil adapter")
	}
	if adapter.Name() != "test-ck" {
		t.Errorf("expected name %q, got %q", "test-ck", adapter.Name())
	}
}

func TestNewAdapter_WhitelistParseError(t *testing.T) {
	cfg := datasource.DataSourceConfig{
		Name:    "test-ck",
		Type:    "clickhouse",
		Enabled: true,
		Options: map[string]any{
			// missing allowed_tables → error
		},
	}

	_, err := NewAdapter("test-ck", cfg, zap.NewNop())
	if err == nil {
		t.Fatal("expected NewAdapter to return error for missing allowed_tables")
	}
}

// --- Name/Type tests ---

func TestName(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	if got := adapter.Name(); got != "test-clickhouse" {
		t.Errorf("Name() = %q, want %q", got, "test-clickhouse")
	}
}

func TestType(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	if got := adapter.Type(); got != "clickhouse" {
		t.Errorf("Type() = %q, want %q", got, "clickhouse")
	}
}

// --- Connect tests ---

func TestConnect_Success(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectPing()

	adapter := &Adapter{
		name: "test-clickhouse",
		config: datasource.DataSourceConfig{
			Name:    "test-clickhouse",
			Type:    "clickhouse",
			Enabled: true,
			Connection: map[string]any{
				"host":     "localhost",
				"port":     9000,
				"username": "default",
				"password": "",
				"database": "default",
			},
		},
		queryBuilder: NewSQLQueryBuilder(nil),
		typeMapper:   NewTypeMapper(zap.NewNop()),
		logger:       zap.NewNop(),
	}

	// Inject mock db directly to bypass real connection
	adapter.db = db
	adapter.available = true

	// Calling Connect when db is already set should be a no-op (idempotent)
	ctx := context.Background()
	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() returned error: %v", err)
	}

	if !adapter.IsAvailable() {
		t.Error("expected adapter to be available after Connect")
	}
}

func TestConnect_PingFail(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectPing().WillReturnError(fmt.Errorf("connection refused"))

	adapter := &Adapter{
		name: "test-clickhouse",
		config: datasource.DataSourceConfig{
			Name:    "test-clickhouse",
			Type:    "clickhouse",
			Enabled: true,
			Connection: map[string]any{
				"host":     "localhost",
				"port":     9000,
				"username": "default",
				"password": "",
				"database": "default",
			},
		},
		queryBuilder: NewSQLQueryBuilder(nil),
		typeMapper:   NewTypeMapper(zap.NewNop()),
		logger:       zap.NewNop(),
	}

	// Inject mock db and test HealthCheck for ping failure
	adapter.db = db
	adapter.available = true

	ctx := context.Background()
	err = adapter.HealthCheck(ctx)
	if err == nil {
		t.Fatal("expected HealthCheck to fail when ping fails")
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after ping failure")
	}
}

// --- Execute tests ---

func TestExecute_Success(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	columns := []string{"id", "name"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(1, "alice").
			AddRow(2, "bob"))

	ctx := context.Background()
	req := datasource.QueryRequest{
		Fields: []string{"id", "name"},
		Options: map[string]any{
			"table": "users",
		},
	}

	result, err := adapter.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if len(result.Data) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Data))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecute_WithCount(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	columns := []string{"id", "name"}
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(1, "alice"))

	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).
			AddRow(42))

	ctx := context.Background()
	req := datasource.QueryRequest{
		Fields:    []string{"id", "name"},
		NeedCount: true,
		Options: map[string]any{
			"table": "users",
		},
	}

	result, err := adapter.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if result.TotalCount == nil {
		t.Fatal("expected TotalCount to be set")
	}
	if *result.TotalCount != 42 {
		t.Errorf("expected TotalCount=42, got %d", *result.TotalCount)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecute_DBNil(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	adapter.db.Close()
	adapter.db = nil

	ctx := context.Background()
	req := datasource.QueryRequest{
		Fields: []string{"id", "name"},
		Options: map[string]any{
			"table": "users",
		},
	}

	_, err := adapter.Execute(ctx, req)
	if err == nil {
		t.Fatal("expected Execute to return error when db is nil")
	}
}

// --- ExecuteWrite tests ---

func TestExecuteWrite_Success(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("INSERT INTO").
		WithArgs("alice", 30).
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	affected, err := adapter.ExecuteWrite(ctx, "INSERT INTO users (name, age) VALUES (?, ?)", []any{"alice", 30})
	if err != nil {
		t.Fatalf("ExecuteWrite() returned error: %v", err)
	}

	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecuteWrite_CircuitBreakerOpen(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	// Create a circuit breaker and force it to OPEN state.
	cb := datasource.NewCircuitBreaker(datasource.CircuitBreakerConfig{
		FailureThreshold: 1,
		OpenDuration:     60_000_000_000, // 60 seconds — stays open for the test
	})
	// Trip the breaker by recording a failure.
	cb.RecordFailure()

	adapter.SetCircuitBreaker(cb)

	ctx := context.Background()
	_, err := adapter.ExecuteWrite(ctx, "INSERT INTO users (name) VALUES (?)", []any{"alice"})
	if err == nil {
		t.Fatal("expected ExecuteWrite to return error when circuit breaker is open")
	}
}

func TestExecuteWrite_DBNil(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	adapter.db.Close()
	adapter.db = nil

	ctx := context.Background()
	_, err := adapter.ExecuteWrite(ctx, "INSERT INTO users (name) VALUES (?)", []any{"alice"})
	if err == nil {
		t.Fatal("expected ExecuteWrite to return error when db is nil")
	}
}

// --- ExecuteRaw tests ---

func TestExecuteRaw_Success(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	columns := []string{"id", "name"}
	mock.ExpectQuery("SELECT id, name FROM users WHERE id = ?").
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows(columns).
			AddRow(1, "alice"))

	ctx := context.Background()
	result, err := adapter.ExecuteRaw(ctx, "SELECT id, name FROM users WHERE id = ?", 1)
	if err != nil {
		t.Fatalf("ExecuteRaw() returned error: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Data))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecuteRaw_DBNil(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	adapter.db.Close()
	adapter.db = nil

	ctx := context.Background()
	_, err := adapter.ExecuteRaw(ctx, "SELECT 1")
	if err == nil {
		t.Fatal("expected ExecuteRaw to return error when db is nil")
	}
}

// --- HealthCheck tests ---

func TestHealthCheck_Success(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectPing()

	ctx := context.Background()
	if err := adapter.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck() returned error: %v", err)
	}

	if !adapter.IsAvailable() {
		t.Error("expected adapter to be available after successful health check")
	}
}

func TestHealthCheck_Fail(t *testing.T) {
	adapter, mock := newTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectPing().WillReturnError(fmt.Errorf("connection lost"))

	ctx := context.Background()
	err := adapter.HealthCheck(ctx)
	if err == nil {
		t.Fatal("expected HealthCheck to return error")
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after failed health check")
	}
}

func TestHealthCheck_DBNil(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	adapter.db.Close()
	adapter.db = nil

	ctx := context.Background()
	err := adapter.HealthCheck(ctx)
	if err == nil {
		t.Fatal("expected HealthCheck to return error when db is nil")
	}
}

// --- IsAvailable tests ---

func TestIsAvailable_True(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	if !adapter.IsAvailable() {
		t.Error("expected adapter to be available")
	}
}

func TestIsAvailable_False(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	adapter.mu.Lock()
	adapter.available = false
	adapter.mu.Unlock()

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable")
	}
}

// --- Close tests ---

func TestClose(t *testing.T) {
	adapter, mock := newTestAdapter(t)

	mock.ExpectClose()

	ctx := context.Background()
	if err := adapter.Close(ctx); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	if adapter.IsAvailable() {
		t.Error("expected adapter to be unavailable after Close")
	}

	if adapter.db != nil {
		t.Error("expected adapter.db to be nil after Close")
	}
}

// --- SchemaFiles tests ---

func TestSchemaFiles_ReturnsNil(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	defer adapter.db.Close()

	files := adapter.SchemaFiles()
	if files != nil {
		t.Errorf("expected SchemaFiles() to return nil, got %v", files)
	}
}
