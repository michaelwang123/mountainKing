package cache

import (
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func TestRedisCache_InterfaceCompliance(t *testing.T) {
	var _ Cache = (*RedisCache)(nil)
}

func TestNewRedisCache(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{
		Addr: "localhost:6379",
	})
	defer client.Close()

	rc := NewRedisCache(client)
	if rc == nil {
		t.Fatal("expected non-nil RedisCache")
	}
	if rc.client != client {
		t.Fatal("expected RedisCache to wrap the provided client")
	}
}
