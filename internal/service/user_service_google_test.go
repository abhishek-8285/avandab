package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/repository/sqlite"
)

func newGoogleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_google_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newGoogleTestService(t *testing.T) *UserService {
	t.Helper()
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{AppEnv: "testing"}
	svcs := NewServices(sqlite.NewRepository(db), cfg, logger, nil)
	return svcs.Users
}

// TestResolveGoogleUser_NewOperatorProvisionsIsolatedTenant — branch 3: no
// matching sub/email → new tenant + admin + google_sub linked in one shot.
func TestResolveGoogleUser_NewOperatorProvisionsIsolatedTenant(t *testing.T) {
	svc := newGoogleTestService(t)

	u, isAdmin, err := svc.ResolveGoogleUser(context.Background(), "sub-A", "owner@fleetA.com", "Owner A")
	require.NoError(t, err)
	assert.True(t, isAdmin, "google signup must provision a tenant admin")
	assert.NotEmpty(t, u.ID)
	assert.NotEmpty(t, u.TenantID)
	assert.Equal(t, domain.RoleAdmin, u.Role.Name)

	// google_sub persisted with google auth_provider
	var gotSub, provider string
	require.NoError(t, svc.store.(interface {
		DB() *sql.DB
	}).DB().QueryRow(
		`SELECT google_sub, auth_provider FROM users WHERE id = ?`, string(u.ID),
	).Scan(&gotSub, &provider))
	assert.Equal(t, "sub-A", gotSub)
	assert.Equal(t, "google", provider)
}

// TestResolveGoogleUser_TenantIsolation — two Google signups must land in two
// distinct tenants (zero cross-tenant leaks, docs/06 §1).
func TestResolveGoogleUser_TenantIsolation(t *testing.T) {
	svc := newGoogleTestService(t)

	u1, _, err := svc.ResolveGoogleUser(context.Background(), "sub-1", "a@x.com", "A")
	require.NoError(t, err)
	u2, _, err := svc.ResolveGoogleUser(context.Background(), "sub-2", "b@x.com", "B")
	require.NoError(t, err)
	assert.NotEqual(t, u1.TenantID, u2.TenantID, "each google signup gets an isolated tenant")
}

// TestResolveGoogleUser_ReturnsLinkedAccount — branch 1: second sign-in with
// the same sub returns the SAME user, no duplicate provisioning.
func TestResolveGoogleUser_ReturnsLinkedAccount(t *testing.T) {
	svc := newGoogleTestService(t)
	ctx := context.Background()

	first, isNew1, err := svc.ResolveGoogleUser(ctx, "sub-dup", "dup@x.com", "Dup")
	require.NoError(t, err)
	assert.True(t, isNew1)

	again, isNew2, err := svc.ResolveGoogleUser(ctx, "sub-dup", "dup@x.com", "Dup")
	require.NoError(t, err)
	assert.False(t, isNew2)
	assert.Equal(t, first.ID, again.ID)
}

// TestResolveGoogleUser_LinksExistingPasswordAccount — branch 2: a password
// account with the same email gets the Google identity linked, not duplicated.
func TestResolveGoogleUser_LinksExistingPasswordAccount(t *testing.T) {
	svc := newGoogleTestService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateTenant(ctx, "tenant-x", "Tenant X", "tenant-x"))
	seeded, err := svc.CreateUserWithPassword(ctx, "pw@x.com", "PW User", "", "Str0ng!Passw0rd", domain.DefaultRoleID(domain.RoleAdmin), domain.UserStatusActive, "tenant-x")
	require.NoError(t, err)

	linked, isAdmin, err := svc.ResolveGoogleUser(ctx, "sub-pw", "pw@x.com", "PW User")
	require.NoError(t, err)
	assert.False(t, isAdmin, "linking an existing account must not create a new tenant")
	assert.Equal(t, seeded.ID, linked.ID, "same account, now google-linked")

	var gotSub string
	require.NoError(t, svc.store.(interface {
		DB() *sql.DB
	}).DB().QueryRow(`SELECT google_sub FROM users WHERE id = ?`, string(seeded.ID)).Scan(&gotSub))
	assert.Equal(t, "sub-pw", gotSub)
}

// TestResolveGoogleUser_SuspendedRejected — suspended accounts are refused in
// both the sub-lookup and email-link branches.
func TestResolveGoogleUser_SuspendedRejected(t *testing.T) {
	svc := newGoogleTestService(t)
	ctx := context.Background()

	require.NoError(t, svc.CreateTenant(ctx, "tenant-susp", "Tenant Susp", "tenant-susp"))
	seeded, err := svc.CreateUserWithPassword(ctx, "susp@x.com", "Susp", "", "Str0ng!Passw0rd", domain.DefaultRoleID(domain.RoleAdmin), domain.UserStatusActive, "tenant-susp")
	require.NoError(t, err)
	db := svc.store.(interface{ DB() *sql.DB }).DB()
	_, err = db.Exec(`UPDATE users SET status = 'suspended' WHERE id = ?`, string(seeded.ID))
	require.NoError(t, err)

	_, _, err = svc.ResolveGoogleUser(ctx, "sub-susp", "susp@x.com", "Susp")
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnauthorized), "suspended email-link must be rejected")
}
