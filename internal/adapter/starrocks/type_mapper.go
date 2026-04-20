// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package starrocks

import (
	"strings"

	"go.uber.org/zap"
)

// GraphQLType represents a GraphQL type name.
type GraphQLType string

const (
	// GraphQLInt maps to the GraphQL Int scalar type.
	GraphQLInt GraphQLType = "Int"
	// GraphQLFloat maps to the GraphQL Float scalar type.
	GraphQLFloat GraphQLType = "Float"
	// GraphQLString maps to the GraphQL String scalar type.
	GraphQLString GraphQLType = "String"
	// GraphQLBoolean maps to the GraphQL Boolean scalar type.
	GraphQLBoolean GraphQLType = "Boolean"
	// GraphQLDateTime maps to the custom DateTime scalar type.
	GraphQLDateTime GraphQLType = "DateTime"
	// GraphQLJSON maps to the custom JSON scalar type.
	GraphQLJSON GraphQLType = "JSON"
)

// TypeMapper maps StarRocks SQL types to GraphQL types.
type TypeMapper struct {
	logger *zap.Logger
}

// NewTypeMapper creates a new TypeMapper.
func NewTypeMapper(logger *zap.Logger) *TypeMapper {
	return &TypeMapper{logger: logger}
}

// MapType maps a StarRocks SQL type string to a GraphQL type.
//
// Mapping rules:
//   - INT, BIGINT, TINYINT, SMALLINT â†?Int
//   - FLOAT, DOUBLE â†?Float
//   - VARCHAR, STRING, CHAR, TEXT â†?String
//   - BOOLEAN, BOOL â†?Boolean
//   - DECIMAL, NUMERIC â†?String (preserve precision)
//   - DATETIME, DATE, TIMESTAMP â†?DateTime
//   - JSON â†?JSON
//   - Unsupported types â†?String (with warning log)
func (m *TypeMapper) MapType(sqlType string) GraphQLType {
	// Normalize: uppercase and trim whitespace.
	normalized := strings.ToUpper(strings.TrimSpace(sqlType))

	// Strip parenthesized parameters, e.g. VARCHAR(255) â†?VARCHAR, DECIMAL(10,2) â†?DECIMAL.
	if idx := strings.IndexByte(normalized, '('); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}

	switch normalized {
	case "INT", "BIGINT", "TINYINT", "SMALLINT", "INTEGER", "LARGEINT":
		return GraphQLInt
	case "FLOAT", "DOUBLE":
		return GraphQLFloat
	case "VARCHAR", "STRING", "CHAR", "TEXT":
		return GraphQLString
	case "BOOLEAN", "BOOL":
		return GraphQLBoolean
	case "DECIMAL", "NUMERIC":
		return GraphQLString
	case "DATETIME", "DATE", "TIMESTAMP":
		return GraphQLDateTime
	case "JSON":
		return GraphQLJSON
	default:
		m.logger.Warn("unsupported StarRocks SQL type, falling back to String",
			zap.String("sql_type", sqlType),
			zap.String("normalized", normalized),
		)
		return GraphQLString
	}
}
