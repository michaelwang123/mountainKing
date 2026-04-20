// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package datasource

import "context"

// MockDataSource is a test helper that implements the DataSource interface.
// All methods delegate to configurable function fields, allowing tests to
// control behavior per-method. When a function field is nil, the method
// returns a sensible zero-value default.
type MockDataSource struct {
	// NameVal is the value returned by Name().
	NameVal string
	// TypeVal is the value returned by Type().
	TypeVal string
	// AvailableVal is the value returned by IsAvailable().
	AvailableVal bool
	// SchemaFilesVal is the value returned by SchemaFiles().
	SchemaFilesVal []string

	// ConnectFunc, when non-nil, is called by Connect.
	ConnectFunc func(ctx context.Context) error
	// ExecuteFunc, when non-nil, is called by Execute.
	ExecuteFunc func(ctx context.Context, query QueryRequest) (*QueryResult, error)
	// HealthCheckFunc, when non-nil, is called by HealthCheck.
	HealthCheckFunc func(ctx context.Context) error
	// CloseFunc, when non-nil, is called by Close.
	CloseFunc func(ctx context.Context) error
}

// Verify interface compliance at compile time.
var _ DataSource = (*MockDataSource)(nil)

// Name returns the configured data source name.
func (m *MockDataSource) Name() string { return m.NameVal }

// Type returns the configured data source type identifier.
func (m *MockDataSource) Type() string { return m.TypeVal }

// IsAvailable returns the configured availability flag.
func (m *MockDataSource) IsAvailable() bool { return m.AvailableVal }

// SchemaFiles returns the configured list of schema file paths.
func (m *MockDataSource) SchemaFiles() []string { return m.SchemaFilesVal }

// Connect delegates to ConnectFunc if set; otherwise returns nil.
func (m *MockDataSource) Connect(ctx context.Context) error {
	if m.ConnectFunc != nil {
		return m.ConnectFunc(ctx)
	}
	return nil
}

// Execute delegates to ExecuteFunc if set; otherwise returns an empty QueryResult.
func (m *MockDataSource) Execute(ctx context.Context, query QueryRequest) (*QueryResult, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, query)
	}
	return &QueryResult{}, nil
}

// HealthCheck delegates to HealthCheckFunc if set; otherwise returns nil.
func (m *MockDataSource) HealthCheck(ctx context.Context) error {
	if m.HealthCheckFunc != nil {
		return m.HealthCheckFunc(ctx)
	}
	return nil
}

// Close delegates to CloseFunc if set; otherwise returns nil.
func (m *MockDataSource) Close(ctx context.Context) error {
	if m.CloseFunc != nil {
		return m.CloseFunc(ctx)
	}
	return nil
}
