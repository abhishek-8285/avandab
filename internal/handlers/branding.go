package handlers

import (
	"context"
	"database/sql"

	appdb "transport-app/internal/database"
	"transport-app/internal/shared"
)

// brandView is the customer-facing identity for white-labelled shipper pages.
type brandView struct {
	Name string
	Logo string
}

// tenantBrand resolves the transporter's own brand: tenant profile first,
// platform company settings second, product default last. White-label means
// the shipper must see the transporter, never the platform vendor.
func (a *App) tenantBrand(ctx context.Context) brandView {
	b := brandView{Name: "Avandab"}
	if a == nil || a.DB == nil {
		return b
	}

	var name, logo sql.NullString
	if tid := string(shared.TenantIDFromContext(ctx)); tid != "" {
		if q, err := appdb.Rebind(`SELECT company_name, logo_path FROM tenant_company_profiles WHERE tenant_id = ?`); err == nil {
			_ = a.DB.QueryRowContext(ctx, q, tid).Scan(&name, &logo)
		}
	}
	if !name.Valid || name.String == "" {
		if q, err := appdb.Rebind(`SELECT company_name, logo_path FROM company_settings WHERE id = 1`); err == nil {
			_ = a.DB.QueryRowContext(ctx, q).Scan(&name, &logo)
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

// withBrand injects the brand into an existing template Extra map.
func (a *App) withBrand(ctx context.Context, extra map[string]interface{}) map[string]interface{} {
	if extra == nil {
		extra = map[string]interface{}{}
	}
	b := a.tenantBrand(ctx)
	extra["BrandName"] = b.Name
	extra["BrandLogo"] = b.Logo
	return extra
}
