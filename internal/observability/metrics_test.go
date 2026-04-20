// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestNewMetricsCollector_NilConfig(t *testing.T) {
	mc := NewMetricsCollector(nil)
	if mc == nil {
		t.Fatal("expected non-nil MetricsCollector")
	}
	if mc.Registry() == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestNewMetricsCollector_AllMetricsRegistered(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Gather all metrics from the registry.
	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	expected := []string{
		"graphql_request_duration_seconds",
		"graphql_requests_total",
		"graphql_requests_in_flight",
		"graphql_datasource_query_duration_seconds",
		"graphql_datasource_connection_pool_active",
		"graphql_datasource_connection_pool_idle",
		"graphql_datasource_connection_pool_waiting",
		"graphql_errors_total",
		"graphql_cache_hits_total",
		"graphql_cache_misses_total",
	}

	// Some metrics only appear after first observation. Observe them.
	mc.RequestDuration.WithLabelValues("test", "query", "ds1").Observe(0.1)
	mc.RequestsTotal.WithLabelValues("test", "query", "success", "ds1").Inc()
	mc.RequestsInFlight.Set(1)
	mc.DSQueryDuration.WithLabelValues("ds1", "starrocks").Observe(0.05)
	mc.DSPoolActive.WithLabelValues("ds1").Set(5)
	mc.DSPoolIdle.WithLabelValues("ds1").Set(3)
	mc.DSPoolWaiting.WithLabelValues("ds1").Set(0)
	mc.ErrorsTotal.WithLabelValues("timeout", "ds1").Inc()
	mc.CacheHitsTotal.WithLabelValues("ds1", "memory").Inc()
	mc.CacheMissesTotal.WithLabelValues("ds1", "memory").Inc()

	mfs, err = mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error after observations: %v", err)
	}

	names = make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("metric %q not found in registry", name)
		}
	}
}

func TestNewMetricsCollector_CustomLabels(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{
		CustomLabels: map[string]string{
			"env":     "test",
			"cluster": "us-east-1",
		},
	})

	// Observe a metric so it appears in output.
	mc.RequestsTotal.WithLabelValues("op", "query", "success", "ds").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	found := false
	for _, mf := range mfs {
		if mf.GetName() == "graphql_requests_total" {
			found = true
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}
				if labels["env"] != "test" {
					t.Errorf("expected env=test, got %q", labels["env"])
				}
				if labels["cluster"] != "us-east-1" {
					t.Errorf("expected cluster=us-east-1, got %q", labels["cluster"])
				}
			}
		}
	}
	if !found {
		t.Fatal("graphql_requests_total not found")
	}
}

func TestMetricsCollector_Handler(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe some metrics so they appear in output.
	mc.RequestsInFlight.Set(42)

	handler := mc.Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if !strings.Contains(bodyStr, "graphql_requests_in_flight") {
		t.Error("expected graphql_requests_in_flight in /metrics output")
	}
}

func TestMetricsCollector_HistogramBuckets(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe values to populate histogram buckets.
	mc.RequestDuration.WithLabelValues("op", "query", "ds").Observe(0.05)
	mc.DSQueryDuration.WithLabelValues("ds", "starrocks").Observe(0.01)

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		switch mf.GetName() {
		case "graphql_request_duration_seconds":
			for _, m := range mf.GetMetric() {
				h := m.GetHistogram()
				if len(h.GetBucket()) != len(requestDurationBuckets) {
					t.Errorf("requestDuration: expected %d buckets, got %d",
						len(requestDurationBuckets), len(h.GetBucket()))
				}
			}
		case "graphql_datasource_query_duration_seconds":
			for _, m := range mf.GetMetric() {
				h := m.GetHistogram()
				if len(h.GetBucket()) != len(dsQueryDurationBuckets) {
					t.Errorf("dsQueryDuration: expected %d buckets, got %d",
						len(dsQueryDurationBuckets), len(h.GetBucket()))
				}
			}
		}
	}
}

