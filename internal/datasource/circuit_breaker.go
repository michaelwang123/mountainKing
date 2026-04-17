// Package datasource provides the circuit breaker implementation for data source resilience.
// The CircuitBreaker follows the standard three-state pattern (CLOSED → OPEN → HALF_OPEN)
// and is fully thread-safe: all state checks and transitions happen within a single lock acquisition.
package datasource

import (
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed is the normal state where requests pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen is the tripped state where requests fail fast.
	CircuitOpen
	// CircuitHalfOpen is the probing state where limited requests are allowed.
	CircuitHalfOpen
)

// String returns a human-readable state name.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig holds circuit breaker parameters.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures to trip the breaker (default 5).
	FailureThreshold int
	// OpenDuration is how long to stay in OPEN state before transitioning to HALF_OPEN (default 30s).
	OpenDuration time.Duration
	// HalfOpenMaxRequests is the max number of probe requests allowed in HALF_OPEN state (default 1).
	HalfOpenMaxRequests int
	// SuccessThreshold is the number of consecutive successes in HALF_OPEN to close the breaker (default 2).
	SuccessThreshold int
}

// CircuitBreaker implements the circuit breaker pattern for a single data source.
// All state checks and updates happen within the same lock to prevent race conditions.
type CircuitBreaker struct {
	mu               sync.Mutex
	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureAt    time.Time
	openedAt         time.Time
	halfOpenRequests int
	config           CircuitBreakerConfig
}

// NewCircuitBreaker creates a new CircuitBreaker with the given config.
// Zero-value fields in cfg are replaced with sensible defaults.
func NewCircuitBreaker(cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = 30 * time.Second
	}
	if cfg.HalfOpenMaxRequests <= 0 {
		cfg.HalfOpenMaxRequests = 1
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: cfg,
	}
}

// AllowRequest checks if a request should be allowed through.
// Returns true if allowed, false if the circuit is OPEN and the request should fail fast.
// State check and any resulting transition happen atomically within the same lock.
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if enough time has elapsed to transition to HALF_OPEN.
		if time.Since(cb.openedAt) >= cb.config.OpenDuration {
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			cb.halfOpenRequests = 0
			// Fall through to HALF_OPEN logic below.
		} else {
			return false
		}
		// Transitioned to HALF_OPEN — check if we can allow a probe request.
		if cb.halfOpenRequests < cb.config.HalfOpenMaxRequests {
			cb.halfOpenRequests++
			return true
		}
		return false

	case CircuitHalfOpen:
		if cb.halfOpenRequests < cb.config.HalfOpenMaxRequests {
			cb.halfOpenRequests++
			return true
		}
		return false
	}

	return false
}

// RecordSuccess records a successful request.
// In HALF_OPEN: increments success count, transitions to CLOSED if threshold met.
// In CLOSED: resets failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failureCount = 0

	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.successCount = 0
			cb.halfOpenRequests = 0
		}
	}
}

// RecordFailure records a failed request.
// In CLOSED: increments failure count, transitions to OPEN if threshold met.
// In HALF_OPEN: transitions back to OPEN immediately.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		cb.failureCount++
		cb.lastFailureAt = time.Now()
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
			cb.openedAt = time.Now()
		}

	case CircuitHalfOpen:
		cb.state = CircuitOpen
		cb.openedAt = time.Now()
		cb.failureCount = 0
		cb.successCount = 0
		cb.halfOpenRequests = 0
	}
}

// State returns the current circuit state (for monitoring/logging).
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
