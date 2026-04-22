// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

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
		name: "test-starrocks",
		config: datasource.DataSourceConfig{
			Name:    "test-starrocks",
			Type:    "starrocks",
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

func TestConnect_Success(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectPing()

	adapter := &Adapter{
		name: "test-starrocks",
		config: datasource.DataSourceConfig{
			Name:    "test-starrocks",
			Type:    "starrocks",
			Enabled: true,
			Connection: map[string]any{
				"host":     "localhost",
				"port":     9030,
				"username": "root",
				"password": "",
				"database": "test",
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
		name: "test-starrocks",
		config: datasource.DataSourceConfig{
			Name:    "test-starrocks",
			Type:    "starrocks",
			Enabled: true,
			Connection: map[string]any{
				"host":     "localhost",
				"port":     9030,
				"username": "root",
				"password": "",
				"database": "test",
			},
		},
		queryBuilder: NewSQLQueryBuilder(nil),
		typeMapper:   NewTypeMapper(zap.NewNop()),
		logger:       zap.NewNop(),
	}

	// We need to test the actual ping failure path.
	// Set db to nil so Connect will try to open and ping.
	// But since we can't intercept sql.Open with sqlmock easily,
	// we test HealthCheck for ping failure instead.
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
