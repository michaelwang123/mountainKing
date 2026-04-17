package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript is the Lua script for atomic token bucket operations.
// It performs refill calculation, token check, deduction, and TTL update
// in a single EVAL call to guarantee atomicity.
//
// KEYS[1] = "ratelimit:{key}"
// ARGV[1] = now (unix seconds, float)
// ARGV[2] = max_tokens (int)
// ARGV[3] = refill_rate (tokens/sec, float)
// ARGV[4] = requested (int)
// ARGV[5] = ttl (seconds, int)
//
// Returns: [allowed (0/1), remaining (int), reset_seconds (float)]
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local now = tonumber(ARGV[1])
local max_tokens = tonumber(ARGV[2])
local refill_rate = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local data = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(data[1])
local last_refill = tonumber(data[2])

if tokens == nil then
    tokens = max_tokens
    last_refill = now
end

local elapsed = now - last_refill
if elapsed > 0 then
    tokens = math.min(max_tokens, tokens + elapsed * refill_rate)
    last_refill = now
end

local allowed = 0
if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
end

redis.call('HMSET', key, 'tokens', tostring(tokens), 'last_refill', tostring(last_refill))
redis.call('EXPIRE', key, ttl)

local reset_seconds = 0
if tokens < max_tokens then
    reset_seconds = (max_tokens - tokens) / refill_rate
end

return {allowed, math.floor(tokens), tostring(reset_seconds)}
`)

// DistributedRateLimiter implements RateLimiter using Redis + Lua script
// for atomic token bucket operations. Suitable for multi-instance deployments
// where global rate limiting accuracy is required.
type DistributedRateLimiter struct {
	client     *redis.Client
	script     *redis.Script
	maxTokens  int
	refillRate float64
	windowSize time.Duration
}

// NewDistributedRateLimiter creates a new Redis-backed distributed rate limiter.
// Parameters:
//   - client: a connected go-redis client instance
//   - requestsPerWindow: max requests allowed per window (bucket capacity)
//   - windowSize: duration of the rate limit window
func NewDistributedRateLimiter(client *redis.Client, requestsPerWindow int, windowSize time.Duration) *DistributedRateLimiter {
	return &DistributedRateLimiter{
		client:     client,
		script:     tokenBucketScript,
		maxTokens:  requestsPerWindow,
		refillRate: float64(requestsPerWindow) / windowSize.Seconds(),
		windowSize: windowSize,
	}
}

// Allow checks whether count tokens can be consumed for the given key
// by executing the Lua script atomically on Redis.
func (drl *DistributedRateLimiter) Allow(ctx context.Context, key string, count int) (*RateLimitResult, error) {
	redisKey := fmt.Sprintf("ratelimit:%s", key)
	now := float64(time.Now().Unix())
	ttl := int(drl.windowSize.Seconds()) * 2 // TTL = 2× window for safety

	result, err := drl.script.Run(ctx, drl.client, []string{redisKey},
		now,
		drl.maxTokens,
		drl.refillRate,
		count,
		ttl,
	).Slice()
	if err != nil {
		return nil, fmt.Errorf("redis rate limit script failed: %w", err)
	}

	if len(result) < 3 {
		return nil, fmt.Errorf("unexpected lua script result length: %d", len(result))
	}

	allowed, err := toInt64(result[0])
	if err != nil {
		return nil, fmt.Errorf("parsing allowed: %w", err)
	}

	remaining, err := toInt64(result[1])
	if err != nil {
		return nil, fmt.Errorf("parsing remaining: %w", err)
	}

	resetAt := time.Now().Add(drl.windowSize)

	return &RateLimitResult{
		Allowed:   allowed == 1,
		Limit:     drl.maxTokens,
		Remaining: int(remaining),
		ResetAt:   resetAt,
	}, nil
}

// toInt64 converts a Lua script result value to int64.
func toInt64(v interface{}) (int64, error) {
	switch val := v.(type) {
	case int64:
		return val, nil
	case string:
		var n int64
		_, err := fmt.Sscanf(val, "%d", &n)
		return n, err
	default:
		return 0, fmt.Errorf("unexpected type %T", v)
	}
}
