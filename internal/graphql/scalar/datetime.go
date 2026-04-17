package scalar

import (
	"fmt"
	"io"
	"time"

	"github.com/99designs/gqlgen/graphql"
)

// DateTime is a custom GraphQL scalar for ISO 8601 date-time values.
type DateTime struct {
	Time time.Time
}

// MarshalDateTime implements the graphql.Marshaler interface for DateTime.
func MarshalDateTime(t DateTime) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, `"`+t.Time.Format(time.RFC3339)+`"`)
	})
}

// UnmarshalDateTime implements the graphql.Unmarshaler interface for DateTime.
func UnmarshalDateTime(v any) (DateTime, error) {
	switch v := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return DateTime{}, fmt.Errorf("DateTime must be in RFC3339 format: %w", err)
		}
		return DateTime{Time: t}, nil
	default:
		return DateTime{}, fmt.Errorf("DateTime must be a string, got %T", v)
	}
}
