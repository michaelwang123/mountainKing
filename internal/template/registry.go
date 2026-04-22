// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package template

import (
	"sort"
	"sync"
	"text/template"
	"time"

	"github.com/michaelwang123/mountainKing/internal/config"
)

// RegisteredTemplate holds a compiled template along with its metadata and
// runtime configuration. Instances are stored in the TemplateRegistry.
type RegisteredTemplate struct {
	Name         string
	Description  string
	Config       config.TemplateConfig
	Template     *template.Template // compiled Go text/template
	ParamSchemas []ParamSchema      // from validator.go — pre-compiled param schemas
	CacheEnabled bool
	CacheTTL     *time.Duration
	CountEnabled bool
}

// TemplateRegistry manages all registered templates with concurrent-safe access.
// It uses sync.RWMutex to allow many concurrent readers while serialising writes.
// Updates are performed atomically by replacing the entire map reference.
type TemplateRegistry struct {
	mu        sync.RWMutex
	templates map[string]*RegisteredTemplate // name → template
	hashes    map[string]string              // name → SHA-256 hex hash
}

// NewTemplateRegistry creates an empty TemplateRegistry ready for use.
func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[string]*RegisteredTemplate),
		hashes:    make(map[string]string),
	}
}

// Get returns the registered template for the given name.
// The second return value indicates whether the template was found.
func (r *TemplateRegistry) Get(name string) (*RegisteredTemplate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.templates[name]
	return t, ok
}

// GetAll returns a snapshot slice of all registered templates, sorted by name.
// The caller receives a copy of the slice; the underlying map is not exposed.
func (r *TemplateRegistry) GetAll() []*RegisteredTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*RegisteredTemplate, 0, len(r.templates))
	for _, t := range r.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Update atomically replaces the entire registry contents.
// This is used during hot-reload to swap in a new set of templates without
// exposing intermediate states to concurrent readers.
func (r *TemplateRegistry) Update(templates map[string]*RegisteredTemplate, hashes map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates = templates
	r.hashes = hashes
}

// GetHash returns the SHA-256 hex hash for the named template.
// The second return value indicates whether the hash was found.
func (r *TemplateRegistry) GetHash(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hashes[name]
	return h, ok
}
