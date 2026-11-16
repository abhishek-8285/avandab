package storage

import (
	"context"
	"database/sql"
)

// TenantFileExists reports whether key is registered to tenantID in the files
// table. Handlers use it to serve private uploads without revealing whether a
// path belongs to another org (unknown path and foreign file look identical).
func TenantFileExists(ctx context.Context, db *sql.DB, key, tenantID string) bool {
	if db == nil {
		return false
	}
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT 1 FROM files WHERE path = $1 AND tenant_id = $2 LIMIT 1`,
		key, tenantID).Scan(&exists); err != nil {
		return false
	}
	return true
}
