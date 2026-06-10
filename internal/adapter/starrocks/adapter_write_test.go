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
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// newWriteTestAdapter creates an Adapter with a circuit breaker for write tests.
func newWriteTestAdapter(t *testing.T) (*Adapter, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	allowedTables := map[string]map[string]bool{
		"users": {"id": true, "name": true, "email": true},
	}

	cb := datasource.NewCircuitBreaker(datasource.CircuitBreakerConfig{
		FailureThreshold: 5,
	})

	adapter := &Adapter{
		name: "test-starrocks",
		config: datasource.DataSourceConfig{
			Name:    "test-starrocks",
			Type:    "starrocks",
			Enabled: true,
		},
		queryBuilder:   NewSQLQueryBuilder(allowedTables),
		typeMapper:     NewTypeMapper(zap.NewNop()),
		logger:         zap.NewNop(),
		db:             db,
		available:      true,
		circuitBreaker: cb,
	}

	return adapter, mock
}

func TestExecuteWrite_Success(t *testing.T) {
	adapter, mock := newWriteTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("INSERT INTO").
		WithArgs("alice", "alice@example.com").
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`, `email`) VALUES (?, ?)"
	params := []any{"alice", "alice@example.com"}

	affected, err := adapter.ExecuteWrite(ctx, sql, params)
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

func TestExecuteWrite_MultipleRowsAffected(t *testing.T) {
	adapter, mock := newWriteTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("UPDATE").
		WithArgs("bob", int64(1), int64(2), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 3))

	ctx := context.Background()
	sql := "UPDATE `users` SET `name` = ? WHERE `id` IN (?, ?, ?)"
	params := []any{"bob", int64(1), int64(2), int64(3)}

	affected, err := adapter.ExecuteWrite(ctx, sql, params)
	if err != nil {
		t.Fatalf("ExecuteWrite() returned error: %v", err)
	}

	if affected != 3 {
		t.Errorf("expected 3 affected rows, got %d", affected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecuteWrite_NilDB_ReturnsUnavailable(t *testing.T) {
	adapter, _ := newWriteTestAdapter(t)
	adapter.db.Close()
	adapter.db = nil // Simulate not connected state.

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`) VALUES (?)"
	params := []any{"alice"}

	affected, err := adapter.ExecuteWrite(ctx, sql, params)
	if err == nil {
		t.Fatal("expected error when db is nil, got nil")
	}

	if affected != 0 {
		t.Errorf("expected 0 affected rows, got %d", affected)
	}

	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *apierrors.APIError, got %T", err)
	}

	if apiErr.Code != apierrors.ErrDatasourceUnavailable {
		t.Errorf("expected error code %q, got %q", apierrors.ErrDatasourceUnavailable, apiErr.Code)
	}
}

func TestExecuteWrite_SQLError_Propagated(t *testing.T) {
	adapter, mock := newWriteTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("INSERT INTO").
		WithArgs("alice").
		WillReturnError(fmt.Errorf("duplicate entry 'alice' for key 'name'"))

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`) VALUES (?)"
	params := []any{"alice"}

	affected, err := adapter.ExecuteWrite(ctx, sql, params)
	if err == nil {
		t.Fatal("expected error on SQL failure, got nil")
	}

	if affected != 0 {
		t.Errorf("expected 0 affected rows, got %d", affected)
	}

	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *apierrors.APIError, got %T", err)
	}

	if apiErr.Code != apierrors.ErrDatasourceQueryError {
		t.Errorf("expected error code %q, got %q", apierrors.ErrDatasourceQueryError, apiErr.Code)
	}
}

func TestExecuteWrite_CircuitBreakerOpen_ReturnsError(t *testing.T) {
	adapter, _ := newWriteTestAdapter(t)
	defer adapter.db.Close()

	// Trip the circuit breaker by recording enough failures.
	for i := 0; i < 5; i++ {
		adapter.circuitBreaker.RecordFailure()
	}

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`) VALUES (?)"
	params := []any{"alice"}

	affected, err := adapter.ExecuteWrite(ctx, sql, params)
	if err == nil {
		t.Fatal("expected error when circuit breaker is open, got nil")
	}

	if affected != 0 {
		t.Errorf("expected 0 affected rows, got %d", affected)
	}

	apiErr, ok := err.(*apierrors.APIError)
	if !ok {
		t.Fatalf("expected *apierrors.APIError, got %T", err)
	}

	if apiErr.Code != apierrors.ErrDatasourceCircuitOpen {
		t.Errorf("expected error code %q, got %q", apierrors.ErrDatasourceCircuitOpen, apiErr.Code)
	}
}

func TestExecuteWrite_CircuitBreakerRecordsFailure(t *testing.T) {
	adapter, mock := newWriteTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("INSERT INTO").
		WithArgs("alice").
		WillReturnError(fmt.Errorf("connection reset"))

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`) VALUES (?)"
	params := []any{"alice"}

	_, err := adapter.ExecuteWrite(ctx, sql, params)
	if err == nil {
		t.Fatal("expected error on SQL failure")
	}

	// After 1 failure, circuit breaker should still be closed (threshold=5).
	if adapter.circuitBreaker.State() != datasource.CircuitClosed {
		t.Errorf("expected circuit state CLOSED after 1 failure, got %s", adapter.circuitBreaker.State())
	}

	// Record 4 more failures to trip the breaker.
	for i := 0; i < 4; i++ {
		mock.ExpectExec("INSERT INTO").
			WithArgs("alice").
			WillReturnError(fmt.Errorf("connection reset"))

		_, _ = adapter.ExecuteWrite(ctx, sql, params)
	}

	if adapter.circuitBreaker.State() != datasource.CircuitOpen {
		t.Errorf("expected circuit state OPEN after 5 failures, got %s", adapter.circuitBreaker.State())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestExecuteWrite_CircuitBreakerRecordsSuccess(t *testing.T) {
	adapter, mock := newWriteTestAdapter(t)
	defer adapter.db.Close()

	mock.ExpectExec("INSERT INTO").
		WithArgs("alice").
		WillReturnResult(sqlmock.NewResult(1, 1))

	ctx := context.Background()
	sql := "INSERT INTO `users` (`name`) VALUES (?)"
	params := []any{"alice"}

	_, err := adapter.ExecuteWrite(ctx, sql, params)
	if err != nil {
		t.Fatalf("ExecuteWrite() returned unexpected error: %v", err)
	}

	// After success, circuit breaker should remain closed.
	if adapter.circuitBreaker.State() != datasource.CircuitClosed {
		t.Errorf("expected circuit state CLOSED after success, got %s", adapter.circuitBreaker.State())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
