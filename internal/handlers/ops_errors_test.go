package handlers

import (
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	opserrors "transport-app/internal/operations/errors"
	"transport-app/internal/shared"
)

func newOpsErrorsTestApp(t *testing.T) (*App, *opserrors.Reporter) {
	t.Helper()
	name := fmt.Sprintf("test_ops_errors_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cwd, _ := os.Getwd()
	migrationsDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)

	if filepath.Base(cwd) == "handlers" {
		_ = os.Chdir("../..")
	}
	authSrv := allowAllAuthSvc{}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Templates: tmpl,
		AuthSrv:   authSrv,
	}
	rep := opserrors.NewReporter(nil, opserrors.NewSQLiteStore(db), "test", "v-test")
	app.OpsErrors = NewOpsErrorsHandler(app, rep)
	return app, rep
}

func TestOpsErrorsPage_ListsErrorsAndIncidents(t *testing.T) {
	app, rep := newOpsErrorsTestApp(t)
	ctx := shared.ContextWithTenantID(t.Context(), "1")

	_, err := rep.Report(ctx, opserrors.ErrorReport{
		URL: "/api/v1/bookings", Method: "POST",
		Message: "insert failed\nat db.go:1", Severity: opserrors.SeverityHigh,
	})
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/ops/errors", app.OpsErrors.Page)

	req := withSession(httptest.NewRequest(http.MethodGet, "/ops/errors?severity=HIGH", nil), "u1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	if !assert.Contains(t, w.Body.String(), "Error Reports") {
		t.Logf("BODY SNIPPET: %.20000s", w.Body.String())
	}
	assert.Contains(t, w.Body.String(), "insert failed")
	assert.Contains(t, w.Body.String(), "/api/v1/bookings")
}

