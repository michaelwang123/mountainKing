// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/michaelwang123/mountainKing/internal/config"
)

// --- JWT Operations claim tests ---

func TestJWT_OperationsClaim_Absent_DefaultsToQuery(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	// JWT without "operations" claim
	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub": "user-readonly",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if len(identity.Operations) != 1 {
		t.Fatalf("Operations length = %d, want 1", len(identity.Operations))
	}
	if identity.Operations[0] != "query" {
		t.Errorf("Operations[0] = %q, want %q", identity.Operations[0], "query")
	}
}

func TestJWT_OperationsClaim_Present_PopulatesOperations(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	// JWT with "operations" claim containing query and mutation
	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub":        "user-writer",
		"exp":        jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"operations": []interface{}{"query", "mutation"},
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if len(identity.Operations) != 2 {
		t.Fatalf("Operations length = %d, want 2", len(identity.Operations))
	}
	if identity.Operations[0] != "query" {
		t.Errorf("Operations[0] = %q, want %q", identity.Operations[0], "query")
	}
	if identity.Operations[1] != "mutation" {
		t.Errorf("Operations[1] = %q, want %q", identity.Operations[1], "mutation")
	}
}

// --- JWT Datasources claim tests ---

func TestJWT_DatasourcesClaim_Absent_DefaultsToNil(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	// JWT without "datasources" claim
	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub": "user-unrestricted",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if identity.Datasources != nil {
		t.Errorf("Datasources = %v, want nil (unrestricted)", identity.Datasources)
	}
}

func TestJWT_DatasourcesClaim_Present_PopulatesDatasources(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	// JWT with "datasources" claim
	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub":         "user-restricted",
		"exp":         jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"datasources": []interface{}{"analytics_db"},
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if len(identity.Datasources) != 1 {
		t.Fatalf("Datasources length = %d, want 1", len(identity.Datasources))
	}
	if identity.Datasources[0] != "analytics_db" {
		t.Errorf("Datasources[0] = %q, want %q", identity.Datasources[0], "analytics_db")
	}
}

// --- JWT with both claims populated ---

func TestJWT_BothClaims_PopulatedCorrectly(t *testing.T) {
	secret := "test-secret-key-at-least-32-bytes!"
	auth, err := NewJWTAuthenticator(config.JWTConfig{
		Secret: secret,
	}, "HS256")
	if err != nil {
		t.Fatalf("NewJWTAuthenticator: %v", err)
	}

	// JWT with both "operations" and "datasources" claims
	tokenStr := makeHS256Token(t, secret, jwt.MapClaims{
		"sub":         "user-full",
		"exp":         jwt.NewNumericDate(time.Now().Add(time.Hour)),
		"operations":  []interface{}{"query", "mutation"},
		"datasources": []interface{}{"analytics_db", "reporting_db"},
	})

	identity, err := auth.Authenticate(newRequest(t, "Bearer "+tokenStr))
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// Verify subject and method
	if identity.Subject != "user-full" {
		t.Errorf("Subject = %q, want %q", identity.Subject, "user-full")
	}
	if identity.Method != "jwt" {
		t.Errorf("Method = %q, want %q", identity.Method, "jwt")
	}

	// Verify operations
	if len(identity.Operations) != 2 {
		t.Fatalf("Operations length = %d, want 2", len(identity.Operations))
	}
	if identity.Operations[0] != "query" {
		t.Errorf("Operations[0] = %q, want %q", identity.Operations[0], "query")
	}
	if identity.Operations[1] != "mutation" {
		t.Errorf("Operations[1] = %q, want %q", identity.Operations[1], "mutation")
	}

	// Verify datasources
	if len(identity.Datasources) != 2 {
		t.Fatalf("Datasources length = %d, want 2", len(identity.Datasources))
	}
	if identity.Datasources[0] != "analytics_db" {
		t.Errorf("Datasources[0] = %q, want %q", identity.Datasources[0], "analytics_db")
	}
	if identity.Datasources[1] != "reporting_db" {
		t.Errorf("Datasources[1] = %q, want %q", identity.Datasources[1], "reporting_db")
	}
}
