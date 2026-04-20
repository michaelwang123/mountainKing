// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package health provides HTTP health check and readiness probe handlers
// for Kubernetes liveness and readiness probes.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
)

// DataSourceHealthChecker is the interface used by HealthChecker to check
// datasource health. It is satisfied by datasource.DataSourceManager.
type DataSourceHealthChecker interface {
	HealthCheckAll(ctx context.Context) map[string]error
}

// HealthChecker provides HTTP handlers for liveness (/health) and
// readiness (/ready) probes.
type HealthChecker struct {
	dsManager  DataSourceHealthChecker
	version    string
	buildTime  string
	mu         sync.RWMutex
	components map[string]func() error // optional extra liveness components
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker(dsManager DataSourceHealthChecker, version, buildTime string) *HealthChecker {
	return &HealthChecker{
		dsManager:  dsManager,
		version:    version,
		buildTime:  buildTime,
		components: make(map[string]func() error),
	}
}

// RegisterComponent registers an additional liveness check component.
func (hc *HealthChecker) RegisterComponent(name string, check func() error) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.components[name] = check
}

// healthResponse is the JSON structure returned by health endpoints.
type healthResponse struct {
	Status      string            `json:"status"`
	Version     string            `json:"version"`
	BuildTime   string            `json:"build_time"`
	Components  map[string]string `json:"components,omitempty"`
	Datasources map[string]string `json:"datasources,omitempty"`
}

// LivenessCheck handles GET /health.
// All core components normal → 200; any abnormal → 503.
func (hc *HealthChecker) LivenessCheck(w http.ResponseWriter, r *http.Request) {
	hc.mu.RLock()
	components := make(map[string]func() error, len(hc.components))
	for k, v := range hc.components {
		components[k] = v
	}
	hc.mu.RUnlock()

	resp := healthResponse{
		Status:     "ok",
		Version:    hc.version,
		BuildTime:  hc.buildTime,
		Components: make(map[string]string),
	}

	allHealthy := true
	for name, check := range components {
		if err := check(); err != nil {
			resp.Components[name] = err.Error()
			allHealthy = false
		} else {
			resp.Components[name] = "ok"
		}
	}

	statusCode := http.StatusOK
	if !allHealthy {
		resp.Status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}

// ReadinessCheck handles GET /ready.
// At least one datasource available → 200; all unavailable → 503.
func (hc *HealthChecker) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	results := hc.dsManager.HealthCheckAll(r.Context())

	resp := healthResponse{
		Status:      "ok",
		Version:     hc.version,
		BuildTime:   hc.buildTime,
		Datasources: make(map[string]string),
	}

	anyHealthy := false
	for name, err := range results {
		if err != nil {
			resp.Datasources[name] = err.Error()
		} else {
			resp.Datasources[name] = "ok"
			anyHealthy = true
		}
	}

	// If there are no datasources at all, treat as not ready.
	if len(results) == 0 {
		anyHealthy = false
	}

	statusCode := http.StatusOK
	if !anyHealthy {
		resp.Status = "unavailable"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}
