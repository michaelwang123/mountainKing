// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"net/http"
	"testing"
	"time"

	"github.com/michaelwang123/mountainKing/internal/config"
)

func newTestLimiter(t *testing.T, threshold int, window, banDur time.Duration, proxies []string) *AuthFailureLimiter {
	t.Helper()
	cfg := config.AuthFailureConfig{
		Enabled:     true,
		Threshold:   threshold,
		Window:      window,
		BanDuration: banDur,
	}
	afl, err := NewAuthFailureLimiter(cfg, proxies)
	if err != nil {
		t.Fatalf("NewAuthFailureLimiter: %v", err)
	}
	t.Cleanup(func() { afl.Stop() })
	return afl
}

func TestAuthFailureLimiter_AllowsBeforeThreshold(t *testing.T) {
	afl := newTestLimiter(t, 3, 5*time.Minute, 15*time.Minute, nil)

	ip := "192.168.1.1"
	afl.RecordFailure(ip)
	afl.RecordFailure(ip)

	if !afl.Check(ip) {
		t.Error("expected IP to be allowed before reaching threshold")
	}
}

func TestAuthFailureLimiter_BansAtThreshold(t *testing.T) {
	afl := newTestLimiter(t, 3, 5*time.Minute, 15*time.Minute, nil)

	ip := "192.168.1.1"
	for i := 0; i < 3; i++ {
		afl.RecordFailure(ip)
	}

	if afl.Check(ip) {
		t.Error("expected IP to be banned after reaching threshold")
	}
}

func TestAuthFailureLimiter_DifferentIPsIndependent(t *testing.T) {
	afl := newTestLimiter(t, 2, 5*time.Minute, 15*time.Minute, nil)

	afl.RecordFailure("10.0.0.1")
	afl.RecordFailure("10.0.0.1")

	if afl.Check("10.0.0.1") {
		t.Error("expected 10.0.0.1 to be banned")
	}
	if !afl.Check("10.0.0.2") {
		t.Error("expected 10.0.0.2 to be allowed (no failures)")
	}
}

func TestAuthFailureLimiter_WindowReset(t *testing.T) {
	// Use a very short window so we can test expiration.
	afl := newTestLimiter(t, 3, 50*time.Millisecond, 15*time.Minute, nil)

	ip := "192.168.1.1"
	afl.RecordFailure(ip)
	afl.RecordFailure(ip)

	// Wait for the window to expire.
	time.Sleep(60 * time.Millisecond)

	// After window expires, counter should reset. One more failure should not ban.
	afl.RecordFailure(ip)
	if !afl.Check(ip) {
		t.Error("expected IP to be allowed after window reset")
	}
}

func TestAuthFailureLimiter_BanExpires(t *testing.T) {
	afl := newTestLimiter(t, 2, 5*time.Minute, 50*time.Millisecond, nil)

	ip := "192.168.1.1"
	afl.RecordFailure(ip)
	afl.RecordFailure(ip)

	if afl.Check(ip) {
		t.Error("expected IP to be banned")
	}

	// Wait for ban to expire.
	time.Sleep(60 * time.Millisecond)

	if !afl.Check(ip) {
		t.Error("expected IP to be allowed after ban expires")
	}
}

func TestAuthFailureLimiter_UnknownIPAllowed(t *testing.T) {
	afl := newTestLimiter(t, 3, 5*time.Minute, 15*time.Minute, nil)

	if !afl.Check("1.2.3.4") {
		t.Error("expected unknown IP to be allowed")
	}
}

func TestAuthFailureLimiter_RecordFailureWhileBanned(t *testing.T) {
	afl := newTestLimiter(t, 2, 5*time.Minute, 1*time.Hour, nil)

	ip := "10.0.0.1"
	afl.RecordFailure(ip)
	afl.RecordFailure(ip)

	if afl.Check(ip) {
		t.Error("expected IP to be banned")
	}

	// Additional failures while banned should not change state.
	afl.RecordFailure(ip)
	if afl.Check(ip) {
		t.Error("expected IP to still be banned")
	}
}

func TestNewAuthFailureLimiter_InvalidCIDR(t *testing.T) {
	cfg := config.AuthFailureConfig{
		Enabled:     true,
		Threshold:   10,
		Window:      5 * time.Minute,
		BanDuration: 15 * time.Minute,
	}
	_, err := NewAuthFailureLimiter(cfg, []string{"not-a-cidr"})
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestExtractClientIP_NoTrustedProxies(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, nil)

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:12345"
	r.Header.Set("X-Forwarded-For", "5.6.7.8, 9.10.11.12")

	// Without trusted proxies, should always use RemoteAddr.
	got := afl.ExtractClientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", got)
	}
}

func TestExtractClientIP_WithTrustedProxies(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, []string{"10.0.0.0/8"})

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.5")

	// RemoteAddr is trusted, XFF rightmost non-trusted is 203.0.113.50
	// (10.0.0.5 is trusted, so skip it).
	got := afl.ExtractClientIP(r)
	if got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", got)
	}
}

func TestExtractClientIP_RemoteAddrNotTrusted(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, []string{"10.0.0.0/8"})

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.1:12345"
	r.Header.Set("X-Forwarded-For", "5.6.7.8")

	// RemoteAddr is not trusted, so use RemoteAddr directly.
	got := afl.ExtractClientIP(r)
	if got != "203.0.113.1" {
		t.Errorf("expected 203.0.113.1, got %s", got)
	}
}

func TestExtractClientIP_AllXFFTrusted(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, []string{"10.0.0.0/8", "172.16.0.0/12"})

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	r.Header.Set("X-Forwarded-For", "10.0.0.2, 172.16.0.1")

	// All XFF IPs are trusted, fall back to RemoteAddr.
	got := afl.ExtractClientIP(r)
	if got != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", got)
	}
}

func TestExtractClientIP_NoXFFHeader(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, []string{"10.0.0.0/8"})

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:12345"

	// Trusted proxy but no XFF header, fall back to RemoteAddr.
	got := afl.ExtractClientIP(r)
	if got != "10.0.0.1" {
		t.Errorf("expected 10.0.0.1, got %s", got)
	}
}

func TestExtractClientIP_RemoteAddrWithoutPort(t *testing.T) {
	afl := newTestLimiter(t, 10, 5*time.Minute, 15*time.Minute, nil)

	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4"

	got := afl.ExtractClientIP(r)
	if got != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %s", got)
	}
}

func TestCleanup_RemovesExpiredRecords(t *testing.T) {
	afl := newTestLimiter(t, 3, 50*time.Millisecond, 50*time.Millisecond, nil)

	afl.RecordFailure("1.1.1.1")

	// Wait for window to expire.
	time.Sleep(60 * time.Millisecond)

	afl.cleanup()

	afl.mu.RLock()
	_, exists := afl.failures["1.1.1.1"]
	afl.mu.RUnlock()

	if exists {
		t.Error("expected expired record to be cleaned up")
	}
}

func TestCleanup_RemovesExpiredBans(t *testing.T) {
	afl := newTestLimiter(t, 2, 5*time.Minute, 50*time.Millisecond, nil)

	afl.RecordFailure("2.2.2.2")
	afl.RecordFailure("2.2.2.2")

	// Wait for ban to expire.
	time.Sleep(60 * time.Millisecond)

	afl.cleanup()

	afl.mu.RLock()
	_, exists := afl.failures["2.2.2.2"]
	afl.mu.RUnlock()

	if exists {
		t.Error("expected expired ban record to be cleaned up")
	}
}
