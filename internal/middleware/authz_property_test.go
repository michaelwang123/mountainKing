// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

// --- helpers ---

// genBcryptHash generates a bcrypt hash for the given plaintext key using min cost for speed.
func genBcryptHash(t *rapid.T, plaintext string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return string(hash)
}

// TestProperty52_APIKeyPermissionIsolation validates that each API Key's
// permissions are isolated �?key A's permissions don't affect key B.
//
// Feature: graphql-multi-datasource-api, Property 52: API Key 权限隔离
// **Validates: Requirements 13.10**
func TestProperty52_APIKeyPermissionIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two distinct API keys with different permissions.
		rawKeyA := fmt.Sprintf("key-a-%s", rapid.StringMatching(`[a-z0-9]{8,16}`).Draw(t, "rawKeyA"))
		rawKeyB := fmt.Sprintf("key-b-%s", rapid.StringMatching(`[a-z0-9]{8,16}`).Draw(t, "rawKeyB"))

		dsA := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "dsA")
		dsB := rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "dsB")
		for dsB == dsA {
			dsB = rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "dsB_retry")
		}

		opA := rapid.SampledFrom([]string{"query", "mutation"}).Draw(t, "opA")
		opB := "mutation"
		if opA == "mutation" {
			opB = "query"
		}

		hashA := genBcryptHash(t, rawKeyA)
		hashB := genBcryptHash(t, rawKeyB)

		auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
			Keys: []config.APIKeyConfigEntry{
				{
					ID:  "client-a",
					Key: hashA,
					Permissions: struct {
						Datasources []string `mapstructure:"datasources"`
						Operations  []string `mapstructure:"operations"`
					}{
						Datasources: []string{dsA},
						Operations:  []string{opA},
					},
				},
				{
					ID:  "client-b",
					Key: hashB,
					Permissions: struct {
						Datasources []string `mapstructure:"datasources"`
						Operations  []string `mapstructure:"operations"`
					}{
						Datasources: []string{dsB},
						Operations:  []string{opB},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("NewAPIKeyAuthenticator: %v", err)
		}

		authz := &DefaultAuthorizer{}

		// Authenticate with key A.
		reqA, _ := http.NewRequest(http.MethodPost, "/graphql", nil)
		reqA.Header.Set("X-API-Key", rawKeyA)
		identityA, err := auth.Authenticate(reqA)
		if err != nil {
			t.Fatalf("Authenticate keyA: %v", err)
		}

		// Authenticate with key B.
		reqB, _ := http.NewRequest(http.MethodPost, "/graphql", nil)
		reqB.Header.Set("X-API-Key", rawKeyB)
		identityB, err := auth.Authenticate(reqB)
		if err != nil {
			t.Fatalf("Authenticate keyB: %v", err)
		}

		// Property: key A's identity has key A's permissions, not key B's.
		if identityA.Subject != "client-a" {
			t.Fatalf("expected subject client-a, got %s", identityA.Subject)
		}
		if identityB.Subject != "client-b" {
			t.Fatalf("expected subject client-b, got %s", identityB.Subject)
		}

		// Property: key A can access dsA with opA.
		if err := authz.Authorize(identityA, dsA, opA); err != nil {
			t.Fatalf("keyA should access dsA/opA: %v", err)
		}

		// Property: key A cannot access dsB (key B's datasource).
		if err := authz.Authorize(identityA, dsB, opA); err == nil {
			t.Fatalf("keyA should NOT access dsB=%s", dsB)
		}

		// Property: key A cannot use opB (key B's operation).
		if err := authz.Authorize(identityA, dsA, opB); err == nil {
			t.Fatalf("keyA should NOT use opB=%s", opB)
		}

		// Property: key B can access dsB with opB.
		if err := authz.Authorize(identityB, dsB, opB); err != nil {
			t.Fatalf("keyB should access dsB/opB: %v", err)
		}

		// Property: key B cannot access dsA (key A's datasource).
		if err := authz.Authorize(identityB, dsA, opB); err == nil {
			t.Fatalf("keyB should NOT access dsA=%s", dsA)
		}
	})
}

