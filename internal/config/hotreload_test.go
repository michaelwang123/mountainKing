// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// =============================================================================
// Property 73: 配置热更�?
// **Validates: Requirements 17.9**
// For any change to logging.level, rate_limit params, or cache TTL in the
// config file, the HotReloader should detect the change and fire the registered
// callback with the new value.
// =============================================================================

func TestProperty73_ConfigHotReload(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Hot-reloadable keys and their value generators
	type keySpec struct {
		key     string
		genOld  func(rt *rapid.T) string
		genNew  func(rt *rapid.T) string
		fmtYAML func(value string) string
	}

	specs := []keySpec{
		{
			key:    "logging.level",
			genOld: func(rt *rapid.T) string { return rapid.SampledFrom([]string{"info", "warn"}).Draw(rt, "oldLevel") },
			genNew: func(rt *rapid.T) string { return rapid.SampledFrom([]string{"debug", "error"}).Draw(rt, "newLevel") },
			fmtYAML: func(v string) string {
				return fmt.Sprintf("logging:\n  level: %s\nrate_limit:\n  requests_per_window: 100\n  window_size: 60s\ncache:\n  default_ttl: 60s\n", v)
			},
		},
		{
			key:    "rate_limit.requests_per_window",
			genOld: func(rt *rapid.T) string { return fmt.Sprintf("%d", rapid.IntRange(50, 100).Draw(rt, "oldRPW")) },
			genNew: func(rt *rapid.T) string { return fmt.Sprintf("%d", rapid.IntRange(200, 500).Draw(rt, "newRPW")) },
			fmtYAML: func(v string) string {
				return fmt.Sprintf("logging:\n  level: info\nrate_limit:\n  requests_per_window: %s\n  window_size: 60s\ncache:\n  default_ttl: 60s\n", v)
			},
		},
		{
			key:    "cache.default_ttl",
			genOld: func(rt *rapid.T) string { return rapid.SampledFrom([]string{"30s", "60s"}).Draw(rt, "oldTTL") },
			genNew: func(rt *rapid.T) string { return rapid.SampledFrom([]string{"120s", "300s"}).Draw(rt, "newTTL") },
			fmtYAML: func(v string) string {
				return fmt.Sprintf("logging:\n  level: info\nrate_limit:\n  requests_per_window: 100\n  window_size: 60s\ncache:\n  default_ttl: %s\n", v)
			},
		},
	}

	rapid.Check(t, func(rt *rapid.T) {
		// Pick a random hot-reloadable key to test
		specIdx := rapid.IntRange(0, len(specs)-1).Draw(rt, "specIdx")
		spec := specs[specIdx]

		oldVal := spec.genOld(rt)
		newVal := spec.genNew(rt)

		// Create temp config file with old value
		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte(spec.fmtYAML(oldVal)), 0644); err != nil {
			rt.Fatalf("failed to write initial config: %v", err)
		}

		hr := NewHotReloader(cfgPath, logger)

		// Track callback invocation
		var callbackFired atomic.Int32
		var receivedKey string
		var receivedValue interface{}
		done := make(chan struct{}, 1)

		hr.OnChange(spec.key, func(key string, value interface{}) {
			receivedKey = key
			receivedValue = value
			callbackFired.Add(1)
			select {
			case done <- struct{}{}:
			default:
			}
		})

		if err := hr.Start(); err != nil {
			rt.Fatalf("failed to start hot reloader: %v", err)
		}
		defer hr.Stop()

		// Wait a bit for watcher to be ready
		time.Sleep(100 * time.Millisecond)

		// Write new config value
		if err := os.WriteFile(cfgPath, []byte(spec.fmtYAML(newVal)), 0644); err != nil {
			rt.Fatalf("failed to write updated config: %v", err)
		}

		// Wait for callback with generous timeout (debounce 500ms + buffer)
		select {
		case <-done:
			// Callback fired successfully
		case <-time.After(3 * time.Second):
			rt.Fatalf("callback for key %q was not fired within 3s after config change", spec.key)
		}

		if callbackFired.Load() == 0 {
			rt.Fatalf("callback was not fired for key %q", spec.key)
		}
		if receivedKey != spec.key {
			rt.Fatalf("expected callback key=%q, got %q", spec.key, receivedKey)
		}
		// Verify the new value was received (compare as string since viper may return different types)
		gotStr := fmt.Sprintf("%v", receivedValue)
		if gotStr != newVal {
			rt.Fatalf("expected new value=%q for key %q, got %q", newVal, spec.key, gotStr)
		}
	})
}

// =============================================================================
// Property 90: 配置热更�?Debounce
// **Validates: Design - ConfigMap 兼容�?*
// For any config file that receives multiple rapid changes within 500ms,
// the HotReloader should only trigger one reload (debounce).
// =============================================================================

func TestProperty90_ConfigHotReloadDebounce(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	rapid.Check(t, func(rt *rapid.T) {
		// Number of rapid writes within the debounce window
		writeCount := rapid.IntRange(3, 8).Draw(rt, "writeCount")

		tmpDir := t.TempDir()
		cfgPath := filepath.Join(tmpDir, "config.yaml")

		// Write initial config
		initialYAML := "logging:\n  level: info\nrate_limit:\n  requests_per_window: 100\n  window_size: 60s\ncache:\n  default_ttl: 60s\n"
		if err := os.WriteFile(cfgPath, []byte(initialYAML), 0644); err != nil {
			rt.Fatalf("failed to write initial config: %v", err)
		}

		hr := NewHotReloader(cfgPath, logger)

		// Count callback invocations
		var callbackCount atomic.Int32
		hr.OnChange("logging.level", func(key string, value interface{}) {
			callbackCount.Add(1)
		})

		if err := hr.Start(); err != nil {
			rt.Fatalf("failed to start hot reloader: %v", err)
		}
		defer hr.Stop()

		// Wait for watcher to be ready
		time.Sleep(100 * time.Millisecond)

		// Write to the config file multiple times rapidly (within 500ms)
		levels := []string{"debug", "warn", "error", "debug", "warn", "error", "debug", "warn"}
		for i := 0; i < writeCount; i++ {
			level := levels[i%len(levels)]
			yaml := fmt.Sprintf("logging:\n  level: %s\nrate_limit:\n  requests_per_window: 100\n  window_size: 60s\ncache:\n  default_ttl: 60s\n", level)
			if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
				rt.Fatalf("failed to write config iteration %d: %v", i, err)
			}
			// Small delay between writes but stay well within the 500ms debounce window
			time.Sleep(20 * time.Millisecond)
		}

		// Wait for debounce (500ms) + generous buffer for the reload to complete
		time.Sleep(1500 * time.Millisecond)

		count := callbackCount.Load()
		if count != 1 {
			rt.Fatalf("expected exactly 1 callback invocation after %d rapid writes (debounce), got %d", writeCount, count)
		}
	})
}
