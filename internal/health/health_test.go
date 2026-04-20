// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockDSHealthChecker implements DataSourceHealthChecker for testing.
type mockDSHealthChecker struct {
	results map[string]error
}

func (m *mockDSHealthChecker) HealthCheckAll(_ context.Context) map[string]error {
	return m.results
}

func TestLivenessCheck_AllHealthy(t *testing.T) {
	hc := NewHealthChecker(&mockDSHealthChecker{}, "1.0.0", "2024-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	hc.LivenessCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
	if resp.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", resp.Version)
	}
}

func TestLivenessCheck_ComponentUnhealthy(t *testing.T) {
	hc := NewHealthChecker(&mockDSHealthChecker{}, "1.0.0", "2024-01-01T00:00:00Z")
	hc.RegisterComponent("db", func() error {
		return errors.New("connection lost")
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	hc.LivenessCheck(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "degraded" {
		t.Fatalf("expected status degraded, got %s", resp.Status)
	}
}

func TestReadinessCheck_AtLeastOneHealthy(t *testing.T) {
	mock := &mockDSHealthChecker{
		results: map[string]error{
			"ds1": nil,
			"ds2": errors.New("down"),
		},
	}
	hc := NewHealthChecker(mock, "1.0.0", "2024-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	hc.ReadinessCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestReadinessCheck_AllUnhealthy(t *testing.T) {
	mock := &mockDSHealthChecker{
		results: map[string]error{
			"ds1": errors.New("down"),
			"ds2": errors.New("timeout"),
		},
	}
	hc := NewHealthChecker(mock, "1.0.0", "2024-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	hc.ReadinessCheck(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestReadinessCheck_NoDatasources(t *testing.T) {
	mock := &mockDSHealthChecker{
		results: map[string]error{},
	}
	hc := NewHealthChecker(mock, "1.0.0", "2024-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	hc.ReadinessCheck(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestReadinessCheck_AllHealthy(t *testing.T) {
	mock := &mockDSHealthChecker{
		results: map[string]error{
			"ds1": nil,
			"ds2": nil,
		},
	}
	hc := NewHealthChecker(mock, "2.0.0", "2024-06-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	hc.ReadinessCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != "2.0.0" {
		t.Fatalf("expected version 2.0.0, got %s", resp.Version)
	}
	if resp.BuildTime != "2024-06-01T00:00:00Z" {
		t.Fatalf("expected build time 2024-06-01T00:00:00Z, got %s", resp.BuildTime)
	}
}

func TestLivenessCheck_ResponseContentType(t *testing.T) {
	hc := NewHealthChecker(&mockDSHealthChecker{}, "1.0.0", "2024-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	hc.LivenessCheck(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}
}
