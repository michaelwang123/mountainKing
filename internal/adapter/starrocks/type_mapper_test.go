package starrocks

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMapType_IntTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"INT", "int", "BIGINT", "TINYINT", "SMALLINT", "INTEGER", "LARGEINT"} {
		if got := mapper.MapType(sqlType); got != GraphQLInt {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLInt)
		}
	}
}

func TestMapType_FloatTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"FLOAT", "DOUBLE", "float", "double"} {
		if got := mapper.MapType(sqlType); got != GraphQLFloat {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLFloat)
		}
	}
}

func TestMapType_StringTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"VARCHAR", "STRING", "CHAR", "TEXT", "varchar", "VARCHAR(255)", "CHAR(10)"} {
		if got := mapper.MapType(sqlType); got != GraphQLString {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLString)
		}
	}
}

func TestMapType_BooleanTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"BOOLEAN", "BOOL", "boolean", "bool"} {
		if got := mapper.MapType(sqlType); got != GraphQLBoolean {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLBoolean)
		}
	}
}

func TestMapType_DecimalPreservesPrecision(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"DECIMAL", "DECIMAL(10,2)", "NUMERIC", "NUMERIC(18,4)"} {
		if got := mapper.MapType(sqlType); got != GraphQLString {
			t.Errorf("MapType(%q) = %q, want %q (preserve precision)", sqlType, got, GraphQLString)
		}
	}
}

func TestMapType_DateTimeTypes(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"DATETIME", "DATE", "TIMESTAMP", "datetime", "date"} {
		if got := mapper.MapType(sqlType); got != GraphQLDateTime {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLDateTime)
		}
	}
}

func TestMapType_JSON(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	for _, sqlType := range []string{"JSON", "json"} {
		if got := mapper.MapType(sqlType); got != GraphQLJSON {
			t.Errorf("MapType(%q) = %q, want %q", sqlType, got, GraphQLJSON)
		}
	}
}

func TestMapType_UnsupportedFallsBackToStringWithWarning(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)
	mapper := NewTypeMapper(logger)

	got := mapper.MapType("BITMAP")
	if got != GraphQLString {
		t.Errorf("MapType(BITMAP) = %q, want %q", got, GraphQLString)
	}

	if logs.Len() != 1 {
		t.Fatalf("expected 1 warning log, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Level != zapcore.WarnLevel {
		t.Errorf("expected Warn level, got %v", entry.Level)
	}
	if entry.ContextMap()["sql_type"] != "BITMAP" {
		t.Errorf("expected sql_type=BITMAP in log, got %v", entry.ContextMap()["sql_type"])
	}
}

func TestMapType_WhitespaceHandling(t *testing.T) {
	mapper := NewTypeMapper(zap.NewNop())
	if got := mapper.MapType("  INT  "); got != GraphQLInt {
		t.Errorf("MapType(\"  INT  \") = %q, want %q", got, GraphQLInt)
	}
}
