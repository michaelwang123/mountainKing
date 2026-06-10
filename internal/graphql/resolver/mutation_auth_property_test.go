// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package resolver

import (
	"context"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/michaelwang123/mountainKing/internal/config"
	ctxkeys "github.com/michaelwang123/mountainKing/internal/context"
	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
	"github.com/michaelwang123/mountainKing/internal/middleware"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// operationGen generates a random non-"mutation" operation string.
// Used to populate Operations lists that specifically do NOT include the
// "mutation" permission.
func operationGen() *rapid.Generator[string] {
	// Generate random alphanumeric strings that are never "mutation".
	return rapid.Custom(func(t *rapid.T) string {
		candidates := []string{"query", "read", "admin", "subscribe", "introspection", "report", "export"}
		idx := rapid.IntRange(0, len(candidates)-1).Draw(t, "opIdx")
		return candidates[idx]
	})
}

// datasourceNameGen generates a valid datasource name string.
func datasourceNameGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		candidates := []string{"analytics_db", "monitoring", "logs_db", "warehouse", "staging", "prod_sr"}
		idx := rapid.IntRange(0, len(candidates)-1).Draw(t, "dsIdx")
		return candidates[idx]
	})
}

// TestProperty8_AuthorizationMutationPermissionRequired validates that when an
// authenticated identity does NOT have "mutation" in its Operations list,
// checkMutationAuth returns an error with AUTH_INSUFFICIENT_PERMISSION code.
//
// **Property 8: Authorization — Mutation Permission Required**
// no "mutation" in Operations → error
//
// **Validates: Requirements 5.1, 5.2**
func TestProperty8_AuthorizationMutationPermissionRequired(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate an Operations list that does NOT contain "mutation".
		numOps := rapid.IntRange(0, 5).Draw(t, "numOps")
		operations := make([]string, numOps)
		for i := 0; i < numOps; i++ {
			operations[i] = operationGen().Draw(t, "op")
		}

		// Ensure "mutation" is never in the list (defensive — our generator
		// never produces "mutation", but be explicit).
		for i, op := range operations {
			if op == "mutation" {
				operations[i] = "query"
			}
		}

		// Generate a target datasource name.
		targetDS := datasourceNameGen().Draw(t, "targetDS")

		// Create identity with the generated operations (no "mutation").
		identity := &middleware.AuthIdentity{
			Subject:     "test-user",
			Method:      "jwt",
			Operations:  operations,
			Datasources: nil, // unrestricted datasource access
		}

		// Build context with auth identity.
		ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)

		// Create a minimal mutationResolver (only checkMutationAuth is tested).
		r := &mutationResolver{&Resolver{}}

		// Call checkMutationAuth.
		err := r.checkMutationAuth(ctx, targetDS)

		// Property: error must be returned.
		if err == nil {
			t.Fatalf("expected error when 'mutation' not in Operations %v, got nil", operations)
		}

		// Verify error code is AUTH_INSUFFICIENT_PERMISSION.
		gqlErr, ok := err.(*gqlerror.Error)
		if !ok {
			t.Fatalf("expected *gqlerror.Error, got %T: %v", err, err)
		}
		code, _ := gqlErr.Extensions["code"].(string)
		if code != apierrors.ErrAuthInsufficientPermission {
			t.Fatalf("expected error code %q, got %q", apierrors.ErrAuthInsufficientPermission, code)
		}
	})
}

