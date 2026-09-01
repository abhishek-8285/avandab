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
)

// newTestRouter builds a chi router with the telemetry routes wired to a
// real Ingestor over a migrated in-memory DB.
func newTestRouter(t *testing.T) (chi.Router, *Ingestor) {
	t.Helper()
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute)
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
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute)
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
	req := httptest.NewRequest("POST", "/api/v1/telemetry/sync", bytes.NewReader(body))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
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
	req := httptest.NewRequest("POST", "/api/v1/telemetry/snapshots", bytes.NewReader(body))
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
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute)

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
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/telemetry/sync", bytes.NewReader(body)))
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
