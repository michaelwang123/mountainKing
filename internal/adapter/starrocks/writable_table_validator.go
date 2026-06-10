// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// WritableTableValidator enforces table/column whitelist and operation restrictions
// for mutation operations.
type WritableTableValidator struct {
	writableTables map[string]*WritableTableConfig
	allowedTables  map[string]map[string]bool // broader read whitelist (for filter field validation)
}

// NewWritableTableValidator creates a validator from parsed writable table configs
// and the broader allowed_tables read whitelist.
func NewWritableTableValidator(
	writable map[string]*WritableTableConfig,
	allowed map[string]map[string]bool,
) *WritableTableValidator {
	return &WritableTableValidator{
		writableTables: writable,
		allowedTables:  allowed,
	}
}

// ValidateTable checks that the table exists in writable_tables.
func (v *WritableTableValidator) ValidateTable(table string) error {
	if _, ok := v.writableTables[table]; !ok {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("table %q is not in the writable tables whitelist", table))
	}
	return nil
}

// ValidateOperation checks that the operation type is permitted for the table.
// The operation must be one of "insert", "update", "delete".
func (v *WritableTableValidator) ValidateOperation(table string, operation string) error {
	cfg, ok := v.writableTables[table]
	if !ok {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("table %q is not in the writable tables whitelist", table))
	}
	if !cfg.AllowedOperations[operation] {
		return apierrors.NewAPIError(apierrors.ErrMutationOperationNotSupported,
			fmt.Sprintf("operation %q is not allowed on table %q", operation, table), 400)
	}
	return nil
}

// ValidateWriteColumns checks that all columns exist in writable_tables[table].Columns.
// Used for INSERT SET columns and batch INSERT columns.
func (v *WritableTableValidator) ValidateWriteColumns(table string, columns []string) error {
	cfg, ok := v.writableTables[table]
	if !ok {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("table %q is not in the writable tables whitelist", table))
	}
	for _, col := range columns {
		if !cfg.Columns[col] {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("column %q is not in the writable whitelist for table %q", col, table))
		}
	}
	return nil
}

// ValidateFilterColumns checks filter fields against allowed_tables (broader read whitelist).
// UPDATE/DELETE filters can reference any readable column, not just writable columns.
func (v *WritableTableValidator) ValidateFilterColumns(table string, fields []string) error {
	allowedCols, ok := v.allowedTables[table]
	if !ok {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("table %q is not in the allowed tables whitelist", table))
	}
	for _, field := range fields {
		if !allowedCols[field] {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("filter field %q is not in the allowed columns for table %q", field, table))
		}
	}
	return nil
}
