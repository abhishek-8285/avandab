package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/reportbuilder"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupReportBuilderTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"admin-user:reports:read":   true,
			"admin-user:reports:write":  true,
			"viewer-user:reports:read":  true,
			"viewer-user:reports:write": false,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}

	app.ReportBuilder = &ReportBuilderHandlers{App: app}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	app.ReportBuilder.RegisterAPIRoutes(r)
	return app, r
}

func TestReportBuilderAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupReportBuilderTestApp(t)
	defer app.DB.Close()

	tenantA := "tenant-rb-a"
	tenantB := "tenant-rb-b"

	_, err := app.DB.Exec(`INSERT INTO tenants (id, name, slug) VALUES (?, 'Tenant A', 'tenant-a'), (?, 'Tenant B', 'tenant-b')`, tenantA, tenantB)
	require.NoError(t, err)

	// Seed trips in tenant A and tenant B
	_, err = app.DB.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES 
		('r-a', ?, 'City A1', 'City A2', 150.0, 3.0, 5000.0),
		('r-b', ?, 'City B1', 'City B2', 270.0, 5.0, 8000.0)`, tenantA, tenantB)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO trips (id, tenant_id, trip_number, route_id, departure_time, status, estimated_margin, toll_costs, fuel_consumed_liters) VALUES 
		('tr-a1', ?, 'TRP-A1', 'r-a', '2026-10-01 10:00:00', 'completed', 3500.0, 600.0, 90.0),
		('tr-a2', ?, 'TRP-A2', 'r-a', '2026-10-02 11:00:00', 'completed', 5000.0, 800.0, 110.0),
		('tr-b1', ?, 'TRP-B1', 'r-b', '2026-10-01 10:00:00', 'completed', 9000.0, 1500.0, 200.0)`,
		tenantA, tenantA, tenantB)
	require.NoError(t, err)

	// 1. RBAC Check: Anonymous request without session -> 401 Unauthorized
	reqAnon := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/datasets", nil)
	wAnon := httptest.NewRecorder()
	r.ServeHTTP(wAnon, reqAnon)
	assert.Equal(t, http.StatusUnauthorized, wAnon.Code)

	// 2. Metadata Catalog
	reqCatalog := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/datasets", nil)
	reqCatalog.Header.Set("X-Test-User", "viewer-user")
	reqCatalog.Header.Set("X-Test-Tenant", tenantA)
	wCatalog := httptest.NewRecorder()
	r.ServeHTTP(wCatalog, reqCatalog)
	assert.Equal(t, http.StatusOK, wCatalog.Code)

	var catalog map[reportbuilder.DatasetType]reportbuilder.DatasetMeta
	err = json.Unmarshal(wCatalog.Body.Bytes(), &catalog)
	require.NoError(t, err)
	assert.Len(t, catalog, 10)
	assert.Contains(t, catalog, reportbuilder.DatasetTrips)

	// 3. List Reports (defaults include system reports)
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/reports", nil)
	reqList.Header.Set("X-Test-User", "viewer-user")
	reqList.Header.Set("X-Test-Tenant", tenantA)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	assert.Equal(t, http.StatusOK, wList.Code)

	var listResp struct {
		Reports []reportbuilder.CustomReport `json:"reports"`
		Total   int                          `json:"total"`
	}
	err = json.Unmarshal(wList.Body.Bytes(), &listResp)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, listResp.Total, 1)

	// 4. RBAC Check: Viewer attempts to create a custom report (reports:write required) -> 403 Forbidden
	createPayload := reportbuilder.CreateReportInput{
		Name:            "Tenant A Trip Summary",
		Description:     "Trip margins and toll costs",
		Dataset:         reportbuilder.DatasetTrips,
		SelectedColumns: []string{"trip_number", "status", "estimated_margin", "toll_costs"},
		Filters: []reportbuilder.ReportFilter{
			{Column: "status", Operator: reportbuilder.OpEq, Value: "completed"},
		},
		SortBy:        "estimated_margin",
		SortDirection: "DESC",
		ChartType:     "TABLE",
	}
	bodyBytes, _ := json.Marshal(createPayload)

	reqCreateViewer := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/reports", bytes.NewReader(bodyBytes))
	reqCreateViewer.Header.Set("X-Test-User", "viewer-user")
	reqCreateViewer.Header.Set("X-Test-Tenant", tenantA)
	wCreateViewer := httptest.NewRecorder()
	r.ServeHTTP(wCreateViewer, reqCreateViewer)
	assert.Equal(t, http.StatusForbidden, wCreateViewer.Code)

	// 5. Admin creates a custom report -> 201 Created
	reqCreateAdmin := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/reports", bytes.NewReader(bodyBytes))
	reqCreateAdmin.Header.Set("X-Test-User", "admin-user")
	reqCreateAdmin.Header.Set("X-Test-Tenant", tenantA)
	wCreateAdmin := httptest.NewRecorder()
	r.ServeHTTP(wCreateAdmin, reqCreateAdmin)
	assert.Equal(t, http.StatusCreated, wCreateAdmin.Code)

	var createdRep reportbuilder.CustomReport
	err = json.Unmarshal(wCreateAdmin.Body.Bytes(), &createdRep)
	require.NoError(t, err)
	assert.NotEmpty(t, createdRep.ID)
	assert.Equal(t, "Tenant A Trip Summary", createdRep.Name)

	// 6. Get Report Details
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/reports/"+createdRep.ID, nil)
	reqGet.Header.Set("X-Test-User", "viewer-user")
	reqGet.Header.Set("X-Test-Tenant", tenantA)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	assert.Equal(t, http.StatusOK, wGet.Code)

	// 7. Update Report
	newDesc := "Updated description by admin"
	updatePayload := reportbuilder.UpdateReportInput{
		Description: &newDesc,
	}
	updBytes, _ := json.Marshal(updatePayload)
	reqUpd := httptest.NewRequest(http.MethodPut, "/api/v1/report-builder/reports/"+createdRep.ID, bytes.NewReader(updBytes))
	reqUpd.Header.Set("X-Test-User", "admin-user")
	reqUpd.Header.Set("X-Test-Tenant", tenantA)
	wUpd := httptest.NewRecorder()
	r.ServeHTTP(wUpd, reqUpd)
	assert.Equal(t, http.StatusOK, wUpd.Code)

	// 8. Try updating system report -> 403 Forbidden
	reqUpdSys := httptest.NewRequest(http.MethodPut, "/api/v1/report-builder/reports/sys-rep-trip-profitability", bytes.NewReader(updBytes))
	reqUpdSys.Header.Set("X-Test-User", "admin-user")
	reqUpdSys.Header.Set("X-Test-Tenant", tenantA)
	wUpdSys := httptest.NewRecorder()
	r.ServeHTTP(wUpdSys, reqUpdSys)
	assert.Equal(t, http.StatusForbidden, wUpdSys.Code)

	// 9. Run Report (JSON)
	reqRunJSON := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/reports/"+createdRep.ID+"/run", nil)
	reqRunJSON.Header.Set("X-Test-User", "viewer-user")
	reqRunJSON.Header.Set("X-Test-Tenant", tenantA)
	wRunJSON := httptest.NewRecorder()
	r.ServeHTTP(wRunJSON, reqRunJSON)
	assert.Equal(t, http.StatusOK, wRunJSON.Code)

	var runRes reportbuilder.RunReportResult
	err = json.Unmarshal(wRunJSON.Body.Bytes(), &runRes)
	require.NoError(t, err)
	assert.Equal(t, 2, runRes.RowCount, "Tenant A should see only their 2 completed trips")
	assert.Equal(t, "TRP-A2", runRes.QueryResult.Rows[0]["trip_number"])

	// 10. Run Report (CSV Export)
	reqRunCSV := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/reports/"+createdRep.ID+"/run?format=CSV", nil)
	reqRunCSV.Header.Set("X-Test-User", "viewer-user")
	reqRunCSV.Header.Set("X-Test-Tenant", tenantA)
	wRunCSV := httptest.NewRecorder()
	r.ServeHTTP(wRunCSV, reqRunCSV)
	assert.Equal(t, http.StatusOK, wRunCSV.Code)
	assert.Equal(t, "text/csv", wRunCSV.Header().Get("Content-Type"))
	csvBody := wRunCSV.Body.String()
	assert.Contains(t, csvBody, "trip_number,status,estimated_margin,toll_costs")
	assert.Contains(t, csvBody, "TRP-A2,completed,5000,800")
	assert.NotContains(t, csvBody, "TRP-B1", "Tenant B data must never leak into Tenant A CSV")

	// 11. Run Ad-hoc Query Preview
	adhocReq := AdHocRunRequest{
		Report: reportbuilder.CreateReportInput{
			Name:            "Adhoc Margins",
			Dataset:         reportbuilder.DatasetTrips,
			SelectedColumns: []string{"trip_number", "estimated_margin"},
		},
		Run: reportbuilder.RunReportInput{
			Format: "JSON",
		},
	}
	adhocBytes, _ := json.Marshal(adhocReq)
	reqAdhoc := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/run", bytes.NewReader(adhocBytes))
	reqAdhoc.Header.Set("X-Test-User", "viewer-user")
	reqAdhoc.Header.Set("X-Test-Tenant", tenantB)
	wAdhoc := httptest.NewRecorder()
	r.ServeHTTP(wAdhoc, reqAdhoc)
	assert.Equal(t, http.StatusOK, wAdhoc.Code)

	var adhocRes reportbuilder.RunReportResult
	err = json.Unmarshal(wAdhoc.Body.Bytes(), &adhocRes)
	require.NoError(t, err)
	assert.Equal(t, 1, adhocRes.RowCount, "Tenant B sees only their 1 trip")
	assert.Equal(t, "TRP-B1", adhocRes.QueryResult.Rows[0]["trip_number"])

	// 12. Schedules Lifecycle
	schedCreateIn := reportbuilder.CreateScheduleInput{
		ReportID:   createdRep.ID,
		Name:       "Weekly Trip Margin Delivery",
		Frequency:  "WEEKLY",
		Format:     "CSV",
		Channel:    "EMAIL",
		Recipients: []string{"billing@tenant-a.com"},
	}
	schedBytes, _ := json.Marshal(schedCreateIn)
	reqCreateSched := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/schedules", bytes.NewReader(schedBytes))
	reqCreateSched.Header.Set("X-Test-User", "admin-user")
	reqCreateSched.Header.Set("X-Test-Tenant", tenantA)
	wCreateSched := httptest.NewRecorder()
	r.ServeHTTP(wCreateSched, reqCreateSched)
	assert.Equal(t, http.StatusCreated, wCreateSched.Code)

	var createdSched reportbuilder.ReportSchedule
	err = json.Unmarshal(wCreateSched.Body.Bytes(), &createdSched)
	require.NoError(t, err)
	assert.NotEmpty(t, createdSched.ID)

	// List schedules
	reqListSched := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/schedules", nil)
	reqListSched.Header.Set("X-Test-User", "viewer-user")
	reqListSched.Header.Set("X-Test-Tenant", tenantA)
	wListSched := httptest.NewRecorder()
	r.ServeHTTP(wListSched, reqListSched)
	assert.Equal(t, http.StatusOK, wListSched.Code)

	// Trigger schedule
	reqTrigger := httptest.NewRequest(http.MethodPost, "/api/v1/report-builder/schedules/"+createdSched.ID+"/trigger", nil)
	reqTrigger.Header.Set("X-Test-User", "admin-user")
	reqTrigger.Header.Set("X-Test-Tenant", tenantA)
	wTrigger := httptest.NewRecorder()
	r.ServeHTTP(wTrigger, reqTrigger)
	assert.Equal(t, http.StatusOK, wTrigger.Code)

	// 13. List Execution history
	reqExecs := httptest.NewRequest(http.MethodGet, "/api/v1/report-builder/executions", nil)
	reqExecs.Header.Set("X-Test-User", "viewer-user")
	reqExecs.Header.Set("X-Test-Tenant", tenantA)
	wExecs := httptest.NewRecorder()
	r.ServeHTTP(wExecs, reqExecs)
	assert.Equal(t, http.StatusOK, wExecs.Code)

	var execsResp struct {
		Executions []reportbuilder.ReportExecution `json:"executions"`
		Total      int                             `json:"total"`
	}
	err = json.Unmarshal(wExecs.Body.Bytes(), &execsResp)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, execsResp.Total, 1)

	// 14. Delete Schedule
	reqDelSched := httptest.NewRequest(http.MethodDelete, "/api/v1/report-builder/schedules/"+createdSched.ID, nil)
	reqDelSched.Header.Set("X-Test-User", "admin-user")
	reqDelSched.Header.Set("X-Test-Tenant", tenantA)
	wDelSched := httptest.NewRecorder()
	r.ServeHTTP(wDelSched, reqDelSched)
	assert.Equal(t, http.StatusOK, wDelSched.Code)

	// 15. Delete Custom Report
	reqDelRep := httptest.NewRequest(http.MethodDelete, "/api/v1/report-builder/reports/"+createdRep.ID, nil)
	reqDelRep.Header.Set("X-Test-User", "admin-user")
	reqDelRep.Header.Set("X-Test-Tenant", tenantA)
	wDelRep := httptest.NewRecorder()
	r.ServeHTTP(wDelRep, reqDelRep)
	assert.Equal(t, http.StatusOK, wDelRep.Code)

	// Try deleting system report -> 403 Forbidden
	reqDelSys := httptest.NewRequest(http.MethodDelete, "/api/v1/report-builder/reports/sys-rep-trip-profitability", nil)
	reqDelSys.Header.Set("X-Test-User", "admin-user")
	reqDelSys.Header.Set("X-Test-Tenant", tenantA)
	wDelSys := httptest.NewRecorder()
	r.ServeHTTP(wDelSys, reqDelSys)
	assert.Equal(t, http.StatusForbidden, wDelSys.Code)
}
