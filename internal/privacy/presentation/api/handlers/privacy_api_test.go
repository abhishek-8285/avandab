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

func newPrivacyHandlers(t *testing.T) (*PrivacyHandlers, string) {
	t.Helper()
	name := fmt.Sprintf("test_privacy_h_%d", time.Now().UnixNano())
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

	const tenant = "tenant_privacy_h"
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		tenant, tenant, tenant)
	require.NoError(t, err)

	return NewPrivacyHandlers(privacyapp.NewPrivacyService(db, id.NewUUIDGenerator())), tenant
}

// privacyCall dispatches directly to the handler (router permission middleware
// is covered by TestRouteGuardPermissions_ExistInDB + the 00154 backfill).
func privacyCall(t *testing.T, ph *PrivacyHandlers, method, path, rawQuery, tenant, id, body string) *httptest.ResponseRecorder {
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
	ph, tenant := newPrivacyHandlers(t)

	rr := privacyCall(t, ph, http.MethodPost, "/api/v1/privacy/breaches", "", tenant, "",
		`{"title":"lost laptop","affected_count":3}`)
	require.Equal(t, http.StatusOK, rr.Code)
	var created map[string]interface{}
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
	id, _ := created["id"].(string)
	require.NotEmpty(t, id)

	getPath := "/api/v1/privacy/breaches/" + id

	// Foreign tenant reads as 404, never another org's incident.
	rr = privacyCall(t, ph, http.MethodGet, getPath, "", "someone-else", id, "")
	require.Equal(t, http.StatusNotFound, rr.Code)

	rr = privacyCall(t, ph, http.MethodGet, getPath, "", tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, ph, http.MethodPost, getPath+"/notify", "", tenant, id,
		`{"board":true,"principals":true}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, ph, http.MethodPost, getPath+"/detail", "", tenant, id,
		`{"report":"wiped remotely"}`)
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, ph, http.MethodGet, "/api/v1/privacy/breaches", "overdue=1", tenant, "", "")
	require.Equal(t, http.StatusOK, rr.Code)

	rr = privacyCall(t, ph, http.MethodPost, getPath+"/close", "", tenant, id, "")
	require.Equal(t, http.StatusOK, rr.Code)

	// Tenantless callers get 401.
	rr = privacyCall(t, ph, http.MethodGet, "/api/v1/privacy/breaches", "", "", "", "")
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
