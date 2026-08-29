package test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func isAlreadyExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// TestMultiTenantUserIsolation — Spec 24 §Business logic: users are created
// into an explicit tenant, list/count are tenant-scoped, password hashes never
// leak into search results, and login is rejected while the tenant is
// suspended.
func TestMultiTenantUserIsolation(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	if err := svc.Users.CreateTenant(ctx, "acme", "Acme Ltd", "acme"); err != nil && !isAlreadyExists(err) {
		require.NoError(t, err)
	}
	if err := svc.Users.CreateTenant(ctx, "beta", "Beta Ltd", "beta"); err != nil && !isAlreadyExists(err) {
		require.NoError(t, err)
	}

	acmeCtx := shared.ContextWithTenantID(ctx, "acme")
	betaCtx := shared.ContextWithTenantID(ctx, "beta")

	acmeAdmin, err := svc.Users.CreateUserWithPassword(acmeCtx, "owner@acme.test", "Acme Owner", "9000000001", "StrongPass123!", 1, domain.UserStatusActive, "acme")
	require.NoError(t, err)
	assert.Equal(t, "acme", acmeAdmin.TenantID)

	acmeViewer, err := svc.Users.CreateUserWithPassword(acmeCtx, "clerk@acme.test", "Acme Clerk", "9000000003", "StrongPass123!", 4, domain.UserStatusActive, "acme")
	require.NoError(t, err)

	betaOwner, err := svc.Users.CreateUserWithPassword(betaCtx, "owner@beta.test", "Beta Owner", "9000000002", "StrongPass123!", 1, domain.UserStatusActive, "beta")
	require.NoError(t, err)
	assert.Equal(t, "beta", betaOwner.TenantID)

	list, total, err := svc.Users.ListUsers(acmeCtx, "", "", 100, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "count must be scoped to the acme tenant")
	assert.Len(t, list, 2, "list must be scoped to the acme tenant")
	ids := []string{string(list[0].ID), string(list[1].ID)}
	assert.Contains(t, ids, string(acmeAdmin.ID))
	assert.Contains(t, ids, string(acmeViewer.ID))
	assert.NotContains(t, ids, string(betaOwner.ID), "cross-tenant rows must not leak")
	for _, u := range list {
		assert.Empty(t, u.PasswordHash, "password_hash must never appear in list results")
	}

	betaList, betaTotal, err := svc.Users.ListUsers(betaCtx, "", "", 100, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, betaTotal)
	assert.Len(t, betaList, 1)
	assert.Equal(t, string(betaOwner.ID), string(betaList[0].ID))

	require.NoError(t, svc.Users.SetTenantStatus(ctx, "beta", "suspended"))
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM tenants WHERE id = 'beta'`).Scan(&status))
	assert.Equal(t, "suspended", status)

	_, err = svc.Auth.Login(betaCtx, service.LoginRequest{
		Email:    "owner@beta.test",
		Password: "StrongPass123!",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, auth.ErrTenantSuspended, "login must be rejected while tenant is suspended")
}
