package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/shared"
)

func setupMaintenancePlansHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:maint_plan_api_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE vehicles (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_number TEXT NOT NULL,
			registration_number TEXT NOT NULL,
			fleet_class TEXT NOT NULL DEFAULT 'CV',
			status TEXT NOT NULL DEFAULT 'available',
			odometer REAL NOT NULL DEFAULT 0,
			current_mileage REAL NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE vehicle_measuring_points (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
			unit TEXT NOT NULL DEFAULT 'KM',
			annual_estimate REAL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE vehicle_measurements (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			point_id TEXT NOT NULL,
			doc_number TEXT,
			counter_reading REAL NOT NULL,
			difference_reading REAL,
			total_counter_reading REAL,
			measured_at DATETIME,
			read_by TEXT,
			remarks TEXT,
			recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			recorded_by TEXT
		);
		CREATE TABLE maintenance_plans (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			plan_number TEXT NOT NULL,
			vehicle_id TEXT NOT NULL REFERENCES vehicles(id),
			measuring_point_id TEXT REFERENCES vehicle_measuring_points(id),
			service_type TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			cycle_interval_km REAL,
			cycle_interval_days INTEGER,
			call_horizon_percent REAL NOT NULL DEFAULT 100.0,
			last_scheduled_km REAL,
			last_scheduled_date DATETIME,
			next_due_km REAL,
			next_due_date DATETIME,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tenant_id, plan_number)
		);
		CREATE TABLE work_orders (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_id TEXT NOT NULL,
			schedule_id TEXT,
			trip_id TEXT,
			plan_id TEXT REFERENCES maintenance_plans(id),
			due_km REAL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			assignee TEXT NOT NULL DEFAULT '',
			vendor TEXT NOT NULL DEFAULT '',
			cost_estimate REAL,
			cost_actual REAL,
			status TEXT NOT NULL DEFAULT 'open',
			due_at DATETIME,
			closed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		INSERT INTO tenants (id, name) VALUES ('tenant-A', 'Org A'), ('tenant-B', 'Org B');
		INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, odometer)
		VALUES ('veh-1', 'tenant-A', 'V-01', 'KA01TR1111', 19500.0);
		INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, annual_estimate)
		VALUES ('mp-1', 'tenant-A', 'veh-1', 'ODO', 73000.0);
	`)
	require.NoError(t, err)
	return db
}

func TestMaintenancePlansAPI(t *testing.T) {
	db := setupMaintenancePlansHandlerDB(t)
	app := &App{DB: db}
	handlers := NewMaintenanceHandlers(app, db)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			tID := req.Header.Get("X-Tenant-ID")
			if tID == "" {
				tID = "tenant-A"
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tID))
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})

	r.Post("/api/v1/maintenance/plans", handlers.APICreateMaintenancePlan)
	r.Get("/api/v1/maintenance/plans", handlers.APIListMaintenancePlans)
	r.Get("/api/v1/maintenance/plans/{id}", handlers.APIGetMaintenancePlan)
	r.Post("/api/v1/maintenance/plans/{id}/evaluate", handlers.APIEvaluateMaintenancePlan)

	// 1. Create maintenance plan (IP41)
	intervalKM := 20000.0
	nextDueKM := 20000.0
	mpID := "mp-1"
	createReq := CreateMaintenancePlanRequest{
		PlanNumber:         "MP-KA01-01",
		VehicleID:          "veh-1",
		MeasuringPointID:   &mpID,
		ServiceType:        "oil_change",
		Description:        "Full synthetic service",
		CycleIntervalKM:    &intervalKM,
		NextDueKM:          &nextDueKM,
		CallHorizonPercent: 90.0, // 18,000 km call threshold
	}
	body, _ := json.Marshal(createReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/maintenance/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var created domain.MaintenancePlan
	err := json.Unmarshal(w.Body.Bytes(), &created)
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "tenant-A", created.TenantID)
	assert.Equal(t, "MP-KA01-01", created.PlanNumber)

	// 2. List maintenance plans
	req = httptest.NewRequest(http.MethodGet, "/api/v1/maintenance/plans", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var listResp struct {
		Plans []domain.MaintenancePlan `json:"maintenance_plans"`
		Count int                      `json:"count"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &listResp)
	require.NoError(t, err)
	assert.Equal(t, 1, listResp.Count)
	assert.Equal(t, created.ID, listResp.Plans[0].ID)

	// 3. Get single plan with live projections
	req = httptest.NewRequest(http.MethodGet, "/api/v1/maintenance/plans/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var getResp struct {
		Plan           domain.MaintenancePlan `json:"maintenance_plan"`
		CurrentOdo     float64                `json:"current_odometer"`
		AnnualEstimate float64                `json:"annual_estimate"`
		CallDue        bool                   `json:"call_horizon_due"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &getResp)
	require.NoError(t, err)
	assert.Equal(t, 19500.0, getResp.CurrentOdo)
	assert.Equal(t, 73000.0, getResp.AnnualEstimate)
	assert.True(t, getResp.CallDue, "at 19,500 km, call threshold is reached")

	// 4. Evaluate plan -> generates WorkOrder (Job Card)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/maintenance/plans/"+created.ID+"/evaluate", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var evalResp struct {
		Status        string            `json:"status"`
		CallTriggered bool              `json:"call_triggered"`
		WorkOrder     *domain.WorkOrder `json:"work_order"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &evalResp)
	require.NoError(t, err)
	assert.True(t, evalResp.CallTriggered)
	require.NotNil(t, evalResp.WorkOrder)
	assert.Equal(t, domain.WorkOrderOpen, evalResp.WorkOrder.Status)
	assert.Equal(t, created.ID, *evalResp.WorkOrder.PlanID)

	// 5. Cross-tenant isolation check: Tenant-B gets 404
	req = httptest.NewRequest(http.MethodGet, "/api/v1/maintenance/plans/"+created.ID, nil)
	req.Header.Set("X-Tenant-ID", "tenant-B")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
