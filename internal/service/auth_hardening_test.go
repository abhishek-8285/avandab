package service

// Regression tests for the audit hardening on the password-login path:
// configurable session lifetime, per-account lockout enforced inside Login,
// and self-registration refusal when multi-tenancy is off.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/repository/sqlite"
)

func TestLogin_SessionTTLHonorsConfig(t *testing.T) {
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		AppEnv:        "testing",
		SessionMaxAge: 48 * time.Hour,
		MultiTenant:   config.MultiTenantConfig{Enabled: true},
	}
	svcs := NewServices(sqlite.NewRepository(db), cfg, logger, nil)
	_, _, err := svcs.Users.RegisterSelfServiceAccount(
		context.Background(), "ttl@test.local", "TTL User", "9999999999", "Str0ng!Pass", "TTL Co")
	require.NoError(t, err)

	_, err = svcs.Auth.Login(context.Background(), LoginRequest{Email: "ttl@test.local", Password: "Str0ng!Pass"})
	require.NoError(t, err)

	var expires time.Time
	require.NoError(t, db.QueryRow(`SELECT expires_at FROM sessions ORDER BY created_at DESC LIMIT 1`).Scan(&expires))
	remaining := time.Until(expires)
	assert.Greater(t, remaining, 47*time.Hour, "DB session must honour SESSION_MAX_AGE, got %v", remaining)
	assert.Less(t, remaining, 49*time.Hour)
}

func TestLogin_LocksAccountAfterFailures(t *testing.T) {
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{AppEnv: "testing", MultiTenant: config.MultiTenantConfig{Enabled: true}}
	svcs := NewServices(sqlite.NewRepository(db), cfg, logger, nil)
	_, _, err := svcs.Users.RegisterSelfServiceAccount(
		context.Background(), "lock@test.local", "Lock User", "9999999999", "Str0ng!Pass", "Lock Co")
	require.NoError(t, err)

	ctx := context.Background()
	for i := 0; i < loginMaxFailures; i++ {
		_, err := svcs.Auth.Login(ctx, LoginRequest{Email: "lock@test.local", Password: "wrong-password"})
		require.Error(t, err)
	}

	// The correct password is refused while the account lock is active.
	_, err = svcs.Auth.Login(ctx, LoginRequest{Email: "lock@test.local", Password: "Str0ng!Pass"})
	require.ErrorIs(t, err, auth.ErrAccountLocked)
}

func TestSelfRegistration_DisabledWithoutMultiTenant(t *testing.T) {
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{AppEnv: "testing", MultiTenant: config.MultiTenantConfig{Enabled: false}}
	svcs := NewServices(sqlite.NewRepository(db), cfg, logger, nil)

	_, _, err := svcs.Users.RegisterSelfServiceAccount(
		context.Background(), "single@test.local", "Single User", "9999999999", "Str0ng!Pass", "Single Co")
	require.ErrorIs(t, err, ErrSelfRegistrationDisabled)

	// The reason is actionable, not a storage internals leak.
	assert.True(t, strings.Contains(err.Error(), "disabled"))
}
