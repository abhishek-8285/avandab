package service

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	userdomain "transport-app/internal/domain/user"
	"transport-app/internal/events"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

func TestRegisterSelfServiceAccount_TenantIsolation(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	svc := NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	ctx := context.Background()

	// 1. User A registers with explicit company name
	userA, isAdminA, err := svc.Users.RegisterSelfServiceAccount(ctx, "owner-a@fleet.com", "Alice Owner", "9900112233", "strong-pass-1", "Apex Logistics")
	require.NoError(t, err)
	assert.True(t, isAdminA, "self-registered account must be admin of their tenant")
	assert.NotEmpty(t, userA.TenantID)
	assert.NotEqual(t, string(shared.DefaultTenant), userA.TenantID, "new signup must not get default tenant 1")

	var roleIDA int
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, string(userA.ID)).Scan(&roleIDA))
	assert.Equal(t, int(userdomain.DefaultRoleID(userdomain.RoleAdmin)), roleIDA)

	var tenantNameA, slugA string
	require.NoError(t, db.QueryRow(`SELECT name, slug FROM tenants WHERE id = ?`, userA.TenantID).Scan(&tenantNameA, &slugA))
	assert.Equal(t, "Apex Logistics", tenantNameA)
	assert.Equal(t, "apex-logistics", slugA)

	// 2. User B registers with empty company name (defaults to "<User>'s Fleet")
	userB, isAdminB, err := svc.Users.RegisterSelfServiceAccount(ctx, "owner-b@fleet.com", "Bob Trans", "9900112234", "strong-pass-2", "")
	require.NoError(t, err)
	assert.True(t, isAdminB, "second self-registered account must also be admin of their own isolated tenant")
	assert.NotEmpty(t, userB.TenantID)
	assert.NotEqual(t, string(shared.DefaultTenant), userB.TenantID, "user B must not get default tenant 1")
	assert.NotEqual(t, userA.TenantID, userB.TenantID, "user A and user B must have different isolated tenant IDs")

	var roleIDB int
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, string(userB.ID)).Scan(&roleIDB))
	assert.Equal(t, int(userdomain.DefaultRoleID(userdomain.RoleAdmin)), roleIDB)

	var tenantNameB, slugB string
	require.NoError(t, db.QueryRow(`SELECT name, slug FROM tenants WHERE id = ?`, userB.TenantID).Scan(&tenantNameB, &slugB))
	assert.Equal(t, "Bob Trans's Fleet", tenantNameB)
	assert.Equal(t, "bob-transs-fleet", slugB)

	// 3. Verify cross-tenant data isolation: User A's tenant list only sees User A, User B's only sees User B
	ctxA := shared.ContextWithTenantID(ctx, shared.TenantID(userA.TenantID))
	usersA, totalA, err := svc.Users.ListUsers(ctxA, "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalA)
	require.Len(t, usersA, 1)
	assert.Equal(t, string(userA.ID), string(usersA[0].ID))

	ctxB := shared.ContextWithTenantID(ctx, shared.TenantID(userB.TenantID))
	usersB, totalB, err := svc.Users.ListUsers(ctxB, "", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalB)
	require.Len(t, usersB, 1)
	assert.Equal(t, string(userB.ID), string(usersB[0].ID))

	// 4. Duplicate email rejection
	_, _, err = svc.Users.RegisterSelfServiceAccount(ctx, "owner-a@fleet.com", "Dup", "9900112235", "strong-pass-3", "Other Corp")
	assert.ErrorIs(t, err, domain.ErrUserEmailExists, "duplicate email must be rejected")
}

func TestRegisterSelfServiceAccount_SlugDeduplication(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	svc := NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	ctx := context.Background()

	user1, _, err := svc.Users.RegisterSelfServiceAccount(ctx, "corp1@apex.test", "User 1", "9900110001", "strong-pass-1", "Apex Logistics")
	require.NoError(t, err)

	user2, _, err := svc.Users.RegisterSelfServiceAccount(ctx, "corp2@apex.test", "User 2", "9900110002", "strong-pass-2", "Apex Logistics")
	require.NoError(t, err)

	assert.NotEqual(t, user1.TenantID, user2.TenantID)

	var slug1, slug2 string
	require.NoError(t, db.QueryRow(`SELECT slug FROM tenants WHERE id = ?`, user1.TenantID).Scan(&slug1))
	require.NoError(t, db.QueryRow(`SELECT slug FROM tenants WHERE id = ?`, user2.TenantID).Scan(&slug2))

	assert.Equal(t, "apex-logistics", slug1)
	assert.Contains(t, slug2, "apex-logistics-", "duplicate slug should be disambiguated")
}
