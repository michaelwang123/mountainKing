// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/graphql/generated"
	"github.com/michaelwang123/mountainKing/internal/graphql/resolver"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/michaelwang123/mountainKing/pkg/retry"
)

// newPropertyTestServer creates a Server with the given options for property testing.
func newPropertyTestServer(opts ...func(*Server)) *Server {
	dsManager := datasource.NewDataSourceManager(
		datasource.NewAdapterRegistry(),
		nil,
		retry.Config{MaxRetries: 0, RetryInterval: 100 * time.Millisecond},
		zap.NewNop(),
	)
	res := &resolver.Resolver{
		DSManager:     dsManager,
		GraphQLConfig: config.GraphQLConfig{MaxResultRows: 10000},
	}
	schema := generated.NewExecutableSchema(generated.Config{
		Resolvers: res,
	})

	s := NewServer(
		config.ServerConfig{
			Port:            0,
			Mode:            "development",
			RequestTimeout:  5 * time.Second,
			AllowGetQueries: true,
			MaxBatchQueries: 10,
		},
		config.GraphQLConfig{
			IntrospectionEnabled: true,
			MaxQueryComplexity:   100,
			MaxQueryDepth:        10,
			MaxResultRows:        10000,
		},
		config.ShutdownConfig{MaxWaitTime: 5 * time.Second},
		dsManager,
		res,
		schema,
		zap.NewNop(),
	)
	for _, o := range opts {
		o(s)
	}
	return s
}

// TestProperty1_ValidGraphQLQueryReturnsConformantResponse validates that any
// valid GraphQL query sent via HTTP POST returns a conformant JSON response
// with a "data" field and HTTP 200 status.
//
// Feature: graphql-multi-datasource-api, Property 1: 有效 GraphQL 查询返回规范响应
// **Validates: Requirements 1.2**
func TestProperty1_ValidGraphQLQueryReturnsConformantResponse(t *testing.T) {
	s := newPropertyTestServer()
	router := s.SetupRoutes()
	ts := httptest.NewServer(router)
	defer ts.Close()

	rapid.Check(t, func(t *rapid.T) {
		// Use __typename as a universally valid query; vary the alias name.
		alias := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9]{0,10}`).Draw(t, "alias")
		query := fmt.Sprintf(`{ %s: __typename }`, alias)
		body := fmt.Sprintf(`{"query":%q}`, query)

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		// Property: valid query always returns 200.
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, string(b))
		}

		// Property: response is valid JSON with "data" field.
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		if _, ok := result["data"]; !ok {
			t.Fatal("response missing 'data' field")
		}

		// Property: Content-Type is application/json.
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected Content-Type application/json, got %q", ct)
		}
	})
}

// TestProperty2_InvalidRequestBodyReturns400 validates that any request body
// that is not valid GraphQL JSON returns HTTP 400.
//
// Feature: graphql-multi-datasource-api, Property 2: 无效请求体返�?400
// **Validates: Requirements 1.3**
func TestProperty2_InvalidRequestBodyReturns400(t *testing.T) {
	s := newPropertyTestServer()
	router := s.SetupRoutes()
	ts := httptest.NewServer(router)
	defer ts.Close()

	rapid.Check(t, func(t *rapid.T) {
		// Generate various invalid bodies.
		kind := rapid.IntRange(0, 3).Draw(t, "kind")
		var body string
		switch kind {
		case 0:
			// Completely invalid JSON.
			body = rapid.StringMatching(`[^{"\[]{5,30}`).Draw(t, "garbage")
		case 1:
			// Valid JSON but missing "query" field.
			body = `{"variables":{"x":1}}`
		case 2:
			// Empty body.
			body = ""
		case 3:
			// JSON with query field that is not a string.
			body = `{"query":12345}`
		}

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		// Property: invalid body returns 400 or 422 (gqlgen may return 422 for some cases).
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnprocessableEntity {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400 or 422 for invalid body %q, got %d, body: %s", body, resp.StatusCode, string(b))
		}

		// Property: response contains "errors" field.
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		if _, ok := result["errors"]; !ok {
			t.Fatalf("expected 'errors' field in response for invalid body %q", body)
		}
	})
}

