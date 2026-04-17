package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// FallbackRateLimiter wraps a DistributedRateLimiter (primary) and a
// KeyedRateLimiter (fallback). Under normal operation it delegates to the
// primary. When the primary returns an error (e.g. Redis unavailable) it
// automatically degrades to the local fallback and starts a background
// recovery probe that periodically PINGs Redis. Once Redis is reachable
// again the probe switches back to the primary.
type FallbackRateLimiter struct {
	primary       *DistributedRateLimiter
	fallback      *KeyedRateLimiter
	useFallback   atomic.Bool
	probeInterval time.Duration
	logger        *zap.Logger

	// stopCh signals the recovery probe goroutine to exit.
	stopCh chan struct{}
	// probeOnce ensures only one recovery probe runs at a time.
	probeOnce sync.Once
	// mu protects probeOnce reset.
	mu sync.Mutex
}

// NewFallbackRateLimiter creates a FallbackRateLimiter.
// probeInterval controls how often the background goroutine PINGs Redis
// after a degradation (default 30s if <= 0).
func NewFallbackRateLimiter(
	primary *DistributedRateLimiter,
	fallback *KeyedRateLimiter,
	probeInterval time.Duration,
	logger *zap.Logger,
) *FallbackRateLimiter {
	if probeInterval <= 0 {
		probeInterval = 30 * time.Second
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &FallbackRateLimiter{
		primary:       primary,
		fallback:      fallback,
		probeInterval: probeInterval,
		logger:        logger,
		stopCh:        make(chan struct{}),
	}
}

// Allow checks whether count tokens can be consumed for the given key.
// It tries the primary (distributed) limiter first. On error it switches
// to the local fallback and kicks off the recovery probe.
func (frl *FallbackRateLimiter) Allow(ctx context.Context, key string, count int) (*RateLimitResult, error) {
	if frl.useFallback.Load() {
		return frl.fallback.Allow(ctx, key, count)
	}

	result, err := frl.primary.Allow(ctx, key, count)
	if err != nil {
		frl.logger.Warn("distributed rate limiter failed, degrading to local",
			zap.Error(err),
		)
		frl.useFallback.Store(true)
		frl.startRecoveryProbe()
		return frl.fallback.Allow(ctx, key, count)
	}
	return result, nil
}

// Stop terminates the background recovery probe (if running) and the
// fallback's cleanup goroutine.
func (frl *FallbackRateLimiter) Stop() {
	close(frl.stopCh)
	frl.fallback.Stop()
}

// startRecoveryProbe launches a background goroutine (at most once per
// degradation cycle) that periodically PINGs Redis. On success it switches
// back to the primary and resets so a future degradation can start a new
// probe.
func (frl *FallbackRateLimiter) startRecoveryProbe() {
	frl.mu.Lock()
	once := &frl.probeOnce
	frl.mu.Unlock()

	once.Do(func() {
		go frl.runProbe()
	})
}

func (frl *FallbackRateLimiter) runProbe() {
	ticker := time.NewTicker(frl.probeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-frl.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := frl.primary.client.Ping(ctx).Err()
			cancel()

			if err != nil {
				frl.logger.Warn("redis recovery probe failed, staying on local fallback",
					zap.Error(err),
				)
				continue
			}

			frl.logger.Info("redis recovered, switching back to distributed rate limiter")
			frl.useFallback.Store(false)

			// Reset probeOnce so a future degradation can start a new probe.
			frl.mu.Lock()
			frl.probeOnce = sync.Once{}
			frl.mu.Unlock()
			return
		}
	}
}
