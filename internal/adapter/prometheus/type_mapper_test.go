// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package prometheus

import (
	"math"
	"testing"
)

func TestMapResultType(t *testing.T) {
	tests := []struct {
		name     string
		input    PrometheusResultType
		expected GraphQLTypeName
	}{
		{"scalar maps to Float", ResultTypeScalar, GQLFloat},
		{"string maps to String", ResultTypeString, GQLString},
		{"vector maps to PrometheusVector", ResultTypeVector, GQLPrometheusVector},
		{"matrix maps to PrometheusMatrix", ResultTypeMatrix, GQLPrometheusMatrix},
		{"unknown type falls back to String", PrometheusResultType("unknown"), GQLString},
		{"empty type falls back to String", PrometheusResultType(""), GQLString},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapResultType(tt.input)
			if got != tt.expected {
				t.Errorf("MapResultType(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConvertValue(t *testing.T) {
	tests := []struct {
		name            string
		input           float64
		expectNil       bool
		expectedVal     float64
		expectedWarning string
	}{
		{"NaN returns nil with warning", math.NaN(), true, 0, "NaN value converted to null"},
		{"+Inf returns nil with warning", math.Inf(1), true, 0, "+Inf value converted to null"},
		{"-Inf returns nil with warning", math.Inf(-1), true, 0, "-Inf value converted to null"},
		{"zero returns pointer", 0, false, 0, ""},
		{"positive value returns pointer", 42.5, false, 42.5, ""},
		{"negative value returns pointer", -3.14, false, -3.14, ""},
		{"max float64 returns pointer", math.MaxFloat64, false, math.MaxFloat64, ""},
		{"smallest positive float64", math.SmallestNonzeroFloat64, false, math.SmallestNonzeroFloat64, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, warning := ConvertValue(tt.input)

			if tt.expectNil {
				if val != nil {
					t.Errorf("ConvertValue(%v): expected nil, got %v", tt.input, *val)
				}
			} else {
				if val == nil {
					t.Fatalf("ConvertValue(%v): expected non-nil, got nil", tt.input)
				}
				if *val != tt.expectedVal {
					t.Errorf("ConvertValue(%v): value = %v, want %v", tt.input, *val, tt.expectedVal)
				}
			}

			if warning != tt.expectedWarning {
				t.Errorf("ConvertValue(%v): warning = %q, want %q", tt.input, warning, tt.expectedWarning)
			}
		})
	}
}

func TestConvertValues(t *testing.T) {
	input := []float64{1.0, math.NaN(), 3.0, math.Inf(1), math.Inf(-1), 0}
	result, warnings := ConvertValues(input)

	if len(result) != len(input) {
		t.Fatalf("ConvertValues: result length = %d, want %d", len(result), len(input))
	}

	// Check normal values are preserved.
	if result[0] == nil || *result[0] != 1.0 {
		t.Errorf("result[0]: expected 1.0, got %v", result[0])
	}
	if result[2] == nil || *result[2] != 3.0 {
		t.Errorf("result[2]: expected 3.0, got %v", result[2])
	}
	if result[5] == nil || *result[5] != 0 {
		t.Errorf("result[5]: expected 0, got %v", result[5])
	}

	// Check special values are nil.
	if result[1] != nil {
		t.Errorf("result[1] (NaN): expected nil, got %v", *result[1])
	}
	if result[3] != nil {
		t.Errorf("result[3] (+Inf): expected nil, got %v", *result[3])
	}
	if result[4] != nil {
		t.Errorf("result[4] (-Inf): expected nil, got %v", *result[4])
	}

	// Expect 3 warnings (NaN, +Inf, -Inf).
	if len(warnings) != 3 {
		t.Fatalf("ConvertValues: warnings count = %d, want 3", len(warnings))
	}
}

func TestConvertValues_Empty(t *testing.T) {
	result, warnings := ConvertValues([]float64{})
	if len(result) != 0 {
		t.Errorf("ConvertValues(empty): result length = %d, want 0", len(result))
	}
	if len(warnings) != 0 {
		t.Errorf("ConvertValues(empty): warnings count = %d, want 0", len(warnings))
	}
}