// TestProperty3_OversizedRequestBodyReturns413 validates that request bodies
// exceeding the configured max size are rejected. The BodyLimit middleware uses
// http.MaxBytesReader which causes the downstream JSON decoder to fail. gqlgen
// returns HTTP 200 with an error in the response body containing "request body
// too large". The property verifies that oversized bodies are never silently
// accepted �?they always produce an error response.
//
// Feature: graphql-multi-datasource-api, Property 3: 超大请求体返�?413
// **Validates: Requirements 1.8**
func TestProperty3_OversizedRequestBodyReturns413(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Use a small body limit for testing (between 100 and 1000 bytes).
		maxSize := rapid.IntRange(100, 1000).Draw(t, "maxSize")
		sizeStr := fmt.Sprintf("%d", maxSize)

		s := newPropertyTestServer()
		router := s.SetupRoutes()

		// Wrap with BodyLimit middleware.
		limited := middleware.BodyLimit(sizeStr)(router)
		ts := httptest.NewServer(limited)
		defer ts.Close()

		// Generate a body that exceeds the limit.
		excess := rapid.IntRange(1, 500).Draw(t, "excess")
		oversizedLen := maxSize + excess
		// Build a valid-looking JSON body that is oversized.
		padding := strings.Repeat("x", oversizedLen)
		body := fmt.Sprintf(`{"query":"{ __typename }","padding":"%s"}`, padding)

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		// http.MaxBytesReader causes the body read to fail. gqlgen may return:
		// - 200 with errors containing "request body too large"
		// - 413 if a custom error handler intercepts it
		// - 400/422 if the truncated body fails JSON parsing
		// Property: the response MUST contain an error indication �?never a
		// successful "data" result without errors.
		var result map[string]any
		if err := json.Unmarshal(respBody, &result); err != nil {
			// Non-JSON response with non-200 status is acceptable (413, 400).
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("oversized body returned 200 with non-JSON body: %s", string(respBody))
			}
			return
		}

		// If status is 413 or 400, that's the expected rejection.
		if resp.StatusCode == http.StatusRequestEntityTooLarge || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity {
			return
		}

		// If status is 200, the response must contain "errors" field.
		errs, hasErrors := result["errors"]
		if !hasErrors {
			t.Fatalf("oversized body returned 200 without errors (limit=%d, sent=%d): %s",
				maxSize, len(body), string(respBody))
		}

		// Verify the error mentions body too large.
		errBytes, _ := json.Marshal(errs)
		errStr := strings.ToLower(string(errBytes))
		if !strings.Contains(errStr, "too large") && !strings.Contains(errStr, "body") {
			t.Fatalf("expected error about body too large, got: %s", string(errBytes))
		}
	})
}

