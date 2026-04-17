package datasource

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestProperty74_CircuitBreakerStateTransitions validates the four state
// transitions of the circuit breaker:
//  1. CLOSED → OPEN: consecutive failures >= FailureThreshold
//  2. OPEN → HALF_OPEN: after OpenDuration elapses
//  3. HALF_OPEN → CLOSED: consecutive successes >= SuccessThreshold
//  4. HALF_OPEN → OPEN: any failure
//
// Feature: graphql-multi-datasource-api, Property 74: 熔断器状态转换
// **Validates: Design - 熔断器弹性设计**
func TestProperty74_CircuitBreakerStateTransitions(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		failureThreshold := rapid.IntRange(1, 10).Draw(t, "failureThreshold")
		successThreshold := rapid.IntRange(1, 5).Draw(t, "successThreshold")

		cfg := CircuitBreakerConfig{
			FailureThreshold:    failureThreshold,
			OpenDuration:        time.Millisecond, // short for fast tests
			HalfOpenMaxRequests: successThreshold, // allow enough probes
			SuccessThreshold:    successThreshold,
		}

		cb := NewCircuitBreaker(cfg)

		// --- Transition 1: CLOSED → OPEN ---
		// Verify starts CLOSED.
		if cb.State() != CircuitClosed {
			t.Fatalf("expected initial state CLOSED, got %s", cb.State())
		}

		// Record failures up to threshold-1: should stay CLOSED.
		for i := 0; i < failureThreshold-1; i++ {
			cb.RecordFailure()
			if cb.State() != CircuitClosed {
				t.Fatalf("after %d failures (threshold=%d), expected CLOSED, got %s",
					i+1, failureThreshold, cb.State())
			}
		}

		// One more failure should trip to OPEN.
		cb.RecordFailure()
		if cb.State() != CircuitOpen {
			t.Fatalf("after %d failures, expected OPEN, got %s", failureThreshold, cb.State())
		}

		// --- Transition 2: OPEN → HALF_OPEN ---
		// Wait for OpenDuration to elapse.
		time.Sleep(2 * time.Millisecond)

		// AllowRequest should transition from OPEN to HALF_OPEN.
		allowed := cb.AllowRequest()
		if !allowed {
			t.Fatal("expected AllowRequest to return true after OpenDuration elapsed")
		}
		if cb.State() != CircuitHalfOpen {
			t.Fatalf("expected HALF_OPEN after OpenDuration, got %s", cb.State())
		}

		// --- Transition 4: HALF_OPEN → OPEN (any failure) ---
		cb.RecordFailure()
		if cb.State() != CircuitOpen {
			t.Fatalf("expected OPEN after failure in HALF_OPEN, got %s", cb.State())
		}

		// --- Transition 3: HALF_OPEN → CLOSED ---
		// Wait again for OpenDuration.
		time.Sleep(2 * time.Millisecond)

		// Transition back to HALF_OPEN.
		allowed = cb.AllowRequest()
		if !allowed {
			t.Fatal("expected AllowRequest to return true after second OpenDuration")
		}
		if cb.State() != CircuitHalfOpen {
			t.Fatalf("expected HALF_OPEN, got %s", cb.State())
		}

		// Record enough successes to close the breaker.
		for i := 0; i < successThreshold; i++ {
			// For probes beyond the first, we need AllowRequest to permit them.
			if i > 0 {
				if !cb.AllowRequest() {
					t.Fatalf("expected AllowRequest to permit probe %d in HALF_OPEN", i+1)
				}
			}
			cb.RecordSuccess()
		}

		if cb.State() != CircuitClosed {
			t.Fatalf("after %d successes in HALF_OPEN, expected CLOSED, got %s",
				successThreshold, cb.State())
		}
	})
}

// TestProperty75_CircuitBreakerOpenStateFastFail validates that when the
// circuit breaker is in OPEN state (and OpenDuration has NOT elapsed),
// AllowRequest returns false (fast fail) for every call.
//
// Feature: graphql-multi-datasource-api, Property 75: 熔断器 OPEN 状态快速失败
// **Validates: Design - 熔断器弹性设计**
func TestProperty75_CircuitBreakerOpenStateFastFail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		failureThreshold := rapid.IntRange(1, 10).Draw(t, "failureThreshold")
		numFastFailChecks := rapid.IntRange(1, 50).Draw(t, "numFastFailChecks")

		cfg := CircuitBreakerConfig{
			FailureThreshold:    failureThreshold,
			OpenDuration:        time.Hour, // very long so it won't transition during test
			HalfOpenMaxRequests: 1,
			SuccessThreshold:    1,
		}

		cb := NewCircuitBreaker(cfg)

		// Trip the breaker to OPEN.
		for i := 0; i < failureThreshold; i++ {
			cb.AllowRequest()
			cb.RecordFailure()
		}

		if cb.State() != CircuitOpen {
			t.Fatalf("expected OPEN after %d failures, got %s", failureThreshold, cb.State())
		}

		// Every AllowRequest call should return false (fast fail).
		for i := 0; i < numFastFailChecks; i++ {
			if cb.AllowRequest() {
				t.Fatalf("AllowRequest returned true on check %d while OPEN", i+1)
			}
		}

		// State should still be OPEN.
		if cb.State() != CircuitOpen {
			t.Fatalf("expected state to remain OPEN, got %s", cb.State())
		}
	})
}
