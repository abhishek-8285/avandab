package handlers

import (
	"database/sql"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"transport-app/internal/shared"
	"transport-app/internal/storage"
)

// TenantUploadsHandler serves files under /uploads/* only when the requested
// path maps to a files row owned by the caller's tenant.
//
// The previous mount was auth-only and served the raw upload directory: any
// logged-in user could read any tenant's driver licence, RC, receipt or POD
// document if they learned the path, bypassing the tenant-scoped /files/{id}
// ACL entirely. UUID filenames are not an authorization boundary.
//
// POD assets keep their separate signed public mount (/uploads/pod/*);
// everything else now fails closed unless the file record proves ownership.
func TenantUploadsHandler(db *sql.DB, uploadDir string) http.Handler {
	baseDir := filepath.Clean(uploadDir)
	if baseDir == "." {
		baseDir = ""
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if db == nil {
			http.Error(w, "storage unavailable", http.StatusInternalServerError)
			return
		}
		tenantID, err := shared.TenantRequired(r.Context())
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/uploads/")
		if rel == "" || rel == "." || strings.HasPrefix(rel, "/") ||
			strings.Contains(rel, `\`) || strings.Contains(rel, "..") {
			http.NotFound(w, r)
			return
		}

		if !storage.TenantFileExists(r.Context(), db, rel, string(tenantID)) {
			// Unknown path or another tenant's file: never reveal which.
			http.NotFound(w, r)
			return
		}

		clean := filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(rel)))
		if baseDir != "" &&
			!strings.HasPrefix(clean, baseDir+string(os.PathSeparator)) &&
			clean != baseDir {
			http.NotFound(w, r)
			return
		}
		// Serve through an os.Root: it resolves the name inside baseDir and
		// refuses to escape it by construction, so no request-derived string
		// reaches a bare filesystem call.
		root, rootErr := os.OpenRoot(baseDir)
		if rootErr != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = root.Close() }()
		f, err := root.Open(filepath.FromSlash(rel))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
	})
}
