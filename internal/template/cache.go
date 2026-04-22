// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/cespare/xxhash/v2"
	"github.com/michaelwang123/mountainKing/internal/cache"
)

// generateCacheKey produces a deterministic cache key for a template query.
//
// Format: cache:template:{template_name}:{xxhash64(canonical_string)}
//
// The canonical_string is built from sorted params, sorted fields, first,
// offset, and orderBy (original order preserved) joined by "|".
func generateCacheKey(
	templateName string,
	params map[string]any,
	fields []string,
	first *int,
	offset *int,
	orderBy []TemplateOrderByParam,
) string {
	canonical := buildCanonicalString(params, fields, first, offset, orderBy)

	h := xxhash.New()
	_, _ = h.WriteString(canonical)

	return fmt.Sprintf("cache:template:%s:%016x", templateName, h.Sum64())
}

// generateCountCacheKey produces a deterministic cache key for a COUNT query.
//
// Format: cache:template:{template_name}:{xxhash64(sorted_params)}:count
//
// Only templateName + params are used (no fields/first/offset/orderBy) because
// COUNT(*) results are independent of pagination and field selection.
func generateCountCacheKey(templateName string, params map[string]any) string {
	paramStr := buildSortedParams(params)

	h := xxhash.New()
	_, _ = h.WriteString(paramStr)

	return fmt.Sprintf("cache:template:%s:%016x:count", templateName, h.Sum64())
}

// shouldCache returns whether the query result should be cached.
// It returns false if the template has caching disabled or the client
// requested a cache bypass (extensions.cache=false).
func shouldCache(tmpl *RegisteredTemplate, skipCache bool) bool {
	if !tmpl.CacheEnabled {
		return false
	}
	if skipCache {
		return false
	}
	return true
}

// executeWithCache executes a data query with optional cache integration.
//
// If cacheLayer is nil, the loader is called directly.
// Otherwise, CacheLayer.GetOrLoad is used with JSON serialization:
//   - loader: calls the provided loader, JSON-marshals the result to []byte
//   - cache hit: JSON-unmarshals []byte back to []map[string]any
//
// Returns (data, loaderCalled, err) where loaderCalled indicates whether the
// loader was actually invoked (true = cache miss, false = cache hit).
func executeWithCache(
	ctx context.Context,
	cacheLayer *cache.CacheLayer,
	datasourceName string,
	cacheKey string,
	loader func() ([]map[string]any, error),
) ([]map[string]any, bool, error) {
	// No cache layer — call loader directly.
	if cacheLayer == nil {
		data, err := loader()
		return data, true, err
	}

	loaderCalled := false

	raw, err := cacheLayer.GetOrLoad(ctx, cacheKey, datasourceName, func() ([]byte, error) {
		loaderCalled = true
		data, loadErr := loader()
		if loadErr != nil {
			return nil, loadErr
		}
		b, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			return nil, fmt.Errorf("json marshal cache data: %w", marshalErr)
		}
		return b, nil
	})
	if err != nil {
		return nil, loaderCalled, err
	}

	// Empty / nil result from cache (e.g. empty marker).
	if len(raw) == 0 {
		return nil, loaderCalled, nil
	}

	var result []map[string]any
	if unmarshalErr := json.Unmarshal(raw, &result); unmarshalErr != nil {
		return nil, loaderCalled, fmt.Errorf("json unmarshal cached data: %w", unmarshalErr)
	}
	return result, loaderCalled, nil
}

// executeCount executes a COUNT query with optional cache integration.
// Similar to executeWithCache but serialises int64 as JSON.
func executeCount(
	ctx context.Context,
	cacheLayer *cache.CacheLayer,
	datasourceName string,
	countKey string,
	loader func() (int64, error),
) (int64, error) {
	// No cache layer — call loader directly.
	if cacheLayer == nil {
		return loader()
	}

	raw, err := cacheLayer.GetOrLoad(ctx, countKey, datasourceName, func() ([]byte, error) {
		count, loadErr := loader()
		if loadErr != nil {
			return nil, loadErr
		}
		b, marshalErr := json.Marshal(count)
		if marshalErr != nil {
			return nil, fmt.Errorf("json marshal count: %w", marshalErr)
		}
		return b, nil
	})
	if err != nil {
		return 0, err
	}

	if len(raw) == 0 {
		return 0, nil
	}

	var count int64
	if unmarshalErr := json.Unmarshal(raw, &count); unmarshalErr != nil {
		return 0, fmt.Errorf("json unmarshal cached count: %w", unmarshalErr)
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Internal helpers for canonical string construction
// ---------------------------------------------------------------------------

// buildCanonicalString constructs the deterministic string used for cache key
// hashing. Parts are joined by "|":
//
//	params|fields|first|offset|orderBy
func buildCanonicalString(
	params map[string]any,
	fields []string,
	first *int,
	offset *int,
	orderBy []TemplateOrderByParam,
) string {
	paramStr := buildSortedParams(params)

	// Fields: sort alphabetically, join with ","
	sortedFields := make([]string, len(fields))
	copy(sortedFields, fields)
	sort.Strings(sortedFields)
	fieldStr := joinStrings(sortedFields, ",")

	// First
	firstStr := "nil"
	if first != nil {
		firstStr = strconv.Itoa(*first)
	}

	// Offset
	offsetStr := "nil"
	if offset != nil {
		offsetStr = strconv.Itoa(*offset)
	}

	// OrderBy: keep original order, each as "field:direction", join with ","
	obParts := make([]string, 0, len(orderBy))
	for _, ob := range orderBy {
		obParts = append(obParts, ob.Field+":"+ob.Direction)
	}
	obStr := joinStrings(obParts, ",")

	return paramStr + "|" + fieldStr + "|" + firstStr + "|" + offsetStr + "|" + obStr
}

// buildSortedParams sorts parameter keys alphabetically and formats each pair
// as "key=value", joined by "&".
func buildSortedParams(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+fmt.Sprintf("%v", params[k]))
	}
	return joinStrings(parts, "&")
}

// joinStrings joins a slice of strings with the given separator.
// Returns "" for an empty slice.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}
