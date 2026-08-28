package test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// randomPassword returns a cryptographically random 16-byte hex string.
func randomPassword(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return hex.EncodeToString(b)
}

// createTestAdmin provisions an admin user directly through the service,
// mirroring the env-based bootstrap flow (migrations no longer seed one).
func createTestAdmin(t *testing.T, svc *service.Services) (domain.User, string) {
	t.Helper()
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	password := randomPassword(t)

	created, err := svc.Users.CreateUserWithPassword(ctx, "admin@transport.local", "Admin User", "555-0100", password, 1, domain.UserStatusActive, string(shared.DefaultTenant))
	require.NoError(t, err)
	return created, password
}

func TestAuthService_Login(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	_, password := createTestAdmin(t, svc)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	result, err := svc.Auth.Login(ctx, service.LoginRequest{
		Email:    "admin@transport.local",
		Password: password,
	})

	require.NoError(t, err)
	assert.Equal(t, "admin@transport.local", result.User.Email)
	assert.Equal(t, domain.RoleAdmin, result.User.Role.Name)
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	_, _ = createTestAdmin(t, svc)

	_, err := svc.Auth.Login(shared.ContextWithTenantID(context.Background(), "1"), service.LoginRequest{
		Email:    "admin@transport.local",
		Password: "wrongpassword-that-does-not-match",
	})

	assert.Error(t, err)
}

func TestAuthService_GetProfile(t *testing.T) {
	db := NewTestDB(t)
	svc := NewTestServices(t, db)
	admin, _ := createTestAdmin(t, svc)

	user, err := svc.Auth.GetProfile(shared.ContextWithTenantID(context.Background(), "1"), admin.ID)
	require.NoError(t, err)
	assert.Equal(t, "admin@transport.local", user.Email)
	assert.Equal(t, "Admin User", user.Name)
}
