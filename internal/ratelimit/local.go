package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// limiterEntry holds a per-key rate.Limiter and the last time it was accessed.
type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// KeyedRateLimiter implements RateLimiter using per-key token buckets.
// Each unique key gets its own rate.Limiter instance.
type KeyedRateLimiter struct {
	mu              sync.Mutex
	limiters        map[string]*limiterEntry
	ratePerSec      rate.Limit
	burst           int
	maxEntries      int
	windowSize      time.Duration
	cleanupInterval time.Duration
	stopCh          chan struct{}
}

// NewKeyedRateLimiter creates a new local rate limiter.
// Parameters:
//   - requestsPerWindow: max requests allowed per window (also the bucket burst size)
//   - windowSize: duration of the rate limit window
//   - maxEntries: maximum number of tracked keys (prevents DDoS memory growth)
func NewKeyedRateLimiter(requestsPerWindow int, windowSize time.Duration, maxEntries int) *KeyedRateLimiter {
	ratePerSec := rate.Limit(float64(requestsPerWindow) / windowSize.Seconds())

	krl := &KeyedRateLimiter{
		limiters:        make(map[string]*limiterEntry),
		ratePerSec:      ratePerSec,
		burst:           requestsPerWindow,
		maxEntries:      maxEntries,
		windowSize:      windowSize,
		cleanupInterval: 2 * windowSize,
		stopCh:          make(chan struct{}),
	}

	go krl.startCleanup()

	return krl
}

// Allow checks whether count tokens can be consumed for the given key.
// If the key doesn't exist and maxEntries is reached, returns Allowed=false.
func (krl *KeyedRateLimiter) Allow(_ context.Context, key string, count int) (*RateLimitResult, error) {
	krl.mu.Lock()
	defer krl.mu.Unlock()

	now := time.Now()

	entry, exists := krl.limiters[key]
	if !exists {
		// Check maxEntries limit before creating a new limiter.
		if len(krl.limiters) >= krl.maxEntries {
			return &RateLimitResult{
				Allowed:   false,
				Limit:     krl.burst,
				Remaining: 0,
				ResetAt:   now.Add(krl.windowSize),
			}, nil
		}
		entry = &limiterEntry{
			limiter:  rate.NewLimiter(krl.ratePerSec, krl.burst),
			lastSeen: now,
		}
		krl.limiters[key] = entry
	}

	entry.lastSeen = now

	allowed := entry.limiter.AllowN(now, count)

	// Estimate remaining tokens. rate.Limiter doesn't expose exact remaining,
	// so we use TokensAt which returns the float token count at a given time.
	tokens := entry.limiter.TokensAt(now)
	remaining := int(tokens)
	if remaining < 0 {
		remaining = 0
	}

	resetAt := now.Add(krl.windowSize)

	return &RateLimitResult{
		Allowed:   allowed,
		Limit:     krl.burst,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}

// Stop terminates the background cleanup goroutine.
func (krl *KeyedRateLimiter) Stop() {
	close(krl.stopCh)
}

// startCleanup periodically removes limiter entries that haven't been
// accessed for longer than 2×windowSize.
func (krl *KeyedRateLimiter) startCleanup() {
	ticker := time.NewTicker(krl.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-krl.stopCh:
			return
		case now := <-ticker.C:
			krl.mu.Lock()
			for key, entry := range krl.limiters {
				if now.Sub(entry.lastSeen) > krl.cleanupInterval {
					delete(krl.limiters, key)
				}
			}
			krl.mu.Unlock()
		}
	}
}
