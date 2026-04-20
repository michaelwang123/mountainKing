// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package datasource defines the core interfaces and types for the multi-datasource
// adapter architecture. All data source adapters (e.g., StarRocks, Prometheus) must
// implement the DataSource interface defined in this package.
package datasource

import "context"

// FilterOperator represents the type of comparison operation used in a filter condition.
type FilterOperator int

const (
	// FilterOpEQ represents an equality comparison (=).
	FilterOpEQ FilterOperator = iota
	// FilterOpNEQ represents a not-equal comparison (!=).
	FilterOpNEQ
	// FilterOpGT represents a greater-than comparison (>).
	FilterOpGT
	// FilterOpGTE represents a greater-than-or-equal comparison (>=).
	FilterOpGTE
	// FilterOpLT represents a less-than comparison (<).
	FilterOpLT
	// FilterOpLTE represents a less-than-or-equal comparison (<=).
	FilterOpLTE
	// FilterOpLIKE represents a pattern matching comparison (LIKE).
	FilterOpLIKE
	// FilterOpIN represents a set membership check (IN).
	FilterOpIN
	// FilterOpNOT_IN represents a negated set membership check (NOT IN).
	FilterOpNOT_IN
	// FilterOpIS_NULL represents a null check (IS NULL).
	FilterOpIS_NULL
	// FilterOpIS_NOT_NULL represents a non-null check (IS NOT NULL).
	FilterOpIS_NOT_NULL
)

// SortDirection represents the ordering direction for query results.
type SortDirection int

const (
	// SortASC represents ascending sort order.
	SortASC SortDirection = iota
	// SortDESC represents descending sort order.
	SortDESC
)

// DataSource defines the unified interface that all data source adapters must implement.
// It provides methods for connection management, query execution, health checking,
// schema provisioning, and resource cleanup.
type DataSource interface {
	// Name returns the data source name as configured (the "name" field in config).
	Name() string

	// Type returns the data source type identifier (e.g., "starrocks", "prometheus").
	Type() string

	// Connect establishes a connection to the data source. This operation is idempotent;
	// calling Connect on an already-connected data source is a no-op.
	Connect(ctx context.Context) error

	// IsAvailable reports whether the data source is currently reachable and healthy.
	IsAvailable() bool

	// Execute runs a query against the data source and returns the result.
	Execute(ctx context.Context, query QueryRequest) (*QueryResult, error)

	// HealthCheck verifies the data source connection is alive and responsive.
	HealthCheck(ctx context.Context) error

	// SchemaFiles returns the list of .graphql schema file paths provided by this adapter.
	SchemaFiles() []string

	// Close shuts down the connection and releases all associated resources.
	Close(ctx context.Context) error
}

// DataSourceConfig holds the configuration for instantiating a data source adapter.
type DataSourceConfig struct {
	// Name is the unique identifier for this data source instance.
	Name string
	// Type is the adapter type identifier used to look up the factory in the registry.
	Type string
	// Enabled indicates whether this data source should be initialized at startup.
	Enabled bool
	// Connection contains adapter-specific connection parameters (host, port, credentials, etc.).
	Connection map[string]interface{}
	// Options contains adapter-specific custom options (pool size, timeouts, etc.).
	Options map[string]interface{}
}

// AdapterFactory is a function type that creates a new DataSource instance from a name
// and configuration. Each adapter registers its factory with the AdapterRegistry.
type AdapterFactory func(name string, config DataSourceConfig) (DataSource, error)

// FilterCondition represents a single filter predicate applied to query results.
type FilterCondition struct {
	// Field is the name of the field to filter on.
	Field string
	// Operator is the comparison operator (EQ, NEQ, GT, GTE, LT, LTE, LIKE, IN, NOT_IN, IS_NULL, IS_NOT_NULL).
	Operator FilterOperator
	// Value is the filter value to compare against. The concrete type depends on the operator.
	Value interface{}
}

// OrderByClause specifies a single sort criterion for query results.
type OrderByClause struct {
	// Field is the name of the field to sort by.
	Field string
	// Direction is the sort direction (ASC or DESC).
	Direction SortDirection
}

// PaginationParams holds pagination parameters supporting both Relay-style cursor
// pagination and traditional offset/limit pagination.
type PaginationParams struct {
	// First specifies the number of items to return (Relay-style).
	First *int
	// After is the cursor indicating the starting point (Relay-style).
	After *string
	// Offset specifies the number of items to skip (traditional pagination).
	Offset *int
	// Limit specifies the maximum number of items to return (traditional pagination).
	Limit *int
}

// QueryRequest represents a unified query sent to a data source adapter.
type QueryRequest struct {
	// Fields lists the requested field names for field-selection optimization.
	Fields []string
	// Filters contains the filter conditions to apply.
	Filters []FilterCondition
	// OrderBy contains the sort criteria to apply.
	OrderBy []OrderByClause
	// Pagination holds the pagination parameters, or nil if no pagination is needed.
	Pagination *PaginationParams
	// NeedCount indicates whether the total record count should be computed and returned.
	NeedCount bool
	// Options holds data-source-specific parameters (e.g., Prometheus query, startTime, endTime, step).
	Options map[string]interface{}
}

// QueryResult holds the unified result returned by a data source query.
type QueryResult struct {
	// Data contains the result rows, each represented as a field-name to value map.
	Data []map[string]interface{}
	// TotalCount holds the total number of matching records when QueryRequest.NeedCount is true.
	// It is nil when the count was not requested.
	TotalCount *int64
	// Warnings contains informational messages such as special value conversions or result truncation notices.
	Warnings []string
}
