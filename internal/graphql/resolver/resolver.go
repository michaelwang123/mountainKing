// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

import (
	"context"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// CacheClearer is the interface needed by the mutation resolver for cache
// clearing. It allows clearing result cache entries by datasource prefix
// or clearing all result cache entries. The implementation must only clear
// result cache (cache: prefix) and not APQ cache (apq: prefix).
type CacheClearer interface {
	// ClearByDatasource clears cached results for the specified datasource.
	ClearByDatasource(ctx context.Context, datasource string) error
	// ClearAll clears all cached results.
	ClearAll(ctx context.Context) error
}

// Resolver is the root resolver struct that holds all dependencies
// required by the GraphQL resolvers.
type Resolver struct {
	// DSManager provides access to data source lifecycle and query execution.
	DSManager *datasource.DataSourceManager
	// GraphQLConfig holds GraphQL engine settings such as max_result_rows.
	GraphQLConfig config.GraphQLConfig
	// CacheClearer provides cache clearing capability for the clearCache
	// mutation. It can be nil when caching is disabled.
	CacheClearer CacheClearer
}
