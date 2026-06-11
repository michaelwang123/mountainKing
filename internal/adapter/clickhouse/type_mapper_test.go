// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package clickhouse

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTypeMapper_IntTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		// Int types that fit in GraphQL Int (32-bit signed)
		{"Int8", GraphQLInt},
		{"Int16", GraphQLInt},
		{"Int32", GraphQLInt},
		{"UInt8", GraphQLInt},
		{"UInt16", GraphQLInt},
		{"UInt32", GraphQLInt},
		// Case insensitive
		{"int8", GraphQLInt},
		{"INT32", GraphQLInt},
		{"uint16", GraphQLInt},
		// Large int types → String (exceed 32-bit range)
		{"Int64", GraphQLString},
		{"UInt64", GraphQLString},
		{"Int128", GraphQLString},
		{"Int256", GraphQLString},
		{"UInt128", GraphQLString},
		{"UInt256", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_FloatTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Float32", GraphQLFloat},
		{"Float64", GraphQLFloat},
		{"BFloat16", GraphQLFloat},
		// Case insensitive
		{"float32", GraphQLFloat},
		{"FLOAT64", GraphQLFloat},
		{"bfloat16", GraphQLFloat},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_StringTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"String", GraphQLString},
		{"FixedString(16)", GraphQLString},
		{"FixedString(256)", GraphQLString},
		// Case insensitive
		{"string", GraphQLString},
		{"FIXEDSTRING(32)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_BooleanTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Bool", GraphQLBoolean},
		{"Boolean", GraphQLBoolean},
		// Case insensitive
		{"bool", GraphQLBoolean},
		{"BOOLEAN", GraphQLBoolean},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_DecimalTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Decimal(10,2)", GraphQLString},
		{"Decimal32(4)", GraphQLString},
		{"Decimal64(8)", GraphQLString},
		{"Decimal128(18)", GraphQLString},
		{"Decimal256(38)", GraphQLString},
		// Without params
		{"Decimal", GraphQLString},
		// Case insensitive
		{"DECIMAL(9,3)", GraphQLString},
		{"decimal128(10)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_DateTimeTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Date", GraphQLDateTime},
		{"Date32", GraphQLDateTime},
		{"DateTime", GraphQLDateTime},
		{"DateTime64(3)", GraphQLDateTime},
		{"DateTime64(9, 'UTC')", GraphQLDateTime},
		// Case insensitive
		{"date", GraphQLDateTime},
		{"DATETIME", GraphQLDateTime},
		{"datetime64(6)", GraphQLDateTime},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_TimeTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Time", GraphQLString},
		{"Time64", GraphQLString},
		// Case insensitive
		{"time", GraphQLString},
		{"TIME64", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_UUIDAndIPTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"UUID", GraphQLString},
		{"IPv4", GraphQLString},
		{"IPv6", GraphQLString},
		// Case insensitive
		{"uuid", GraphQLString},
		{"ipv4", GraphQLString},
		{"ipv6", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_EnumTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Enum8('active' = 1, 'inactive' = 2)", GraphQLString},
		{"Enum16('a' = 1, 'b' = 2, 'c' = 3)", GraphQLString},
		// Case insensitive
		{"ENUM8('x' = 0)", GraphQLString},
		{"enum16('y' = 1)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_ComplexTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Array(UInt32)", GraphQLJSON},
		{"Tuple(String, Int32)", GraphQLJSON},
		{"Map(String, Int64)", GraphQLJSON},
		{"JSON", GraphQLJSON},
		{"Variant(String, Int32, Float64)", GraphQLJSON},
		{"Dynamic", GraphQLJSON},
		// Case insensitive
		{"array(String)", GraphQLJSON},
		{"TUPLE(Int8, String)", GraphQLJSON},
		{"map(String, UInt8)", GraphQLJSON},
		{"json", GraphQLJSON},
		{"variant(String, Bool)", GraphQLJSON},
		{"dynamic", GraphQLJSON},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_GeoTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Point", GraphQLJSON},
		{"Ring", GraphQLJSON},
		{"Polygon", GraphQLJSON},
		{"MultiPolygon", GraphQLJSON},
		// Case insensitive
		{"point", GraphQLJSON},
		{"RING", GraphQLJSON},
		{"polygon", GraphQLJSON},
		{"MULTIPOLYGON", GraphQLJSON},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_LowCardinality(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"LowCardinality(String)", GraphQLString},
		{"LowCardinality(FixedString(8))", GraphQLString},
		{"LowCardinality(UInt32)", GraphQLInt},
		{"LowCardinality(Float64)", GraphQLFloat},
		// Case insensitive
		{"lowcardinality(String)", GraphQLString},
		{"LOWCARDINALITY(Int32)", GraphQLInt},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_Nullable(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Nullable(Int32)", GraphQLInt},
		{"Nullable(String)", GraphQLString},
		{"Nullable(Float64)", GraphQLFloat},
		{"Nullable(DateTime)", GraphQLDateTime},
		{"Nullable(Bool)", GraphQLBoolean},
		// Case insensitive
		{"nullable(UInt16)", GraphQLInt},
		{"NULLABLE(UUID)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_NestedWrappers(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"Nullable(LowCardinality(String))", GraphQLString},
		{"LowCardinality(Nullable(String))", GraphQLString},
		{"Nullable(LowCardinality(FixedString(16)))", GraphQLString},
		{"Nullable(LowCardinality(Int32))", GraphQLInt},
		{"LowCardinality(Nullable(Float32))", GraphQLFloat},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_SimpleAggregateFunction(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"SimpleAggregateFunction(anyLast, String)", GraphQLString},
		{"SimpleAggregateFunction(max, Int32)", GraphQLInt},
		{"SimpleAggregateFunction(min, Float64)", GraphQLFloat},
		{"SimpleAggregateFunction(any, DateTime)", GraphQLDateTime},
		{"SimpleAggregateFunction(anyLast, Array(UInt8))", GraphQLJSON},
		// Case insensitive
		{"simpleaggregatefunction(anyLast, String)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_AggregateFunction(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"AggregateFunction(uniq, UInt64)", GraphQLString},
		{"AggregateFunction(quantiles(0.5, 0.9), Float64)", GraphQLString},
		// Case insensitive
		{"AGGREGATEFUNCTION(count, String)", GraphQLString},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}

func TestTypeMapper_RecursionDepthExceeded(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)
	mapper := NewTypeMapper(logger)

	// Build a type with nesting depth > 10
	// Nullable(Nullable(Nullable(...Nullable(String)...)))
	sqlType := "String"
	for i := 0; i < 12; i++ {
		sqlType = "Nullable(" + sqlType + ")"
	}

	// Should not panic
	got := mapper.MapType(sqlType)
	if got != GraphQLString {
		t.Errorf("MapType(deeply nested) = %q, want %q", got, GraphQLString)
	}

	// Should have logged a warning about depth exceeded
	found := false
	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "recursion depth exceeded") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning log about recursion depth exceeded, but none found")
	}
}

