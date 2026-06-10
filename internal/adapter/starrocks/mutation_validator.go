// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"fmt"
	"regexp"

	"github.com/michaelwang123/mountainKing/internal/datasource"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// mutationIdentifierRe is stricter than the existing query identifierRe.
// Existing query builder uses ^[a-zA-Z0-9_]+$ (allows digit start) for backward compat.
// Mutations use ^[a-zA-Z_][a-zA-Z0-9_]*$ (standard SQL identifier: must start with letter/underscore).
var mutationIdentifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// MutationValidator validates mutation inputs before SQL construction.
type MutationValidator struct {
	maxBatchSize int
	maxSQLLength int
}

// NewMutationValidator creates a validator from mutation config limits.
func NewMutationValidator(maxBatchSize, maxSQLLength int) *MutationValidator {
	return &MutationValidator{
		maxBatchSize: maxBatchSize,
		maxSQLLength: maxSQLLength,
	}
}

// maxIdentifierLength is the maximum allowed length for SQL identifiers in mutations.
// This prevents regex DoS on excessively long strings and aligns with MySQL's 64-char limit.
const maxIdentifierLength = 128

// ValidateMutationIdentifier checks that name matches ^[a-zA-Z_][a-zA-Z0-9_]*$ and is
// within the maximum length limit. This is intentionally stricter than the existing
// ValidateIdentifier() in query_builder.go which uses ^[a-zA-Z0-9_]+$ for backward
// compatibility with existing read queries.
func ValidateMutationIdentifier(name string) error {
	if name == "" {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"identifier must not be empty")
	}
	if len(name) > maxIdentifierLength {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			fmt.Sprintf("identifier length %d exceeds maximum allowed %d", len(name), maxIdentifierLength))
	}
	if !mutationIdentifierRe.MatchString(name) {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			fmt.Sprintf("identifier %q does not match required pattern [a-zA-Z_][a-zA-Z0-9_]*", name))
	}
	return nil
}

// ValidateInsertInput validates inputs for a single INSERT mutation:
// - columns and values must have equal length
// - values must not be empty
// - table and all columns must be valid identifiers
func (v *MutationValidator) ValidateInsertInput(table string, columns []string, values []any) error {
	if err := ValidateMutationIdentifier(table); err != nil {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("invalid table name: %s", err.Error()))
	}

	if len(values) == 0 {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"insert values must not be empty")
	}

	if len(columns) != len(values) {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			fmt.Sprintf("columns count (%d) must equal values count (%d)", len(columns), len(values)))
	}

	for _, col := range columns {
		if err := ValidateMutationIdentifier(col); err != nil {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("invalid column name: %s", err.Error()))
		}
	}

	return nil
}

// ValidateUpdateInput validates inputs for an UPDATE mutation:
// - set columns must not be empty
// - filters must not be empty (unfiltered updates not allowed)
// - table, set columns, and filter fields must be valid identifiers
func (v *MutationValidator) ValidateUpdateInput(table string, setCols []string, filters []datasource.FilterCondition) error {
	if err := ValidateMutationIdentifier(table); err != nil {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("invalid table name: %s", err.Error()))
	}

	if len(setCols) == 0 {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"update SET columns must not be empty")
	}

	if len(filters) == 0 {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"update filters must not be empty; unfiltered updates are not permitted")
	}

	for _, col := range setCols {
		if err := ValidateMutationIdentifier(col); err != nil {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("invalid SET column name: %s", err.Error()))
		}
	}

	for _, f := range filters {
		if err := ValidateMutationIdentifier(f.Field); err != nil {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("invalid filter field name: %s", err.Error()))
		}
	}

	return nil
}

// ValidateDeleteInput validates inputs for a DELETE mutation:
// - filters must not be empty (unfiltered deletes not allowed)
// - table and filter fields must be valid identifiers
func (v *MutationValidator) ValidateDeleteInput(table string, filters []datasource.FilterCondition) error {
	if err := ValidateMutationIdentifier(table); err != nil {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("invalid table name: %s", err.Error()))
	}

	if len(filters) == 0 {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"delete filters must not be empty; unfiltered deletes are not permitted")
	}

	for _, f := range filters {
		if err := ValidateMutationIdentifier(f.Field); err != nil {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("invalid filter field name: %s", err.Error()))
		}
	}

	return nil
}

// ValidateBatchInsertInput validates inputs for a batch INSERT mutation:
// - batch size must not exceed maxBatchSize
// - columns must not be empty
// - each row width must equal the column count
// - table and all columns must be valid identifiers
func (v *MutationValidator) ValidateBatchInsertInput(table string, columns []string, rows [][]any) error {
	if err := ValidateMutationIdentifier(table); err != nil {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidTable,
			fmt.Sprintf("invalid table name: %s", err.Error()))
	}

	if len(rows) > v.maxBatchSize {
		return apierrors.ValidationError(apierrors.ErrValidationBatchLimitExceeded,
			fmt.Sprintf("batch size %d exceeds maximum allowed %d", len(rows), v.maxBatchSize))
	}

	if len(columns) == 0 {
		return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
			"batch insert columns must not be empty")
	}

	for _, col := range columns {
		if err := ValidateMutationIdentifier(col); err != nil {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("invalid column name: %s", err.Error()))
		}
	}

	expectedWidth := len(columns)
	for i, row := range rows {
		if len(row) != expectedWidth {
			return apierrors.ValidationError(apierrors.ErrValidationInvalidField,
				fmt.Sprintf("row %d has %d values, expected %d (column count)", i, len(row), expectedWidth))
		}
	}

	return nil
}

// ValidateSQLLength checks that the constructed SQL does not exceed the configured
// maximum SQL statement length.
func (v *MutationValidator) ValidateSQLLength(sql string) error {
	if len(sql) > v.maxSQLLength {
		return apierrors.ValidationError(apierrors.ErrValidationPayloadTooLarge,
			fmt.Sprintf("SQL statement length %d exceeds maximum allowed %d", len(sql), v.maxSQLLength))
	}
	return nil
}
