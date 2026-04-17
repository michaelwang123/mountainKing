// Package redis provides a shared Redis client factory for the GraphQL API service.
// The client is used by distributed rate limiting, Redis cache backend, and Redis tracing hook.
package redis

import (
	"context"
	"fmt"

	"github.com/example/graphql-api/internal/config"
	goredis "github.com/redis/go-redis/v9"
)

// NewRedisClient creates a configured *redis.Client from the given RedisConfig.
// The client uses lazy connection — it does not attempt to connect or ping
// at creation time. Use Ping to verify connectivity when needed.
func NewRedisClient(cfg config.RedisConfig) (*goredis.Client, error) {
	if cfg.Addr == "" {
		return nil, fmt.Errorf("redis address is required")
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	return client, nil
}

// Ping checks the Redis connection health by sending a PING command.
func Ping(ctx context.Context, client *goredis.Client) error {
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return client.Ping(ctx).Err()
}
