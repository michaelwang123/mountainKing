// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"strings"

	"github.com/michaelwang123/mountainKing/internal/datasource"
)

// MutationSQLBuilder constructs parameterized SQL for write operations.
// INVARIANT: All inputs are pre-validated before calling builder methods.
// Builder methods NEVER return errors — invalid inputs indicate a programming bug.
type MutationSQLBuilder struct{}

// MutationSQL holds the constructed SQL and its parameter slice.
type MutationSQL struct {
	SQL    string
	Params []any
}

// BuildInsert constructs: INSERT INTO `table` (`col1`, `col2`) VALUES (?, ?)
// Pre-condition: len(columns) > 0, len(columns) == len(values), all identifiers valid.
func (b *MutationSQLBuilder) BuildInsert(table string, columns []string, values []any) *MutationSQL {
	var sb strings.Builder

	sb.WriteString("INSERT INTO ")
	sb.WriteString(quoteIdentifier(table))
	sb.WriteString(" (")

	for i, col := range columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdentifier(col))
	}

	sb.WriteString(") VALUES (")

	for i := range values {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("?")
	}

	sb.WriteString(")")

	// Copy params to avoid sharing the caller's slice (mutation safety).
	params := make([]any, len(values))
	copy(params, values)

	return &MutationSQL{
		SQL:    sb.String(),
		Params: params,
	}
}

// BuildUpdate constructs: UPDATE `table` SET `col1` = ?, `col2` = ? WHERE `f1` op ? AND ...
// Pre-condition: len(setCols) > 0, len(setCols) == len(setVals), len(filters) > 0, all identifiers valid.
func (b *MutationSQLBuilder) BuildUpdate(table string, setCols []string, setVals []any, filters []datasource.FilterCondition) *MutationSQL {
	var sb strings.Builder
	params := make([]any, 0, len(setVals)+len(filters))

	sb.WriteString("UPDATE ")
	sb.WriteString(quoteIdentifier(table))
	sb.WriteString(" SET ")

	for i, col := range setCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdentifier(col))
		sb.WriteString(" = ?")
	}
	params = append(params, setVals...)

	sb.WriteString(" WHERE ")
	whereClause, whereParams := buildMutationWhereClause(filters)
	sb.WriteString(whereClause)
	params = append(params, whereParams...)

	return &MutationSQL{
		SQL:    sb.String(),
		Params: params,
	}
}

// BuildDelete constructs: DELETE FROM `table` WHERE `f1` op ? AND ...
// Pre-condition: len(filters) > 0, all identifiers valid.
func (b *MutationSQLBuilder) BuildDelete(table string, filters []datasource.FilterCondition) *MutationSQL {
	var sb strings.Builder

	sb.WriteString("DELETE FROM ")
	sb.WriteString(quoteIdentifier(table))
	sb.WriteString(" WHERE ")

	whereClause, params := buildMutationWhereClause(filters)
	sb.WriteString(whereClause)

	return &MutationSQL{
		SQL:    sb.String(),
		Params: params,
	}
}

