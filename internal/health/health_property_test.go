// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"pgregory.net/rapid"
)

// Property 59: 健康检查状态码
// **Validates: Requirements 15.3, 15.4**
//
// - When all datasources healthy → /health returns 200, /ready returns 200
// - When all datasources unhealthy → /health returns 200 (liveness), /ready returns 503
// - When at least one datasource healthy → /ready returns 200
func TestProperty59_HealthCheckStatusCodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate 1-10 datasources with random health states.
		numDS := rapid.IntRange(1, 10).Draw(t, "numDatasources")
		dsResults := make(map[string]error, numDS)
		healthyCount := 0

		for i := 0; i < numDS; i++ {
			name := rapid.StringMatching(`^ds[a-z]{1,5}$`).Draw(t, "dsName")
			// Ensure unique names by appending index.
			name = name + rapid.StringMatching(`^[0-9]{1,3}$`).Draw(t, "dsSuffix")
			isHealthy := rapid.Bool().Draw(t, "isHealthy")
			if isHealthy {
				dsResults[name] = nil
				healthyCount++
			} else {
				dsResults[name] = errors.New("unhealthy")
			}
		}

		mock := &mockDSHealthChecker{results: dsResults}
		version := rapid.StringMatching(`^[0-9]+\.[0-9]+\.[0-9]+$`).Draw(t, "version")
		buildTime := rapid.StringMatching(`^2024-[0-9]{2}-[0-9]{2}T00:00:00Z$`).Draw(t, "buildTime")
		hc := NewHealthChecker(mock, version, buildTime)

		// --- Liveness: /health ---
		// With no registered components, liveness always returns 200.
		reqHealth := httptest.NewRequest(http.MethodGet, "/health", nil)
		wHealth := httptest.NewRecorder()
		hc.LivenessCheck(wHealth, reqHealth)

		if wHealth.Code != http.StatusOK {
			t.Fatalf("liveness: expected 200 (no components registered), got %d", wHealth.Code)
		}

		var livenessResp healthResponse
		if err := json.NewDecoder(wHealth.Body).Decode(&livenessResp); err != nil {
			t.Fatalf("liveness: failed to decode response: %v", err)
		}
		if livenessResp.Version != version {
			t.Fatalf("liveness: expected version %s, got %s", version, livenessResp.Version)
		}
		if livenessResp.BuildTime != buildTime {
			t.Fatalf("liveness: expected buildTime %s, got %s", buildTime, livenessResp.BuildTime)
		}

		// --- Readiness: /ready ---
		reqReady := httptest.NewRequest(http.MethodGet, "/ready", nil)
		wReady := httptest.NewRecorder()
		hc.ReadinessCheck(wReady, reqReady)

		if healthyCount > 0 {
			// At least one datasource healthy → 200
			if wReady.Code != http.StatusOK {
				t.Fatalf("readiness: expected 200 (healthy=%d), got %d", healthyCount, wReady.Code)
			}
		} else {
			// All datasources unhealthy → 503
			if wReady.Code != http.StatusServiceUnavailable {
				t.Fatalf("readiness: expected 503 (all unhealthy), got %d", wReady.Code)
			}
		}

		var readyResp healthResponse
		if err := json.NewDecoder(wReady.Body).Decode(&readyResp); err != nil {
			t.Fatalf("readiness: failed to decode response: %v", err)
		}
		if readyResp.Version != version {
			t.Fatalf("readiness: expected version %s, got %s", version, readyResp.Version)
		}
	})
}

// Test liveness with unhealthy components returns 503.
func TestProperty59_LivenessWithUnhealthyComponent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numComponents := rapid.IntRange(1, 5).Draw(t, "numComponents")
		hasUnhealthy := rapid.Bool().Draw(t, "hasUnhealthy")

		mock := &mockDSHealthChecker{results: map[string]error{"ds1": nil}}
		hc := NewHealthChecker(mock, "1.0.0", "2024-01-01T00:00:00Z")

		unhealthyCount := 0
		for i := 0; i < numComponents; i++ {
			name := fmt.Sprintf("comp_%d", i)
			if hasUnhealthy && i == 0 {
				hc.RegisterComponent(name, func() error {
					return errors.New("component failure")
				})
				unhealthyCount++
			} else {
				hc.RegisterComponent(name, func() error {
					return nil
				})
			}
		}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		hc.LivenessCheck(w, req)

		if unhealthyCount > 0 {
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503 with unhealthy component, got %d", w.Code)
			}
		} else {
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 with all healthy components, got %d", w.Code)
			}
		}
	})
}

// Verify the mockDSHealthChecker satisfies the interface.
var _ DataSourceHealthChecker = (*mockDSHealthChecker)(nil)
