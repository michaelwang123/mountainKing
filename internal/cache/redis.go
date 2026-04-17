package cache

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RedisCache is a Redis-backed Cache implementation.
// It uses gob serialization for values and SCAN + DEL for prefix deletion.
type RedisCache struct {
	client *goredis.Client
}

// NewRedisCache creates a new RedisCache wrapping the given shared Redis client.
// The client should be created via internal/redis.NewRedisClient.
func NewRedisCache(client *goredis.Client) *RedisCache {
	return &RedisCache{client: client}
}

// Get retrieves a cached value by key from Redis.
func (rc *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := rc.client.Get(ctx, key).Bytes()
	if err == goredis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

// Set stores a value with the given key and TTL in Redis.
func (rc *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return rc.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a single cache entry by key from Redis.
func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	return rc.client.Del(ctx, key).Err()
}

// DeleteByPrefix removes all cache entries whose keys match the given prefix.
// Uses SCAN + DEL to avoid blocking Redis (KEYS command is not used).
func (rc *RedisCache) DeleteByPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	for {
		keys, nextCursor, err := rc.client.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := rc.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// Clear removes all cache entries by flushing the current Redis database.
func (rc *RedisCache) Clear(ctx context.Context) error {
	return rc.client.FlushDB(ctx).Err()
}