// TestProperty53_APIKeyExpirationRejection validates that API Keys with
// past expires_at are rejected with AUTH_TOKEN_EXPIRED.
//
// Feature: graphql-multi-datasource-api, Property 53: API Key 过期失效
// **Validates: Requirements 13.11**
func TestProperty53_APIKeyExpirationRejection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rawKey := fmt.Sprintf("expkey-%s", rapid.StringMatching(`[a-z0-9]{8,16}`).Draw(t, "rawKey"))
		hash := genBcryptHash(t, rawKey)

		// Generate a random expiration time in the past (1 second to 1 year ago).
		secondsAgo := rapid.IntRange(1, 365*24*3600).Draw(t, "secondsAgo")
		expTime := time.Now().Add(-time.Duration(secondsAgo) * time.Second)
		expiresAt := expTime.Format(time.RFC3339)

		auth, err := NewAPIKeyAuthenticator(config.APIKeyConfig{
			Keys: []config.APIKeyConfigEntry{
				{
					ID:        "expired-key",
					Key:       hash,
					ExpiresAt: expiresAt,
					Permissions: struct {
						Datasources []string `mapstructure:"datasources"`
						Operations  []string `mapstructure:"operations"`
					}{
						Datasources: []string{"starrocks"},
						Operations:  []string{"query"},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("NewAPIKeyAuthenticator: %v", err)
		}

		req, _ := http.NewRequest(http.MethodPost, "/graphql", nil)
		req.Header.Set("X-API-Key", rawKey)

		_, authErr := auth.Authenticate(req)

		// Property: expired API key is rejected.
		if authErr == nil {
			t.Fatalf("expected error for expired API key (expires_at=%s)", expiresAt)
		}

		ae, ok := authErr.(*AuthError)
		if !ok {
			t.Fatalf("expected *AuthError, got %T", authErr)
		}

		// Property: error code is AUTH_TOKEN_EXPIRED.
		if ae.Code != apierrors.ErrAuthTokenExpired {
			t.Fatalf("expected code %s, got %s", apierrors.ErrAuthTokenExpired, ae.Code)
		}

		// Property: status code is 401.
		if ae.StatusCode != 401 {
			t.Fatalf("expected status 401, got %d", ae.StatusCode)
		}
	})
}

// TestProperty76_BruteForceProtection validates that after N auth failures
// from the same IP within the window, the IP is banned; ban expires after ban_duration.
//
// Feature: graphql-multi-datasource-api, Property 76: 认证失败暴力破解防护
// **Validates: Design - 安全加固**
func TestProperty76_BruteForceProtection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		threshold := rapid.IntRange(2, 10).Draw(t, "threshold")
		// Use short durations for fast testing.
		windowMs := rapid.IntRange(200, 500).Draw(t, "windowMs")
		banMs := rapid.IntRange(100, 300).Draw(t, "banMs")

		window := time.Duration(windowMs) * time.Millisecond
		banDur := time.Duration(banMs) * time.Millisecond

		afl, err := NewAuthFailureLimiter(config.AuthFailureConfig{
			Enabled:     true,
			Threshold:   threshold,
			Window:      window,
			BanDuration: banDur,
		}, nil)
		if err != nil {
			t.Fatalf("NewAuthFailureLimiter: %v", err)
		}
		defer afl.Stop()

		// Generate a random IP.
		ip := fmt.Sprintf("%d.%d.%d.%d",
			rapid.IntRange(1, 254).Draw(t, "ip1"),
			rapid.IntRange(0, 255).Draw(t, "ip2"),
			rapid.IntRange(0, 255).Draw(t, "ip3"),
			rapid.IntRange(1, 254).Draw(t, "ip4"),
		)

		// Property: IP is allowed before reaching threshold.
		for i := 0; i < threshold-1; i++ {
			afl.RecordFailure(ip)
		}
		if !afl.Check(ip) {
			t.Fatalf("IP should be allowed before threshold (%d failures, threshold=%d)", threshold-1, threshold)
		}

		// Property: IP is banned at threshold.
		afl.RecordFailure(ip)
		if afl.Check(ip) {
			t.Fatalf("IP should be banned at threshold=%d", threshold)
		}

		// Property: other IPs are not affected.
		otherIP := fmt.Sprintf("%d.%d.%d.%d",
			rapid.IntRange(1, 254).Draw(t, "otherIP1"),
			rapid.IntRange(0, 255).Draw(t, "otherIP2"),
			rapid.IntRange(0, 255).Draw(t, "otherIP3"),
			rapid.IntRange(1, 254).Draw(t, "otherIP4"),
		)
		if !afl.Check(otherIP) {
			t.Fatalf("other IP %s should not be affected by ban on %s", otherIP, ip)
		}

		// Property: ban expires after ban_duration.
		time.Sleep(banDur + 20*time.Millisecond)
		if !afl.Check(ip) {
			t.Fatalf("IP should be allowed after ban expires (banDur=%v)", banDur)
		}
	})
}

