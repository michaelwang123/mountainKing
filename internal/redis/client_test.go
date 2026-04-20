// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package redis

import (
	"context"
	"testing"

	"github.com/michaelwang123/mountainKing/internal/config"
)

func TestNewRedisClient_ValidConfig(t *testing.T) {
	cfg := config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "secret",
		DB:       1,
	}

	client, err := NewRedisClient(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	opts := client.Options()
	if opts.Addr != cfg.Addr {
		t.Errorf("expected addr %q, got %q", cfg.Addr, opts.Addr)
	}
	if opts.Password != cfg.Password {
		t.Errorf("expected password %q, got %q", cfg.Password, opts.Password)
	}
	if opts.DB != cfg.DB {
		t.Errorf("expected db %d, got %d", cfg.DB, opts.DB)
	}

	// Close to clean up resources (no actual connection was made)
	_ = client.Close()
}

func TestNewRedisClient_EmptyAddr(t *testing.T) {
	cfg := config.RedisConfig{
		Addr: "",
	}

	client, err := NewRedisClient(cfg)
	if err == nil {
		t.Fatal("expected error for empty addr")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
}

func TestNewRedisClient_DefaultDB(t *testing.T) {
	cfg := config.RedisConfig{
		Addr: "localhost:6379",
	}

	client, err := NewRedisClient(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if client.Options().DB != 0 {
		t.Errorf("expected default db 0, got %d", client.Options().DB)
	}

	_ = client.Close()
}

func TestNewRedisClient_NoConnectionAtCreation(t *testing.T) {
	// Use an unreachable address â€?creation should succeed (lazy connection)
	cfg := config.RedisConfig{
		Addr: "192.0.2.1:6379", // RFC 5737 TEST-NET, guaranteed unreachable
	}

	client, err := NewRedisClient(cfg)
	if err != nil {
		t.Fatalf("expected no error at creation time, got %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	_ = client.Close()
}

func TestPing_NilClient(t *testing.T) {
	err := Ping(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}
