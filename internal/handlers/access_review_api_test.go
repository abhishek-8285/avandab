package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// accessReviewCall dispatches directly to the handler (router permission
// middleware is covered by TestRouteGuardPermissions_ExistInDB; routes reuse
// privacy:manage so no new permission row is needed).
func accessReviewCall(t *testing.T, app *App, method, path, tenant, id, body string) *httptest.ResponseRecorder {
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
	ah := &AccessReviewHandlers{App: app}
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
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Review Web", "review@web.test", "Str0ng!Pass", "Review Web Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)
	user, err := app.Services.Users.GetUserByEmail(context.Background(), "review@web.test")
	require.NoError(t, err)
	tenant := user.TenantID

	due := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	rr = accessReviewCall(t, app, http.MethodPost, "/api/v1/access-reviews/open", tenant, "",
		`{"user_id":"`+string(user.ID)+`","role_name":"org_admin","period":"2026-H2","due_at":"`+due+`"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	var created map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)

	getPath := "/api/v1/access-reviews/" + id

	// Foreign tenant reads as 404, never another org's review.
	rr = accessReviewCall(t, app, http.MethodGet, getPath, "someone-else", id, "")
	require.Equal(t, http.StatusNotFound, rr.Code)

	rr = accessReviewCall(t, app, http.MethodGet, getPath, tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	// Due watchlist carries the overdue pending row.
	rr = accessReviewCall(t, app, http.MethodGet, "/api/v1/access-reviews/due", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	var listed map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&listed))
	reviews, _ := listed["reviews"].([]interface{})
	require.Len(t, reviews, 1)

	rr = accessReviewCall(t, app, http.MethodPost, getPath+"/certify", tenant, id,
		`{"reviewed_by":"boss-1"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = accessReviewCall(t, app, http.MethodPost, getPath+"/revoke", tenant, id,
		`{"reviewed_by":"boss-1","note":"left the org"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	// Certified→revoked row leaves the due watchlist.
	rr = accessReviewCall(t, app, http.MethodGet, "/api/v1/access-reviews/due", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)
	listed = nil
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&listed))
	reviews, _ = listed["reviews"].([]interface{})
	require.Empty(t, reviews)

	// Tenantless callers get 401.
	rr = accessReviewCall(t, app, http.MethodGet, "/api/v1/access-reviews/due", "", "", "")
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
