// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import "github.com/prometheus/client_golang/prometheus"

// TemplateMetrics holds Prometheus metrics for template query observability.
type TemplateMetrics struct {
	QueryDuration        *prometheus.HistogramVec // graphql_template_query_duration_seconds
	QueriesTotal         *prometheus.CounterVec   // graphql_template_queries_total
	RenderDuration       *prometheus.HistogramVec // graphql_template_render_duration_seconds
	SemaphoreWait        *prometheus.HistogramVec // graphql_template_semaphore_wait_seconds
	CacheHitsTotal       *prometheus.CounterVec   // graphql_template_cache_hits_total
	RenderGoroutineLeaks prometheus.Gauge         // graphql_template_render_goroutine_leaks
}

// NewTemplateMetrics creates a TemplateMetrics instance with all Prometheus
// metrics registered on the provided Registerer. customLabels are attached as
// constant labels on every metric.
func NewTemplateMetrics(reg prometheus.Registerer, customLabels map[string]string) *TemplateMetrics {
	constLabels := prometheus.Labels{}
	for k, v := range customLabels {
		constLabels[k] = v
	}

	m := &TemplateMetrics{
		QueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "graphql_template_query_duration_seconds",
			Help:        "Template query processing latency in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"template_name", "datasource"}),

		QueriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "graphql_template_queries_total",
			Help:        "Total number of template queries",
			ConstLabels: constLabels,
		}, []string{"template_name", "status"}),

		RenderDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "graphql_template_render_duration_seconds",
			Help:        "Template rendering latency in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"template_name"}),

		SemaphoreWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "graphql_template_semaphore_wait_seconds",
			Help:        "Time spent waiting for semaphore",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"template_name"}),

		CacheHitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "graphql_template_cache_hits_total",
			Help:        "Template query cache hit/miss count",
			ConstLabels: constLabels,
		}, []string{"template_name", "result"}),

		RenderGoroutineLeaks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "graphql_template_render_goroutine_leaks",
			Help:        "Number of leaked render goroutines",
			ConstLabels: constLabels,
		}),
	}

	reg.MustRegister(
		m.QueryDuration,
		m.QueriesTotal,
		m.RenderDuration,
		m.SemaphoreWait,
		m.CacheHitsTotal,
		m.RenderGoroutineLeaks,
	)

	return m
}
