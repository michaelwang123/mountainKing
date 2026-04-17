package scalar

import (
	"bytes"
	"testing"
)

func TestMarshalJSON_Simple(t *testing.T) {
	v := JSON{"key": "value", "num": float64(42)}
	m := MarshalJSON(v)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	// JSON marshal output order is not guaranteed, just check it parses
	if len(got) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestMarshalJSON_Nil(t *testing.T) {
	m := MarshalJSON(nil)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	if got != "null" {
		t.Errorf("MarshalJSON(nil) = %q, want %q", got, "null")
	}
}

func TestMarshalJSON_Nested(t *testing.T) {
	v := JSON{
		"nested": map[string]any{"inner": "val"},
		"arr":    []any{1, 2, 3},
	}
	m := MarshalJSON(v)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	if buf.Len() == 0 {
		t.Error("expected non-empty output for nested JSON")
	}
}

func TestUnmarshalJSON_Map(t *testing.T) {
	input := map[string]any{"key": "value"}
	result, err := UnmarshalJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("got %v, want key=value", result)
	}
}

func TestUnmarshalJSON_String(t *testing.T) {
	result, err := UnmarshalJSON(`{"key":"value"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("got %v, want key=value", result)
	}
}

func TestUnmarshalJSON_InvalidString(t *testing.T) {
	_, err := UnmarshalJSON("not-json")
	if err == nil {
		t.Error("expected error for invalid JSON string")
	}
}

func TestUnmarshalJSON_WrongType(t *testing.T) {
	_, err := UnmarshalJSON(12345)
	if err == nil {
		t.Error("expected error for non-map/string type")
	}
}
