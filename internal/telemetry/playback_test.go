package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/shared"
)

func TestPlaybackHandler_ServerSummaryAndStops(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	now := time.Now().UTC()
	// Points: start -> move -> stop cluster -> move
	insertLiveSnapshotWithTrip(t, db, "p1", "t1", "v1", now.Add(-60*time.Minute), 50.0, 19.0, 72.8)
	insertLiveSnapshotWithTrip(t, db, "p2", "t1", "v1", now.Add(-50*time.Minute), 60.0, 19.1, 72.9)
	// Stop: 3 points with speed 0 spanning 6 min, same location drift <200m
	insertLiveSnapshotWithTrip(t, db, "p3", "t1", "v1", now.Add(-40*time.Minute), 0.0, 19.2, 73.0)
	insertLiveSnapshotWithTrip(t, db, "p4", "t1", "v1", now.Add(-37*time.Minute), 0.0, 19.2001, 73.0001)
	insertLiveSnapshotWithTrip(t, db, "p5", "t1", "v1", now.Add(-34*time.Minute), 0.0, 19.20005, 73.00005)
	insertLiveSnapshotWithTrip(t, db, "p6", "t1", "v1", now.Add(-20*time.Minute), 40.0, 19.3, 73.1)
	// Also insert into telemetry_positions directly to test dual-source (positions preferred)
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos1','1','imei1',?,datetime('now'),19.0,72.8,50,'1',1000,'t1','v1','own','raw1')`, now.Add(-60*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos2','1','imei1',?,datetime('now'),19.1,72.9,60,'90',1010,'t1','v1','own','raw2')`, now.Add(-50*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos3','1','imei1',?,datetime('now'),19.2,73.0,0,'0',1020,'t1','v1','own','raw3')`, now.Add(-40*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos4','1','imei1',?,datetime('now'),19.2001,73.0001,0,'0',1020,'t1','v1','own','raw4')`, now.Add(-37*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos5','1','imei1',?,datetime('now'),19.20005,73.00005,0,'0',1020,'t1','v1','own','raw5')`, now.Add(-34*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, heading, odometer, trip_id, vehicle_id, provider, raw_event_id) VALUES ('pos6','1','imei1',?,datetime('now'),19.3,73.1,40,'180',1030,'t1','v1','own','raw6')`, now.Add(-20*time.Minute).Format("2006-01-02 15:04:05"))

	// Geofence event and detention for overlay
	_, _ = db.Exec(`INSERT INTO geofences (id, tenant_id, name, kind, shape, center_lat, center_lng, radius_m) VALUES ('g1','1','Warehouse','depot','circle',19.1,72.9,500)`)
	_, _ = db.Exec(`INSERT INTO geofence_events (id, tenant_id, vehicle_id, trip_id, geofence_id, zone_kind, event_type, latitude, longitude, created_at) VALUES ('ge1','1','v1','t1','g1','depot','entering',19.1,72.9,?)`, now.Add(-45*time.Minute).Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO trip_detentions (id, tenant_id, trip_id, vehicle_id, geofence_id, zone_kind, entered_at, dwell_seconds, status) VALUES ('d1','1','t1','v1','g1','pickup',?,100,'open')`, now.Add(-40*time.Minute).Format("2006-01-02 15:04:05"))

	handler := PlaybackHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t1/playback?limit=100", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	handler(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlaybackResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Equal(t, "t1", resp.TripID)
	require.Len(t, resp.Points, 6)
	// Server-side distance must be >0 (haversine)
	assert.Greater(t, resp.Summary.DistanceKM, 0.0)
	assert.Equal(t, int64((40 * 60)), resp.Summary.DurationSeconds) // 60m to 20m = 40m = 2400s
	assert.Greater(t, resp.Summary.MaxSpeedKMH, 0.0)
	// Stops: one detected (>5m, <3km/h, drift <200m)
	require.Len(t, resp.Stops, 1)
	assert.Equal(t, int64(360), resp.Stops[0].DurationSeconds) // 6 min = 360s
	// Geofence overlay present
	require.Len(t, resp.GeofenceEvents, 1)
	assert.Equal(t, "entering", resp.GeofenceEvents[0].EventType)
	// Detentions present
	require.Len(t, resp.Detentions, 1)
	assert.Equal(t, "pickup", resp.Detentions[0].ZoneKind)
	assert.False(t, resp.Truncated)
}

func TestPlaybackHandler_PaginationTruncated(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		id := "pp" + string(rune('0'+i))
		ts := now.Add(time.Duration(-60+i*10) * time.Minute)
		insertLiveSnapshotWithTrip(t, db, id, "t1", "v1", ts, 30.0, 19.0+float64(i)*0.01, 72.8+float64(i)*0.01)
		_, _ = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, received_at, latitude, longitude, speed, trip_id, vehicle_id, provider) VALUES (?, '1','imei1', ?, datetime('now'), 19.0, 72.8, 30, 't1','v1','own')`, id+"pos", ts.Format("2006-01-02 15:04:05"))
	}
	handler := PlaybackHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t1/playback?limit=2", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	handler(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlaybackResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Points, 2)
	assert.True(t, resp.Truncated)
	require.NotNil(t, resp.NextFrom)
	// Follow pagination (RFC3339Nano preserves ms for exclusive cursor)
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t1/playback?limit=2&after="+resp.NextFrom.Format(time.RFC3339Nano), nil)
	req2.SetPathValue("id", "t1")
	w2 := httptest.NewRecorder()
	handler(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 PlaybackResponse
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&resp2))
	assert.Greater(t, len(resp2.Points), 0)
	// Ensure no overlap: second page first point after first page last point
	assert.True(t, resp2.Points[0].Ts.After(resp.Points[len(resp.Points)-1].Ts))
}

func TestPlaybackHandler_RequiresTripID(t *testing.T) {
	db := newTestIngestorDB(t)
	handler := PlaybackHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/playback", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPlaybackHandler_TenantScoped(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	now := time.Now().UTC()
	insertLiveSnapshotWithTrip(t, db, "p1", "t1", "v1", now.Add(-10*time.Minute), 30.0, 19.0, 72.8)
	// Change trip tenant to 9
	_, _ = db.Exec(`UPDATE trips SET tenant_id='9' WHERE id='t1'`)
	handler := PlaybackHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/t1/playback", nil)
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "7"))
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	handler(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp PlaybackResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.Points)
}
