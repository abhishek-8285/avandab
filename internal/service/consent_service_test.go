package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/repository/sqlite"
)

func newConsentTestServices(t *testing.T) *Services {
	t.Helper()
	db := newGoogleTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{AppEnv: "testing"}
	return NewServices(sqlite.NewRepository(db), cfg, logger, nil)
}

func registerConsentUser(t *testing.T, svcs *Services, email string) domain.User {
	t.Helper()
	u, _, err := svcs.Users.RegisterSelfServiceAccount(
		context.Background(), email, "Consent User", "9999999999", "Str0ng!Pass", "Consent Co")
	require.NoError(t, err)
	return u
}

func loginConsentUser(t *testing.T, svcs *Services, email string) error {
	t.Helper()
	_, err := svcs.Auth.Login(context.Background(), LoginRequest{Email: email, Password: "Str0ng!Pass"})
	return err
}

// Registration binds platform-use consent in the same transaction.
func TestConsent_GrantedAtRegistration(t *testing.T) {
	svcs := newConsentTestServices(t)
	u := registerConsentUser(t, svcs, "grant@consent.test")

	grantedAt, withdrawnAt, err := svcs.Users.ConsentStatus(context.Background(), u.TenantID, string(u.ID))
	require.NoError(t, err)
	require.True(t, grantedAt.Valid, "registration must grant consent")
	require.False(t, withdrawnAt.Valid)
	require.NoError(t, loginConsentUser(t, svcs, "grant@consent.test"))
}

// Withdrawal ceases processing: login refuses until re-granted (DPDP §6(4)).
func TestConsent_WithdrawBlocksLoginUntilRegrant(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()
	u := registerConsentUser(t, svcs, "withdraw@consent.test")

	require.NoError(t, svcs.Users.WithdrawConsent(ctx, u.TenantID, string(u.ID)))

	_, withdrawnAt, err := svcs.Users.ConsentStatus(ctx, u.TenantID, string(u.ID))
	require.NoError(t, err)
	require.True(t, withdrawnAt.Valid)

	err = loginConsentUser(t, svcs, "withdraw@consent.test")
	require.ErrorIs(t, err, auth.ErrConsentWithdrawn)

	require.NoError(t, svcs.Users.GrantConsent(ctx, u.TenantID, string(u.ID)))
	_, withdrawnAt, err = svcs.Users.ConsentStatus(ctx, u.TenantID, string(u.ID))
	require.NoError(t, err)
	require.False(t, withdrawnAt.Valid, "re-grant must clear withdrawal")
	require.NoError(t, loginConsentUser(t, svcs, "withdraw@consent.test"))
}

// ConsentNeedsRefresh is the future version re-bind trigger: stale notice
// version or standing withdrawal demands a refresh; fresh and legacy stay quiet.
func TestConsent_NeedsRefresh(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()
	u := registerConsentUser(t, svcs, "refresh@consent.test")

	require.False(t, svcs.Users.ConsentNeedsRefresh(ctx, u.TenantID, string(u.ID)))

	db := svcs.DB()
	require.NotNil(t, db)
	_, err := db.Exec(`UPDATE user_consents SET notice_version = 'v0'
		WHERE tenant_id = $1 AND user_id = $2`, u.TenantID, string(u.ID))
	require.NoError(t, err)
	require.True(t, svcs.Users.ConsentNeedsRefresh(ctx, u.TenantID, string(u.ID)))

	require.NoError(t, svcs.Users.GrantConsent(ctx, u.TenantID, string(u.ID)))
	require.False(t, svcs.Users.ConsentNeedsRefresh(ctx, u.TenantID, string(u.ID)))

	require.NoError(t, svcs.Users.WithdrawConsent(ctx, u.TenantID, string(u.ID)))
	require.True(t, svcs.Users.ConsentNeedsRefresh(ctx, u.TenantID, string(u.ID)))

	require.False(t, svcs.Users.ConsentNeedsRefresh(ctx, u.TenantID, "no-such-user"))
}

// No ledger row (legacy / OAuth-linked / admin-created users) stays allowed —
// only an explicit withdrawal blocks, mirroring tenantActive.
func TestConsent_LegacyNoRowAllowed(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()

	_, err := svcs.Users.CreateUser(ctx, "legacy@consent.test", "Legacy", "9999999999",
		domain.DefaultRoleID(domain.RoleViewer), domain.UserStatusActive, "1")
	require.NoError(t, err)
	require.NoError(t, svcs.Users.SetPasswordByEmail(ctx, "legacy@consent.test", "Str0ng!Pass"))

	_, err = svcs.Auth.Login(ctx, LoginRequest{Email: "legacy@consent.test", Password: "Str0ng!Pass"})
	if errors.Is(err, auth.ErrConsentWithdrawn) {
		t.Fatal("missing ledger row must not block login")
	}
}
