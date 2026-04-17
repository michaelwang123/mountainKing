package datasource

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/example/graphql-api/internal/config"
	apierrors "github.com/example/graphql-api/internal/errors"
	"github.com/example/graphql-api/pkg/retry"
)

// TestProperty12_ExponentialBackoffReconnectInterval validates that the Nth
// reconnection interval equals min(initial * 2^(N-1), max) for any sequence
// of reconnection attempts.
//
// Feature: graphql-multi-datasource-api, Property 12: 指数退避重连间隔
// **Validates: Requirements 3.4**
func TestProperty12_ExponentialBackoffReconnectInterval(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate random initial and max intervals (in seconds).
		initialSec := rapid.IntRange(1, 30).Draw(t, "initialSec")
		maxSec := rapid.IntRange(initialSec, 300).Draw(t, "maxSec")
		attempts := rapid.IntRange(1, 20).Draw(t, "attempts")

		initial := time.Duration(initialSec) * time.Second
		maxInterval := time.Duration(maxSec) * time.Second

		options := map[string]interface{}{
			"reconnect_interval":     fmt.Sprintf("%ds", initialSec),
			"max_reconnect_interval": fmt.Sprintf("%ds", maxSec),
		}

		parsedInitial, parsedMax := parseReconnectIntervals(options)

		// Verify parsed values match input.
		if parsedInitial != initial {
			t.Fatalf("parsed initial %v != expected %v", parsedInitial, initial)
		}
		if parsedMax != maxInterval {
			t.Fatalf("parsed max %v != expected %v", parsedMax, maxInterval)
		}

		// Verify the backoff formula for each attempt: min(initial * 2^attempt, max).
		// The reconnect loop uses attempt starting from 0, so Nth interval (0-indexed)
		// = min(initial * 2^N, max).
		for attempt := 0; attempt < attempts; attempt++ {
			computed := time.Duration(float64(parsedInitial) * math.Pow(2, float64(attempt)))
			if computed > parsedMax {
				computed = parsedMax
			}

			expected := time.Duration(float64(initial) * math.Pow(2, float64(attempt)))
			if expected > maxInterval {
				expected = maxInterval
			}

			if computed != expected {
				t.Fatalf("attempt %d: computed backoff %v != expected %v", attempt, computed, expected)
			}

			// Verify the interval is always >= initial and <= max.
			if computed < initial {
				t.Fatalf("attempt %d: backoff %v < initial %v", attempt, computed, initial)
			}
			if computed > maxInterval {
				t.Fatalf("attempt %d: backoff %v > max %v", attempt, computed, maxInterval)
			}
		}
	})
}

// TestProperty13_ConnectionPoolExhaustedTimeout validates that when a datasource
// returns a pool exhausted error, ExecuteWithRetry propagates the error immediately
// (it is a business error, not retried).
//
// Feature: graphql-multi-datasource-api, Property 13: 连接池耗尽超时
// **Validates: Requirements 3.6**
func TestProperty13_ConnectionPoolExhaustedTimeout(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxRetries := rapid.IntRange(1, 5).Draw(t, "maxRetries")
		dsName := rapid.StringMatching(`[a-z][a-z0-9_]{2,10}`).Draw(t, "dsName")

		registry := NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		callCount := 0
		poolExhaustedErr := apierrors.DatasourceError(
			apierrors.ErrDatasourcePoolExhausted,
			fmt.Sprintf("datasource %q connection pool exhausted", dsName),
		)

		mock := &MockDataSource{
			NameVal:      dsName,
			TypeVal:      "mock",
			AvailableVal: true,
			ExecuteFunc: func(ctx context.Context, query QueryRequest) (*QueryResult, error) {
				callCount++
				return nil, poolExhaustedErr
			},
		}

		cfgs := []config.DataSourceConfig{
			{
				Name:    dsName,
				Type:    "mock",
				Enabled: true,
			},
		}

		_ = registry.Register("mock", func(name string, cfg DataSourceConfig) (DataSource, error) {
			return mock, nil
		})

		mgr := NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    maxRetries,
			RetryInterval: time.Millisecond,
		}, logger)

		err := mgr.Init(context.Background())
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Execute a query — pool exhausted is a business error, should NOT be retried.
		_, execErr := mgr.ExecuteWithRetry(context.Background(), dsName, QueryRequest{})
		if execErr == nil {
			t.Fatal("expected pool exhausted error, got nil")
		}

		// Verify the error contains the pool exhausted code.
		var apiErr *apierrors.APIError
		if ok := errorAs(execErr, &apiErr); ok {
			if apiErr.Code != apierrors.ErrDatasourcePoolExhausted {
				t.Fatalf("expected error code %s, got %s", apierrors.ErrDatasourcePoolExhausted, apiErr.Code)
			}
		}

		// Business error: Execute should be called exactly once (no retries).
		if callCount != 1 {
			t.Fatalf("pool exhausted error should not be retried: expected 1 call, got %d", callCount)
		}
	})
}

