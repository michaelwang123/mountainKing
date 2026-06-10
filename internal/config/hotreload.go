// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

// Package config provides configuration loading, validation, and hot-reloading
// for the GraphQL API service.
package config

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// HotReloadCallback is called when a hot-reloadable config value changes.
// The key is the config path (e.g. "logging.level") and value is the new value.
type HotReloadCallback func(key string, value any)

// hotReloadableKeys defines the set of config keys that support hot-reloading.
// Changes to keys not in this set require a service restart.
var hotReloadableKeys = map[string]bool{
	"logging.level":                  true,
	"rate_limit.requests_per_window": true,
	"rate_limit.window_size":         true,
	"cache.default_ttl":              true,
	"cache.per_datasource":           true,

	// Mutation config keys (hot-reloadable to allow toggling without restart)
	"mutations.enabled":                        true,
	"mutations.max_affected_rows":              true,
	"mutations.rate_limit.requests_per_window": true,
	"mutations.rate_limit.window_size":         true,
}

// HotReloader watches a configuration file for changes and triggers callbacks
// for hot-reloadable configuration keys. It uses fsnotify for file watching
// with a 500ms debounce to handle K8s ConfigMap symlink replacements.
type HotReloader struct {
	viper      *viper.Viper
	configPath string
	callbacks  map[string][]HotReloadCallback
	mu         sync.RWMutex
	debounce   time.Duration
	logger     *zap.Logger
	stopCh     chan struct{}
	snapshot   map[string]any
}

// NewHotReloader creates a new HotReloader for the given config file.
// The configPath should be an absolute or relative path to the YAML config file.
func NewHotReloader(configPath string, logger *zap.Logger) *HotReloader {
	v := viper.New()
	v.SetConfigFile(configPath)

	return &HotReloader{
		viper:      v,
		configPath: configPath,
		callbacks:  make(map[string][]HotReloadCallback),
		debounce:   500 * time.Millisecond,
		logger:     logger,
		stopCh:     make(chan struct{}),
	}
}

// OnChange registers a callback for a specific config key path.
// Supported hot-reloadable keys:
//   - "logging.level"
//   - "rate_limit.requests_per_window"
//   - "rate_limit.window_size"
//   - "cache.default_ttl"
//   - "cache.per_datasource"
func (hr *HotReloader) OnChange(key string, callback HotReloadCallback) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.callbacks[key] = append(hr.callbacks[key], callback)
}

// Start begins watching the config file for changes.
// It reads the initial config snapshot, then uses fsnotify with 500ms debounce
// to handle multiple rapid events (e.g., K8s ConfigMap symlink replacement:
// REMOVE + CREATE + CHMOD).
func (hr *HotReloader) Start() error {
	// Read initial config to establish baseline snapshot.
	if err := hr.viper.ReadInConfig(); err != nil {
		return fmt.Errorf("hotreload: failed to read initial config: %w", err)
	}
	hr.snapshot = hr.captureSnapshot()

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("hotreload: failed to create watcher: %w", err)
	}

	// Watch the directory containing the config file, not just the file itself.
	// This is required for K8s ConfigMap compatibility where the file is replaced
	// via symlink swap (the inode changes, so watching the file directly would
	// stop receiving events after the first replacement).
	dir := filepath.Dir(hr.configPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("hotreload: failed to watch directory %s: %w", dir, err)
	}

	hr.logger.Info("config hot-reload started",
		zap.String("config", hr.configPath),
		zap.String("watchDir", dir),
		zap.Duration("debounce", hr.debounce),
	)

	go hr.watchLoop(watcher)
	return nil
}

// Stop stops the file watcher and cleans up resources.
func (hr *HotReloader) Stop() {
	close(hr.stopCh)
}

// watchLoop is the main goroutine that processes fsnotify events with debounce.
func (hr *HotReloader) watchLoop(watcher *fsnotify.Watcher) {
	defer watcher.Close()

	var debounceTimer *time.Timer
	configFile := filepath.Base(hr.configPath)

	for {
		select {
		case <-hr.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			hr.logger.Info("config hot-reload stopped")
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only react to events on the config file itself.
			if filepath.Base(event.Name) != configFile {
				continue
			}

			// Reset the debounce timer on every relevant event.
			// This coalesces rapid events (K8s ConfigMap: REMOVE+CREATE+CHMOD)
			// into a single reload after 500ms of quiet.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(hr.debounce, func() {
				hr.reload()
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			hr.logger.Error("config watcher error", zap.Error(err))
		}
	}
}

// reload re-reads the config file, compares values, and fires callbacks for changed keys.
func (hr *HotReloader) reload() {
	if err := hr.viper.ReadInConfig(); err != nil {
		hr.logger.Warn("config hot-reload: failed to read config file, skipping reload",
			zap.String("config", hr.configPath),
			zap.Error(err),
		)
		return
	}

	newSnapshot := hr.captureSnapshot()

	hr.mu.RLock()
	defer hr.mu.RUnlock()

	for key, cbs := range hr.callbacks {
		oldVal, oldExists := hr.snapshot[key]
		newVal := newSnapshot[key]

		if !oldExists || fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
			hr.logger.Info("config hot-reload: value changed",
				zap.String("key", key),
				zap.Any("old", oldVal),
				zap.Any("new", newVal),
			)
			for _, cb := range cbs {
				func() {
					defer func() {
						if r := recover(); r != nil {
							hr.logger.Error("config hot-reload: callback panicked",
								zap.String("key", key),
								zap.Any("panic", r),
							)
						}
					}()
					cb(key, newVal)
				}()
			}
		}
	}

	// Update snapshot after successful reload.
	hr.snapshot = newSnapshot
}

// captureSnapshot takes a snapshot of all hot-reloadable config values.
func (hr *HotReloader) captureSnapshot() map[string]any {
	snap := make(map[string]any, len(hotReloadableKeys))
	for key := range hotReloadableKeys {
		snap[key] = hr.viper.Get(key)
	}
	return snap
}
