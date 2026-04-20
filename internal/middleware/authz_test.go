// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package middleware

import (
	"testing"

	apierrors "github.com/michaelwang123/mountainKing/internal/errors"
)

func TestDefaultAuthorizer_NilIdentity(t *testing.T) {
	authz := &DefaultAuthorizer{}
	err := authz.Authorize(nil, "starrocks", "query")
	if err == nil {
		t.Fatal("expected error for nil identity")
	}
	ae, ok := err.(*AuthError)
	if !ok {
		t.Fatalf("expected *AuthError, got %T", err)
	}
	if ae.Code != apierrors.ErrAuthInsufficientPermission {
		t.Errorf("expected code %s, got %s", apierrors.ErrAuthInsufficientPermission, ae.Code)
	}
	if ae.StatusCode != 403 {
		t.Errorf("expected status 403, got %d", ae.StatusCode)
	}
}

func TestDefaultAuthorizer_EmptyPermissions_FullAccess(t *testing.T) {
	authz := &DefaultAuthorizer{}
	identity := &AuthIdentity{
		Subject:     "user1",
		Method:      "jwt",
		Datasources: nil,
		Operations:  nil,
	}

	// Empty slices mean full access â€?any datasource and operation allowed.
	if err := authz.Authorize(identity, "starrocks", "query"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := authz.Authorize(identity, "prometheus", "mutation"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := authz.Authorize(identity, "anything", "query"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestDefaultAuthorizer_DatasourceRestriction(t *testing.T) {
	authz := &DefaultAuthorizer{}
	identity := &AuthIdentity{
		Subject:     "key-1",
		Method:      "apikey",
		Datasources: []string{"starrocks"},
		Operations:  nil,
	}

	if err := authz.Authorize(identity, "starrocks", "query"); err != nil {
		t.Errorf("expected nil for allowed datasource, got %v", err)
	}

	err := authz.Authorize(identity, "prometheus", "query")
	if err == nil {
		t.Fatal("expected error for disallowed datasource")
	}
	ae := err.(*AuthError)
	if ae.StatusCode != 403 {
		t.Errorf("expected 403, got %d", ae.StatusCode)
	}
	if ae.Code != apierrors.ErrAuthInsufficientPermission {
		t.Errorf("expected code %s, got %s", apierrors.ErrAuthInsufficientPermission, ae.Code)
	}
}

func TestDefaultAuthorizer_OperationRestriction(t *testing.T) {
	authz := &DefaultAuthorizer{}
	identity := &AuthIdentity{
		Subject:    "key-2",
		Method:     "apikey",
		Operations: []string{"query"},
	}

	if err := authz.Authorize(identity, "starrocks", "query"); err != nil {
		t.Errorf("expected nil for allowed operation, got %v", err)
	}

	err := authz.Authorize(identity, "starrocks", "mutation")
	if err == nil {
		t.Fatal("expected error for disallowed operation")
	}
	ae := err.(*AuthError)
	if ae.StatusCode != 403 {
		t.Errorf("expected 403, got %d", ae.StatusCode)
	}
}

func TestDefaultAuthorizer_BothRestrictions(t *testing.T) {
	authz := &DefaultAuthorizer{}
	identity := &AuthIdentity{
		Subject:     "key-3",
		Method:      "apikey",
		Datasources: []string{"starrocks", "prometheus"},
		Operations:  []string{"query"},
	}

	// Allowed combination.
	if err := authz.Authorize(identity, "starrocks", "query"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if err := authz.Authorize(identity, "prometheus", "query"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}

	// Disallowed datasource.
	if err := authz.Authorize(identity, "unknown", "query"); err == nil {
		t.Error("expected error for unknown datasource")
	}

	// Disallowed operation.
	if err := authz.Authorize(identity, "starrocks", "mutation"); err == nil {
		t.Error("expected error for disallowed operation")
	}
}

func TestDefaultAuthorizer_EmptyDatasourceOrOperation(t *testing.T) {
	authz := &DefaultAuthorizer{}
	identity := &AuthIdentity{
		Subject:     "key-4",
		Method:      "apikey",
		Datasources: []string{"starrocks"},
		Operations:  []string{"query"},
	}

	// Empty datasource string skips datasource check.
	if err := authz.Authorize(identity, "", "query"); err != nil {
		t.Errorf("expected nil for empty datasource, got %v", err)
	}

	// Empty operation string skips operation check.
	if err := authz.Authorize(identity, "starrocks", ""); err != nil {
		t.Errorf("expected nil for empty operation, got %v", err)
	}
}

func TestDefaultAuthorizer_ImplementsInterface(t *testing.T) {
	var _ Authorizer = (*DefaultAuthorizer)(nil)
}
