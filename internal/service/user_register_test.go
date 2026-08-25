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

func TestRegisterSelfServiceAccount_FirstRunClaim(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	svc := NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	ctx := context.Background()

	first, firstAdmin, err := svc.Users.RegisterSelfServiceAccount(ctx, "owner@fleet.com", "Fleet Owner", "9900112233", "strong-pass-1")
	require.NoError(t, err)
	assert.True(t, firstAdmin, "first self-registered account must claim admin")

	var roleID int
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, string(first.ID)).Scan(&roleID))
	assert.Equal(t, 1, roleID)

	second, secondAdmin, err := svc.Users.RegisterSelfServiceAccount(ctx, "clerk@fleet.com", "Viewer Two", "9900112234", "strong-pass-2")
	require.NoError(t, err)
	assert.False(t, secondAdmin, "later registrations must stay least-privilege viewer")
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, string(second.ID)).Scan(&roleID))
	assert.Equal(t, int(userdomain.DefaultRoleID(userdomain.RoleViewer)), roleID)

	var admins int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE role_id = 1`).Scan(&admins))
	assert.Equal(t, 1, admins, "exactly one admin must exist after the claim")

	_, _, err = svc.Users.RegisterSelfServiceAccount(ctx, "owner@fleet.com", "Dup", "9900112235", "strong-pass-3")
	assert.Error(t, err, "duplicate email must fail even inside the claim path")
}

func TestRegisterSelfServiceAccount_ExistingAdminBlocksClaim(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	svc := NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	ctx := context.Background()

	seeded, err := svc.Users.CreateUserWithPassword(ctx, "boot@admin.com", "Boot", "9900112299", "strong-pass-0", userdomain.DefaultRoleID(userdomain.RoleAdmin), domain.UserStatusActive, string(shared.DefaultTenant))
	require.NoError(t, err)
	_ = seeded

	reg, claimed, err := svc.Users.RegisterSelfServiceAccount(ctx, "late@fleet.com", "Late", "9900112200", "strong-pass-9")
	require.NoError(t, err)
	assert.False(t, claimed, "claim must be blocked when an admin already exists")

	var roleID int
	require.NoError(t, db.QueryRow(`SELECT role_id FROM users WHERE id = ?`, string(reg.ID)).Scan(&roleID))
	assert.Equal(t, int(userdomain.DefaultRoleID(userdomain.RoleViewer)), roleID)
}
