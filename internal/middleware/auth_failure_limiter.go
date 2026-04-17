// Package middleware provides HTTP middleware components for the GraphQL API service.
// AuthFailureLimiter implements brute-force protection for authentication failures.
// It tracks failed authentication attempts per IP and bans IPs that exceed
// the configured threshold within a time window.
package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/graphql-api/internal/config"
)

// failureRecord tracks authentication failure attempts for a single IP.
type failureRecord struct {
	count    int
	firstAt  time.Time
	bannedAt *time.Time
}

// AuthFailureLimiter provides brute-force protection by tracking authentication
// failures per IP address. When an IP exceeds the failure threshold within the
// configured window, it is banned for the configured ban duration.
type AuthFailureLimiter struct {
	mu             sync.RWMutex
	failures       map[string]*failureRecord
	threshold      int
	window         time.Duration
	banDur         time.Duration
	trustedProxies []*net.IPNet
	stopCh         chan struct{}
}

// NewAuthFailureLimiter creates a new AuthFailureLimiter from the given config.
// trustedProxies is a list of CIDR strings (e.g. "10.0.0.0/8") used to extract
// the real client IP from X-Forwarded-For headers. Returns an error if any
// CIDR string is invalid.
func NewAuthFailureLimiter(cfg config.AuthFailureConfig, trustedProxies []string) (*AuthFailureLimiter, error) {
	var nets []*net.IPNet
	for _, cidr := range trustedProxies {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		nets = append(nets, ipNet)
	}

	afl := &AuthFailureLimiter{
		failures:       make(map[string]*failureRecord),
		threshold:      cfg.Threshold,
		window:         cfg.Window,
		banDur:         cfg.BanDuration,
		trustedProxies: nets,
		stopCh:         make(chan struct{}),
	}

	afl.startCleanup()
	return afl, nil
}

// Check returns true if the given IP is allowed (not currently banned).
// Returns false if the IP is banned due to excessive authentication failures.
func (afl *AuthFailureLimiter) Check(ip string) bool {
	afl.mu.RLock()
	defer afl.mu.RUnlock()

	rec, ok := afl.failures[ip]
	if !ok {
		return true
	}

	if rec.bannedAt != nil {
		if time.Since(*rec.bannedAt) < afl.banDur {
			return false
		}
		// Ban has expired — allow through; cleanup goroutine will remove the record.
		return true
	}

	return true
}

// RecordFailure records a single authentication failure for the given IP.
// If the failure count exceeds the threshold within the configured window,
// the IP is banned for the configured ban duration.
func (afl *AuthFailureLimiter) RecordFailure(ip string) {
	now := time.Now()

	afl.mu.Lock()
	defer afl.mu.Unlock()

	rec, ok := afl.failures[ip]
	if !ok {
		afl.failures[ip] = &failureRecord{
			count:   1,
			firstAt: now,
		}
		return
	}

	// If the IP is currently banned, don't update the record.
	if rec.bannedAt != nil && time.Since(*rec.bannedAt) < afl.banDur {
		return
	}

	// If the window has expired, reset the counter.
	if time.Since(rec.firstAt) > afl.window {
		rec.count = 1
		rec.firstAt = now
		rec.bannedAt = nil
		return
	}

	rec.count++
	if rec.count >= afl.threshold {
		rec.bannedAt = &now
	}
}

// ExtractClientIP extracts the real client IP from the request.
// If trusted proxies are configured and the request comes from a trusted proxy,
// it parses X-Forwarded-For and returns the rightmost non-trusted IP.
// Otherwise, it returns the IP from RemoteAddr.
func (afl *AuthFailureLimiter) ExtractClientIP(r *http.Request) string {
	remoteIP := stripPort(r.RemoteAddr)

	// If no trusted proxies configured, always use RemoteAddr.
	if len(afl.trustedProxies) == 0 {
		return remoteIP
	}

	// Check if the direct connection is from a trusted proxy.
	if !afl.isTrusted(remoteIP) {
		return remoteIP
	}

	// Parse X-Forwarded-For from right to left, returning the first non-trusted IP.
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteIP
	}

	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		if !afl.isTrusted(ip) {
			return ip
		}
	}

	// All IPs in the chain are trusted; fall back to RemoteAddr.
	return remoteIP
}

// Stop stops the background cleanup goroutine.
func (afl *AuthFailureLimiter) Stop() {
	close(afl.stopCh)
}

// startCleanup launches a background goroutine that periodically removes
// expired failure records and expired bans.
func (afl *AuthFailureLimiter) startCleanup() {
	go func() {
		ticker := time.NewTicker(afl.window)
		defer ticker.Stop()

		for {
			select {
			case <-afl.stopCh:
				return
			case <-ticker.C:
				afl.cleanup()
			}
		}
	}()
}

// cleanup removes expired failure records and expired bans.
func (afl *AuthFailureLimiter) cleanup() {
	now := time.Now()

	afl.mu.Lock()
	defer afl.mu.Unlock()

	for ip, rec := range afl.failures {
		// Remove expired bans.
		if rec.bannedAt != nil {
			if now.Sub(*rec.bannedAt) >= afl.banDur {
				delete(afl.failures, ip)
			}
			continue
		}
		// Remove expired failure windows.
		if now.Sub(rec.firstAt) >= afl.window {
			delete(afl.failures, ip)
		}
	}
}

// isTrusted checks if the given IP is within any of the configured trusted proxy CIDRs.
func (afl *AuthFailureLimiter) isTrusted(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range afl.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// stripPort removes the port from an address string (e.g. "1.2.3.4:8080" → "1.2.3.4").
func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// addr might not have a port.
		return addr
	}
	return host
}
