package starrocks

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"pgregory.net/rapid"
)

// knownTypeMappings defines all known StarRocks SQL type → expected GraphQL type mappings.
var knownTypeMappings = map[string]GraphQLType{
	"INT":       GraphQLInt,
	"BIGINT":    GraphQLInt,
	"TINYINT":   GraphQLInt,
	"SMALLINT":  GraphQLInt,
	"INTEGER":   GraphQLInt,
	"LARGEINT":  GraphQLInt,
	"FLOAT":     GraphQLFloat,
	"DOUBLE":    GraphQLFloat,
	"VARCHAR":   GraphQLString,
	"STRING":    GraphQLString,
	"CHAR":      GraphQLString,
	"TEXT":      GraphQLString,
	"BOOLEAN":   GraphQLBoolean,
	"BOOL":      GraphQLBoolean,
	"DECIMAL":   GraphQLString,
	"NUMERIC":   GraphQLString,
	"DATETIME":  GraphQLDateTime,
	"DATE":      GraphQLDateTime,
	"TIMESTAMP": GraphQLDateTime,
	"JSON":      GraphQLJSON,
}

// knownTypeNames returns a slice of all known type name keys.
func knownTypeNames() []string {
	names := make([]string, 0, len(knownTypeMappings))
	for k := range knownTypeMappings {
		names = append(names, k)
	}
	return names
}

// parenthesizedTypes are types that commonly accept size/precision parameters.
var parenthesizedTypes = map[string]bool{
	"VARCHAR": true, "CHAR": true, "DECIMAL": true, "NUMERIC": true,
}

// TestProperty18_StarRocksTypeMapping validates that for any StarRocks SQL data type:
// 1. Known types map correctly to their expected GraphQL types
// 2. Unsupported types map to String (with warning log)
// 3. Type mapping is case-insensitive
// 4. Parenthesized variants (e.g., VARCHAR(255)) map correctly
//
// Feature: graphql-multi-datasource-api, Property 18: StarRocks 类型映射
// **Validates: Requirements 4.8, 4.9**
func TestProperty18_StarRocksTypeMapping(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		types := knownTypeNames()
		sqlType := types[rapid.IntRange(0, len(types)-1).Draw(t, "typeIdx")]
		expected := knownTypeMappings[sqlType]

		mapper := NewTypeMapper(zap.NewNop())

		// Sub-property 1: exact uppercase match maps correctly.
		got := mapper.MapType(sqlType)
		if got != expected {
			t.Fatalf("MapType(%q) = %q, want %q", sqlType, got, expected)
		}

		// Sub-property 3: case-insensitive — lowercase maps correctly.
		got = mapper.MapType(strings.ToLower(sqlType))
		if got != expected {
			t.Fatalf("MapType(%q) = %q, want %q", strings.ToLower(sqlType), got, expected)
		}

		// Sub-property 3: case-insensitive — mixed case maps correctly.
		mixed := mixCase(t, sqlType)
		got = mapper.MapType(mixed)
		if got != expected {
			t.Fatalf("MapType(%q) = %q, want %q", mixed, got, expected)
		}

		// Sub-property 4: parenthesized variants map correctly.
		if parenthesizedTypes[sqlType] {
			size := rapid.IntRange(1, 65535).Draw(t, "paramSize")
			parameterized := sqlType + "(" + intToStr(size) + ")"
			got = mapper.MapType(parameterized)
			if got != expected {
				t.Fatalf("MapType(%q) = %q, want %q", parameterized, got, expected)
			}

			// Also test lowercase parenthesized.
			parameterizedLower := strings.ToLower(sqlType) + "(" + intToStr(size) + ")"
			got = mapper.MapType(parameterizedLower)
			if got != expected {
				t.Fatalf("MapType(%q) = %q, want %q", parameterizedLower, got, expected)
			}
		}
	})
}

// TestProperty18_StarRocksTypeMapping_UnsupportedFallback validates that unsupported
// types fall back to String and produce a warning log.
//
// Feature: graphql-multi-datasource-api, Property 18: StarRocks 类型映射 (unsupported fallback)
// **Validates: Requirements 4.8, 4.9**
func TestProperty18_StarRocksTypeMapping_UnsupportedFallback(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a type name that is NOT in the known mappings.
		unsupported := rapid.StringMatching(`[A-Z][A-Z0-9_]{2,10}`).Draw(t, "unsupportedType")
		normalized := strings.ToUpper(strings.TrimSpace(unsupported))
		if _, ok := knownTypeMappings[normalized]; ok {
			t.Skip("generated type is actually known, skipping")
		}

		core, logs := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)
		mapper := NewTypeMapper(logger)

		got := mapper.MapType(unsupported)
		if got != GraphQLString {
			t.Fatalf("MapType(%q) = %q, want %q (fallback)", unsupported, got, GraphQLString)
		}

		// Verify a warning log was emitted.
		if logs.Len() < 1 {
			t.Fatalf("expected at least 1 warning log for unsupported type %q, got %d", unsupported, logs.Len())
		}
		entry := logs.All()[0]
		if entry.Level != zapcore.WarnLevel {
			t.Fatalf("expected Warn level log, got %v", entry.Level)
		}
	})
}

// mixCase generates a mixed-case variant of the input string.
func mixCase(t *rapid.T, s string) string {
	runes := []rune(s)
	for i := range runes {
		if rapid.Bool().Draw(t, "caseFlip") {
			runes[i] = []rune(strings.ToLower(string(runes[i])))[0]
		}
	}
	return string(runes)
}

// intToStr converts an int to its string representation without importing strconv.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
