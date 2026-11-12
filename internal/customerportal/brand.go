package customerportal

// White-label brand resolution for the shipper portal, extracted from
// internal/handlers/customer_portal.go (handlers-end-state ADR, Phase 2).
// The handler keeps App.tenantBrand/withBrand as thin delegating wrappers so
// its branding characterization tests and template rendering stay unchanged.

import (
	"context"
	"database/sql"

	appdb "transport-app/internal/database"
	"transport-app/internal/shared"
)

// Brand is the customer-facing identity for white-labelled shipper pages.
type Brand struct {
	Name string
	Logo string
}

// TenantBrand resolves the transporter's own brand: tenant profile first,
// platform company settings second, product default last. White-label means
// the shipper must see the transporter, never the platform vendor.
func TenantBrand(ctx context.Context, db *sql.DB) Brand {
	b := Brand{Name: "Avandab"}
	if db == nil {
		return b
	}

	var name, logo sql.NullString
	if tid := string(shared.TenantIDFromContext(ctx)); tid != "" {
		if q, err := appdb.Rebind(`SELECT company_name, logo_path FROM tenant_company_profiles WHERE tenant_id = ?`); err == nil {
			_ = db.QueryRowContext(ctx, q, tid).Scan(&name, &logo)
		}
	}
	if !name.Valid || name.String == "" {
		if q, err := appdb.Rebind(`SELECT company_name, logo_path FROM company_settings WHERE id = 1`); err == nil {
			_ = db.QueryRowContext(ctx, q).Scan(&name, &logo)
		}
	}

	if name.Valid && name.String != "" {
		b.Name = name.String
	}
	if logo.Valid && logo.String != "" {
		b.Logo = logo.String
	}
	return b
}
