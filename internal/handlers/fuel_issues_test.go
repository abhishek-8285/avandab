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

	"transport-app/internal/shared"
)

func setupFuelIssueHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:fuel_api_%d?mode=memory&cache=shared", time.Now().UnixNano()))
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
			blocked INTEGER NOT NULL DEFAULT 0,
			blocked_reason TEXT,
			valid_to DATETIME,
			odometer REAL NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE vehicle_measuring_points (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
			unit TEXT NOT NULL DEFAULT 'KM',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
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
			recorded_at DATETIME NOT NULL DEFAULT (datetime('now')),
			recorded_by TEXT
		);
		CREATE TABLE fuel_issues (
			id                TEXT PRIMARY KEY,
			tenant_id         TEXT NOT NULL,
			issue_number      TEXT,
			fuel_station_id   TEXT NOT NULL,
			pump_point_id     TEXT,
			vehicle_id        TEXT NOT NULL,
			driver_id         TEXT,
			trip_id           TEXT,
			fuel_type         TEXT NOT NULL DEFAULT 'diesel',
			opening_reading   REAL NOT NULL DEFAULT 0,
			closing_reading   REAL NOT NULL DEFAULT 0,
			litres_issued     REAL NOT NULL CHECK (litres_issued > 0),
			vehicle_odometer  REAL,
			rate_per_litre    REAL,
			total_cost        REAL,
			remarks           TEXT NOT NULL DEFAULT '',
			issued_at         DATETIME NOT NULL DEFAULT (datetime('now')),
			created_by        TEXT NOT NULL DEFAULT '',
			created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Org 1');
		INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class)
		VALUES ('fs-1', 'tenant-1', 'FS-01', 'REG-FS-01', 'FS'),
		       ('tr-1', 'tenant-1', 'TR-01', 'REG-TR-01', 'CV');
	`)
	require.NoError(t, err)
	return db
}

func TestFuelIssuesAPI_Endpoints(t *testing.T) {
	db := setupFuelIssueHandlerDB(t)
	authSvc := allowAuthSvc{}

	app := &App{
		DB:      db,
		AuthSrv: authSvc,
	}
	fa := &FuelAuditHandlers{App: app}

	r := chi.NewRouter()
	fa.RegisterFuelIssueAPIRoutes(r)

	// 1. POST /api/v1/fuel-issues
	body := map[string]interface{}{
		"fuel_station_id":  "fs-1",
		"vehicle_id":       "tr-1",
		"opening_reading":  1500.0,
		"closing_reading":  1650.0,
		"vehicle_odometer": 84200.0,
		"rate_per_litre":   95.0,
		"remarks":          "Depot dispensing",
	}
	b, _ := json.Marshal(body)

	req := withSession(httptest.NewRequest(http.MethodPost, "/api/v1/fuel-issues", bytes.NewReader(b)), "user-1", "admin")
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "tenant-1"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var created map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&created)
	require.NoError(t, err)
	assert.Equal(t, 150.0, created["litres_issued"])
	assert.Equal(t, 14250.0, created["total_cost"])
	createdID := created["id"].(string)

	// 2. GET /api/v1/fuel-issues
	reqList := withSession(httptest.NewRequest(http.MethodGet, "/api/v1/fuel-issues", nil), "user-1", "admin")
	reqList = reqList.WithContext(shared.ContextWithTenantID(reqList.Context(), "tenant-1"))
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)

	require.Equal(t, http.StatusOK, wList.Code)
	var listResp map[string]interface{}
	err = json.NewDecoder(wList.Body).Decode(&listResp)
	require.NoError(t, err)
	assert.Equal(t, float64(1), listResp["count"])

	// 3. GET /api/v1/fuel-issues/{id}
	reqGet := withSession(httptest.NewRequest(http.MethodGet, "/api/v1/fuel-issues/"+createdID, nil), "user-1", "admin")
	reqGet = reqGet.WithContext(shared.ContextWithTenantID(reqGet.Context(), "tenant-1"))
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)

	require.Equal(t, http.StatusOK, wGet.Code)
	var got map[string]interface{}
	err = json.NewDecoder(wGet.Body).Decode(&got)
	require.NoError(t, err)
	assert.Equal(t, createdID, got["id"])
}

func TestFuelIssuesAPI_RBAC(t *testing.T) {
	db := setupFuelIssueHandlerDB(t)
	authSvc := denyAuthSvc{}

	app := &App{
		DB:      db,
		AuthSrv: authSvc,
	}
	fa := &FuelAuditHandlers{App: app}

	r := chi.NewRouter()
	fa.RegisterFuelIssueAPIRoutes(r)

	req := withSession(httptest.NewRequest(http.MethodGet, "/api/v1/fuel-issues", nil), "viewer-1", "viewer")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "tenant-1"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
