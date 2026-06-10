// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"testing"
)

func TestMutationMetrics_Registered(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe mutation metrics so they appear in gathered output.
	mc.MutationDuration.WithLabelValues("insert", "starrocks-main", "orders", "success").Observe(0.05)
	mc.MutationsTotal.WithLabelValues("insert", "starrocks-main", "orders", "success").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}

	expected := []string{
		"graphql_mutation_duration_seconds",
		"graphql_mutation_total",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("metric %q not found in registry", name)
		}
	}
}

func TestMutationMetrics_Types(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe to populate.
	mc.MutationDuration.WithLabelValues("update", "starrocks-main", "events", "success").Observe(0.1)
	mc.MutationsTotal.WithLabelValues("update", "starrocks-main", "events", "success").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	typeMap := make(map[string]string)
	for _, mf := range mfs {
		typeMap[mf.GetName()] = mf.GetType().String()
	}

	expectations := map[string]string{
		"graphql_mutation_duration_seconds": "HISTOGRAM",
		"graphql_mutation_total":            "COUNTER",
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

func TestMutationMetrics_Labels(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe both metrics with all label values.
	mc.MutationDuration.WithLabelValues("delete", "starrocks-main", "orders", "error").Observe(0.2)
	mc.MutationsTotal.WithLabelValues("delete", "starrocks-main", "orders", "error").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	// Build a map of metric name → set of label names.
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

	expectedLabels := []string{"operation", "datasource", "table", "status"}

	for _, metricName := range []string{"graphql_mutation_duration_seconds", "graphql_mutation_total"} {
		labels, ok := labelMap[metricName]
		if !ok {
			t.Errorf("metric %q not found", metricName)
			continue
		}
		for _, l := range expectedLabels {
			if !labels[l] {
				t.Errorf("metric %q missing label %q", metricName, l)
			}
		}
	}
}

func TestMutationMetrics_HistogramBuckets(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	mc.MutationDuration.WithLabelValues("insert", "ds1", "table1", "success").Observe(0.025)

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "graphql_mutation_duration_seconds" {
			for _, m := range mf.GetMetric() {
				h := m.GetHistogram()
				if len(h.GetBucket()) != len(mutationDurationBuckets) {
					t.Errorf("mutationDuration: expected %d buckets, got %d",
						len(mutationDurationBuckets), len(h.GetBucket()))
				}
			}
		}
	}
}

func TestMutationMetrics_CounterIncrement(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Increment counter multiple times.
	mc.MutationsTotal.WithLabelValues("insert", "starrocks-main", "orders", "success").Inc()
	mc.MutationsTotal.WithLabelValues("insert", "starrocks-main", "orders", "success").Inc()
	mc.MutationsTotal.WithLabelValues("insert", "starrocks-main", "orders", "success").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "graphql_mutation_total" {
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}
				if labels["operation"] == "insert" && labels["table"] == "orders" && labels["status"] == "success" {
					val := m.GetCounter().GetValue()
					if val != 3 {
						t.Errorf("expected counter value 3, got %v", val)
					}
				}
			}
		}
	}
}

func TestMutationMetrics_HistogramObserve(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Observe multiple values.
	mc.MutationDuration.WithLabelValues("update", "ds1", "users", "success").Observe(0.01)
	mc.MutationDuration.WithLabelValues("update", "ds1", "users", "success").Observe(0.05)
	mc.MutationDuration.WithLabelValues("update", "ds1", "users", "success").Observe(0.1)

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "graphql_mutation_duration_seconds" {
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}
				if labels["operation"] == "update" && labels["table"] == "users" {
					h := m.GetHistogram()
					if h.GetSampleCount() != 3 {
						t.Errorf("expected sample count 3, got %d", h.GetSampleCount())
					}
					expectedSum := 0.01 + 0.05 + 0.1
					if h.GetSampleSum() < expectedSum-0.001 || h.GetSampleSum() > expectedSum+0.001 {
						t.Errorf("expected sample sum ~%v, got %v", expectedSum, h.GetSampleSum())
					}
				}
			}
		}
	}
}

func TestMutationMetrics_MultipleOperationLabels(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{})

	// Record metrics for different operations.
	mc.MutationsTotal.WithLabelValues("insert", "ds1", "orders", "success").Inc()
	mc.MutationsTotal.WithLabelValues("update", "ds1", "orders", "success").Inc()
	mc.MutationsTotal.WithLabelValues("delete", "ds1", "orders", "error").Inc()
	mc.MutationsTotal.WithLabelValues("insertBatch", "ds1", "events", "success").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "graphql_mutation_total" {
			if len(mf.GetMetric()) != 4 {
				t.Errorf("expected 4 metric series, got %d", len(mf.GetMetric()))
			}
		}
	}
}

func TestMutationMetrics_CustomLabels(t *testing.T) {
	mc := NewMetricsCollector(&MetricsConfig{
		CustomLabels: map[string]string{
			"env": "staging",
		},
	})

	mc.MutationsTotal.WithLabelValues("insert", "ds1", "orders", "success").Inc()

	mfs, err := mc.Registry().Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "graphql_mutation_total" {
			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}
				if labels["env"] != "staging" {
					t.Errorf("expected env=staging, got %q", labels["env"])
				}
			}
		}
	}
}
