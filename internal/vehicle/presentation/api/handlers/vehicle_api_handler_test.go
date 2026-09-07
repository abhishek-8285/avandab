package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/vehicle/application"
)

const apiTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL UNIQUE,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL,
    capacity INTEGER NOT NULL,
    fuel_type TEXT NOT NULL,
    insurance_expiry DATETIME NOT NULL,
    fitness_expiry DATETIME NOT NULL,
    permit_expiry DATETIME NOT NULL,
    status TEXT NOT NULL,
    current_mileage REAL,
    tenant_id TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    fleet_class TEXT NOT NULL DEFAULT 'CV',
    ownership TEXT NOT NULL DEFAULT 'O',
    fleet_number TEXT,
    description TEXT,
    manufacturer TEXT,
    manuf_country TEXT,
    model TEXT,
    constr_year_month TEXT,
    acquisition_value REAL,
    acquisition_currency TEXT NOT NULL DEFAULT 'INR',
    acquisition_date DATETIME,
    purchase_vendor TEXT,
    valid_from DATETIME,
    valid_to DATETIME,
    facility_id TEXT,
    maint_plant TEXT,
    planning_plant TEXT,
    company_code TEXT,
    business_area TEXT,
    cost_center TEXT,
    asset_no TEXT,
    fleet_object_no TEXT,
    chassis_no TEXT,
    vehicle_category TEXT,
    engine_number TEXT,
    engine_power TEXT,
    engine_capacity TEXT,
    cylinder_count INTEGER,
    max_speed REAL,
    weight REAL,
    weight_unit TEXT NOT NULL DEFAULT 'TO',
    load_volume REAL,
    volume_unit TEXT,
    secondary_fuel TEXT,
    usage_indicator TEXT
);
CREATE TABLE vehicle_measuring_points (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT 'M', kind TEXT NOT NULL,
  meas_position TEXT NOT NULL DEFAULT 'DISTANCE', unit TEXT NOT NULL DEFAULT 'KM',
  decimal_places INTEGER NOT NULL DEFAULT 0, annual_estimate REAL,
  count_backwards INTEGER NOT NULL DEFAULT 0, is_counter INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE vehicle_measurements (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, point_id TEXT NOT NULL,
  doc_number TEXT, counter_reading REAL NOT NULL, difference_reading REAL,
  total_counter_reading REAL, measured_at DATETIME, read_by TEXT, remarks TEXT,
  recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP, recorded_by TEXT
);
CREATE TABLE trips (
  id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '1',
  vehicle_id TEXT, status TEXT NOT NULL DEFAULT 'draft'
);
CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY, aggregate_id TEXT NOT NULL, aggregate_type TEXT NOT NULL,
    event_type TEXT NOT NULL, payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL, published_at DATETIME
);
`

type stubAuthSvc struct{}

func (stubAuthSvc) Can(uid, resource, action string) bool { return true }
func (stubAuthSvc) Reload() error                         { return nil }
func (stubAuthSvc) AddRoleForUser(uid, role string) error { return nil }
func (stubAuthSvc) DeleteRolesForUser(uid string) error   { return nil }

var _ auth.AuthorizationService = stubAuthSvc{}

func newAPITestHandler(t *testing.T, dbConn *sql.DB) *APIVehicleHandler {
	t.Helper()
	unitOfWork := uow.NewSQLUnitOfWork(dbConn)
	clockImpl := clock.NewRealClock()
	idGenImpl := id.NewUUIDGenerator()
	return NewAPIVehicleHandler(
		application.NewCreateVehicleUseCase(unitOfWork, idGenImpl, clockImpl),
		application.NewUpdateVehicleUseCase(unitOfWork, clockImpl),
		application.NewDeleteVehicleUseCase(unitOfWork, clockImpl),
		application.NewGetVehicleUseCase(unitOfWork),
		application.NewListVehiclesUseCase(unitOfWork),
		application.NewCreateMeasuringPointUseCase(unitOfWork, idGenImpl),
		application.NewRecordMeasurementUseCase(unitOfWork, idGenImpl, clockImpl),
		application.NewListVehicleMeasurements(unitOfWork),
		stubAuthSvc{},
	)
}

func newAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	dbConn, err := sql.Open("sqlite", "file:"+safeName+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(apiTestSchema)
	require.NoError(t, err)
	return dbConn
}

func apiRequest(t *testing.T, h http.HandlerFunc, method, target string, body any, params map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("t1"))
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	}
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func futureDateStr() string { return time.Now().AddDate(1, 0, 0).Format("2006-01-02") }

func TestVehicleAPI_CRUD(t *testing.T) {
	h := newAPITestHandler(t, newAPITestDB(t))

	// Create with SOP profile.
	w := apiRequest(t, h.Create, http.MethodPost, "/api/v1/vehicles", map[string]any{
		"registration_number": "KA30P1234", "vehicle_number": "V-API",
		"vehicle_type": "truck", "capacity": 10000, "fuel_type": "diesel",
		"insurance_expiry": futureDateStr(), "fitness_expiry": futureDateStr(), "permit_expiry": futureDateStr(),
		"profile": map[string]any{"fleet_class": "CV", "ownership": "O", "manufacturer": "TATA", "facility_id": "MM21000000757"},
	}, nil)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created["id"])
	vid := created["id"]

	// Create rejects bad dates and bad fleet class.
	w = apiRequest(t, h.Create, http.MethodPost, "/api/v1/vehicles", map[string]any{
		"registration_number": "KA99XX9999", "vehicle_number": "V-BAD",
		"insurance_expiry": "not-a-date", "fitness_expiry": futureDateStr(), "permit_expiry": futureDateStr(),
	}, nil)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Get round-trips the profile.
	w = apiRequest(t, h.Get, http.MethodGet, "/api/v1/vehicles/"+vid, nil, map[string]string{"id": vid})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "KA30P1234", got["registration_number"])
	require.Equal(t, "TATA", got["profile"].(map[string]any)["manufacturer"])

	// Get missing → 404.
	w = apiRequest(t, h.Get, http.MethodGet, "/api/v1/vehicles/nope", nil, map[string]string{"id": "nope"})
	require.Equal(t, http.StatusNotFound, w.Code)

	// List with fleet_class filter.
	w = apiRequest(t, h.List, http.MethodGet, "/api/v1/vehicles?fleet_class=CV", nil, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, float64(1), list["total"])

	w = apiRequest(t, h.List, http.MethodGet, "/api/v1/vehicles?fleet_class=FS", nil, nil)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, float64(0), list["total"])

	// Partial PUT preserves profile.
	w = apiRequest(t, h.Update, http.MethodPut, "/api/v1/vehicles/"+vid, map[string]any{
		"vehicle_number": "V-API-2",
	}, map[string]string{"id": vid})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = apiRequest(t, h.Get, http.MethodGet, "/api/v1/vehicles/"+vid, nil, map[string]string{"id": vid})
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "V-API-2", got["vehicle_number"])
	require.Equal(t, "TATA", got["profile"].(map[string]any)["manufacturer"], "partial PUT must preserve profile")
}

func TestVehicleAPI_MeasuringFlow(t *testing.T) {
	h := newAPITestHandler(t, newAPITestDB(t))

	w := apiRequest(t, h.Create, http.MethodPost, "/api/v1/vehicles", map[string]any{
		"registration_number": "KA01KA0123", "vehicle_number": "V-M",
		"vehicle_type": "truck", "fuel_type": "diesel",
		"insurance_expiry": futureDateStr(), "fitness_expiry": futureDateStr(), "permit_expiry": futureDateStr(),
	}, nil)
	require.Equal(t, http.StatusCreated, w.Code)
	var created map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// IK01 point.
	w = apiRequest(t, h.CreatePoint, http.MethodPost, "/api/v1/vehicles/"+created["id"]+"/points", map[string]any{
		"kind": "ODO", "annual_estimate": 50000, "description": "Tata Truck KA01KA0123",
	}, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var point map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &point))

	// Bad kind rejected.
	w = apiRequest(t, h.CreatePoint, http.MethodPost, "/api/v1/vehicles/"+created["id"]+"/points", map[string]any{
		"kind": "NOPE",
	}, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// IK11 document: first difference = counter.
	w = apiRequest(t, h.RecordMeasurement, http.MethodPost, "/api/v1/vehicles/"+created["id"]+"/measurements", map[string]any{
		"point_id": point["id"], "counter_reading": 1200, "read_by": "TCS795488",
	}, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var doc map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, float64(1200), doc["counter_reading"])
	assert.Equal(t, float64(1200), doc["difference_reading"])

	// Backwards counter rejected.
	w = apiRequest(t, h.RecordMeasurement, http.MethodPost, "/api/v1/vehicles/"+created["id"]+"/measurements", map[string]any{
		"point_id": point["id"], "counter_reading": 100,
	}, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Journal lists the document.
	w = apiRequest(t, h.ListMeasurements, http.MethodGet, "/api/v1/vehicles/"+created["id"]+"/measurements", nil, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var docs []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &docs))
	require.Len(t, docs, 1)
}

func TestVehicleAPI_DeleteGuard(t *testing.T) {
	dbConn := newAPITestDB(t)
	h := newAPITestHandler(t, dbConn)

	w := apiRequest(t, h.Create, http.MethodPost, "/api/v1/vehicles", map[string]any{
		"registration_number": "KA02AA0123", "vehicle_number": "V-D",
		"vehicle_type": "truck", "fuel_type": "diesel",
		"insurance_expiry": futureDateStr(), "fitness_expiry": futureDateStr(), "permit_expiry": futureDateStr(),
	}, nil)
	require.Equal(t, http.StatusCreated, w.Code)
	var created map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Live trip blocks the delete.
	_, err := dbConn.Exec(`INSERT INTO trips (id, tenant_id, vehicle_id, status) VALUES ('t1','t1',?, 'assigned')`, created["id"])
	require.NoError(t, err)
	w = apiRequest(t, h.Delete, http.MethodDelete, "/api/v1/vehicles/"+created["id"], nil, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "live trip")

	// Terminal trip no longer blocks.
	_, err = dbConn.Exec(`UPDATE trips SET status = 'completed' WHERE id = 't1'`)
	require.NoError(t, err)
	w = apiRequest(t, h.Delete, http.MethodDelete, "/api/v1/vehicles/"+created["id"], nil, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Gone afterwards.
	w = apiRequest(t, h.Get, http.MethodGet, "/api/v1/vehicles/"+created["id"], nil, map[string]string{"id": created["id"]})
	require.Equal(t, http.StatusNotFound, w.Code)

	// Deleting unknown id → 404.
	w = apiRequest(t, h.Delete, http.MethodDelete, "/api/v1/vehicles/nope", nil, map[string]string{"id": "nope"})
	require.Equal(t, http.StatusNotFound, w.Code)
}
