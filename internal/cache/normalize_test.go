package cache

import (
	"testing"
)

func TestNormalizeQuery_RemovesExtraWhitespace(t *testing.T) {
	input := "{  users  {  id   name  } }"
	want := "{ users { id name } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_RemovesNewlines(t *testing.T) {
	input := "{\n  users {\n    id\n    name\n  }\n}"
	want := "{ users { id name } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_RemovesComments(t *testing.T) {
	input := "# this is a comment\n{ users { id } }"
	want := "{ users { id } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_RemovesInlineComments(t *testing.T) {
	input := "{ users { id # inline comment\n name } }"
	want := "{ users { id name } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_LowercasesKeywords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"QUERY { users { id } }", "query { users { id } }"},
		{"Query GetUsers { users { id } }", "query GetUsers { users { id } }"},
		{"MUTATION { clearCache }", "mutation { clearCache }"},
		{"FRAGMENT UserFields ON User { id }", "fragment UserFields on User { id }"},
	}

	for _, tt := range tests {
		got := NormalizeQuery(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeQuery(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeQuery_PreservesFieldOrder(t *testing.T) {
	input := "{ users { name id email } }"
	want := "{ users { name id email } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("field order changed: got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_PreservesNonKeywordCase(t *testing.T) {
	input := "{ Users { ID Name } }"
	want := "{ Users { ID Name } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("non-keyword case changed: got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_HandlesSpreadOperator(t *testing.T) {
	input := "{ users { ...UserFields } }"
	want := "{ users { ... UserFields } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_EmptyQuery(t *testing.T) {
	got := NormalizeQuery("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestNormalizeQuery_WhitespaceOnly(t *testing.T) {
	got := NormalizeQuery("   \n\t  ")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestNormalizeQuery_BooleanLiterals(t *testing.T) {
	input := "{ users(active: TRUE) { id } }"
	want := "{ users ( active : true ) { id } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_NullLiteral(t *testing.T) {
	input := "{ users(name: NULL) { id } }"
	want := "{ users ( name : null ) { id } }"
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_ComplexQuery(t *testing.T) {
	input := `
		# Get user data
		QUERY GetUsers($limit: Int!) {
			users(limit: $limit) {
				id
				name # user name
				email
			}
		}
	`
	want := `query GetUsers ( $ limit : Int ! ) { users ( limit : $ limit ) { id name email } }`
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalizeQuery_PreservesStringLiterals(t *testing.T) {
	// Hash inside a string should not be treated as a comment
	input := `{ users(filter: "a#b") { id } }`
	want := `{ users ( filter : "a#b" ) { id } }`
	got := NormalizeQuery(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
