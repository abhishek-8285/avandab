package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/tyre"
)

func setupTyreTestApp(t *testing.T) (*App, *chi.Mux) {
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
			"admin-user:tyres:read":   true,
			"admin-user:tyres:write":  true,
			"viewer-user:tyres:read":  true,
			"viewer-user:tyres:write": false,
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

	app.Tyres = &TyreHandlers{App: app}

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

	app.Tyres.RegisterAPIRoutes(r)
	return app, r
}

func TestTyresAPI_FullLifecycleAndRBAC(t *testing.T) {
	app, r := setupTyreTestApp(t)

	tenantA := "tenant-tyre-a"
	tenantB := "tenant-tyre-b"

	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'Tenant A', 'tenant-a'), ($2, 'Tenant B', 'tenant-b')`, tenantA, tenantB)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, current_mileage)
		VALUES ('veh-t1', $1, 'DL01TY0001', 'DL01TY0001', 'truck', 12000, 50000)`, tenantA)
	require.NoError(t, err)

	// 1. Viewer cannot create tyre (RBAC tyres:write required)
	bodyCreate := `{
		"serial_number": "MRF-R100-01",
		"brand": "MRF",
		"model": "Steel Muscle-S",
		"size": "295/80 R22.5",
		"initial_tread_depth_mm": 16.0,
		"min_tread_depth_mm": 3.0,
		"purchase_cost": 24000.0
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tyres", bytes.NewBufferString(bodyCreate))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// 2. Admin creates tyre in Tenant A
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tyres", bytes.NewBufferString(bodyCreate))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var createResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp))
	created := createResp.Tyre
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "MRF-R100-01", created.SerialNumber)
	assert.Equal(t, tyre.TyreStatusInStock, created.Status)
	assert.Equal(t, 16.0, created.CurrentTreadDepthMM)

	// 3. Admin creates a second tyre in Tenant A
	bodyCreate2 := `{
		"serial_number": "MRF-R100-02",
		"brand": "MRF",
		"model": "Steel Muscle-S",
		"size": "295/80 R22.5",
		"initial_tread_depth_mm": 16.0,
		"min_tread_depth_mm": 3.0,
		"purchase_cost": 24000.0
	}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tyres", bytes.NewBufferString(bodyCreate2))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	var createResp2 struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &createResp2))
	created2 := createResp2.Tyre

	// 4. List tyres in Tenant A (both viewer and admin can read)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tyres?brand=MRF", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Tyres []tyre.Tyre `json:"tyres"`
		Total int         `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 2, listResp.Total)
	assert.Len(t, listResp.Tyres, 2)

	// 5. Cross-tenant isolation: Tenant B sees 0 tyres
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tyres", nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Total)

	// 6. Get tyre details
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tyres/%s", created.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var getResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &getResp))
	assert.Equal(t, created.ID, getResp.Tyre.ID)

	// Cross-tenant: Tenant B cannot fetch Tenant A's tyre
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tyres/%s", created.ID), nil)
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantB)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 7. Update tyre details
	updatePayload := `{"brand": "MRF Fleet"}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/tyres/%s", created.ID), bytes.NewBufferString(updatePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 8. Mount tyre 1 onto veh-t1 at FL (Front Left)
	mountPayload := `{
		"vehicle_id": "veh-t1",
		"axle_position": "FL",
		"mount_odometer_km": 50000.0,
		"notes": "Mounted new tyre"
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/mount", created.ID), bytes.NewBufferString(mountPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var mountResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &mountResp))
	mounted := mountResp.Tyre
	assert.Equal(t, tyre.TyreStatusMounted, mounted.Status)
	assert.Equal(t, "veh-t1", *mounted.VehicleID)
	assert.Equal(t, "FL", *mounted.AxlePosition)

	// 9. Mount tyre 2 onto veh-t1 at FR (Front Right)
	mountPayload2 := `{
		"vehicle_id": "veh-t1",
		"axle_position": "FR",
		"mount_odometer_km": 50000.0
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/mount", created2.ID), bytes.NewBufferString(mountPayload2))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 10. Fetch vehicle tyres: GET /api/v1/vehicles/{id}/tyres
	req = httptest.NewRequest(http.MethodGet, "/api/v1/vehicles/veh-t1/tyres", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var vehResp struct {
		VehicleID string      `json:"vehicle_id"`
		Tyres     []tyre.Tyre `json:"tyres"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &vehResp))
	assert.Len(t, vehResp.Tyres, 2)

	// Vehicle travels 20,000 km (current_mileage advances from 50,000 to 70,000 km)
	_, err = app.DB.Exec(`UPDATE vehicles SET current_mileage = 70000.0 WHERE id = 'veh-t1'`)
	require.NoError(t, err)

	// 11. Rotate tyres (FL <-> FR)
	rotatePayload := fmt.Sprintf(`{
		"vehicle_id": "veh-t1",
		"odometer_km": 70000.0,
		"rotations": [
			{"tyre_id": "%s", "from_position": "FL", "to_position": "FR"},
			{"tyre_id": "%s", "from_position": "FR", "to_position": "FL"}
		]
	}`, created.ID, created2.ID)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tyres/rotate", bytes.NewBufferString(rotatePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 12. Record inspection for tyre 1
	inspectPayload := `{
		"tread_depth_mm": 12.0,
		"pressure_psi": 120.0,
		"odometer_km": 70000.0,
		"inspector_name": "Ramesh Mech",
		"action_taken": "Rotated and aligned"
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/inspect", created.ID), bytes.NewBufferString(inspectPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var inspResp struct {
		Inspection tyre.TyreInspection `json:"inspection"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inspResp))
	insp := inspResp.Inspection
	assert.Equal(t, 12.0, insp.TreadDepthMM)

	// Check tyre updated tread depth and metrics
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tyres/%s", created.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var inspectedResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inspectedResp))
	inspectedTyre := inspectedResp.Tyre
	assert.Equal(t, 12.0, inspectedTyre.CurrentTreadDepthMM)
	require.NotNil(t, inspectedTyre.Metrics)
	assert.Greater(t, inspectedTyre.Metrics.KMPerMM, 0.0)
	assert.Greater(t, inspectedTyre.Metrics.ProjectedRemainingKM, 0.0)

	// 13. Dismount tyre 1
	dismountPayload := `{
		"dismount_odometer_km": 90000.0,
		"reason": "Sent for retreading",
		"notes": "Casing in excellent condition"
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/dismount", created.ID), bytes.NewBufferString(dismountPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 14. Send tyre 1 for retreading
	retreadSendPayload := `{
		"action": "SEND",
		"vendor_name": "Apollo Retread Hub",
		"cost": 0.0,
		"notes": "Precure cold process"
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/retread", created.ID), bytes.NewBufferString(retreadSendPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Complete retreading
	retreadCompletedPayload := `{
		"action": "RECEIVE",
		"vendor_name": "Apollo Retread Hub",
		"cost": 4500.0,
		"new_tread_depth_mm": 14.0
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/retread", created.ID), bytes.NewBufferString(retreadCompletedPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var retreadResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &retreadResp))
	retreaded := retreadResp.Tyre
	assert.Equal(t, tyre.TyreStatusInStock, retreaded.Status)
	assert.Equal(t, 1, retreaded.RetreadCount)
	assert.Equal(t, 14.0, retreaded.CurrentTreadDepthMM)

	// 15. Scrap tyre 1
	scrapPayload := `{
		"reason": "Sidewall crack beyond repair limit",
		"recovery_amount": 1800.0
	}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/tyres/%s/scrap", created.ID), bytes.NewBufferString(scrapPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "admin-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var scrapResp struct {
		Tyre tyre.Tyre `json:"tyre"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &scrapResp))
	scrapped := scrapResp.Tyre
	assert.Equal(t, tyre.TyreStatusScrapped, scrapped.Status)
	assert.Equal(t, 1800.0, scrapped.ScrapRecoveryAmount)
	require.NotNil(t, scrapped.Metrics)
	assert.Greater(t, scrapped.Metrics.CPKM, 0.0)

	// 16. Verify inspection history: GET /api/v1/tyres/{id}/inspections
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tyres/%s/inspections", created.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var inspListResp struct {
		Inspections []tyre.TyreInspection `json:"inspections"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inspListResp))
	assert.Len(t, inspListResp.Inspections, 1)

	// 17. Verify activity history: GET /api/v1/tyres/{id}/activities
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/tyres/%s/activities", created.ID), nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var actListResp struct {
		Activities []tyre.TyreActivity `json:"activities"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &actListResp))
	assert.GreaterOrEqual(t, len(actListResp.Activities), 5) // CREATE, MOUNT, ROTATE, DISMOUNT, RETREAD, SCRAP

	// 18. Fleet Summary: GET /api/v1/tyres/summary
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tyres/summary", nil)
	req.Header.Set("X-Test-User", "viewer-user")
	req.Header.Set("X-Test-Tenant", tenantA)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var sumResp struct {
		Summary tyre.FleetTyreSummary `json:"summary"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &sumResp))
	summary := sumResp.Summary
	assert.Equal(t, 2, summary.TotalTyres)
	assert.Equal(t, 1, summary.MountedCount)
	assert.Equal(t, 1, summary.ScrappedCount)
	assert.Greater(t, summary.FleetAverageCPKM, 0.0)
}
