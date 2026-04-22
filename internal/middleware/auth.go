// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/michaelwang123/mountainKing/internal/config"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// Authenticator defines the interface for authenticating HTTP requests.
// Implementations extract credentials from the request, validate them,
// and return the authenticated identity or an error.
type Authenticator interface {
	Authenticate(r *http.Request) (*AuthIdentity, error)
}

// AuthIdentity represents the authenticated principal's identity.
// It is stored in the request context under CtxKeyAuthIdentity.
type AuthIdentity struct {
	Subject     string   // JWT "sub" claim or API Key ID
	Method      string   // "jwt" or "apikey"
	Datasources []string // allowed datasource names
	Operations  []string // allowed operation types (query, mutation)
}

// AuthError represents an authentication/authorization error with a
// machine-readable code, HTTP status code, and human-readable message.
type AuthError struct {
	Code       string // e.g. AUTH_MISSING, AUTH_TOKEN_EXPIRED, AUTH_TOKEN_INVALID
	StatusCode int    // 401 or 403
	Message    string
}

// Error implements the error interface.
func (e *AuthError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// JWTAuthenticator validates JWT Bearer tokens from the Authorization header.
type JWTAuthenticator struct {
	algorithm string
	secret    []byte           // HMAC symmetric key (HS256)
	publicKey crypto.PublicKey // RSA or ECDSA public key (RS256/ES256)
	issuer    string
}

// NewJWTAuthenticator creates a JWTAuthenticator from the given JWT config.
// For HS256, cfg.Secret must be non-empty.
// For RS256/ES256, cfg.PublicKeyFile must point to a valid PEM-encoded public key.
func NewJWTAuthenticator(cfg config.JWTConfig, algorithm string) (*JWTAuthenticator, error) {
	auth := &JWTAuthenticator{
		algorithm: algorithm,
		issuer:    cfg.Issuer,
	}

	switch algorithm {
	case "HS256":
		if cfg.Secret == "" {
			return nil, fmt.Errorf("jwt: secret is required for HS256")
		}
		auth.secret = []byte(cfg.Secret)

	case "RS256":
		pub, err := loadPublicKey(cfg.PublicKeyFile)
		if err != nil {
			return nil, fmt.Errorf("jwt: failed to load RS256 public key: %w", err)
		}
		if _, ok := pub.(*rsa.PublicKey); !ok {
			return nil, fmt.Errorf("jwt: public key is not RSA")
		}
		auth.publicKey = pub

	case "ES256":
		pub, err := loadPublicKey(cfg.PublicKeyFile)
		if err != nil {
			return nil, fmt.Errorf("jwt: failed to load ES256 public key: %w", err)
		}
		if _, ok := pub.(*ecdsa.PublicKey); !ok {
			return nil, fmt.Errorf("jwt: public key is not ECDSA")
		}
		auth.publicKey = pub

	default:
		return nil, fmt.Errorf("jwt: unsupported algorithm %q", algorithm)
	}

	return auth, nil
}

// Authenticate extracts and validates a JWT Bearer token from the request.
// Returns AuthIdentity on success, or an AuthError on failure.
func (j *JWTAuthenticator) Authenticate(r *http.Request) (*AuthIdentity, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "missing Authorization header",
		}
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthTokenInvalid,
			StatusCode: 401,
			Message:    "Authorization header must use Bearer scheme",
		}
	}

	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == "" {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "missing Bearer token",
		}
	}

	// Build parser options.
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{j.algorithm}),
	}
	if j.issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(j.issuer))
	}

	token, err := jwt.Parse(tokenStr, j.keyFunc, parserOpts...)
	if err != nil {
		return nil, j.classifyError(err)
	}

	if !token.Valid {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthTokenInvalid,
			StatusCode: 401,
			Message:    "invalid token",
		}
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthTokenInvalid,
			StatusCode: 401,
			Message:    "invalid token claims",
		}
	}

	sub, _ := claims.GetSubject()

	return &AuthIdentity{
		Subject: sub,
		Method:  "jwt",
	}, nil
}

// keyFunc returns the appropriate key for JWT signature verification.
func (j *JWTAuthenticator) keyFunc(_ *jwt.Token) (any, error) {
	switch j.algorithm {
	case "HS256":
		return j.secret, nil
	case "RS256", "ES256":
		return j.publicKey, nil
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", j.algorithm)
	}
}

