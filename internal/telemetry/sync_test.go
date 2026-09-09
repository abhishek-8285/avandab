package telemetry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// newTestRouter builds a chi router with the telemetry routes wired to a
// real Ingestor over a migrated in-memory DB.
func newTestRouter(t *testing.T) (chi.Router, *Ingestor) {
	t.Helper()
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)
	return r, ing
}

// newTestRouterWithDevice registers an active device + vehicle and returns
// the router plus the device IMEI and vehicle ID.
func newTestRouterWithDevice(t *testing.T) (chi.Router, string, string) {
	t.Helper()
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	vID := "vh-sync-1"
	insertTestVehicle(t, db, vID)
	imei := "IMEI-SYNC-1"
	insertTestDevice(t, db, imei, DeviceStatusActive, &vID)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)
	return r, imei, vID
}

func TestHandleTelemetrySync_Success(t *testing.T) {
	r, imei, _ := newTestRouterWithDevice(t)

	reqPayload := SyncBatchRequest{
		DeviceID: imei,
		Logs: []GPSLogPayload{
			{ID: 1, Latitude: 19.076, Longitude: 72.877, Timestamp: "2026-08-13T00:00:00Z"},
			{ID: 2, Latitude: 19.080, Longitude: 72.880, Timestamp: "2026-08-13T00:01:00Z"},
		},
	}

	body, _ := json.Marshal(reqPayload)
	req := syncReqWithTenant("POST", "/api/v1/telemetry/sync", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SyncBatchResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if !resp.Success || resp.SyncedCount != 2 {
		t.Fatalf("expected synced_count=2, got %d", resp.SyncedCount)
	}
	// Real synced_ids returned (not echoed)
	assert.Equal(t, []int64{1, 2}, resp.SyncedIDs)
}

func TestHandleTelemetrySync_InvalidBody(t *testing.T) {
	r, _ := newTestRouter(t)

	req := httptest.NewRequest("POST", "/api/v1/telemetry/sync", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid body, got %d", w.Code)
	}
}

func TestHandleTelemetrySnapshots_SuccessAndInvalid(t *testing.T) {
	r, _, vID := newTestRouterWithDevice(t)

	// Success: a real vehicle with a registered device → pipeline accepts.
	snap := TelemetrySnapshotPayload{
		TripID:    "trp-701",
		VehicleID: vID,
		Timestamp: "2026-08-13T00:15:00Z",
		Latitude:  19.076,
		Longitude: 72.877,
		Speed:     60.0,
		FuelLevel: 50.0,
		Odometer:  1000.0,
	}
	body, _ := json.Marshal(snap)
	req := syncReqWithTenant("POST", "/api/v1/telemetry/snapshots", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid: missing vehicle_id
	reqBad := httptest.NewRequest("POST", "/api/v1/telemetry/snapshots", bytes.NewReader([]byte("{}")))
	wBad := httptest.NewRecorder()

	r.ServeHTTP(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for missing trip_id, got %d", wBad.Code)
	}
}

// TestHandleTelemetrySync_ProviderParityFields (migration 00117): mobile sends
// speed/heading/battery/satellites/motion; they must reach telemetry_positions.
// Older app versions omit them — the NULL columns prove back-compat.
func TestHandleTelemetrySync_ProviderParityFields(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	vID := "vh-sync-par"
	insertTestVehicle(t, db, vID)
	imei := "IMEI-SYNC-PAR"
	insertTestDevice(t, db, imei, DeviceStatusActive, &vID)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	batt := 64.0
	sats := 11
	moving := true
	reqPayload := SyncBatchRequest{
		DeviceID: imei,
		Logs: []GPSLogPayload{
			{ID: 10, Latitude: 19.07, Longitude: 72.87, Timestamp: "2026-08-31T13:30:15Z",
				Speed: 52.5, Heading: 240, BatteryLevel: &batt, Satellites: &sats, Motion: &moving},
			{ID: 11, Latitude: 19.08, Longitude: 72.88, Timestamp: "2026-08-31T13:30:25Z"},
		},
	}
	body, _ := json.Marshal(reqPayload)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, syncReqWithTenant("POST", "/api/v1/telemetry/sync", body))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp SyncBatchResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.SyncedCount)

	// Rich log: parity fields persisted.
	var speed, heading, battery float64
	var sat int
	var motion bool
	require.NoError(t, db.QueryRow(`SELECT p.speed, p.heading, p.battery_level, p.satellites, p.motion
		FROM telemetry_positions p JOIN telemetry_raw_events e ON e.id = p.raw_event_id
		WHERE e.provider_msg_id = 'sync:10'`).Scan(&speed, &heading, &battery, &sat, &motion))
	assert.InDelta(t, 52.5, speed, 0.01)
	assert.InDelta(t, 240, heading, 0.01)
	assert.InDelta(t, 64.0, battery, 0.01)
	assert.Equal(t, 11, sat)
	assert.True(t, motion)

	// Bare log (old app shape): parity columns NULL, position still accepted.
	var nNull int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions p
		JOIN telemetry_raw_events e ON e.id = p.raw_event_id
		WHERE e.provider_msg_id = 'sync:11' AND p.battery_level IS NULL AND p.speed = 0`).Scan(&nNull))
	assert.Equal(t, 1, nNull)
}

func TestHandleTelemetrySync_DistinctIDsNoCollapse(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	vID := "vh-sync-collapse"
	insertTestVehicle(t, db, vID)
	imei := "IMEI-SYNC-COLLAPSE"
	insertTestDevice(t, db, imei, DeviceStatusActive, &vID)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	// Regression: the mobile client once omitted per-log ids, so every frame
	// shared provider_msg_id "sync:0" and N distinct fixes collapsed to one
	// stored position. Distinct ids must yield distinct positions and an
	// exact synced_ids ack for client-side reconciliation.
	logs := []GPSLogPayload{
		{ID: 11, Latitude: 19.076, Longitude: 72.877, Timestamp: "2026-08-13T00:00:00Z"},
		{ID: 12, Latitude: 19.080, Longitude: 72.880, Timestamp: "2026-08-13T00:01:00Z"},
		{ID: 13, Latitude: 19.084, Longitude: 72.883, Timestamp: "2026-08-13T00:02:00Z"},
	}
	body, _ := json.Marshal(SyncBatchRequest{DeviceID: imei, Logs: logs})
	req := syncReqWithTenant(http.MethodPost, "/api/v1/telemetry/sync", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp SyncBatchResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.True(t, resp.Success)
	assert.ElementsMatch(t, []int64{11, 12, 13}, resp.SyncedIDs)

	var positions int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, imei).Scan(&positions))
	assert.Equal(t, 3, positions, "distinct log ids must not dedup-collapse into one position")
}

// syncReqWithTenant mirrors prod middleware: the sync/snapshot routes are
// authed, so the tenant is always in context. Requests without it exercise
// a state production never produces (and the W1 guard fails closed on it).
func syncReqWithTenant(method, url string, body []byte) *http.Request {
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	return req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
}

// W1 spoof guard: a batch claiming another tenant's device is rejected with
// 403 and writes nothing — the pipeline trusts device.TenantID, so the
// boundary must hold at the HTTP edge.
func TestHandleTelemetrySync_CrossTenantDeviceRejected(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	// Victim device lives in tenant "2" (seeded by the harness).
	_, err := db.Exec(`INSERT INTO telemetry_devices (id, tenant_id, imei, device_type, status)
		VALUES ('dev-victim','2','IMEI-VICTIM','mobile_app','active')`)
	require.NoError(t, err)

	body, _ := json.Marshal(SyncBatchRequest{DeviceID: "IMEI-VICTIM", Logs: []GPSLogPayload{
		{ID: 1, Latitude: 19.07, Longitude: 72.87, Timestamp: "2026-09-01T10:00:00Z"},
	}})
	// Caller authed as tenant "1".
	req := syncReqWithTenant("POST", "/api/v1/telemetry/sync", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei = 'IMEI-VICTIM'`).Scan(&n))
	require.Equal(t, 0, n, "cross-tenant frame must write nothing")
}

