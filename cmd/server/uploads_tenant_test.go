package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"

	_ "modernc.org/sqlite"
)

func newUploadsGuardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:uploads_guard_"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE files (id TEXT, path TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	return db
}

// A tenant-A caller must not read a file owned by tenant B, even though the
// file exists on disk behind the authenticated /uploads/* mount.
func TestTenantFileGuardBlocksCrossTenantRead(t *testing.T) {
	db := newUploadsGuardTestDB(t)
	_, err := db.Exec(`INSERT INTO files (id, path, tenant_id) VALUES ('f1', 'trips/secret.jpg', 'tenantB')`)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trips"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trips", "secret.jpg"), []byte("TENANT-B-SECRET"), 0o600))

	guard := tenantFileGuard(db, http.FileServer(http.Dir(dir)))

	req := httptest.NewRequest(http.MethodGet, "/trips/secret.jpg", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID("tenantA")))
	rr := httptest.NewRecorder()
	guard.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	require.NotContains(t, rr.Body.String(), "TENANT-B-SECRET")
}

// The owning tenant still gets the file (guard is not an unconditional deny).
func TestTenantFileGuardAllowsOwningTenant(t *testing.T) {
	db := newUploadsGuardTestDB(t)
	_, err := db.Exec(`INSERT INTO files (id, path, tenant_id) VALUES ('f2', 'trips/mine.jpg', 'tenantA')`)
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "trips"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trips", "mine.jpg"), []byte("TENANT-A-OK"), 0o600))

	guard := tenantFileGuard(db, http.FileServer(http.Dir(dir)))

	req := httptest.NewRequest(http.MethodGet, "/trips/mine.jpg", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID("tenantA")))
	rr := httptest.NewRecorder()
	guard.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), "TENANT-A-OK")
}