func TestOpsErrorsAPIList(t *testing.T) {
	app, rep := newOpsErrorsTestApp(t)
	ctx := shared.ContextWithTenantID(t.Context(), "1")

	first, err := rep.Report(ctx, opserrors.ErrorReport{URL: "/x", Method: "GET", Message: "boom"})
	require.NoError(t, err)
	second, err := rep.Report(ctx, opserrors.ErrorReport{URL: "/x", Method: "GET", Message: "boom"})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(shared.ContextWithTenantID(req.Context(), "1")))
		})
	}).Get("/api/v1/errors", app.OpsErrors.APIList)

	req := withSession(httptest.NewRequest(http.MethodGet, "/api/v1/errors", nil), "u1", "admin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Total  int `json:"total"`
		Errors []struct {
			ID          string `json:"id"`
			Occurrences int    `json:"occurrences"`
			Fingerprint string `json:"fingerprint"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.Total)
	require.Len(t, out.Errors, 1)
	assert.Equal(t, 2, out.Errors[0].Occurrences)
	assert.NotEmpty(t, out.Errors[0].Fingerprint)
}

func TestOpsErrorsAPIGetError(t *testing.T) {
	app, rep := newOpsErrorsTestApp(t)
	ctx := shared.ContextWithTenantID(t.Context(), "1")

	reported, err := rep.Report(ctx, opserrors.ErrorReport{
		URL: "/pay", Method: "POST", Message: "gateway down\nsecond line",
		Severity: opserrors.SeverityHigh, RequestID: "req-42",
		StackTrace: "at pay (handler.go:9)", Environment: "test",
		Metadata: map[string]interface{}{"breadcrumbs": "route /pay\nclick Pay", "source": "server"},
	})
	require.NoError(t, err)

	// Same fingerprint seeded under another tenant must stay invisible to tenant 1.
	otherTenant, err := rep.Report(shared.ContextWithTenantID(t.Context(), "2"), opserrors.ErrorReport{
		URL: "/pay", Method: "POST", Message: "gateway down", Severity: opserrors.SeverityHigh,
	})
	require.NoError(t, err)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(shared.ContextWithTenantID(req.Context(), "1")))
		})
	}).Get("/api/v1/errors/{fingerprint}", app.OpsErrors.APIGetError)

	get := func(fp string) *httptest.ResponseRecorder {
		req := withSession(httptest.NewRequest(http.MethodGet, "/api/v1/errors/"+fp, nil), "u1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get(reported.Fingerprint)
	require.Equal(t, http.StatusOK, w.Code)

	var detail struct {
		Fingerprint string                 `json:"fingerprint"`
		Message     string                 `json:"message"`
		StackTrace  string                 `json:"stack_trace"`
		RequestID   string                 `json:"request_id"`
		Environment string                 `json:"environment"`
		Occurrences int                    `json:"occurrences"`
		Metadata    map[string]interface{} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	assert.Equal(t, reported.Fingerprint, detail.Fingerprint)
	assert.Equal(t, "gateway down\nsecond line", detail.Message)
	assert.Equal(t, "at pay (handler.go:9)", detail.StackTrace)
	assert.Equal(t, "req-42", detail.RequestID)
	assert.Equal(t, "test", detail.Environment)
	assert.Equal(t, 1, detail.Occurrences)
	assert.Equal(t, "server", detail.Metadata["source"])

	unknown := get("deadbeefdeadbeef")
	assert.Equal(t, http.StatusNotFound, unknown.Code)
	assert.Contains(t, unknown.Header().Get("Content-Type"), "application/problem+json")

	crossTenant := get(otherTenant.Fingerprint)
	assert.Equal(t, http.StatusNotFound, crossTenant.Code)
}

func TestOpsErrorsAPIResolveIncident(t *testing.T) {
	app, rep := newOpsErrorsTestApp(t)
	ctx := shared.ContextWithTenantID(t.Context(), "1")

	_, err := rep.Report(ctx, opserrors.ErrorReport{
		URL: "/pay", Method: "POST", Message: "gateway down", Severity: opserrors.SeverityCritical,
	})
	require.NoError(t, err)

	incs, err := rep.ListIncidents(ctx, opserrors.IncidentFilter{TenantID: "1", Status: "OPEN"})
	require.NoError(t, err)
	require.Len(t, incs, 1)
	incidentID := incs[0].ID

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(shared.ContextWithTenantID(req.Context(), "1")))
		})
	}).Post("/api/v1/errors/incidents/{incidentID}/resolve", app.OpsErrors.APIResolveIncident)

	body := `{"status":"RESOLVED","assigned_to":"ops","root_cause":"bad deploy"}`
	req := withSession(httptest.NewRequest(http.MethodPost,
		"/api/v1/errors/incidents/"+incidentID+"/resolve", strings.NewReader(body)), "u1", "admin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	open, err := rep.ListIncidents(ctx, opserrors.IncidentFilter{TenantID: "1", Status: "OPEN"})
	require.NoError(t, err)
	assert.Empty(t, open)

	resolved, err := rep.ListIncidents(ctx, opserrors.IncidentFilter{TenantID: "1", Status: "RESOLVED"})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	assert.Equal(t, "bad deploy", resolved[0].RootCause)

	reqBad := withSession(httptest.NewRequest(http.MethodPost,
		"/api/v1/errors/incidents/inc_missing/resolve", strings.NewReader(body)), "u1", "admin")
	reqBad.Header.Set("Content-Type", "application/json")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqBad)
	assert.Equal(t, http.StatusNotFound, wBad.Code)
}

func TestOpsErrorsAPIResolveValidation(t *testing.T) {
	app, _ := newOpsErrorsTestApp(t)

	r := chi.NewRouter()
	r.Post("/api/v1/errors/incidents/{incidentID}/resolve", app.OpsErrors.APIResolveIncident)

	req := withSession(httptest.NewRequest(http.MethodPost,
		"/api/v1/errors/incidents/inc_x/resolve",
		strings.NewReader(`{"status":"WAT"}`)), "u1", "admin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/problem+json")

	var p struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
	assert.Equal(t, "VALIDATION_ERROR", p.Code)
}

func TestOpsErrorsAPIClientReport(t *testing.T) {
	app, rep := newOpsErrorsTestApp(t)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), "1")
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}).Post("/api/v1/errors/client", app.OpsErrors.APIClientReport)

	payload := `{"message":"TypeError: x is not a function","stack":"at f (app.js:1)","path":"/kharcha/create","request_id":"req-777","breadcrumbs":["route push /kharcha","click Submit"],"user_agent":"test-agent"}`
	req := withSession(httptest.NewRequest(http.MethodPost, "/api/v1/errors/client", strings.NewReader(payload)), "user-9", "admin")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var out struct {
		Status  string `json:"status"`
		ErrorID string `json:"error_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, "received", out.Status)
	assert.NotEmpty(t, out.ErrorID)

	errs, err := rep.ListErrors(shared.ContextWithTenantID(t.Context(), "1"), opserrors.ErrorFilter{})
	require.NoError(t, err)
	require.Len(t, errs, 1)
	assert.Equal(t, "CLIENT", errs[0].Method)
	assert.Equal(t, "user-9", errs[0].UserID)
	assert.Equal(t, "req-777", errs[0].RequestID)

	bad := withSession(httptest.NewRequest(http.MethodPost, "/api/v1/errors/client",
		strings.NewReader(`{invalid`)), "u", "admin")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, bad)
	assert.Equal(t, http.StatusBadRequest, wBad.Code)
}

func TestOpsErrorsPageRequiresSession(t *testing.T) {
	app, _ := newOpsErrorsTestApp(t)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return middleware.RoleRequired(1)(next)
	}).Get("/ops/errors", app.OpsErrors.Page)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ops/errors", nil))
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/login", w.Header().Get("Location"))
	_ = auth.SessionData{}
}
