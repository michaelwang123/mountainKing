package scalar

import (
	"bytes"
	"testing"
	"time"
)

func TestMarshalDateTime(t *testing.T) {
	dt := DateTime{Time: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)}
	m := MarshalDateTime(dt)
	var buf bytes.Buffer
	m.MarshalGQL(&buf)
	got := buf.String()
	want := `"2024-01-15T10:30:00Z"`
	if got != want {
		t.Errorf("MarshalDateTime = %q, want %q", got, want)
	}
}

func TestUnmarshalDateTime_Valid(t *testing.T) {
	dt, err := UnmarshalDateTime("2024-01-15T10:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !dt.Time.Equal(want) {
		t.Errorf("got %v, want %v", dt.Time, want)
	}
}

func TestUnmarshalDateTime_InvalidFormat(t *testing.T) {
	_, err := UnmarshalDateTime("not-a-date")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestUnmarshalDateTime_WrongType(t *testing.T) {
	_, err := UnmarshalDateTime(12345)
	if err == nil {
		t.Error("expected error for non-string type")
	}
}
