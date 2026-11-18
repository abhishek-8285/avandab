package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

func TestValidateSessionToken_ConsentWithdrawalInvalidatesToken(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()
	u := registerConsentUser(t, svcs, "session-consent@consent.test")

	login, err := svcs.Auth.Login(ctx, LoginRequest{Email: "session-consent@consent.test", Password: "Str0ng!Pass"})
	require.NoError(t, err)
	require.NotEmpty(t, login.SessionToken)

	_, err = svcs.Auth.ValidateSessionToken(ctx, login.SessionToken)
	require.NoError(t, err, "token must be valid before withdrawal")

	_, err = svcs.DB().Exec(`UPDATE user_consents SET withdrawn_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND user_id = $2`, u.TenantID, string(u.ID))
	require.NoError(t, err)

	_, err = svcs.Auth.ValidateSessionToken(ctx, login.SessionToken)
	require.ErrorIs(t, err, auth.ErrConsentWithdrawn,
		"withdrawn consent must invalidate a previously minted session token")
}

func TestValidateAPITokenUser_ConsentWithdrawalDeactivatesUser(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()
	u := registerConsentUser(t, svcs, "api-consent@consent.test")

	_, active, err := svcs.Auth.ValidateAPITokenUser(ctx, string(u.ID))
	require.NoError(t, err)
	require.True(t, active, "user must be active before withdrawal")

	_, err = svcs.DB().Exec(`UPDATE user_consents SET withdrawn_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND user_id = $2`, u.TenantID, string(u.ID))
	require.NoError(t, err)

	_, active, err = svcs.Auth.ValidateAPITokenUser(ctx, string(u.ID))
	require.NoError(t, err)
	require.False(t, active, "withdrawn consent must deactivate API token user")
}

func TestWithdrawConsent_RevokesUserSessions(t *testing.T) {
	svcs := newConsentTestServices(t)
	ctx := context.Background()
	u := registerConsentUser(t, svcs, "revoke-consent@consent.test")

	_, err := svcs.Auth.Login(ctx, LoginRequest{Email: "revoke-consent@consent.test", Password: "Str0ng!Pass"})
	require.NoError(t, err)

	var before int
	require.NoError(t, svcs.DB().QueryRow(
		`SELECT COUNT(1) FROM sessions WHERE user_id = $1`, string(u.ID)).Scan(&before))
	require.Equal(t, 1, before, "login must create a session")

	require.NoError(t, svcs.Users.WithdrawConsent(ctx, u.TenantID, string(u.ID)))

	var after int
	require.NoError(t, svcs.DB().QueryRow(
		`SELECT COUNT(1) FROM sessions WHERE user_id = $1`, string(u.ID)).Scan(&after))
	require.Zero(t, after, "withdrawal must delete all sessions for the user")
}