// W1: snapshot for another tenant's vehicle is rejected the same way.
func TestHandleTelemetrySnapshots_CrossTenantVehicleRejected(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
		VALUES ('v-foreign','REG-F','REG-F','truck',15,'2')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO telemetry_devices (id, tenant_id, imei, device_type, status, vehicle_id)
		VALUES ('dev-f','2','IMEI-F','mobile_app','active','v-foreign')`)
	require.NoError(t, err)

	snap := TelemetrySnapshotPayload{
		VehicleID: "v-foreign", Timestamp: "2026-09-01T10:00:00Z",
		Latitude: 19.07, Longitude: 72.87, Speed: 10,
	}
	body, _ := json.Marshal(snap)
	req := syncReqWithTenant("POST", "/api/v1/telemetry/snapshots", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

// W1: DriverID fallback claiming a driver owned by another tenant is
// rejected before any synthetic device is provisioned.
func TestHandleTelemetrySync_ForeignDriverRejected(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES ('d-foreign','user-foreign','F','L','000','available','2')`)
	require.NoError(t, err)

	body, _ := json.Marshal(SyncBatchRequest{DriverID: "user-foreign", Logs: []GPSLogPayload{
		{ID: 1, Latitude: 19.07, Longitude: 72.87, Timestamp: "2026-09-01T10:00:00Z"},
	}})
	req := syncReqWithTenant("POST", "/api/v1/telemetry/sync", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_devices WHERE imei = 'user-foreign'`).Scan(&n))
	require.Equal(t, 0, n, "no device may be provisioned for a foreign driver")
}