// TestProperty84_ClearCacheMutationAuthorization validates that clearCache
// mutation requires "mutation" operation permission; query-only keys are denied.
//
// Feature: graphql-multi-datasource-api, Property 84: clearCache Mutation 授权
// **Validates: Design - Mutation 授权控制**
func TestProperty84_ClearCacheMutationAuthorization(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random datasource name.
		ds := rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "datasource")

		// Test with query-only key.
		queryOnlyIdentity := &AuthIdentity{
			Subject:     "query-only-key",
			Method:      "apikey",
			Datasources: []string{ds},
			Operations:  []string{"query"},
		}

		authz := &DefaultAuthorizer{}

		// Property: query-only key is denied mutation operation.
		err := authz.Authorize(queryOnlyIdentity, ds, "mutation")
		if err == nil {
			t.Fatal("query-only key should be denied mutation operation")
		}
		ae, ok := err.(*AuthError)
		if !ok {
			t.Fatalf("expected *AuthError, got %T", err)
		}
		if ae.StatusCode != 403 {
			t.Fatalf("expected status 403, got %d", ae.StatusCode)
		}
		if ae.Code != apierrors.ErrAuthInsufficientPermission {
			t.Fatalf("expected code %s, got %s", apierrors.ErrAuthInsufficientPermission, ae.Code)
		}

		// Test with mutation-capable key.
		hasMutation := rapid.Bool().Draw(t, "includeMutation")
		ops := []string{"query"}
		if hasMutation {
			ops = append(ops, "mutation")
		}

		mutationIdentity := &AuthIdentity{
			Subject:     "mutation-key",
			Method:      "apikey",
			Datasources: []string{ds},
			Operations:  ops,
		}

		err = authz.Authorize(mutationIdentity, ds, "mutation")
		if hasMutation {
			// Property: key with mutation permission is allowed.
			if err != nil {
				t.Fatalf("key with mutation permission should be allowed: %v", err)
			}
		} else {
			// Property: key without mutation permission is denied.
			if err == nil {
				t.Fatal("key without mutation permission should be denied")
			}
		}

		// Property: JWT identity (empty Operations) always has full access.
		jwtIdentity := &AuthIdentity{
			Subject:    "jwt-user",
			Method:     "jwt",
			Operations: nil, // empty = full access
		}
		if err := authz.Authorize(jwtIdentity, ds, "mutation"); err != nil {
			t.Fatalf("JWT identity should have full access: %v", err)
		}
	})
}

