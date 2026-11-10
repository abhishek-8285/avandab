package customerportal

// Characterization + extraction tests for the white-label brand resolver moved
// out of internal/handlers/customer_portal.go (handlers-end-state ADR, Phase
// 2): tenant_company_profiles first, global company_settings second, product
// default last.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"transport-app/internal/shared"
)

func TestTenantBrand_ResolutionChain(t *testing.T) {
	db := newPortalTestDB(t)
	ctx := context.Background()

	// Empty registry -> product default.
	b := TenantBrand(ctx, db)
	assert.Equal(t, "Avandab", b.Name)
	assert.Empty(t, b.Logo)

	// Global singleton is the platform fallback.
	mustExecPortal(t, db, `INSERT INTO company_settings (id, company_name, logo_path) VALUES (1, 'Transport Company', '/static/platform.svg')`)
	b = TenantBrand(ctx, db)
	assert.Equal(t, "Transport Company", b.Name)
	assert.Equal(t, "/static/platform.svg", b.Logo)

	// Tenant profile wins for that tenant.
	mustExecPortal(t, db, `INSERT INTO tenant_company_profiles (tenant_id, company_name, logo_path) VALUES ('tenant-custom', 'Apex Express', '/static/apex.png')`)
	ctxCustom := shared.ContextWithTenantID(context.Background(), "tenant-custom")
	b = TenantBrand(ctxCustom, db)
	assert.Equal(t, "Apex Express", b.Name)
	assert.Equal(t, "/static/apex.png", b.Logo)

	// Unknown tenant id falls back to the global singleton.
	ctxUnknown := shared.ContextWithTenantID(context.Background(), "tenant-unknown")
	b = TenantBrand(ctxUnknown, db)
	assert.Equal(t, "Transport Company", b.Name)
	assert.Equal(t, "/static/platform.svg", b.Logo)
}

func TestTenantBrand_TenantNameSuppressesGlobalLogo(t *testing.T) {
	// FROZEN LEGACY QUIRK: the shared scan buffer means that once the tenant
	// profile supplies a name, the company_settings fallback is skipped, so a
	// global-only logo does not attach to the tenant brand.
	db := newPortalTestDB(t)
	mustExecPortal(t, db, `INSERT INTO company_settings (id, company_name, logo_path) VALUES (1, 'Transport Company', '/static/platform.svg')`)
	mustExecPortal(t, db, `INSERT INTO tenant_company_profiles (tenant_id, company_name, logo_path) VALUES ('tenant-name-only', 'Apex Express', NULL)`)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-name-only")
	b := TenantBrand(ctx, db)
	assert.Equal(t, "Apex Express", b.Name)
	assert.Empty(t, b.Logo, "global logo must not leak into a tenant that only overrides the name")
}

func TestTenantBrand_GlobalEmptyFallsBackToDefault(t *testing.T) {
	db := newPortalTestDB(t)
	mustExecPortal(t, db, `INSERT INTO company_settings (id, company_name, logo_path) VALUES (1, '', '')`)
	b := TenantBrand(context.Background(), db)
	assert.Equal(t, "Avandab", b.Name)
	assert.Empty(t, b.Logo)

	b = TenantBrand(context.Background(), nil)
	assert.Equal(t, "Avandab", b.Name)
}
