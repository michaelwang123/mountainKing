package datasource

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProperty37_AdapterRegistryOperations tests adapter registry operations.
// Feature: graphql-multi-datasource-api, Property 37: 适配器注册表操作
// Validates: Requirements 10.3, 10.4, 10.5
func TestProperty37_AdapterRegistryOperations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		registry := NewAdapterRegistry()

		// Generate a random type name
		typeName := rapid.StringMatching(`[a-z][a-z0-9_]{0,19}`).Draw(t, "typeName")

		// Create a dummy factory
		factory := func(name string, config DataSourceConfig) (DataSource, error) {
			return nil, nil
		}

		// 1. Register should succeed the first time (Req 10.3)
		err := registry.Register(typeName, factory)
		if err != nil {
			t.Fatalf("first registration should succeed: %v", err)
		}

		// 2. Get should return a non-nil factory (round-trip, Req 10.4)
		got, ok := registry.Get(typeName)
		if !ok {
			t.Fatal("Get should find registered adapter")
		}
		if got == nil {
			t.Fatal("Get should return non-nil factory")
		}

		// 3. Duplicate registration should return an error (Req 10.5)
		err = registry.Register(typeName, factory)
		if err == nil {
			t.Fatal("duplicate registration should return error")
		}

		// 4. List should contain the registered type name
		list := registry.List()
		found := false
		for _, name := range list {
			if name == typeName {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("List should contain %q", typeName)
		}

		// 5. Get for an unregistered type should return false
		_, ok = registry.Get("nonexistent_type_xyz_" + typeName)
		if ok {
			t.Fatal("Get should return false for unregistered type")
		}
	})
}