func TestMetricsCollector_MetricTypes(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe all metrics.
	mc.RequestDuration.WithLabelValues("op", "query", "ds").Observe(0.1)
	mc.RequestsTotal.WithLabelValues("op", "query", "success", "ds").Inc()
	mc.RequestsInFlight.Set(1)
	mc.DSQueryDuration.WithLabelValues("ds", "starrocks").Observe(0.05)
	mc.DSPoolActive.WithLabelValues("ds").Set(1)
	mc.DSPoolIdle.WithLabelValues("ds").Set(1)
	mc.DSPoolWaiting.WithLabelValues("ds").Set(0)
	mc.ErrorsTotal.WithLabelValues("timeout", "ds").Inc()
	mc.CacheHitsTotal.WithLabelValues("ds", "memory").Inc()
	mc.CacheMissesTotal.WithLabelValues("ds", "memory").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	typeMap := make(map[string]string)
	for _, mf := range mfs {
		typeMap[mf.GetName()] = mf.GetType().String()
	}

	expectations := map[string]string{
		"graphql_request_duration_seconds":           "HISTOGRAM",
		"graphql_requests_total":                     "COUNTER",
		"graphql_requests_in_flight":                 "GAUGE",
		"graphql_datasource_query_duration_seconds":  "HISTOGRAM",
		"graphql_datasource_connection_pool_active":  "GAUGE",
		"graphql_datasource_connection_pool_idle":    "GAUGE",
		"graphql_datasource_connection_pool_waiting": "GAUGE",
		"graphql_errors_total":                       "COUNTER",
		"graphql_cache_hits_total":                   "COUNTER",
		"graphql_cache_misses_total":                 "COUNTER",
	}

	for name, expectedType := range expectations {
		got, ok := typeMap[name]
		if !ok {
			t.Errorf("metric %q not found", name)
			continue
		}
		if got != expectedType {
			t.Errorf("metric %q: expected type %s, got %s", name, expectedType, got)
		}
	}
}

func TestMetricsCollector_LabelNames(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe all metrics to populate label sets.
	mc.RequestDuration.WithLabelValues("op", "query", "ds").Observe(0.1)
	mc.RequestsTotal.WithLabelValues("op", "query", "success", "ds").Inc()
	mc.DSQueryDuration.WithLabelValues("ds", "starrocks").Observe(0.05)
	mc.DSPoolActive.WithLabelValues("ds").Set(1)
	mc.DSPoolIdle.WithLabelValues("ds").Set(1)
	mc.DSPoolWaiting.WithLabelValues("ds").Set(0)
	mc.ErrorsTotal.WithLabelValues("timeout", "ds").Inc()
	mc.CacheHitsTotal.WithLabelValues("ds", "memory").Inc()
	mc.CacheMissesTotal.WithLabelValues("ds", "memory").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// Build a map of metric name â†?set of label names.
	labelMap := make(map[string]map[string]bool)
	for _, mf := range mfs {
		name := mf.GetName()
		labels := make(map[string]bool)
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = true
			}
		}
		labelMap[name] = labels
	}

	expectations := map[string][]string{
		"graphql_request_duration_seconds":           {"operation_name", "operation_type", "datasource"},
		"graphql_requests_total":                     {"operation_name", "operation_type", "status", "datasource"},
		"graphql_datasource_query_duration_seconds":  {"datasource", "datasource_type"},
		"graphql_datasource_connection_pool_active":  {"datasource"},
		"graphql_datasource_connection_pool_idle":    {"datasource"},
		"graphql_datasource_connection_pool_waiting": {"datasource"},
		"graphql_errors_total":                       {"error_type", "datasource"},
		"graphql_cache_hits_total":                   {"datasource", "cache_backend"},
		"graphql_cache_misses_total":                 {"datasource", "cache_backend"},
	}

	for name, expectedLabels := range expectations {
		labels, ok := labelMap[name]
		if !ok {
			t.Errorf("metric %q not found", name)
			continue
		}
		for _, l := range expectedLabels {
			if !labels[l] {
				t.Errorf("metric %q missing label %q", name, l)
			}
		}
	}
}

func TestNewMetricsCollector_DuplicateRegistrationPanics(t *testing.T) {
	// Verify that creating two collectors with separate registries works fine.
	mc1 := NewMetricsCollector(nil)
	mc2 := NewMetricsCollector(nil)
	if mc1.Registry() == mc2.Registry() {
		t.Error("expected separate registries for separate collectors")
	}

	// Verify that registering the same metric twice on the same registry panics.
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering duplicate metric")
		}
	}()
	reg := prometheus.NewRegistry()
	c1 := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_dup_total"})
	c2 := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_dup_total"})
	reg.MustRegister(c1)
	reg.MustRegister(c2) // should panic
}
