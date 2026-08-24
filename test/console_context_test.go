package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

// seedFleetContextFixtures seeds tenant-a vehicle v-a with position, active
// trip (with route + driver), pending kharcha, active e-way bill, FASTag
// balance and expiring docs; tenant-b gets an isolated vehicle.
func seedFleetContextFixtures(t *testing.T, db *sql.DB, day time.Time) {
	t.Helper()
	must := func(query string, args ...any) {
		_, err := db.Exec(query, args...)
		require.NoError(t, err, "seed failed: "+query)
	}
	dayStr := day.Format("2006-01-02 15:04:05")

	must(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id, created_at, updated_at)
	      VALUES ('v-a','GJ01EF','GJ01EF','truck',1,?,?,?,'running','tenant-a',?,?)`,
		day.Add(90*24*time.Hour).Format("2006-01-02"), day.Add(90*24*time.Hour).Format("2006-01-02"), day.Add(90*24*time.Hour).Format("2006-01-02"), dayStr, dayStr)
	must(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id, created_at, updated_at)
	      VALUES ('v-b','MH12AB','MH12AB','truck',1,?,?,?,'available','tenant-b',?,?)`,
		day.Add(90*24*time.Hour).Format("2006-01-02"), day.Add(90*24*time.Hour).Format("2006-01-02"), day.Add(90*24*time.Hour).Format("2006-01-02"), dayStr, dayStr)

	must(`INSERT INTO vehicle_latest_position
	      (vehicle_id, tenant_id, imei, device_time, latitude, longitude, speed)
	      VALUES ('v-a','tenant-a','imei-1',?,22.3039,73.2043,48.5)`, dayStr)

	must(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id, created_at, updated_at)
	      VALUES ('r-1','Pune','Nagpur',700,12,20000,'tenant-a',?,?)`, dayStr, dayStr)
	must(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id, created_at, updated_at)
	      VALUES ('drv-1','D001','Ravi','K','+91-9876543210','DL-1',?,'on_trip','tenant-a',?,?)`,
		day.Add(365*24*time.Hour).Format("2006-01-02"), dayStr, dayStr)
	must(`INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, estimated_margin, tenant_id, created_at, updated_at)
	      VALUES ('tr-1','TRP1','drv-1','v-a','r-1',?,'in_transit',6300,'tenant-a',?,?)`, dayStr, dayStr, dayStr)

	must(`INSERT INTO driver_expenses (id, trip_id, driver_id, expense_type, category, amount, status, tenant_id, created_at)
	      VALUES ('de-p1','tr-1','drv-1','fuel','fuel',1250,'pending','tenant-a',?)`, dayStr)

	must(`INSERT INTO eway_bills (id, trip_id, ewb_number, generation_date, valid_until, status)
	      VALUES ('ewb-1','tr-1','111000',?,?,'active')`,
		day.Add(-6*time.Hour).Format(dayStrFmt()), day.Add(4*time.Hour).Format(dayStrFmt()))

	must(`INSERT INTO fastag_tags (id, tenant_id, vehicle_id, tag_id, balance, status)
	      VALUES ('tag-1','tenant-a','v-a','TAG001',340,'ACTIVE')`)

	must(`INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, expiry_date, status)
	      VALUES ('vd-1','v-a','insurance','/files/ins.pdf',?,'verified')`,
		day.Add(12*24*time.Hour).Format("2006-01-02"))
}

func dayStrFmt() string { return "2006-01-02 15:04:05" }

// TestSpec22_VehicleContext — Spec 22 §7 S3: every sub-object comes from its
// real table; unknown vehicle and cross-tenant lookups return 404.
func TestSpec22_VehicleContext(t *testing.T) {
	db := NewTestDB(t)
	day := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	seedFleetContextFixtures(t, db, day)

	console := handlers.NewConsoleHandlers(&handlers.App{}, nil, nil, db, nil, nil)

	r := chi.NewRouter()
	r.With(tenantMW("tenant-a")).Get("/api/fleet/{vehicleId}/context", console.VehicleContext)
	r.With(tenantMW("tenant-b")).Get("/b/api/fleet/{vehicleId}/context", console.VehicleContext)
	r.With(tenantMW("tenant-a")).Get("/api/fleet", console.Fleet)

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("/api/fleet/v-a/context")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var ctxResp struct {
		Vehicle struct {
			ID     string `json:"id"`
			Number string `json:"number"`
			Status string `json:"status"`
		} `json:"vehicle"`
		Position *struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		} `json:"position"`
		Trip *struct {
			ID    string `json:"id"`
			Route string `json:"route"`
		} `json:"trip"`
		Driver *struct {
			Name  string  `json:"name"`
			Phone *string `json:"phone"`
		} `json:"driver"`
		PnlKmToday     float64 `json:"pnl_km_today"`
		KharchaPending []struct {
			ID       string  `json:"id"`
			Amount   float64 `json:"amount"`
			Category string  `json:"category"`
		} `json:"kharcha_pending"`
		EwayBill *struct {
			ID        string `json:"id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"eway_bill"`
		FastagBalance *float64 `json:"fastag_balance"`
		DocsExpiring  []struct {
			Kind      string `json:"kind"`
			ExpiresOn string `json:"expires_on"`
		} `json:"docs_expiring"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ctxResp))

	assert.Equal(t, "GJ01EF", ctxResp.Vehicle.Number)
	require.NotNil(t, ctxResp.Position)
	assert.InDelta(t, 22.3039, ctxResp.Position.Lat, 0.001)
	require.NotNil(t, ctxResp.Trip)
	assert.Equal(t, "Pune→Nagpur", ctxResp.Trip.Route)
	require.NotNil(t, ctxResp.Driver)
	assert.Equal(t, "Ravi K", ctxResp.Driver.Name)
	require.NotNil(t, ctxResp.Driver.Phone)
	assert.InDelta(t, 9.0, ctxResp.PnlKmToday, 0.01, "margin 6300 / 700 km")
	require.Len(t, ctxResp.KharchaPending, 1)
	assert.Equal(t, 1250.0, ctxResp.KharchaPending[0].Amount)
	require.NotNil(t, ctxResp.EwayBill)
	assert.Equal(t, "ewb-1", ctxResp.EwayBill.ID)
	require.NotNil(t, ctxResp.FastagBalance)
	assert.InDelta(t, 340.0, *ctxResp.FastagBalance, 0.001)
	require.Len(t, ctxResp.DocsExpiring, 1)
	assert.Equal(t, "insurance", ctxResp.DocsExpiring[0].Kind)

	// Unknown vehicle → 404.
	assert.Equal(t, http.StatusNotFound, get("/api/fleet/v-none/context").Code)

	// Tenant miss: tenant-b asking for tenant-a's vehicle → 404.
	assert.Equal(t, http.StatusNotFound, get("/b/api/fleet/v-a/context").Code)

	// Fleet strip is tenant-scoped.
	fw := get("/api/fleet")
	require.Equal(t, http.StatusOK, fw.Code)
	var fleet struct {
		Vehicles []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"vehicles"`
	}
	require.NoError(t, json.Unmarshal(fw.Body.Bytes(), &fleet))
	require.Len(t, fleet.Vehicles, 1)
	assert.Equal(t, "v-a", fleet.Vehicles[0].ID)
}

func tenantMW(tenant string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(
				context.WithValue(r.Context(), shared.TenantIDKey, shared.TenantID(tenant))))
		})
	}
}
