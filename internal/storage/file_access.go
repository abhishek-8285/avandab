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

// TenantLegacyUploadExists reports whether key is a pre-files-table upload that
// the tenant still references from its own business rows: customer logos
// (customers.photo_url stores "/uploads/<key>") and company logos
// (company_settings / tenant_company_profiles.logo_path store "<key>").
//
// The /uploads ACL must not break those legacy assets when it starts requiring
// a files row, but it must still refuse paths no row references. The
// bootstrap-tenant company_settings row is the platform-global singleton, so it
// only backs the bootstrap tenant.
func TenantLegacyUploadExists(ctx context.Context, db *sql.DB, key, tenantID string) bool {
	if db == nil || key == "" || tenantID == "" {
		return false
	}
	var exists int
	err := db.QueryRowContext(ctx, `
		SELECT 1 WHERE EXISTS (
		    SELECT 1 FROM customers WHERE tenant_id = $1 AND photo_url = '/uploads/' || $2
		) OR EXISTS (
		    SELECT 1 FROM tenant_company_profiles WHERE tenant_id = $1 AND logo_path = $2
		) OR EXISTS (
		    SELECT 1 FROM company_settings WHERE logo_path = $2 AND $1 = '1'
		) LIMIT 1`, tenantID, key).Scan(&exists)
	if err != nil {
		return false
	}
	return exists == 1
}