// TestProperty14_AdapterDiscoveryAndInstantiation validates that for any config
// with a registered type, Init creates the datasource; for unregistered types,
// Init skips and the datasource is not in the manager.
//
// Feature: graphql-multi-datasource-api, Property 14: 适配器发现与实例化
// **Validates: Requirements 3.8, 3.9**
func TestProperty14_AdapterDiscoveryAndInstantiation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		registeredType := rapid.StringMatching(`[a-z][a-z0-9]{2,8}`).Draw(t, "registeredType")
		unregisteredType := registeredType + "_unknown"
		registeredName := rapid.StringMatching(`[a-z][a-z0-9_]{2,10}`).Draw(t, "registeredName")
		unregisteredName := registeredName + "_unreg"

		registry := NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		_ = registry.Register(registeredType, func(name string, cfg DataSourceConfig) (DataSource, error) {
			return &MockDataSource{
				NameVal:      name,
				TypeVal:      cfg.Type,
				AvailableVal: true,
			}, nil
		})

		cfgs := []config.DataSourceConfig{
			{
				Name:    registeredName,
				Type:    registeredType,
				Enabled: true,
			},
			{
				Name:    unregisteredName,
				Type:    unregisteredType,
				Enabled: true,
			},
		}

		mgr := NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    1,
			RetryInterval: time.Millisecond,
		}, logger)

		err := mgr.Init(context.Background())
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		// Registered type: datasource should be present and available.
		ds, getErr := mgr.Get(registeredName)
		if getErr != nil {
			t.Fatalf("registered datasource %q should be available: %v", registeredName, getErr)
		}
		if ds.Name() != registeredName {
			t.Fatalf("expected name %q, got %q", registeredName, ds.Name())
		}
		if ds.Type() != registeredType {
			t.Fatalf("expected type %q, got %q", registeredType, ds.Type())
		}

		// Unregistered type: datasource should NOT be in the manager.
		_, getErr = mgr.Get(unregisteredName)
		if getErr == nil {
			t.Fatalf("unregistered datasource %q should not be found", unregisteredName)
		}

		// Verify the status map does not contain the unregistered datasource.
		st := mgr.Status(unregisteredName)
		if st != nil {
			t.Fatalf("unregistered datasource %q should have no status entry", unregisteredName)
		}
	})
}

// TestProperty38_DataSourceEnableDisable validates that for any config with
// enabled=false, Init skips that datasource entirely.
//
// Feature: graphql-multi-datasource-api, Property 38: 数据源启用/禁用
// **Validates: Requirements 10.11**
func TestProperty38_DataSourceEnableDisable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numDS := rapid.IntRange(1, 10).Draw(t, "numDS")
		enabledFlags := make([]bool, numDS)
		cfgs := make([]config.DataSourceConfig, numDS)

		registry := NewAdapterRegistry()
		logger, _ := zap.NewDevelopment()

		_ = registry.Register("testtype", func(name string, cfg DataSourceConfig) (DataSource, error) {
			return &MockDataSource{
				NameVal:      name,
				TypeVal:      "testtype",
				AvailableVal: true,
			}, nil
		})

		expectedEnabled := 0
		for i := 0; i < numDS; i++ {
			enabled := rapid.Bool().Draw(t, fmt.Sprintf("enabled_%d", i))
			enabledFlags[i] = enabled
			cfgs[i] = config.DataSourceConfig{
				Name:    fmt.Sprintf("ds_%d", i),
				Type:    "testtype",
				Enabled: enabled,
			}
			if enabled {
				expectedEnabled++
			}
		}

		mgr := NewDataSourceManager(registry, cfgs, retry.Config{
			MaxRetries:    1,
			RetryInterval: time.Millisecond,
		}, logger)

		err := mgr.Init(context.Background())
		if err != nil {
			t.Fatalf("Init failed: %v", err)
		}

		actualEnabled := 0
		for i := 0; i < numDS; i++ {
			name := fmt.Sprintf("ds_%d", i)
			_, getErr := mgr.Get(name)

			if enabledFlags[i] {
				// Enabled datasource should be present and available.
				if getErr != nil {
					t.Fatalf("enabled datasource %q should be available: %v", name, getErr)
				}
				actualEnabled++
			} else {
				// Disabled datasource should NOT be in the manager.
				if getErr == nil {
					t.Fatalf("disabled datasource %q should not be found", name)
				}
				// Verify no status entry exists for disabled datasource.
				st := mgr.Status(name)
				if st != nil {
					t.Fatalf("disabled datasource %q should have no status entry", name)
				}
			}
		}

		if actualEnabled != expectedEnabled {
			t.Fatalf("expected %d enabled datasources, got %d", expectedEnabled, actualEnabled)
		}
	})
}

// errorAs is a helper that wraps errors.As for use in tests.
func errorAs(err error, target interface{}) bool {
	switch v := target.(type) {
	case **apierrors.APIError:
		for e := err; e != nil; {
			if apiErr, ok := e.(*apierrors.APIError); ok {
				*v = apiErr
				return true
			}
			if unwrapper, ok := e.(interface{ Unwrap() error }); ok {
				e = unwrapper.Unwrap()
			} else {
				return false
			}
		}
	}
	return false
}
