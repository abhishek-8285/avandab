package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// TestTenantUploadsHandler_EnforcesTenantACL guards audit finding H4
// (2026-11-12): /uploads/* was auth-only and served the raw upload directory,
// so any logged-in user could read another tenant's driver licence, RC,
// receipt or POD document by path. UUID filenames are not an authorization
// boundary; the files row's tenant_id is.
func TestTenantUploadsHandler_EnforcesTenantACL(t *testing.T) {
	db := handlerTestDB(t)
	uploadDir := t.TempDir()

	rel := "drivers/licence-h4.pdf"
	full := filepath.Join(uploadDir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte("licence-bytes"), 0o600))

	_, err := db.Exec(`INSERT INTO files
		(id, filename, original_name, path, size, mime_type, uploadable_type, uploadable_id, tenant_id)
		VALUES ('file-h4', 'licence-h4.pdf', 'licence.pdf', ?, 13, 'application/pdf',
		        'driver_license', 'drv-h4', '1')`, rel)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug, status)
		VALUES ('tenant-h4', 'Other Tenant', 'other-h4', 'active')`)
	require.NoError(t, err)

	h := TenantUploadsHandler(db, uploadDir)
	serve := func(tenant, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, serve("1", "/uploads/"+rel).Code,
		"owning tenant must read its own file")
	assert.Equal(t, http.StatusNotFound, serve("tenant-h4", "/uploads/"+rel).Code,
		"another tenant must not read the file even with the exact path")
	assert.Equal(t, http.StatusNotFound, serve("1", "/uploads/drivers/missing.pdf").Code,
		"unregistered paths must 404, not fall through to disk")
	assert.Equal(t, http.StatusNotFound, serve("1", "/uploads/../etc/passwd").Code,
		"traversal must be rejected")
	assert.Equal(t, http.StatusUnauthorized, serve("", "/uploads/"+rel).Code,
		"missing tenant in context must fail closed")
}

// TestTenantUploadsHandler_ServesTenantLogos guards the regression the files-row
// ACL introduced: customer and company logos are written straight to disk and
// referenced from customers.photo_url / tenant_company_profiles.logo_path (and
// the bootstrap-only company_settings.logo_path), never from a files row. Those
// assets must keep rendering for their owner while staying invisible to other
// tenants.
func TestTenantUploadsHandler_ServesTenantLogos(t *testing.T) {
	db := handlerTestDB(t)
	uploadDir := t.TempDir()

	for _, rel := range []string{"customers/cust-logo.png", "company/co-logo.png"} {
		full := filepath.Join(uploadDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte("logo-bytes"), 0o600))
	}

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug, status)
		VALUES ('tenant-logo', 'Logo Tenant', 'logo-tenant', 'active')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, phone, tenant_id, photo_url)
		VALUES ('cust-logo', 'Logo Customer', '+919000000009', '1', '/uploads/customers/cust-logo.png')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO company_settings (id, company_name, logo_path)
		VALUES (1, 'Platform Co', 'company/co-logo.png')`)
	if err != nil {
		// Migrations seed the singleton row; set the logo on it instead.
		_, err = db.Exec(`UPDATE company_settings SET logo_path = 'company/co-logo.png' WHERE id = 1`)
	}
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenant_company_profiles (tenant_id, company_name, logo_path)
		VALUES ('tenant-logo', 'Logo Tenant Co', 'company/co-logo.png')`)
	require.NoError(t, err)

	h := TenantUploadsHandler(db, uploadDir)
	serve := func(tenant, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, serve("1", "/uploads/customers/cust-logo.png").Code,
		"the owning tenant must still render its customer logo")
	assert.Equal(t, http.StatusNotFound, serve("tenant-logo", "/uploads/customers/cust-logo.png").Code,
		"another tenant must not fetch a customer logo it does not own")
	assert.Equal(t, http.StatusOK, serve("tenant-logo", "/uploads/company/co-logo.png").Code,
		"a tenant profile logo must render for its own tenant")
	assert.Equal(t, http.StatusNotFound, serve("tenant-logo", "/uploads/customers/unknown-logo.png").Code,
		"paths no row references must stay 404")
}