// TestProperty9_AuthorizationDatasourceAccessRequired validates that when an
// authenticated identity has a non-nil Datasources list that does NOT include
// the target datasource, checkMutationAuth returns an error.
//
// **Property 9: Authorization — Datasource Access Required**
// datasource not in Datasources (when non-nil) → error
//
// **Validates: Requirements 5.2, 5.5**
func TestProperty9_AuthorizationDatasourceAccessRequired(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a target datasource that the identity should NOT have access to.
		targetDS := datasourceNameGen().Draw(t, "targetDS")

		// Generate a non-empty Datasources list that does NOT contain targetDS.
		numDS := rapid.IntRange(1, 5).Draw(t, "numDS")
		datasources := make([]string, numDS)
		for i := 0; i < numDS; i++ {
			ds := datasourceNameGen().Draw(t, "allowedDS")
			// Ensure we never accidentally include the target.
			if ds == targetDS {
				ds = targetDS + "_other"
			}
			datasources[i] = ds
		}

		// Create identity WITH "mutation" permission but WITHOUT target datasource access.
		identity := &middleware.AuthIdentity{
			Subject:     "test-user",
			Method:      "apikey",
			Operations:  []string{"query", "mutation"},
			Datasources: datasources, // non-nil, non-empty, does not contain targetDS
		}

		// Build context with auth identity.
		ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)

		// Create a minimal mutationResolver.
		r := &mutationResolver{&Resolver{}}

		// Call checkMutationAuth.
		err := r.checkMutationAuth(ctx, targetDS)

		// Property: error must be returned.
		if err == nil {
			t.Fatalf("expected error when targetDS %q not in Datasources %v, got nil", targetDS, datasources)
		}

		// Verify error code is AUTH_INSUFFICIENT_PERMISSION.
		gqlErr, ok := err.(*gqlerror.Error)
		if !ok {
			t.Fatalf("expected *gqlerror.Error, got %T: %v", err, err)
		}
		code, _ := gqlErr.Extensions["code"].(string)
		if code != apierrors.ErrAuthInsufficientPermission {
			t.Fatalf("expected error code %q, got %q", apierrors.ErrAuthInsufficientPermission, code)
		}
	})
}

// TestProperty10_FeatureToggleDisabledRejectsAll validates that when
// mutations.enabled=false in the config, the resolver returns a
// FEATURE_DISABLED error for any mutation attempt.
//
// **Property 10: Feature Toggle — Disabled Rejects All**
// mutations.enabled=false → MUTATION_FEATURE_DISABLED
//
// **Validates: Requirements 9.4, 10.4**
func TestProperty10_FeatureToggleDisabledRejectsAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random table name.
		table := rapid.StringMatching(`^[a-zA-Z_][a-zA-Z0-9_]{0,20}$`).Draw(t, "table")

		// Create a config with Enabled=false (all other fields are arbitrary).
		cfg := &config.MutationsConfig{
			Enabled:         false, // KEY: disabled
			DatasourceName:  datasourceNameGen().Draw(t, "dsName"),
			MaxAffectedRows: rapid.IntRange(100, 10000).Draw(t, "maxAffected"),
			MaxBatchSize:    rapid.IntRange(10, 500).Draw(t, "maxBatch"),
			MaxSQLLength:    rapid.IntRange(1024, 1048576).Draw(t, "maxSQL"),
		}

		// Set up the atomic config pointer.
		var cfgPtr atomic.Pointer[config.MutationsConfig]
		cfgPtr.Store(cfg)

		// Create identity with full permissions (to prove feature-flag takes precedence).
		identity := &middleware.AuthIdentity{
			Subject:     "admin-user",
			Method:      "jwt",
			Operations:  []string{"query", "mutation"},
			Datasources: nil, // unrestricted
		}

		// Build context with auth identity.
		ctx := context.WithValue(context.Background(), ctxkeys.CtxKeyAuthIdentity, identity)

		// Create a mutationResolver with the disabled config.
		r := &mutationResolver{&Resolver{
			MutationConfig: &cfgPtr,
		}}

		// Test the feature disabled check by simulating what each resolver does:
		// Reading the config and checking Enabled.
		loadedCfg := r.MutationConfig.Load()
		if loadedCfg.Enabled {
			t.Fatal("config should have Enabled=false")
		}

		// Call InsertStarrocks — it should fail with FEATURE_DISABLED.
		// We need minimal setup: the resolver reads config first and returns early.
		result, err := r.InsertStarrocks(ctx, table, nil)

		// Property: result must be nil and error must indicate feature disabled.
		if result != nil {
			t.Fatal("expected nil result when feature is disabled")
		}
		if err == nil {
			t.Fatal("expected error when mutations feature is disabled")
		}

		// Verify error code is MUTATION_FEATURE_DISABLED.
		gqlErr, ok := err.(*gqlerror.Error)
		if !ok {
			t.Fatalf("expected *gqlerror.Error, got %T: %v", err, err)
		}
		code, _ := gqlErr.Extensions["code"].(string)
		if code != apierrors.ErrMutationFeatureDisabled {
			t.Fatalf("expected error code %q, got %q", apierrors.ErrMutationFeatureDisabled, code)
		}
	})
}