// classifyError maps jwt library errors to AuthError codes.
func (j *JWTAuthenticator) classifyError(err error) *AuthError {
	// Check for token-expired specifically.
	if isTokenExpired(err) {
		return &AuthError{
			Code:       apierrors.ErrAuthTokenExpired,
			StatusCode: 401,
			Message:    "token has expired",
		}
	}

	return &AuthError{
		Code:       apierrors.ErrAuthTokenInvalid,
		StatusCode: 401,
		Message:    fmt.Sprintf("invalid token: %v", err),
	}
}

// isTokenExpired checks whether the error chain contains a token-expired error.
func isTokenExpired(err error) bool {
	// The jwt/v5 library wraps errors; check the full error string for the
	// expiration sentinel. We also use errors.Is for the typed sentinel.
	if strings.Contains(err.Error(), "token is expired") {
		return true
	}
	return false
}

// loadPublicKey reads a PEM-encoded public key file and parses it.
func loadPublicKey(path string) (crypto.PublicKey, error) {
	if path == "" {
		return nil, fmt.Errorf("public key file path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read public key file: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from %s", path)
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	return pub, nil
}

// APIKeyEntry represents a single configured API Key with its bcrypt hash,
// optional expiration, and permission scope.
type APIKeyEntry struct {
	ID          string
	KeyHash     []byte     // bcrypt hash (never store plaintext keys)
	ExpiresAt   *time.Time // nil means no expiration
	Datasources []string   // allowed datasource names
	Operations  []string   // allowed operation types (query, mutation)
}

// APIKeyAuthenticator validates API Keys from the X-API-Key header.
// It uses bcrypt.CompareHashAndPassword for constant-time comparison
// to prevent timing attacks.
type APIKeyAuthenticator struct {
	keys []APIKeyEntry
}

// NewAPIKeyAuthenticator creates an APIKeyAuthenticator from the given config.
// Each configured key's hash string is converted to []byte for bcrypt comparison.
// The expires_at field is parsed as RFC3339 if non-empty.
func NewAPIKeyAuthenticator(cfg config.APIKeyConfig) (*APIKeyAuthenticator, error) {
	entries := make([]APIKeyEntry, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		if k.ID == "" {
			return nil, fmt.Errorf("apikey: key entry missing ID")
		}
		if k.Key == "" {
			return nil, fmt.Errorf("apikey: key entry %q missing key hash", k.ID)
		}

		entry := APIKeyEntry{
			ID:          k.ID,
			KeyHash:     []byte(k.Key),
			Datasources: k.Permissions.Datasources,
			Operations:  k.Permissions.Operations,
		}

		if k.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, k.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("apikey: key %q has invalid expires_at %q: %w", k.ID, k.ExpiresAt, err)
			}
			entry.ExpiresAt = &t
		}

		entries = append(entries, entry)
	}

	return &APIKeyAuthenticator{keys: entries}, nil
}

// Authenticate extracts the API Key from the X-API-Key header, validates it
// against configured bcrypt hashes, checks expiration, and returns the
// associated AuthIdentity on success.
func (a *APIKeyAuthenticator) Authenticate(r *http.Request) (*AuthIdentity, error) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		return nil, &AuthError{
			Code:       apierrors.ErrAuthMissing,
			StatusCode: 401,
			Message:    "missing X-API-Key header",
		}
	}

	for i := range a.keys {
		if bcrypt.CompareHashAndPassword(a.keys[i].KeyHash, []byte(apiKey)) != nil {
			continue
		}

		// Key matched — check expiration.
		if a.keys[i].ExpiresAt != nil && time.Now().After(*a.keys[i].ExpiresAt) {
			return nil, &AuthError{
				Code:       apierrors.ErrAuthTokenExpired,
				StatusCode: 401,
				Message:    "API key has expired",
			}
		}

		return &AuthIdentity{
			Subject:     a.keys[i].ID,
			Method:      "apikey",
			Datasources: a.keys[i].Datasources,
			Operations:  a.keys[i].Operations,
		}, nil
	}

	return nil, &AuthError{
		Code:       apierrors.ErrAuthTokenInvalid,
		StatusCode: 401,
		Message:    "invalid API key",
	}
}
