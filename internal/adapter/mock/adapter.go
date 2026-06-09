// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package mock implements an in-memory mock data source adapter for development mode.
// It satisfies the datasource.DataSource interface and returns pre-defined sample data,
// allowing the server to start without any external database dependency.
package mock

import (
	"context"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// Adapter implements the datasource.DataSource interface using in-memory data.
type Adapter struct {
	name   string
	tables map[string][]map[string]any
}

// Verify interface compliance at compile time.
var _ datasource.DataSource = (*Adapter)(nil)

// NewAdapter creates a new mock adapter with the given name and table data.
func NewAdapter(name string, tables map[string][]map[string]any) *Adapter {
	return &Adapter{
		name:   name,
		tables: tables,
	}
}

// Name returns the data source name.
func (a *Adapter) Name() string {
	return a.name
}

// Type returns "mock".
func (a *Adapter) Type() string {
	return "mock"
}

// Connect is a no-op for the mock adapter (always succeeds).
func (a *Adapter) Connect(ctx context.Context) error {
	return nil
}

// IsAvailable always returns true for the mock adapter.
func (a *Adapter) IsAvailable() bool {
	return true
}

// Execute runs a query against the in-memory mock data.
// It reads the table name from Options["table"] (matching StarRocks resolver parameter format),
// looks up pre-defined data, and supports Pagination (Limit/Offset) and NeedCount → TotalCount.
func (a *Adapter) Execute(ctx context.Context, query datasource.QueryRequest) (*datasource.QueryResult, error) {
	// 从 Options["table"] 获取表名（与 StarRocks resolver 传参一致）
	tableName, _ := query.Options["table"].(string)
	data, exists := a.tables[tableName]
	if !exists {
		return &datasource.QueryResult{Data: nil}, nil // 未知表返回空
	}

	total := int64(len(data))

	// 应用 Pagination (Offset/Limit)，防御负数和越界
	if query.Pagination != nil {
		if query.Pagination.Offset != nil {
			offset := *query.Pagination.Offset
			if offset < 0 {
				offset = 0
			}
			if offset >= len(data) {
				data = nil
			} else {
				data = data[offset:]
			}
		}
		if data != nil && query.Pagination.Limit != nil {
			limit := *query.Pagination.Limit
			if limit < 0 {
				limit = 0
			}
			if limit < len(data) {
				data = data[:limit]
			}
		}
	}

	result := &datasource.QueryResult{Data: data}
	if query.NeedCount {
		result.TotalCount = &total
	}
	return result, nil
}

// HealthCheck always returns nil for the mock adapter.
func (a *Adapter) HealthCheck(ctx context.Context) error {
	return nil
}

// SchemaFiles returns nil; the mock adapter reuses existing schema files.
func (a *Adapter) SchemaFiles() []string {
	return nil
}

// Close is a no-op for the mock adapter.
func (a *Adapter) Close(ctx context.Context) error {
	return nil
}
