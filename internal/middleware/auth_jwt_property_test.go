// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/michaelwang123/mountainKing/internal/config"
	"pgregory.net/rapid"
)

// TestProperty14_JWTOperationsClaimDefault validates that:
// - When JWT has no "operations" claim, AuthIdentity.Operations defaults to ["query"]
// - When JWT has an "operations" claim with values, AuthIdentity.Operations matches those values
//
// **Validates: Requirements 5.5**
func TestProperty14_JWTOperationsClaimDefault(t *testing.T) {
	secret := "property-test-secret-key-32bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{Secret: secret}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		subject := rapid.StringMatching(`[a-zA-Z0-9]{3,20}`).Draw(t, "subject")
		hasOperationsClaim := rapid.Bool().Draw(t, "hasOperationsClaim")

		claims := jwt.MapClaims{
			"sub": subject,
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			"iat": jwt.NewNumericDate(time.Now()),
		}

		var expectedOps []string

		if hasOperationsClaim {
			// Generate a random non-empty list of operation strings.
			numOps := rapid.IntRange(1, 5).Draw(t, "numOps")
			ops := make([]interface{}, numOps)
			expectedOps = make([]string, numOps)
			for i := 0; i < numOps; i++ {
				op := rapid.SampledFrom([]string{"query", "mutation", "admin", "export", "import"}).Draw(t, "op")
				ops[i] = op
				expectedOps[i] = op
			}
			claims["operations"] = ops
		} else {
			// No operations claim — should default to ["query"].
			expectedOps = []string{"query"}
		}

		tokenStr, err := generateHS256Token(secret, claims)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)

		identity, authErr := auth.Authenticate(req)

		// Property: valid token authenticates successfully.
		if authErr != nil {
			t.Fatalf("expected successful auth, got error: %v", authErr)
		}
		if identity == nil {
			t.Fatal("expected identity, got nil")
		}

		// Property: Operations field matches expected value.
		if len(identity.Operations) != len(expectedOps) {
			t.Fatalf("expected %d operations, got %d (expected=%v, got=%v)",
				len(expectedOps), len(identity.Operations), expectedOps, identity.Operations)
		}
		for i, op := range expectedOps {
			if identity.Operations[i] != op {
				t.Fatalf("operations[%d]: expected %q, got %q (full expected=%v, full got=%v)",
					i, op, identity.Operations[i], expectedOps, identity.Operations)
			}
		}

		// Property: when no operations claim, default is exactly ["query"].
		if !hasOperationsClaim {
			if len(identity.Operations) != 1 || identity.Operations[0] != "query" {
				t.Fatalf("no operations claim: expected [\"query\"], got %v", identity.Operations)
			}
		}

		// Property: method is always "jwt".
		if identity.Method != "jwt" {
			t.Fatalf("expected method \"jwt\", got %q", identity.Method)
		}

		// Property: subject is correctly extracted.
		if identity.Subject != subject {
			t.Fatalf("expected subject %q, got %q", subject, identity.Subject)
		}
	})
}
