package datasource

import (
	"fmt"
	"sort"
	"sync"
)

// AdapterRegistry manages registration and discovery of data source adapters.
// It is safe for concurrent use by multiple goroutines.
type AdapterRegistry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterFactory
}

// NewAdapterRegistry creates a new empty AdapterRegistry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters: make(map[string]AdapterFactory),
	}
}

// Register registers an adapter factory under the given type name.
// Returns an error if the type name is already registered; existing
// registrations are never overwritten.
func (r *AdapterRegistry) Register(typeName string, factory AdapterFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.adapters[typeName]; exists {
		return fmt.Errorf("adapter type %q is already registered", typeName)
	}
	r.adapters[typeName] = factory
	return nil
}

// Get retrieves the adapter factory for the given type name.
// Returns the factory and true if found, nil and false otherwise.
func (r *AdapterRegistry) Get(typeName string) (AdapterFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, ok := r.adapters[typeName]
	return factory, ok
}

// List returns all registered adapter type names in sorted order.
func (r *AdapterRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