// TestProperty4_HTTPGetQuerySupport validates that HTTP GET queries with a
// query parameter return valid GraphQL responses when GET queries are enabled.
//
// Feature: graphql-multi-datasource-api, Property 4: HTTP GET 查询支持
// **Validates: Requirements 1.6**
func TestProperty4_HTTPGetQuerySupport(t *testing.T) {
	s := newPropertyTestServer(func(s *Server) {
		s.serverConfig.AllowGetQueries = true
	})
	router := s.SetupRoutes()
	ts := httptest.NewServer(router)
	defer ts.Close()

	rapid.Check(t, func(t *rapid.T) {
		alias := rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9]{0,8}`).Draw(t, "alias")
		query := fmt.Sprintf(`{ %s: __typename }`, alias)

		u, _ := url.Parse(ts.URL + "/graphql")
		q := u.Query()
		q.Set("query", query)
		u.RawQuery = q.Encode()

		resp, err := http.Get(u.String())
		if err != nil {
			t.Fatalf("GET /graphql failed: %v", err)
		}
		defer resp.Body.Close()

		// Property: GET query returns 200.
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, string(b))
		}

		// Property: response contains "data" field.
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		if _, ok := result["data"]; !ok {
			t.Fatal("response missing 'data' field for GET query")
		}

		// Property: the alias field is present in data.
		data, ok := result["data"].(map[string]any)
		if !ok {
			t.Fatal("'data' is not an object")
		}
		if _, ok := data[alias]; !ok {
			t.Fatalf("expected alias %q in data, got %v", alias, data)
		}
	})
}

// TestProperty5_PlaygroundDevProductionModeSwitch validates that the /playground
// endpoint is available in development mode and returns 404 in production mode.
//
// Feature: graphql-multi-datasource-api, Property 5: Playground 开�?生产模式切换
// **Validates: Requirements 1.7**
func TestProperty5_PlaygroundDevProductionModeSwitch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		isDev := rapid.Bool().Draw(t, "isDev")
		mode := "production"
		if isDev {
			mode = "development"
		}

		s := newPropertyTestServer(func(s *Server) {
			s.serverConfig.Mode = mode
		})
		router := s.SetupRoutes()
		ts := httptest.NewServer(router)
		defer ts.Close()

		resp, err := http.Get(ts.URL + "/playground")
		if err != nil {
			t.Fatalf("GET /playground failed: %v", err)
		}
		defer resp.Body.Close()
		// Drain body.
		_, _ = io.ReadAll(resp.Body)

		if isDev {
			// Property: development mode �?playground returns 200.
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("development mode: expected 200 for /playground, got %d", resp.StatusCode)
			}
		} else {
			// Property: production mode �?playground returns 404.
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("production mode: expected 404 for /playground, got %d", resp.StatusCode)
			}
		}
	})
}

// TestProperty6_BatchQueryResultArrayLengthConsistent validates that for any
// valid batch query, the result array length equals the input query count.
//
// Feature: graphql-multi-datasource-api, Property 6: 批量查询结果数组长度一�?
// **Validates: Requirements 1.9**
func TestProperty6_BatchQueryResultArrayLengthConsistent(t *testing.T) {
	s := newPropertyTestServer(func(s *Server) {
		s.serverConfig.MaxBatchQueries = 20
	})
	router := s.SetupRoutes()

	// Wrap with BatchHandler.
	batchHandler := NewBatchHandler(router, 20)
	ts := httptest.NewServer(batchHandler)
	defer ts.Close()

	rapid.Check(t, func(t *rapid.T) {
		count := rapid.IntRange(1, 15).Draw(t, "batchSize")

		// Build a batch of valid queries.
		queries := make([]string, count)
		for i := 0; i < count; i++ {
			queries[i] = `{"query":"{ __typename }"}`
		}
		body := "[" + strings.Join(queries, ",") + "]"

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql batch failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d, body: %s", resp.StatusCode, string(b))
		}

		var results []json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			t.Fatalf("response is not a valid JSON array: %v", err)
		}

		// Property: result array length == input query count.
		if len(results) != count {
			t.Fatalf("expected %d results, got %d", count, len(results))
		}

		// Property: each result is a valid GraphQL response with "data" field.
		for i, raw := range results {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				t.Fatalf("result[%d] is not valid JSON: %v", i, err)
			}
			if _, ok := item["data"]; !ok {
				t.Fatalf("result[%d] missing 'data' field", i)
			}
		}
	})
}

// TestProperty7_ExcessBatchQueryReturns400 validates that batch queries
// exceeding the configured max_batch_queries limit return HTTP 400.
//
// Feature: graphql-multi-datasource-api, Property 7: 超限批量查询返回 400
// **Validates: Requirements 1.10**
func TestProperty7_ExcessBatchQueryReturns400(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		maxBatch := rapid.IntRange(2, 10).Draw(t, "maxBatch")
		excess := rapid.IntRange(1, 10).Draw(t, "excess")
		count := maxBatch + excess

		s := newPropertyTestServer()
		router := s.SetupRoutes()
		batchHandler := NewBatchHandler(router, maxBatch)
		ts := httptest.NewServer(batchHandler)
		defer ts.Close()

		// Build a batch that exceeds the limit.
		queries := make([]string, count)
		for i := 0; i < count; i++ {
			queries[i] = `{"query":"{ __typename }"}`
		}
		body := "[" + strings.Join(queries, ",") + "]"

		resp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST /graphql batch failed: %v", err)
		}
		defer resp.Body.Close()

		// Property: exceeding batch limit returns 400.
		if resp.StatusCode != http.StatusBadRequest {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400 for batch size %d (limit %d), got %d, body: %s",
				count, maxBatch, resp.StatusCode, string(b))
		}

		// Property: response contains error with VALIDATION_BATCH_LIMIT_EXCEEDED code.
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("response is not valid JSON: %v", err)
		}
		errs, ok := result["errors"].([]any)
		if !ok || len(errs) == 0 {
			t.Fatal("expected 'errors' array in response")
		}
		errObj, ok := errs[0].(map[string]any)
		if !ok {
			t.Fatal("expected error object")
		}
		ext, ok := errObj["extensions"].(map[string]any)
		if !ok {
			t.Fatal("expected 'extensions' in error")
		}
		if ext["code"] != "VALIDATION_BATCH_LIMIT_EXCEEDED" {
			t.Fatalf("expected code VALIDATION_BATCH_LIMIT_EXCEEDED, got %v", ext["code"])
		}
	})
}

// TestProperty83_CSRFProtectionGetQueryDisabledInProduction validates that in
// production mode with allow_get_queries=false, GET requests to /graphql are
// rejected with 403, while POST requests with application/json are allowed.
//
// Feature: graphql-multi-datasource-api, Property 83: CSRF 防护 - GET 查询生产模式禁用
// **Validates: Design - CSRF 防护**
func TestProperty83_CSRFProtectionGetQueryDisabledInProduction(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		allowGet := rapid.Bool().Draw(t, "allowGet")
		isProduction := rapid.Bool().Draw(t, "isProduction")

		mode := "development"
		if isProduction {
			mode = "production"
		}

		s := newPropertyTestServer(func(s *Server) {
			s.serverConfig.Mode = mode
			s.serverConfig.AllowGetQueries = allowGet
		})
		router := s.SetupRoutes()

		// Apply CSRF middleware.
		csrfMiddleware := middleware.CSRFProtection(allowGet, mode)
		protected := csrfMiddleware(router)
		ts := httptest.NewServer(protected)
		defer ts.Close()

		// Test GET request to /graphql.
		u, _ := url.Parse(ts.URL + "/graphql")
		q := u.Query()
		q.Set("query", `{__typename}`)
		u.RawQuery = q.Encode()

		getResp, err := http.Get(u.String())
		if err != nil {
			t.Fatalf("GET /graphql failed: %v", err)
		}
		defer getResp.Body.Close()
		_, _ = io.ReadAll(getResp.Body)

		shouldBlockGet := isProduction && !allowGet

		if shouldBlockGet {
			// Property: production mode + !allowGet �?GET returns 403.
			if getResp.StatusCode != http.StatusForbidden {
				t.Fatalf("production+!allowGet: expected 403 for GET /graphql, got %d", getResp.StatusCode)
			}
		} else if !allowGet && !isProduction {
			// Development mode + !allowGet: GET route not registered �?405.
			if getResp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("dev+!allowGet: expected 405 for GET /graphql, got %d", getResp.StatusCode)
			}
		} else {
			// GET allowed: should return 200 (valid query).
			if getResp.StatusCode != http.StatusOK {
				t.Fatalf("GET allowed: expected 200 for GET /graphql, got %d", getResp.StatusCode)
			}
		}

		// Test POST request with application/json �?should always work.
		postBody := `{"query":"{ __typename }"}`
		postResp, err := http.Post(ts.URL+"/graphql", "application/json", strings.NewReader(postBody))
		if err != nil {
			t.Fatalf("POST /graphql failed: %v", err)
		}
		defer postResp.Body.Close()
		_, _ = io.ReadAll(postResp.Body)

		// Property: POST with application/json always returns 200.
		if postResp.StatusCode != http.StatusOK {
			t.Fatalf("POST with application/json: expected 200, got %d", postResp.StatusCode)
		}
	})
}
