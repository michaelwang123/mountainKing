package cache

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cespare/xxhash/v2"
)

// CacheKeyGenerator generates deterministic cache keys for GraphQL query results.
// Key format: "cache:{datasource}:{xxhash64(normalized_query+sorted_variables)}"
type CacheKeyGenerator struct{}

// Generate produces a cache key from the datasource name, query string, and variables.
// The query is normalized before hashing to improve cache hit rates.
// Variables are sorted by key to ensure deterministic output.
func (g *CacheKeyGenerator) Generate(datasource, query string, variables map[string]interface{}) string {
	normalized := NormalizeQuery(query)
	sortedVars := sortedVariablesJSON(variables)

	h := xxhash.New()
	_, _ = h.WriteString(normalized)
	_, _ = h.WriteString(sortedVars)

	return fmt.Sprintf("cache:%s:%016x", datasource, h.Sum64())
}

// sortedVariablesJSON serializes variables as JSON with keys in sorted order.
// Returns an empty string for nil or empty maps.
func sortedVariablesJSON(vars map[string]interface{}) string {
	if len(vars) == 0 {
		return ""
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make([]interface{}, 0, len(keys)*2)
	for _, k := range keys {
		ordered = append(ordered, k, vars[k])
	}

	b, _ := json.Marshal(ordered)
	return string(b)
}
