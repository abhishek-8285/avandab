package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"transport-app/internal/eta"
	"transport-app/internal/shared"
)

// TestHistoryHandler_Trail verifies the breadcrumb endpoint: ascending time
// order, window filtering, tenant scoping, and the required vehicle_id param.
func TestHistoryHandler_Trail(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v1", "REG-1")
	insertTestVehicleReg(t, db, "v2", "REG-2")

	now := time.Now().UTC()
	insertLiveSnapshot(t, db, "h1", "", "v1", now.Add(-30*time.Minute), 40.0)
	insertLiveSnapshot(t, db, "h2", "", "v1", now.Add(-20*time.Minute), 50.0)
	insertLiveSnapshot(t, db, "h3", "", "v1", now.Add(-10*time.Minute), 60.0)
	insertLiveSnapshot(t, db, "h4", "", "v2", now.Add(-10*time.Minute), 10.0)
	// Outside a 15-minute window: must be excluded.
	insertLiveSnapshot(t, db, "h5", "", "v1", now.Add(-40*time.Minute), 30.0)
	// NULL coords: excluded.
	db.Exec(`INSERT INTO telemetry_snapshots (id, vehicle_id, timestamp) VALUES ('h6', 'v1', ?)`,
		now.Add(-5*time.Minute).UTC().Format("2006-01-02 15:04:05"))

	handler := HistoryHandler(db)

	t.Run("requires vehicle_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns ascending trail within window", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?vehicle_id=v1&minutes=25", nil)
		w := httptest.NewRecorder()
		handler(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var points []HistoryPoint
		require.NoError(t, json.NewDecoder(w.Body).Decode(&points))
		require.Len(t, points, 2) // h2 (-20m) + h3 (-10m); h1 (-30m) outside window
		for i := 1; i < len(points); i++ {
			assert.True(t, points[i].Ts.After(points[i-1].Ts) || points[i].Ts.Equal(points[i-1].Ts))
		}
		assert.InDelta(t, 60.0, points[1].Speed, 0.001)

		// Default window (90m) picks up everything except the NULL-coord row.
		req2 := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?vehicle_id=v1", nil)
		w2 := httptest.NewRecorder()
		handler(w2, req2)
		require.Equal(t, http.StatusOK, w2.Code)
		var all []HistoryPoint
		require.NoError(t, json.NewDecoder(w2.Body).Decode(&all))
		assert.Len(t, all, 4) // h1, h2, h3, h5
	})

	t.Run("tenant scoped", func(t *testing.T) {
		_, err := db.Exec(`UPDATE vehicles SET tenant_id = '9' WHERE id = 'v1'`)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/telemetry/history?vehicle_id=v1", nil)
		req = req.WithContext(shared.ContextWithTenantID(req.Context(), "7"))
		w := httptest.NewRecorder()
		handler(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var points []HistoryPoint
		require.NoError(t, json.NewDecoder(w.Body).Decode(&points))
		assert.Empty(t, points)
	})
}

// TestLiveStore_ETA_Cached proves the TTL cache serves repeat lookups without
// re-running Calculate (which would otherwise run 4-5 queries per trip per
// poll). After the first Live() call the trip must be present in the cache;
// a second call within etaCacheTTL must reuse it (same method string, cache
// entry untouched).
func TestLiveStore_ETA_Cached(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestRoute(t, db, "r1")
	insertTestTrip(t, db, "t1")
	insertTestVehicleReg(t, db, "v1", "REG-1")

	now := time.Now().UTC()
	started := now.Add(-1 * time.Hour)
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-start', 't1', 'v1', ?, 19.0, 72.8, 60.0, 80.0, 1000.0)`, started.Format("2006-01-02 15:04:05"))
	_, _ = db.Exec(`INSERT INTO telemetry_snapshots
		(id, trip_id, vehicle_id, timestamp, latitude, longitude, speed, fuel_level, odometer)
		VALUES ('s-latest', 't1', 'v1', ?, 19.3, 72.8, 60.0, 80.0, 1030.0)`, now.Add(-1*time.Minute).Format("2006-01-02 15:04:05"))

	store := NewLiveStore(db, 15*time.Minute).WithEtaService(eta.NewEtaService(db, 15, 30, 5))

	ctx := context.Background()
	vehicles, err := store.Live(ctx, "1", "t1", now)
	require.NoError(t, err)
	require.Len(t, vehicles, 1)
	firstMethod := vehicles[0].EtaMethod
	assert.NotEmpty(t, firstMethod)

	entry, cached := store.etaCache["t1"]
	require.True(t, cached, "first call must populate the ETA cache")
	assert.True(t, entry.ok)
	assert.WithinDuration(t, time.Now().Add(etaCacheTTL), entry.expires, 5*time.Second)

	res, ok := store.cachedEta(ctx, "t1")
	require.True(t, ok)
	assert.Equal(t, firstMethod, res.Method)
}
