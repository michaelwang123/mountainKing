// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsConfig holds configuration for the metrics collector.
type MetricsConfig struct {
	CustomLabels map[string]string
}

// MetricsCollector registers and exposes all Prometheus metrics for the
// GraphQL API service. It covers request-level, datasource-level, error,
// and cache metrics as defined in requirements 11.1-11.12.
type MetricsCollector struct {
	// Request-level metrics
	RequestDuration  *prometheus.HistogramVec // graphql_request_duration_seconds
	RequestsTotal    *prometheus.CounterVec   // graphql_requests_total
	RequestsInFlight prometheus.Gauge         // graphql_requests_in_flight

	// Datasource-level metrics
	DSQueryDuration *prometheus.HistogramVec // graphql_datasource_query_duration_seconds
	DSPoolActive    *prometheus.GaugeVec     // graphql_datasource_connection_pool_active
	DSPoolIdle      *prometheus.GaugeVec     // graphql_datasource_connection_pool_idle
	DSPoolWaiting   *prometheus.GaugeVec     // graphql_datasource_connection_pool_waiting

	// Error metrics
	ErrorsTotal *prometheus.CounterVec // graphql_errors_total

	// Cache metrics
	CacheHitsTotal   *prometheus.CounterVec // graphql_cache_hits_total
	CacheMissesTotal *prometheus.CounterVec // graphql_cache_misses_total

	// Mutation metrics
	MutationDuration *prometheus.HistogramVec // graphql_mutation_duration_seconds
	MutationsTotal   *prometheus.CounterVec   // graphql_mutation_total

	registry     *prometheus.Registry
	customLabels prometheus.Labels
}

// requestDurationBuckets defines custom histogram buckets for request duration,
// aligned with the P95=200ms (single datasource) and P95=500ms (mixed query) targets.
var requestDurationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2.5, 5, 10}

// dsQueryDurationBuckets defines finer-grained histogram buckets for datasource
// query duration, useful for pinpointing datasource-level latency bottlenecks.
var dsQueryDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// mutationDurationBuckets defines histogram buckets for mutation execution duration,
// aligned with database write latency expectations.
var mutationDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// NewMetricsCollector creates a MetricsCollector with all Prometheus metrics
// registered. Custom labels from cfg.CustomLabels are attached as constant
// labels on every metric. A nil cfg is treated as empty config.
func NewMetricsCollector(cfg *MetricsConfig) *MetricsCollector {
	reg := prometheus.NewRegistry()
	// Include default Go runtime and process collectors.
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	cl := prometheus.Labels{}
	if cfg != nil {
		for k, v := range cfg.CustomLabels {
			cl[k] = v
		}
	}

	mc := &MetricsCollector{
		registry:     reg,
		customLabels: cl,
	}

	mc.RequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "graphql_request_duration_seconds",
		Help:        "Histogram of GraphQL request processing duration in seconds.",
		Buckets:     requestDurationBuckets,
		ConstLabels: cl,
	}, []string{"operation_name", "operation_type", "datasource"})

	mc.RequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "graphql_requests_total",
		Help:        "Total number of GraphQL requests.",
		ConstLabels: cl,
	}, []string{"operation_name", "operation_type", "status", "datasource"})

	mc.RequestsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "graphql_requests_in_flight",
		Help:        "Number of GraphQL requests currently being processed.",
		ConstLabels: cl,
	})

	mc.DSQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "graphql_datasource_query_duration_seconds",
		Help:        "Histogram of datasource query duration in seconds.",
		Buckets:     dsQueryDurationBuckets,
		ConstLabels: cl,
	}, []string{"datasource", "datasource_type"})

	mc.DSPoolActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "graphql_datasource_connection_pool_active",
		Help:        "Current active connections in datasource connection pool.",
		ConstLabels: cl,
	}, []string{"datasource"})

	mc.DSPoolIdle = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "graphql_datasource_connection_pool_idle",
		Help:        "Current idle connections in datasource connection pool.",
		ConstLabels: cl,
	}, []string{"datasource"})

	mc.DSPoolWaiting = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "graphql_datasource_connection_pool_waiting",
		Help:        "Current requests waiting for a connection in datasource connection pool.",
		ConstLabels: cl,
	}, []string{"datasource"})

	mc.ErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "graphql_errors_total",
		Help:        "Total number of GraphQL errors.",
		ConstLabels: cl,
	}, []string{"error_type", "datasource"})

	mc.CacheHitsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "graphql_cache_hits_total",
		Help:        "Total number of cache hits.",
		ConstLabels: cl,
	}, []string{"datasource", "cache_backend"})

	mc.CacheMissesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "graphql_cache_misses_total",
		Help:        "Total number of cache misses.",
		ConstLabels: cl,
	}, []string{"datasource", "cache_backend"})

	mc.MutationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "graphql_mutation_duration_seconds",
		Help:        "Histogram of mutation execution duration in seconds.",
		Buckets:     mutationDurationBuckets,
		ConstLabels: cl,
	}, []string{"operation", "datasource", "table", "status"})

	mc.MutationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "graphql_mutation_total",
		Help:        "Total number of mutation operations.",
		ConstLabels: cl,
	}, []string{"operation", "datasource", "table", "status"})

	reg.MustRegister(
		mc.RequestDuration,
		mc.RequestsTotal,
		mc.RequestsInFlight,
		mc.DSQueryDuration,
		mc.DSPoolActive,
		mc.DSPoolIdle,
		mc.DSPoolWaiting,
		mc.ErrorsTotal,
		mc.CacheHitsTotal,
		mc.CacheMissesTotal,
		mc.MutationDuration,
		mc.MutationsTotal,
	)

	return mc
}

// Handler returns an http.Handler that serves the /metrics endpoint
// using the collector's dedicated registry.
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{})
}

// Registry returns the underlying prometheus.Registry for testing or
// advanced usage.
func (mc *MetricsCollector) Registry() *prometheus.Registry {
	return mc.registry
}

// CustomLabels returns a copy of the custom labels attached to all metrics.
// The returned map is safe to modify without affecting the collector.
func (mc *MetricsCollector) CustomLabels() map[string]string {
	result := make(map[string]string, len(mc.customLabels))
	for k, v := range mc.customLabels {
		result[k] = v
	}
	return result
}
