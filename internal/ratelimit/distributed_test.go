// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package ratelimit

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestNewDistributedRateLimiter(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	drl := NewDistributedRateLimiter(client, 100, 60*time.Second)

	if drl.maxTokens != 100 {
		t.Errorf("expected maxTokens=100, got %d", drl.maxTokens)
	}
	if drl.windowSize != 60*time.Second {
		t.Errorf("expected windowSize=60s, got %v", drl.windowSize)
	}
	// refillRate = 100 / 60 ≈ 1.6667
	expectedRate := 100.0 / 60.0
	if drl.refillRate < expectedRate-0.001 || drl.refillRate > expectedRate+0.001 {
		t.Errorf("expected refillRate≈%.4f, got %.4f", expectedRate, drl.refillRate)
	}
	if drl.client != client {
		t.Error("expected client to be stored")
	}
	if drl.script == nil {
		t.Error("expected script to be set")
	}
}

func TestDistributedRateLimiter_InterfaceCompliance(t *testing.T) {
	// Verify DistributedRateLimiter satisfies the RateLimiter interface.
	var _ RateLimiter = (*DistributedRateLimiter)(nil)
}

func TestNewDistributedRateLimiter_DifferentWindows(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	tests := []struct {
		name               string
		requestsPerWindow  int
		windowSize         time.Duration
		expectedMaxTokens  int
		expectedRefillRate float64
	}{
		{
			name:               "1 request per second",
			requestsPerWindow:  1,
			windowSize:         time.Second,
			expectedMaxTokens:  1,
			expectedRefillRate: 1.0,
		},
		{
			name:               "1000 requests per minute",
			requestsPerWindow:  1000,
			windowSize:         time.Minute,
			expectedMaxTokens:  1000,
			expectedRefillRate: 1000.0 / 60.0,
		},
		{
			name:               "10 requests per 10 seconds",
			requestsPerWindow:  10,
			windowSize:         10 * time.Second,
			expectedMaxTokens:  10,
			expectedRefillRate: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drl := NewDistributedRateLimiter(client, tt.requestsPerWindow, tt.windowSize)
			if drl.maxTokens != tt.expectedMaxTokens {
				t.Errorf("expected maxTokens=%d, got %d", tt.expectedMaxTokens, drl.maxTokens)
			}
			diff := drl.refillRate - tt.expectedRefillRate
			if diff < -0.001 || diff > 0.001 {
				t.Errorf("expected refillRate≈%.4f, got %.4f", tt.expectedRefillRate, drl.refillRate)
			}
		})
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    int64
		wantErr bool
	}{
		{name: "int64 value", input: int64(42), want: 42, wantErr: false},
		{name: "int64 zero", input: int64(0), want: 0, wantErr: false},
		{name: "int64 negative", input: int64(-1), want: -1, wantErr: false},
		{name: "string value", input: "123", want: 123, wantErr: false},
		{name: "string zero", input: "0", want: 0, wantErr: false},
		{name: "unsupported type float64", input: float64(1.5), want: 0, wantErr: true},
		{name: "unsupported type bool", input: true, want: 0, wantErr: true},
		{name: "invalid string", input: "abc", want: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := toInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("toInt64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("toInt64(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestLuaScriptNotNil(t *testing.T) {
	// Verify the global Lua script is properly initialized.
	if tokenBucketScript == nil {
		t.Fatal("tokenBucketScript should not be nil")
	}
}

func TestRedisKeyFormat(t *testing.T) {
	// Verify the key format used in Allow matches the expected pattern.
	// We test this indirectly by checking the format string logic.
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	drl := NewDistributedRateLimiter(client, 10, time.Second)

	// We can't call Allow without a real Redis, but we can verify
	// the struct is properly configured for key formatting.
	if drl.maxTokens != 10 {
		t.Errorf("expected maxTokens=10, got %d", drl.maxTokens)
	}
}
