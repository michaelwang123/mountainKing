// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package cache

import (
	"testing"
)

func TestCacheKeyGenerator_Generate_Deterministic(t *testing.T) {
	gen := &CacheKeyGenerator{}
	vars := map[string]any{"limit": 10, "offset": 0}
	query := "{ starrocks(table: \"events\") { id } }"

	key1 := gen.Generate("myds", query, vars)
	key2 := gen.Generate("myds", query, vars)

	if key1 != key2 {
		t.Errorf("expected deterministic keys, got %q and %q", key1, key2)
	}
}

func TestCacheKeyGenerator_Generate_Format(t *testing.T) {
	gen := &CacheKeyGenerator{}
	key := gen.Generate("starrocks", "{ users { id } }", nil)

	// Must start with "cache:starrocks:"
	prefix := "cache:starrocks:"
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("key %q does not have expected prefix %q", key, prefix)
	}

	// Hash part should be 16 hex chars
	hash := key[len(prefix):]
	if len(hash) != 16 {
		t.Errorf("hash part %q should be 16 hex chars, got %d", hash, len(hash))
	}
}

func TestCacheKeyGenerator_Generate_DifferentDatasources(t *testing.T) {
	gen := &CacheKeyGenerator{}
	query := "{ data { id } }"

	key1 := gen.Generate("ds1", query, nil)
	key2 := gen.Generate("ds2", query, nil)

	if key1 == key2 {
		t.Error("keys for different datasources should differ")
	}
}

func TestCacheKeyGenerator_Generate_DifferentQueries(t *testing.T) {
	gen := &CacheKeyGenerator{}

	key1 := gen.Generate("ds", "{ users { id } }", nil)
	key2 := gen.Generate("ds", "{ users { name } }", nil)

	if key1 == key2 {
		t.Error("keys for different queries should differ")
	}
}

func TestCacheKeyGenerator_Generate_DifferentVariables(t *testing.T) {
	gen := &CacheKeyGenerator{}
	query := "{ users { id } }"

	key1 := gen.Generate("ds", query, map[string]any{"a": 1})
	key2 := gen.Generate("ds", query, map[string]any{"a": 2})

	if key1 == key2 {
		t.Error("keys for different variables should differ")
	}
}

func TestCacheKeyGenerator_Generate_VariableOrderIndependent(t *testing.T) {
	gen := &CacheKeyGenerator{}
	query := "{ users { id } }"

	// Go maps are unordered, but our sorted serialization should produce the same key
	vars1 := map[string]any{"b": 2, "a": 1}
	vars2 := map[string]any{"a": 1, "b": 2}

	key1 := gen.Generate("ds", query, vars1)
	key2 := gen.Generate("ds", query, vars2)

	if key1 != key2 {
		t.Errorf("variable order should not affect key: %q vs %q", key1, key2)
	}
}

func TestCacheKeyGenerator_Generate_NilAndEmptyVars(t *testing.T) {
	gen := &CacheKeyGenerator{}
	query := "{ users { id } }"

	keyNil := gen.Generate("ds", query, nil)
	keyEmpty := gen.Generate("ds", query, map[string]any{})

	if keyNil != keyEmpty {
		t.Errorf("nil and empty vars should produce same key: %q vs %q", keyNil, keyEmpty)
	}
}

func TestCacheKeyGenerator_Generate_NormalizesQuery(t *testing.T) {
	gen := &CacheKeyGenerator{}

	// These queries differ only in whitespace →should produce the same key
	key1 := gen.Generate("ds", "{  users  {  id  } }", nil)
	key2 := gen.Generate("ds", "{ users { id } }", nil)

	if key1 != key2 {
		t.Errorf("normalized queries should produce same key: %q vs %q", key1, key2)
	}
}
