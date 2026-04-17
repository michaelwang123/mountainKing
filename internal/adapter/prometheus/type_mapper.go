package prometheus

import (
	"fmt"
	"math"
)

// PrometheusResultType represents the type of Prometheus query result.
type PrometheusResultType string

const (
	// ResultTypeScalar represents a scalar (single numeric value) result.
	ResultTypeScalar PrometheusResultType = "scalar"
	// ResultTypeString represents a string result.
	ResultTypeString PrometheusResultType = "string"
	// ResultTypeVector represents an instant vector result.
	ResultTypeVector PrometheusResultType = "vector"
	// ResultTypeMatrix represents a range vector (matrix) result.
	ResultTypeMatrix PrometheusResultType = "matrix"
)

// GraphQLTypeName represents the GraphQL type that a Prometheus result maps to.
type GraphQLTypeName string

const (
	// GQLFloat maps to the GraphQL Float scalar type.
	GQLFloat GraphQLTypeName = "Float"
	// GQLString maps to the GraphQL String scalar type.
	GQLString GraphQLTypeName = "String"
	// GQLPrometheusVector maps to the custom PrometheusVector GraphQL type.
	GQLPrometheusVector GraphQLTypeName = "PrometheusVector"
	// GQLPrometheusMatrix maps to the custom PrometheusMatrix GraphQL type.
	GQLPrometheusMatrix GraphQLTypeName = "PrometheusMatrix"
)

// MapResultType maps a Prometheus result type to the corresponding GraphQL type name.
//
// Mapping rules (per Requirements 5.8):
//   - scalar → Float
//   - string → String
//   - vector → PrometheusVector
//   - matrix → PrometheusMatrix
//   - unknown types → String (fallback)
func MapResultType(rt PrometheusResultType) GraphQLTypeName {
	switch rt {
	case ResultTypeScalar:
		return GQLFloat
	case ResultTypeString:
		return GQLString
	case ResultTypeVector:
		return GQLPrometheusVector
	case ResultTypeMatrix:
		return GQLPrometheusMatrix
	default:
		return GQLString
	}
}

// ConvertValue converts a Prometheus float64 value, handling NaN and ±Inf.
// Returns the converted value (nil for special values) and any warning message.
//
// Conversion rules (per Requirements 5.9):
//   - NaN  → nil + warning "NaN value converted to null"
//   - +Inf → nil + warning "+Inf value converted to null"
//   - -Inf → nil + warning "-Inf value converted to null"
//   - Normal values → *float64 pointer, empty warning
func ConvertValue(v float64) (*float64, string) {
	switch {
	case math.IsNaN(v):
		return nil, "NaN value converted to null"
	case math.IsInf(v, 1):
		return nil, "+Inf value converted to null"
	case math.IsInf(v, -1):
		return nil, "-Inf value converted to null"
	default:
		return &v, ""
	}
}

// ConvertValues converts a slice of Prometheus float64 values, collecting warnings
// for any special values encountered. Each warning includes the index of the
// affected data point for traceability.
func ConvertValues(values []float64) ([]*float64, []string) {
	result := make([]*float64, len(values))
	var warnings []string

	for i, v := range values {
		converted, warning := ConvertValue(v)
		result[i] = converted
		if warning != "" {
			warnings = append(warnings, fmt.Sprintf("data point [%d]: %s", i, warning))
		}
	}

	return result, warnings
}