// TestProperty87_TrustedProxyIPExtraction validates that with trusted proxies
// configured, ExtractClientIP returns the rightmost non-trusted IP from
// X-Forwarded-For.
//
// Feature: graphql-multi-datasource-api, Property 87: 可信代理 IP 提取
// **Validates: Design - 代理环境 IP 提取**
func TestProperty87_TrustedProxyIPExtraction(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Use 10.0.0.0/8 as trusted proxy CIDR.
		afl, err := NewAuthFailureLimiter(config.AuthFailureConfig{
			Enabled:     true,
			Threshold:   100,
			Window:      5 * time.Minute,
			BanDuration: 15 * time.Minute,
		}, []string{"10.0.0.0/8"})
		if err != nil {
			t.Fatalf("NewAuthFailureLimiter: %v", err)
		}
		defer afl.Stop()

		// Generate a real client IP (non-trusted, not in 10.0.0.0/8).
		clientIP := fmt.Sprintf("%d.%d.%d.%d",
			rapid.SampledFrom([]int{203, 198, 172, 192}).Draw(t, "clientOctet1"),
			rapid.IntRange(0, 255).Draw(t, "clientOctet2"),
			rapid.IntRange(0, 255).Draw(t, "clientOctet3"),
			rapid.IntRange(1, 254).Draw(t, "clientOctet4"),
		)

		// Generate trusted proxy IPs (in 10.0.0.0/8).
		numProxies := rapid.IntRange(1, 3).Draw(t, "numProxies")
		proxyIPs := make([]string, numProxies)
		for i := 0; i < numProxies; i++ {
			proxyIPs[i] = fmt.Sprintf("10.%d.%d.%d",
				rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("proxy%d_2", i)),
				rapid.IntRange(0, 255).Draw(t, fmt.Sprintf("proxy%d_3", i)),
				rapid.IntRange(1, 254).Draw(t, fmt.Sprintf("proxy%d_4", i)),
			)
		}

		// Build X-Forwarded-For: clientIP, proxy1, proxy2, ...
		xff := clientIP
		for _, p := range proxyIPs {
			xff += ", " + p
		}

		// RemoteAddr is the last proxy (trusted).
		remoteAddr := proxyIPs[numProxies-1] + ":12345"

		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("X-Forwarded-For", xff)

		got := afl.ExtractClientIP(req)

		// Property: extracted IP is the real client IP (rightmost non-trusted).
		if got != clientIP {
			t.Fatalf("expected client IP %s, got %s (XFF=%s, RemoteAddr=%s)", clientIP, got, xff, remoteAddr)
		}

		// --- Test: no trusted proxies �?always use RemoteAddr ---
		aflNoProxy, err := NewAuthFailureLimiter(config.AuthFailureConfig{
			Enabled:     true,
			Threshold:   100,
			Window:      5 * time.Minute,
			BanDuration: 15 * time.Minute,
		}, nil)
		if err != nil {
			t.Fatalf("NewAuthFailureLimiter (no proxy): %v", err)
		}
		defer aflNoProxy.Stop()

		directIP := fmt.Sprintf("%d.%d.%d.%d",
			rapid.IntRange(1, 254).Draw(t, "directIP1"),
			rapid.IntRange(0, 255).Draw(t, "directIP2"),
			rapid.IntRange(0, 255).Draw(t, "directIP3"),
			rapid.IntRange(1, 254).Draw(t, "directIP4"),
		)

		req2, _ := http.NewRequest(http.MethodGet, "/", nil)
		req2.RemoteAddr = directIP + ":8080"
		req2.Header.Set("X-Forwarded-For", "5.6.7.8, 9.10.11.12")

		got2 := aflNoProxy.ExtractClientIP(req2)

		// Property: without trusted proxies, XFF is ignored, RemoteAddr is used.
		if got2 != directIP {
			t.Fatalf("no proxy: expected %s, got %s", directIP, got2)
		}

		// --- Test: RemoteAddr not trusted �?use RemoteAddr directly ---
		nonTrustedRemote := fmt.Sprintf("%d.%d.%d.%d",
			rapid.SampledFrom([]int{203, 198, 172, 192}).Draw(t, "ntOctet1"),
			rapid.IntRange(0, 255).Draw(t, "ntOctet2"),
			rapid.IntRange(0, 255).Draw(t, "ntOctet3"),
			rapid.IntRange(1, 254).Draw(t, "ntOctet4"),
		)

		req3, _ := http.NewRequest(http.MethodGet, "/", nil)
		req3.RemoteAddr = nonTrustedRemote + ":9999"
		req3.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.5")

		got3 := afl.ExtractClientIP(req3)

		// Property: when RemoteAddr is not trusted, use RemoteAddr directly.
		if got3 != nonTrustedRemote {
			t.Fatalf("non-trusted remote: expected %s, got %s", nonTrustedRemote, got3)
		}
	})
}
