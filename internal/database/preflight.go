package database

import (
	"context"
	"database/sql"
)

// CountOrphanTenantUsers counts users whose tenant_id has no matching tenants
// row. Fail-closed multi-tenant resolution (MULTI_TENANT_ENABLED=true) rejects
// those logins, so a legacy DB can silently lock people out after an upgrade.
// Startup calls this to warn with a count; remediation SQL is in docs/08.
func CountOrphanTenantUsers(ctx context.Context, db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users u
		LEFT JOIN tenants t ON t.id = u.tenant_id
		WHERE u.tenant_id <> '' AND t.id IS NULL`).Scan(&n)
	return n, err
}