// BuildBatchInsert constructs: INSERT INTO `table` (`col1`, ...) VALUES (?, ...), (?, ...), ...
// Uses strings.Builder with pre-allocated capacity for performance.
// Pre-condition: len(columns) > 0, len(rows) > 0, each row has len(columns) values.
func (b *MutationSQLBuilder) BuildBatchInsert(table string, columns []string, rows [][]any) *MutationSQL {
	numCols := len(columns)
	numRows := len(rows)

	// Pre-allocate params slice.
	params := make([]any, 0, numCols*numRows)

	// Estimate capacity for strings.Builder.
	// "INSERT INTO `table` (cols) VALUES " + row placeholders
	// Each column name averages ~10 chars with backticks and separators.
	// Each row placeholder: "(?, ?, ...)" ~ numCols*3 + 2 chars.
	estimatedCap := len("INSERT INTO ") + len(table) + 2 + // table with backticks
		numCols*12 + // column names with backticks and separators
		len(") VALUES ") +
		numRows*(numCols*3+2+2) // rows with placeholders and separators

	var sb strings.Builder
	sb.Grow(estimatedCap)

	sb.WriteString("INSERT INTO ")
	sb.WriteString(quoteIdentifier(table))
	sb.WriteString(" (")

	for i, col := range columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quoteIdentifier(col))
	}

	sb.WriteString(") VALUES ")

	// Build the single-row placeholder template: (?, ?, ?)
	var rowPlaceholder strings.Builder
	rowPlaceholder.WriteString("(")
	for i := 0; i < numCols; i++ {
		if i > 0 {
			rowPlaceholder.WriteString(", ")
		}
		rowPlaceholder.WriteString("?")
	}
	rowPlaceholder.WriteString(")")
	rowTemplate := rowPlaceholder.String()

	for i, row := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(rowTemplate)
		params = append(params, row...)
	}

	return &MutationSQL{
		SQL:    sb.String(),
		Params: params,
	}
}

// buildMutationWhereClause converts filter conditions to a WHERE clause string
// with ? placeholders. Handles IN/NOT_IN expansion and IS_NULL/IS_NOT_NULL.
// This is the mutation builder's version — it does NOT validate or return errors.
func buildMutationWhereClause(filters []datasource.FilterCondition) (string, []any) {
	clauses := make([]string, 0, len(filters))
	var params []any

	for _, f := range filters {
		col := quoteIdentifier(f.Field)

		switch f.Operator {
		case datasource.FilterOpEQ:
			clauses = append(clauses, col+" = ?")
			params = append(params, f.Value)
		case datasource.FilterOpNEQ:
			clauses = append(clauses, col+" != ?")
			params = append(params, f.Value)
		case datasource.FilterOpGT:
			clauses = append(clauses, col+" > ?")
			params = append(params, f.Value)
		case datasource.FilterOpGTE:
			clauses = append(clauses, col+" >= ?")
			params = append(params, f.Value)
		case datasource.FilterOpLT:
			clauses = append(clauses, col+" < ?")
			params = append(params, f.Value)
		case datasource.FilterOpLTE:
			clauses = append(clauses, col+" <= ?")
			params = append(params, f.Value)
		case datasource.FilterOpLIKE:
			clauses = append(clauses, col+" LIKE ?")
			params = append(params, f.Value)
		case datasource.FilterOpIN:
			values, ok := f.Value.([]any)
			if !ok || len(values) == 0 {
				// Pre-condition violated — should not happen with validated inputs.
				clauses = append(clauses, col+" IN (?)")
				params = append(params, f.Value)
				continue
			}
			var inClause strings.Builder
			inClause.WriteString(col)
			inClause.WriteString(" IN (")
			for i := range values {
				if i > 0 {
					inClause.WriteString(", ")
				}
				inClause.WriteByte('?')
			}
			inClause.WriteByte(')')
			clauses = append(clauses, inClause.String())
			params = append(params, values...)
		case datasource.FilterOpNOT_IN:
			values, ok := f.Value.([]any)
			if !ok || len(values) == 0 {
				// Pre-condition violated — should not happen with validated inputs.
				clauses = append(clauses, col+" NOT IN (?)")
				params = append(params, f.Value)
				continue
			}
			var notInClause strings.Builder
			notInClause.WriteString(col)
			notInClause.WriteString(" NOT IN (")
			for i := range values {
				if i > 0 {
					notInClause.WriteString(", ")
				}
				notInClause.WriteByte('?')
			}
			notInClause.WriteByte(')')
			clauses = append(clauses, notInClause.String())
			params = append(params, values...)
		case datasource.FilterOpIS_NULL:
			clauses = append(clauses, col+" IS NULL")
		case datasource.FilterOpIS_NOT_NULL:
			clauses = append(clauses, col+" IS NOT NULL")
		}
	}

	return strings.Join(clauses, " AND "), params
}
