package observability

import (
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// =============================================================================
// Property 39: Prometheus 指标注册完整性
// **Validates: Requirements 11.3-11.10**
// For any metric name defined in the requirements, that metric should exist in
// the registry after creating a MetricsCollector, and its label set should
// match the requirements definition.
// =============================================================================

func TestProperty39_PrometheusMetricRegistrationCompleteness(t *testing.T) {
	type metricSpec struct {
		name   string
		labels []string
	}

	requiredMetrics := []metricSpec{
		{"graphql_request_duration_seconds", []string{"operation_name", "operation_type", "datasource"}},
		{"graphql_requests_total", []string{"operation_name", "operation_type", "status", "datasource"}},
		{"graphql_requests_in_flight", nil},
		{"graphql_datasource_query_duration_seconds", []string{"datasource", "datasource_type"}},
		{"graphql_datasource_connection_pool_active", []string{"datasource"}},
		{"graphql_datasource_connection_pool_idle", []string{"datasource"}},
		{"graphql_datasource_connection_pool_waiting", []string{"datasource"}},
		{"graphql_errors_total", []string{"error_type", "datasource"}},
		{"graphql_cache_hits_total", []string{"datasource", "cache_backend"}},
		{"graphql_cache_misses_total", []string{"datasource", "cache_backend"}},
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Generate random custom labels to ensure they don't interfere with
		// required metric registration.
		numCustom := rapid.IntRange(0, 3).Draw(rt, "numCustomLabels")
		customLabels := make(map[string]string)
		for i := 0; i < numCustom; i++ {
			key := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "labelKey")
			val := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(rt, "labelVal")
			customLabels[key] = val
		}

		mc := NewMetricsCollector(&MetricsConfig{CustomLabels: customLabels})

		// Observe all metrics so they appear in Gather output.
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
			rt.Fatalf("Gather() error: %v", err)
		}

		gathered := make(map[string]map[string]bool)
		for _, mf := range mfs {
			labels := make(map[string]bool)
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = true
				}
			}
			gathered[mf.GetName()] = labels
		}

		for _, spec := range requiredMetrics {
			labels, ok := gathered[spec.name]
			if !ok {
				rt.Fatalf("required metric %q not found in registry", spec.name)
			}
			for _, l := range spec.labels {
				if !labels[l] {
					rt.Fatalf("metric %q missing required label %q", spec.name, l)
				}
			}
		}
	})
}

// =============================================================================
// Property 40: 指标命名规范
// **Validates: Requirements 11.11**
// For any Prometheus metric registered by the service, the name should use
// lowercase letters and underscores, Counter types should end with _total,
// and Histogram types should contain a unit suffix (_seconds).
// =============================================================================

var metricNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestProperty40_MetricNamingConvention(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
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
			rt.Fatalf("Gather() error: %v", err)
		}

		for _, mf := range mfs {
			name := mf.GetName()

			// Skip Go runtime and process metrics (go_*, process_*).
			if strings.HasPrefix(name, "go_") || strings.HasPrefix(name, "process_") {
				continue
			}

			// Only check our graphql_ metrics.
			if !strings.HasPrefix(name, "graphql_") {
				continue
			}

			// Name must be lowercase + underscores.
			if !metricNamePattern.MatchString(name) {
				rt.Fatalf("metric %q does not match naming convention (lowercase + underscores)", name)
			}

			metricType := mf.GetType().String()

			// Counter metrics must end with _total.
			if metricType == "COUNTER" && !strings.HasSuffix(name, "_total") {
				rt.Fatalf("counter metric %q does not end with _total", name)
			}

			// Histogram metrics should contain a unit suffix (_seconds).
			if metricType == "HISTOGRAM" && !strings.HasSuffix(name, "_seconds") {
				rt.Fatalf("histogram metric %q does not end with _seconds", name)
			}
		}
	})
}

// =============================================================================
// Property 41: 自定义标签附加
// **Validates: Requirements 11.12**
// For any custom labels defined in the configuration, all registered Prometheus
// metrics should include those labels with the correct values.
// =============================================================================

func TestProperty41_CustomLabelAttachment(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate 1-3 random custom labels.
		numLabels := rapid.IntRange(1, 3).Draw(rt, "numLabels")
		customLabels := make(map[string]string)
		for i := 0; i < numLabels; i++ {
			key := rapid.StringMatching(`[a-z]{3,8}`).Draw(rt, "key")
			val := rapid.StringMatching(`[a-z0-9]{1,10}`).Draw(rt, "val")
			customLabels[key] = val
		}

		mc := NewMetricsCollector(&MetricsConfig{CustomLabels: customLabels})

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
			rt.Fatalf("Gather() error: %v", err)
		}

		for _, mf := range mfs {
			name := mf.GetName()
			if !strings.HasPrefix(name, "graphql_") {
				continue
			}

			for _, m := range mf.GetMetric() {
				labels := make(map[string]string)
				for _, lp := range m.GetLabel() {
					labels[lp.GetName()] = lp.GetValue()
				}

				for k, v := range customLabels {
					got, ok := labels[k]
					if !ok {
						rt.Fatalf("metric %q missing custom label %q", name, k)
					}
					if got != v {
						rt.Fatalf("metric %q custom label %q: expected %q, got %q", name, k, v, got)
					}
				}
			}
		}
	})
}
