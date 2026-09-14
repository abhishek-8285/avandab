package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// privacyCall dispatches directly to the handler (router permission middleware
// is covered by TestRouteGuardPermissions_ExistInDB + the 00154 backfill).
func privacyCall(t *testing.T, app *App, method, path, rawQuery, tenant, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if rawQuery != "" {
		req.URL.RawQuery = rawQuery
	}
	if tenant != "" {
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant)))
	}
	if id != "" {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	rr := httptest.NewRecorder()
	ph := &PrivacyHandlers{App: app}
	switch path {
	case "/api/v1/privacy/breaches":
		if method == http.MethodGet {
			ph.ListBreachesAPI(rr, req)
		} else {
			ph.ReportBreachAPI(rr, req)
		}
	default:
		switch {
		case strings.HasSuffix(path, "/notify"):
			ph.NotifyBreachAPI(rr, req)
		case strings.HasSuffix(path, "/detail"):
			ph.DetailBreachAPI(rr, req)
		case strings.HasSuffix(path, "/close"):
			ph.CloseBreachAPI(rr, req)
		default:
			ph.GetBreachAPI(rr, req)
		}
	}
	return rr
}

func TestPrivacyEndpoints_Lifecycle(t *testing.T) {
	app := newRegisterTestApp(t)
	rr := postRegisterWithCompany(app, "Breach Web", "breach@web.test", "Str0ng!Pass", "Breach Web Co")
	require.Equal(t, http.StatusSeeOther, rr.Code)
	user, err := app.Services.Users.GetUserByEmail(context.Background(), "breach@web.test")
	require.NoError(t, err)
	tenant := user.TenantID

	rr = privacyCall(t, app, http.MethodPost, "/api/v1/privacy/breaches", "", tenant, "",
		`{"title":"lost laptop","affected_count":3}`)
	require.Equal(t, http.StatusOK, rr.Code)
	var created map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)

	getPath := "/api/v1/privacy/breaches/" + id

	// Foreign tenant reads as 404, never another org's incident.
	rr = privacyCall(t, app, http.MethodGet, getPath, "", "someone-else", id, "")
	require.Equal(t, http.StatusNotFound, rr.Code)

	rr = privacyCall(t, app, http.MethodGet, getPath, "", tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, app, http.MethodPost, getPath+"/notify", "", tenant, id,
		`{"board":true,"principals":true}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, app, http.MethodPost, getPath+"/detail", "", tenant, id,
		`{"report":"wiped remotely"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, app, http.MethodGet, "/api/v1/privacy/breaches", "overdue=1", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, app, http.MethodPost, getPath+"/close", "", tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	// Tenantless callers get 401.
	rr = privacyCall(t, app, http.MethodGet, "/api/v1/privacy/breaches", "", "", "", "")
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
