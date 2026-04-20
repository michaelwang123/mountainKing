// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package datasource

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.uber.org/zap"
)

// defaultReconnectInterval is the default initial interval between reconnection attempts.
const defaultReconnectInterval = 5 * time.Second

// defaultMaxReconnectInterval is the default maximum interval between reconnection attempts.
const defaultMaxReconnectInterval = 60 * time.Second

// startReconnectLoop starts a background goroutine that attempts to reconnect
// an unavailable data source using exponential backoff.
// Reconnect interval: min(initialInterval * 2^attempt, maxInterval)
// Default: initial=5s, max=60s
func (m *DataSourceManager) startReconnectLoop(dsName string, initialInterval, maxInterval time.Duration) {
	go func() {
		attempt := 0
		for {
			// Calculate backoff: min(initialInterval * 2^attempt, maxInterval)
			backoff := time.Duration(float64(initialInterval) * math.Pow(2, float64(attempt)))
			if backoff > maxInterval {
				backoff = maxInterval
			}

			select {
			case <-m.stopCh:
				m.logger.Info("reconnect loop stopped due to shutdown",
					zap.String("datasource", dsName),
				)
				return
			case <-time.After(backoff):
			}

			m.mu.RLock()
			ds, ok := m.datasources[dsName]
			m.mu.RUnlock()
			if !ok {
				m.logger.Warn("reconnect loop: datasource not found, stopping",
					zap.String("datasource", dsName),
				)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := ds.Connect(ctx)
			cancel()

			if err != nil {
				attempt++
				m.mu.Lock()
				if st, exists := m.status[dsName]; exists {
					st.ReconnectCount++
					st.LastError = err
					st.NextReconnectAt = time.Now().Add(backoff)
				}
				m.mu.Unlock()

				m.logger.Warn("reconnect attempt failed",
					zap.String("datasource", dsName),
					zap.Int("attempt", attempt),
					zap.Duration("next_backoff", backoff),
					zap.Error(err),
				)
				continue
			}

			// Success: update status and return.
			m.mu.Lock()
			if st, exists := m.status[dsName]; exists {
				st.Available = true
				st.ReconnectCount = 0
				st.LastError = nil
				st.NextReconnectAt = time.Time{}
			}
			m.mu.Unlock()

			m.logger.Info("datasource reconnected successfully",
				zap.String("datasource", dsName),
				zap.Int("attempts", attempt+1),
			)
			return
		}
	}()
}

// parseReconnectIntervals extracts reconnect_interval and max_reconnect_interval
// from a data source's Options map, falling back to defaults if not present.
func parseReconnectIntervals(options map[string]interface{}) (initial, max time.Duration) {
	initial = defaultReconnectInterval
	max = defaultMaxReconnectInterval

	if options == nil {
		return initial, max
	}

	if v, ok := options["reconnect_interval"]; ok {
		if d, err := parseDuration(v); err == nil {
			initial = d
		}
	}

	if v, ok := options["max_reconnect_interval"]; ok {
		if d, err := parseDuration(v); err == nil {
			max = d
		}
	}

	return initial, max
}

// parseDuration attempts to parse a duration from various types that may appear
// in a config options map (string like "5s", or numeric seconds).
func parseDuration(v interface{}) (time.Duration, error) {
	switch val := v.(type) {
	case string:
		return time.ParseDuration(val)
	case time.Duration:
		return val, nil
	case float64:
		return time.Duration(val * float64(time.Second)), nil
	case int:
		return time.Duration(val) * time.Second, nil
	case int64:
		return time.Duration(val) * time.Second, nil
	default:
		return 0, fmt.Errorf("unsupported duration type: %T", v)
	}
}
