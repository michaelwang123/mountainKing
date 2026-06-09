// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package server

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
)

//go:embed playground.html
var playgroundHTML string

// PlaygroundTab defines a named query tab shown in the GraphiQL IDE.
// The Name field is used for documentation; GraphiQL derives visible tab names
// from the GraphQL operation name inside the query string.
type PlaygroundTab struct {
	Name  string
	Query string
}

// playgroundConfig holds the template data injected into playground.html.
type playgroundConfig struct {
	Endpoint string
	TabsJSON template.JS
}

// DefaultPlaygroundTabs returns the 3 pre-configured example query tabs for
// the development mode GraphiQL IDE. The queries use the existing starrocks
// GraphQL field which routes to the mock adapter in dev mode.
//
// Each query includes a named operation (e.g. "query DemoUsers") so that
// GraphiQL can display meaningful tab names.
func DefaultPlaygroundTabs() []PlaygroundTab {
	return []PlaygroundTab{
		{
			Name: "查询 demo_users",
			Query: `query DemoUsers {
  starrocks(table: "demo_users", fields: ["id", "name", "email", "role"]) {
    edges {
      node {
        data
      }
    }
    totalCount
  }
}`,
		},
		{
			Name: "分页 demo_orders",
			Query: `query DemoOrdersPaginated {
  starrocks(table: "demo_orders", fields: ["id", "user_id", "amount", "status"], limit: 5, offset: 0) {
    edges {
      node {
        data
      }
    }
    totalCount
    pageInfo {
      hasNextPage
    }
  }
}`,
		},
		{
			Name: "Schema Introspection",
			Query: `query SchemaIntrospection {
  __schema {
    queryType { name }
    types {
      name
      kind
      fields { name }
    }
  }
}`,
		},
	}
}

// CustomPlaygroundHandler returns an http.HandlerFunc that serves the GraphiQL
// IDE with pre-configured query tabs. The endpoint parameter sets the GraphQL
// API URL, and tabs defines the named example queries.
func CustomPlaygroundHandler(endpoint string, tabs []PlaygroundTab) http.HandlerFunc {
	// Convert tabs to the format expected by GraphiQL's defaultTabs prop.
	// GraphiQL expects: [{ query: "..." }, { query: "..." }]
	type graphiqlTab struct {
		Query string `json:"query"`
	}
	gTabs := make([]graphiqlTab, len(tabs))
	for i, t := range tabs {
		gTabs[i] = graphiqlTab{Query: t.Query}
	}
	tabsJSON, err := json.Marshal(gTabs)
	if err != nil {
		log.Printf("playground: failed to marshal tabs JSON: %v", err)
		tabsJSON = []byte("[]")
	}

	tmpl := template.Must(template.New("playground").Parse(playgroundHTML))
	cfg := playgroundConfig{
		Endpoint: endpoint,
		TabsJSON: template.JS(tabsJSON),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if execErr := tmpl.Execute(w, cfg); execErr != nil {
			log.Printf("playground: template execution error: %v", execErr)
		}
	}
}
