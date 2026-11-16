package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

// TestDriver_DeniedKharchaRead pins the read-surface split: the driver role
// keeps trips:read but must not see the tenant-wide expense queue/ledger.
func TestDriver_DeniedKharchaRead(t *testing.T) {
	db := newCustomersSelectedDB(t)
	_, err := db.Exec(`INSERT OR IGNORE INTO users
		(id, email, password_hash, name, role_id, status, created_at, updated_at, timezone, theme_preference, tenant_id, auth_provider)
		VALUES ('u-driver','driver@t.local','x','Driver',5,'active',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'Asia/Kolkata','light','1','local')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES ('u-driver', 5)`)
	require.NoError(t, err)

	authSvc, err := auth.NewCasbinAuthorizationService(db)
	require.NoError(t, err)
	require.False(t, authSvc.Can("u-driver", "kharcha", "read"),
		"drivers must not see the tenant-wide expense ledger")
	require.True(t, authSvc.Can("u-driver", "trips", "read"),
		"sanity: the driver role keeps trip read access")
}
