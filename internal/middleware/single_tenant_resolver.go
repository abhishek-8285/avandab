package middleware

import (
	"context"
	"database/sql"
	"errors"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
)

// SingleTenantResolver pins authenticated users to the bootstrap tenant when
// MULTI_TENANT_ENABLED=false, but only after proving the user actually belongs
// to that tenant and the tenant is still active.
//
// Public self-registration always provisions a new tenant organization. The
// previous constant DefaultTenantResolver ignored users.tenant_id entirely, so
// with the multi-tenant flag off a newly registered org admin was served as
// tenant "1" and could read the bootstrap organization's fleet, billing and
// user data. A startup check cannot catch tenants created after boot; this
// resolver fails closed on every authenticated request instead.
func SingleTenantResolver(db *sql.DB) TenantResolver {
	return func(ctx context.Context, userID string) (shared.TenantID, error) {
		if db == nil {
			return "", errors.New("tenant resolver: database unavailable")
		}
		var tenantID, status string
		if err := db.QueryRowContext(ctx,
			`SELECT u.tenant_id, COALESCE(t.status, 'inactive')
			 FROM users u
			 LEFT JOIN tenants t ON t.id = u.tenant_id
			 WHERE u.id = $1`, userID).Scan(&tenantID, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return "", errors.New("unauthorized: user not found")
			}
			return "", err
		}
		if tenantID != string(shared.DefaultTenant) { //nolint:tenant-default // single-tenant mode serves only the bootstrap tenant
			return "", errors.New("unauthorized: user belongs to another organization while multi-tenant mode is disabled")
		}
		if status != "active" {
			return "", auth.ErrTenantSuspended
		}
		return shared.TenantID(tenantID), nil
	}
}
