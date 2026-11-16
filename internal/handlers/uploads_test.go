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
