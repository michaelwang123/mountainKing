// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// ParseAllowedTables extracts the allowed_tables whitelist from a DataSourceConfig's Options.
// Returns a map of table name → set of allowed column names.
// Returns an error if allowed_tables is missing, empty, or contains invalid identifiers.
//
// Expected Options format:
//
//	options:
//	  allowed_tables:
//	    orders:
//	      columns: [order_id, user_id, amount]
//	    users:
//	      columns: [user_id, username]
func ParseAllowedTables(cfg datasource.DataSourceConfig) (map[string]map[string]bool, error) {
	raw, ok := cfg.Options["allowed_tables"]
	if !ok {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"allowed_tables is missing from data source options")
	}

	tables, ok := raw.(map[string]any)
	if !ok {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"allowed_tables must be a map of table definitions")
	}

	if len(tables) == 0 {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"allowed_tables must not be empty")
	}

	result := make(map[string]map[string]bool, len(tables))

	for tableName, tableDef := range tables {
		if err := ValidateIdentifier(tableName); err != nil {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
				fmt.Sprintf("invalid table name %q: %v", tableName, err))
		}

		tableMap, ok := tableDef.(map[string]any)
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
				fmt.Sprintf("table %q definition must be a map with a columns key", tableName))
		}

		colsRaw, ok := tableMap["columns"]
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("table %q is missing the columns key", tableName))
		}

		colSlice, ok := colsRaw.([]any)
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("table %q columns must be a list", tableName))
		}

		if len(colSlice) == 0 {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("table %q must have at least one column", tableName))
		}

		cols := make(map[string]bool, len(colSlice))
		for _, c := range colSlice {
			colName, ok := c.(string)
			if !ok {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
					fmt.Sprintf("table %q column name must be a string", tableName))
			}
			if err := ValidateIdentifier(colName); err != nil {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
					fmt.Sprintf("table %q has invalid column name %q: %v", tableName, colName, err))
			}
			cols[colName] = true
		}

		result[tableName] = cols
	}

	return result, nil
}
