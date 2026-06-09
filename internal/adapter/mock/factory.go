// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package mock

import "github.com/michaelwang123/mountainKing/internal/datasource"

// Factory returns an AdapterFactory that creates mock DataSource instances.
// The returned factory ignores the configuration parameter because the mock
// adapter uses pre-defined in-memory data and requires no external setup.
func Factory() datasource.AdapterFactory {
	return func(name string, _ datasource.DataSourceConfig) (datasource.DataSource, error) {
		return &Adapter{
			name:   name,
			tables: defaultTables(),
		}, nil
	}
}
