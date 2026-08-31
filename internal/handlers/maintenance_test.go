package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"transport-app/internal/config"
	"transport-app/internal/maintenance"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type maintAllowAuthSvc struct{}

func (maintAllowAuthSvc) Can(userID, resource, action string) bool { return true }
func (maintAllowAuthSvc) Reload() error                            { return nil }
func (maintAllowAuthSvc) AddRoleForUser(userID, role string) error { return nil }
func (maintAllowAuthSvc) DeleteRolesForUser(userID string) error   { return nil }

type maintDenyAuthSvc struct{}

func (maintDenyAuthSvc) Can(userID, resource, action string) bool { return false }
func (maintDenyAuthSvc) Reload() error                            { return nil }
func (maintDenyAuthSvc) AddRoleForUser(userID, role string) error { return nil }
func (maintDenyAuthSvc) DeleteRolesForUser(userID string) error   { return nil }

func newMaintHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_maint_h_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newMaintHandlerApp(t *testing.T, db *sql.DB, authSrv auth.AuthorizationService) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	tmpl, err := parseTemplates(authSrv)
	require.NoError(t, err)

	cfg := &config.Config{
		CookieSecret: "test-cookie-secret-32-chars-long!",
	}

	app := &App{
		DB:        db,
		Templates: tmpl,
		AuthSrv:   authSrv,
		Config:    cfg,
	}
	app.Maintenance = NewMaintenanceHandlers(app, db)
	worker := maintenance.NewWorker(db, nil, nil, 15, "P0A0F,P1602")
	app.Maintenance.SetWorker(worker)
	return app
}

func insertMaintTestVehicle(t *testing.T, db *sql.DB, id, reg string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES (?, ?, ?, 'truck', 15, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), '1')`,
		id, reg, "MH-01-"+reg)
	require.NoError(t, err)
}

func TestMaintenance_Dashboard_Renders(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-dash", "REG-DASH")

	// Set maintenance due on vehicle
	_, err := db.Exec(`UPDATE vehicles SET maintenance_due = '2026-08-19' WHERE id = 'veh-dash'`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Get("/maintenance", app.Maintenance.Index)

	req := withSession(httptest.NewRequest("GET", "/maintenance", nil), "user-1", "admin")
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), shared.DefaultTenant))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Preventive Maintenance")
	assert.Contains(t, w.Body.String(), "veh-dash")
}

func TestMaintenance_Schedule_Creation_And_Evaluation(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-sched", "REG-SCHED")

	// Put high mileage in telemetry snapshots so schedule triggers immediately
	_, err := db.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp, latitude, longitude, speed, odometer)
		VALUES ('snap-sched', 'veh-sched', CURRENT_TIMESTAMP, 19.0, 72.8, 50.0, 60000.0)`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Post("/maintenance/schedules", app.Maintenance.CreateSchedule)

	form := url.Values{
		"vehicle_id":   {"veh-sched"},
		"service_type": {"oil_change"},
		"due_km":       {"50000"},
		"active":       {"1"},
	}
	req := withSession(httptest.NewRequest("POST", "/maintenance/schedules", strings.NewReader(form.Encode())), "user-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)

	// Verify vehicle maintenance_due was set because odometer (60000) >= due_km (50000)
	var due sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-sched'").Scan(&due)
	require.NoError(t, err)
	assert.True(t, due.Valid)
}

func TestMaintenance_Record_Creation_And_Resolution(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-rec", "REG-REC")

	// Set maintenance due
	_, err := db.Exec(`UPDATE vehicles SET maintenance_due = '2026-08-19' WHERE id = 'veh-rec'`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Post("/maintenance/records", app.Maintenance.CreateRecord)

	form := url.Values{
		"vehicle_id":   {"veh-rec"},
		"service_type": {"general"},
		"performed_at": {time.Now().UTC().Format("2006-01-02")},
		"odometer_km":  {"55000"},
		"cost":         {"4500"},
		"vendor":       {"Tata Service Center"},
		"notes":        {"Replaced oil and filters"},
	}
	req := withSession(httptest.NewRequest("POST", "/maintenance/records", strings.NewReader(form.Encode())), "user-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)

	// Verify maintenance_record was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM maintenance_records WHERE vehicle_id = 'veh-rec'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify resolution check cleared the due flag
	var due sql.NullString
	err = db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-rec'").Scan(&due)
	require.NoError(t, err)
	assert.False(t, due.Valid)
}

func TestMaintenance_Vehicle_Override(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-ov", "REG-OV")

	// Set maintenance due
	_, err := db.Exec(`UPDATE vehicles SET maintenance_due = '2026-08-19' WHERE id = 'veh-ov'`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Post("/maintenance/vehicles/{id}/override", app.Maintenance.OverrideBlock)

	form := url.Values{
		"reason": {"Urgent long-haul trip approved by operations manager"},
	}
	req := withSession(httptest.NewRequest("POST", "/maintenance/vehicles/veh-ov/override", strings.NewReader(form.Encode())), "admin-1", "admin")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)

	// Verify override columns set, but maintenance_due flag remains set (Gotcha #6)
	var maintDue, ovBy, ovReason sql.NullString
	err = db.QueryRow("SELECT maintenance_due, maintenance_override_by, maintenance_override_reason FROM vehicles WHERE id = 'veh-ov'").Scan(&maintDue, &ovBy, &ovReason)
	require.NoError(t, err)
	assert.True(t, maintDue.Valid) // Flag remains set!
	assert.Equal(t, "admin-1", ovBy.String)
	assert.Equal(t, "Urgent long-haul trip approved by operations manager", ovReason.String)

	// Verify IsMaintenanceBlocked returns false because override is active
	repo := maintsql.NewMaintenanceRepository(db)
	blocked, _, err := repo.IsMaintenanceBlocked(context.Background(), "veh-ov")
	require.NoError(t, err)
	assert.False(t, blocked) // Block lifted!
}

func TestMaintenance_RBAC_Denial(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintDenyAuthSvc{})

	r := chi.NewRouter()
	r.With(middleware.ResourcePermission(app.AuthSrv, "maintenance", "read")).Get("/maintenance", app.Maintenance.Index)

	req := withSession(httptest.NewRequest("GET", "/maintenance", nil), "viewer-1", "viewer")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
