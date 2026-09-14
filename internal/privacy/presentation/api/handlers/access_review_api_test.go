package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	privacyapp "transport-app/internal/privacy/application"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
)

// accessReviewCall dispatches directly to the handler (router permission
// middleware is covered by TestRouteGuardPermissions_ExistInDB; routes reuse
// privacy:manage so no new permission row is needed).
func newRecertHandlers(t *testing.T) (*AccessReviewHandlers, string) {
	t.Helper()
	name := fmt.Sprintf("test_recert_h_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })

	const tenant = "tenant_recert_h"
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		tenant, tenant, tenant)
	require.NoError(t, err)

	return NewAccessReviewHandlers(privacyapp.NewAccessReviewService(db, id.NewUUIDGenerator())), tenant
}

func accessReviewCall(t *testing.T, ah *AccessReviewHandlers, method, path, tenant, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if tenant != "" {
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	}
	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rr := httptest.NewRecorder()
	switch {
	case path == "/api/v1/access-reviews/due":
		ah.ListDueReviewsAPI(rr, req)
	case path == "/api/v1/access-reviews/open":
		ah.OpenReviewAPI(rr, req)
	case strings.HasSuffix(path, "/certify"):
		ah.CertifyReviewAPI(rr, req)
	case strings.HasSuffix(path, "/revoke"):
		ah.RevokeReviewAPI(rr, req)
	default:
		ah.GetReviewAPI(rr, req)
	}
	return rr
}

func TestAccessReviewEndpoints_Lifecycle(t *testing.T) {
	ah, tenant := newRecertHandlers(t)

	due := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	rr := accessReviewCall(t, ah, http.MethodPost, "/api/v1/access-reviews/open", tenant, "",
		`{"user_id":"u-web-1","role_name":"org_admin","period":"2026-H2","due_at":"`+due+`"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	var created map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)

	getPath := "/api/v1/access-reviews/" + id

	// Foreign tenant reads as 404, never another org's review.
	rr = accessReviewCall(t, ah, http.MethodGet, getPath, "someone-else", id, "")
	require.Equal(t, http.StatusNotFound, rr.Code)

	rr = accessReviewCall(t, ah, http.MethodGet, getPath, tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	// Due watchlist carries the overdue pending row.
	rr = accessReviewCall(t, ah, http.MethodGet, "/api/v1/access-reviews/due", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var listed map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&listed))
	reviews, _ := listed["reviews"].([]interface{})
	require.Len(t, reviews, 1)

	rr = accessReviewCall(t, ah, http.MethodPost, getPath+"/certify", tenant, id,
		`{"reviewed_by":"boss-1"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = accessReviewCall(t, ah, http.MethodPost, getPath+"/revoke", tenant, id,
		`{"reviewed_by":"boss-1","note":"left the org"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	// Certified→revoked row leaves the due watchlist.
	rr = accessReviewCall(t, ah, http.MethodGet, "/api/v1/access-reviews/due", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	listed = nil
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&listed))
	reviews, _ = listed["reviews"].([]interface{})
	require.Empty(t, reviews)

	// Tenantless callers get 401.
	rr = accessReviewCall(t, ah, http.MethodGet, "/api/v1/access-reviews/due", "", "", "")
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
