// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// defaultAllowedOperations defines the default set of allowed mutation operations
// when no explicit allowed_operations list is provided in the configuration.
var defaultAllowedOperations = []string{"insert", "update", "delete"}

// WritableTableConfig holds the writable whitelist for a single table.
type WritableTableConfig struct {
	Columns           map[string]bool
	AllowedOperations map[string]bool // "insert", "update", "delete"
}

// ParseWritableTables extracts the writable_tables whitelist from a DataSourceConfig's Options.
// Returns a map of table name → WritableTableConfig.
// Returns an error if writable_tables is missing, empty, or contains invalid identifiers.
//
// Expected Options format:
//
//	options:
//	  writable_tables:
//	    orders:
//	      columns: [user_id, amount, status]
//	      allowed_operations: [insert, update]
//	    events:
//	      columns: [event_type, payload, created_at]
//	      allowed_operations: [insert]
func ParseWritableTables(cfg datasource.DataSourceConfig) (map[string]*WritableTableConfig, error) {
	raw, ok := cfg.Options["writable_tables"]
	if !ok {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"writable_tables is missing from data source options")
	}

	tables, ok := raw.(map[string]any)
	if !ok {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"writable_tables must be a map of table definitions")
	}

	if len(tables) == 0 {
		return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			"writable_tables must not be empty")
	}

	result := make(map[string]*WritableTableConfig, len(tables))

	for tableName, tableDef := range tables {
		if err := ValidateIdentifier(tableName); err != nil {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
				fmt.Sprintf("invalid writable table name %q: %v", tableName, err))
		}

		tableMap, ok := tableDef.(map[string]any)
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
				fmt.Sprintf("writable table %q definition must be a map with a columns key", tableName))
		}

		// Parse columns
		colsRaw, ok := tableMap["columns"]
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("writable table %q is missing the columns key", tableName))
		}

		colSlice, ok := colsRaw.([]any)
		if !ok {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("writable table %q columns must be a list", tableName))
		}

		if len(colSlice) == 0 {
			return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("writable table %q must have at least one column", tableName))
		}

		cols := make(map[string]bool, len(colSlice))
		for _, c := range colSlice {
			colName, ok := c.(string)
			if !ok {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
					fmt.Sprintf("writable table %q column name must be a string", tableName))
			}
			if err := ValidateIdentifier(colName); err != nil {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidField,
					fmt.Sprintf("writable table %q has invalid column name %q: %v", tableName, colName, err))
			}
			cols[colName] = true
		}

		// Parse allowed_operations (optional, defaults to all operations)
		allowedOps := make(map[string]bool, 3)
		if opsRaw, ok := tableMap["allowed_operations"]; ok {
			opsSlice, ok := opsRaw.([]any)
			if !ok {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
					fmt.Sprintf("writable table %q allowed_operations must be a list", tableName))
			}

			if len(opsSlice) == 0 {
				return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
					fmt.Sprintf("writable table %q allowed_operations must not be empty", tableName))
			}

			for _, op := range opsSlice {
				opStr, ok := op.(string)
				if !ok {
					return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
						fmt.Sprintf("writable table %q allowed_operations entry must be a string", tableName))
				}
				if opStr != "insert" && opStr != "update" && opStr != "delete" {
					return nil, apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
						fmt.Sprintf("writable table %q has invalid operation %q: must be one of [insert, update, delete]", tableName, opStr))
				}
				allowedOps[opStr] = true
			}
		} else {
			// Default to all operations
			for _, op := range defaultAllowedOperations {
				allowedOps[op] = true
			}
		}

		result[tableName] = &WritableTableConfig{
			Columns:           cols,
			AllowedOperations: allowedOps,
		}
	}

	return result, nil
}

// ValidateWritableSubset ensures every writable table/column is also present in allowed_tables.
// A writable table must be readable before it can be writable.
// Called at startup — fails fast on invalid config.
func ValidateWritableSubset(writable map[string]*WritableTableConfig, allowed map[string]map[string]bool) error {
	for tableName, tableConfig := range writable {
		allowedCols, ok := allowed[tableName]
		if !ok {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
				fmt.Sprintf("writable table %q is not present in allowed_tables", tableName))
		}

		for colName := range tableConfig.Columns {
			if !allowedCols[colName] {
				return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
					fmt.Sprintf("writable column %q in table %q is not present in allowed_tables", colName, tableName))
			}
		}
	}

	return nil
}
