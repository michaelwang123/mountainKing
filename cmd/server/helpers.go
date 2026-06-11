// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/middleware"
)

// needsRedis returns true if any feature requires a Redis connection.
func needsRedis(cfg *config.Config) bool {
	return cfg.RateLimit.Mode == "distributed" || (cfg.Cache.Enabled && cfg.Cache.Backend == "redis")
}

// resolveRedisAddr returns the Redis address from the first available config source.
func resolveRedisAddr(cfg *config.Config) string {
	if cfg.RateLimit.Redis.Addr != "" {
		return cfg.RateLimit.Redis.Addr
	}
	if cfg.Cache.Redis.Addr != "" {
		return cfg.Cache.Redis.Addr
	}
	return "localhost:6379"
}

// resolveRedisPassword returns the Redis password from the first available config source.
func resolveRedisPassword(cfg *config.Config) string {
	if cfg.RateLimit.Redis.Password != "" {
		return cfg.RateLimit.Redis.Password
	}
	return cfg.Cache.Redis.Password
}

// resolveRedisDB returns the Redis DB from the first available config source.
func resolveRedisDB(cfg *config.Config) int {
	if cfg.RateLimit.Redis.Addr != "" {
		return cfg.RateLimit.Redis.DB
	}
	if cfg.Cache.Redis.Addr != "" {
		return cfg.Cache.Redis.DB
	}
	return 0
}

// newAuthFailureLimiterMiddleware wraps AuthFailureLimiter as chi middleware
// that checks if the client IP is banned before proceeding.
func newAuthFailureLimiterMiddleware(afl *middleware.AuthFailureLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := afl.ExtractClientIP(r)
			if !afl.Check(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"AUTH_BRUTE_FORCE_BLOCKED","message":"too many authentication failures","classification":"AUTH"}}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// runHealthProbe performs a local HTTP health check against the running server.
// Used by Docker HEALTHCHECK to probe the container without external tools.
// Reads port from GRAPHQL_SERVER_PORT env var (default 8080).
func runHealthProbe() {
	port := os.Getenv("GRAPHQL_SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%s/health", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health check returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	os.Exit(0)
}
