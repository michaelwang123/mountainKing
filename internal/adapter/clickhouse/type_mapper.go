// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"strings"
	"sync"

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

// maxTypeRecursionDepth is the maximum allowed recursion depth when parsing
// nested ClickHouse types (e.g., LowCardinality(Nullable(String))).
const maxTypeRecursionDepth = 10

// TypeMapper maps ClickHouse SQL types to GraphQL types.
// It caches resolved mappings in a sync.Map for thread-safe, lock-free reads
// under high concurrency. The cache eliminates repeated string parsing for
// tables with many columns across concurrent queries.
type TypeMapper struct {
	logger *zap.Logger
	cache  sync.Map // map[string]GraphQLType
}

// NewTypeMapper creates a new TypeMapper.
func NewTypeMapper(logger *zap.Logger) *TypeMapper {
	return &TypeMapper{logger: logger}
}

// MapType maps a ClickHouse SQL type string to a GraphQL type.
// Results are cached after first resolution for subsequent calls.
func (m *TypeMapper) MapType(sqlType string) GraphQLType {
	// Fast path: check cache first.
	if cached, ok := m.cache.Load(sqlType); ok {
		return cached.(GraphQLType)
	}

	// Slow path: resolve and cache.
	result := m.mapTypeRecursive(sqlType, 0)
	m.cache.Store(sqlType, result)
	return result
}

// mapTypeRecursive recursively resolves wrapper types and maps the base type.
func (m *TypeMapper) mapTypeRecursive(sqlType string, depth int) GraphQLType {
	if depth > maxTypeRecursionDepth {
		m.logger.Warn("type recursion depth exceeded, falling back to String",
			zap.String("sql_type", sqlType),
			zap.Int("depth", depth),
		)
		return GraphQLString
	}

	normalized := strings.ToUpper(strings.TrimSpace(sqlType))

	// Recursively unwrap LowCardinality(T) → map T
	if strings.HasPrefix(normalized, "LOWCARDINALITY(") && strings.HasSuffix(normalized, ")") {
		inner := extractInner(sqlType)
		return m.mapTypeRecursive(inner, depth+1)
	}

	// Recursively unwrap Nullable(T) → map T
	if strings.HasPrefix(normalized, "NULLABLE(") && strings.HasSuffix(normalized, ")") {
		inner := extractInner(sqlType)
		return m.mapTypeRecursive(inner, depth+1)
	}

	// SimpleAggregateFunction(func, T) → map T (last type argument)
	if strings.HasPrefix(normalized, "SIMPLEAGGREGATEFUNCTION(") && strings.HasSuffix(normalized, ")") {
		inner := extractLastTypeArg(sqlType)
		return m.mapTypeRecursive(inner, depth+1)
	}

	// Strip parameters: e.g., Decimal(10,2) → DECIMAL, FixedString(16) → FIXEDSTRING
	base := stripParams(normalized)

	switch base {
	// Integer types that fit in GraphQL Int (32-bit signed)
	case "INT8", "INT16", "INT32", "UINT8", "UINT16", "UINT32":
		return GraphQLInt

	// Large integer types that exceed GraphQL Int 32-bit range → String
	case "INT64", "UINT64", "INT128", "INT256", "UINT128", "UINT256":
		return GraphQLString

	// Float types
	case "FLOAT32", "FLOAT64", "BFLOAT16":
		return GraphQLFloat

	// String types
	case "STRING", "FIXEDSTRING":
		return GraphQLString

	// Boolean
	case "BOOL", "BOOLEAN":
		return GraphQLBoolean

	// Decimal types → String (preserve precision)
	case "DECIMAL", "DECIMAL32", "DECIMAL64", "DECIMAL128", "DECIMAL256":
		return GraphQLString

	// Date/Time types → DateTime
	case "DATE", "DATE32", "DATETIME", "DATETIME64":
		return GraphQLDateTime

	// Time types → String (time-of-day only, not full datetime)
	case "TIME", "TIME64":
		return GraphQLString

	// Identifier/address types → String
	case "UUID", "IPV4", "IPV6":
		return GraphQLString

	// Enum types → String
	case "ENUM8", "ENUM16":
		return GraphQLString

	// Complex/container types → JSON
	case "ARRAY", "TUPLE", "MAP", "NESTED", "JSON", "VARIANT", "DYNAMIC":
		return GraphQLJSON

	// Geo types → JSON
	case "POINT", "RING", "POLYGON", "MULTIPOLYGON":
		return GraphQLJSON

	// Aggregate function → String (opaque binary state)
	case "AGGREGATEFUNCTION":
		return GraphQLString

	default:
		m.logger.Warn("unsupported ClickHouse type, falling back to String",
			zap.String("sql_type", sqlType),
			zap.String("normalized", normalized),
		)
		return GraphQLString
	}
}

// extractInner extracts the content inside the outermost parentheses.
// For example: "LowCardinality(String)" → "String"
// It preserves the original casing from the input.
func extractInner(s string) string {
	start := strings.IndexByte(s, '(')
	if start < 0 {
		return s
	}
	// Find matching closing paren (the last one)
	end := strings.LastIndexByte(s, ')')
	if end <= start {
		return s
	}
	return strings.TrimSpace(s[start+1 : end])
}

// extractLastTypeArg extracts the last type argument from a function-style type.
// For example: "SimpleAggregateFunction(anyLast, String)" → "String"
// Handles nested parentheses: "SimpleAggregateFunction(any, Array(UInt8))" → "Array(UInt8)"
func extractLastTypeArg(s string) string {
	inner := extractInner(s)
	if inner == s {
		return s
	}

	// Find the last top-level comma (not inside nested parentheses)
	depth := 0
	lastComma := -1
	for i, ch := range inner {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				lastComma = i
			}
		}
	}

	if lastComma < 0 {
		// No comma found — return the inner content as-is
		return inner
	}

	return strings.TrimSpace(inner[lastComma+1:])
}

// stripParams removes the parenthesized parameter portion from a type name.
// For example: "DECIMAL(10,2)" → "DECIMAL", "FIXEDSTRING(16)" → "FIXEDSTRING"
func stripParams(normalized string) string {
	if idx := strings.IndexByte(normalized, '('); idx >= 0 {
		return strings.TrimSpace(normalized[:idx])
	}
	return normalized
}