func TestTypeMapper_RecursionDepthExceeded_LowCardinality(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	// Build deeply nested LowCardinality
	sqlType := "String"
	for i := 0; i < 15; i++ {
		sqlType = "LowCardinality(" + sqlType + ")"
	}

	// Should not panic and return String
	got := mapper.MapType(sqlType)
	if got != GraphQLString {
		t.Errorf("MapType(deeply nested LowCardinality) = %q, want %q", got, GraphQLString)
	}
}

func TestTypeMapper_UnknownType(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)
	mapper := NewTypeMapper(logger)

	got := mapper.MapType("SomeUnknownType")
	if got != GraphQLString {
		t.Errorf("MapType(SomeUnknownType) = %q, want %q", got, GraphQLString)
	}

	// Should have logged a warning
	if logs.Len() == 0 {
		t.Error("expected warning log for unknown type, but none found")
	}

	entry := logs.All()[0]
	if entry.Level != zapcore.WarnLevel {
		t.Errorf("expected Warn level, got %v", entry.Level)
	}
	if entry.ContextMap()["sql_type"] != "SomeUnknownType" {
		t.Errorf("expected sql_type=SomeUnknownType in log, got %v", entry.ContextMap()["sql_type"])
	}
}

func TestTypeMapper_WhitespaceHandling(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())

	tests := []struct {
		sqlType string
		want    GraphQLType
	}{
		{"  Int32  ", GraphQLInt},
		{" String ", GraphQLString},
		{"  Nullable( Int32 )  ", GraphQLInt},
		{"\tFloat64\t", GraphQLFloat},
	}

	for _, tt := range tests {
		t.Run(tt.sqlType, func(t *testing.T) {
			got := mapper.MapType(tt.sqlType)
			if got != tt.want {
				t.Errorf("MapType(%q) = %q, want %q", tt.sqlType, got, tt.want)
			}
		})
	}
}
