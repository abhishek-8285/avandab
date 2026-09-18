package telemetry

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A mobile is_stale (last-known, not live) fix must be accepted into history
// for audit but must never overwrite vehicle_latest_position — the same
// invalid-fix gate as hardware Valid=false frames.
func TestHandleTelemetrySync_StaleFixStoredButNotLatest(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	insertTestVehicle(t, db, "vh-stale-1")
	imei := "IMEI-STALE-1"
	insertTestDevice(t, db, imei, DeviceStatusActive, strPtr("vh-stale-1"))
	r := chi.NewRouter()
	RegisterTelemetryRoutes(r, ing, db, 15*time.Minute, 60*time.Minute)

	live := SyncBatchRequest{
		DeviceID: imei,
		Logs: []GPSLogPayload{
			{ID: 1, Latitude: 19.076, Longitude: 72.877, Timestamp: "2026-08-13T00:00:00Z"},
		},
	}
	body, _ := json.Marshal(live)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, syncReqWithTenant("POST", "/api/v1/telemetry/sync", body))
	require.Equal(t, 200, w.Code)

	stale := true
	staleBatch := SyncBatchRequest{
		DeviceID: imei,
		Logs: []GPSLogPayload{
			{ID: 2, Latitude: 21.146, Longitude: 79.088, Timestamp: "2026-08-13T00:05:00Z", IsStale: &stale},
		},
	}
	body, _ = json.Marshal(staleBatch)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, syncReqWithTenant("POST", "/api/v1/telemetry/sync", body))
	require.Equal(t, 200, w.Code)

	var resp SyncBatchResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, []int64{2}, resp.SyncedIDs, "stale fix is accepted into history")

	// Latest row still holds the LIVE frame's coordinates.
	var lat, lng float64
	require.NoError(t, db.QueryRow(
		`SELECT latitude, longitude FROM vehicle_latest_position WHERE vehicle_id='vh-stale-1'`).Scan(&lat, &lng))
	assert.InDelta(t, 19.076, lat, 0.001)
	assert.InDelta(t, 72.877, lng, 0.001)

	// Both frames are in history; the stale one is flagged invalid.
	var nValid, nInvalid int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-STALE-1' AND valid=1`).Scan(&nValid))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-STALE-1' AND valid=0`).Scan(&nInvalid))
	assert.Equal(t, 1, nValid)
	assert.Equal(t, 1, nInvalid)
}
