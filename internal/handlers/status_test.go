package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/apm"
	"transport-app/internal/availability"
)

func TestStatusHandler_HTMLPage(t *testing.T) {
	app := newRegisterTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()

	app.Status(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "All Systems Operational")
	assert.Contains(t, body, "30-Day Availability")
	assert.Contains(t, body, "API &amp; Web Gateway")
	assert.Contains(t, body, "Database Persistence Engine")
	assert.Contains(t, body, "Telemetry &amp; GPS Ingestion")
	// P0 stop-fake-data: no invented uptime number and no guarantee claim.
	assert.NotContains(t, body, "99.98%")
	assert.NotContains(t, body, "Guaranteed 99.9% Production SLA")
	assert.Contains(t, body, "99.9% uptime target")
	// Fresh test DB has no availability samples: honest empty state.
	assert.Contains(t, body, "Not measured yet")
	assert.Contains(t, body, "Availability history not yet measured")
}

func TestStatusHandler_HTMLPageWithMeasuredUptime(t *testing.T) {
	app := newRegisterTestApp(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, availability.Record(context.Background(), app.DB, availability.ComponentOverall, true, time.Millisecond))
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	app.Status(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "100.00%")
	assert.Contains(t, body, "From 3 samples")
	assert.Contains(t, body, "Measured history")
}

func TestStatusHandler_JSONEndpoint(t *testing.T) {
	app := newRegisterTestApp(t)

	// Record a couple spans in APM to test percentile extraction
	_, span1 := apm.StartSpan(nil, "http.request")
	span1.End()
	_, span2 := apm.StartSpan(nil, "telemetry.batch_ingest")
	span2.End()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/status", nil)
	rr := httptest.NewRecorder()

	app.StatusJSON(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var res StatusPageData
	err := json.NewDecoder(rr.Body).Decode(&res)
	require.NoError(t, err)

	assert.Equal(t, "Operational", res.OverallStatus)
	// No samples yet: 0 + measured=false, never a fabricated percentage
	// (P0 stop-fake-data).
	assert.Equal(t, 0.0, res.UptimePercentage)
	assert.False(t, res.UptimeMeasured)
	assert.Equal(t, 0, res.UptimeSamples)
	assert.Equal(t, "99.9%", res.SLATarget)
	assert.Len(t, res.Subsystems, 6)
	assert.Len(t, res.HistoricalDays, 30, "one bucket per UTC day")
	assert.False(t, res.HistoryAvailable)
	assert.Equal(t, 6, res.SubsystemCount)
	assert.NotEmpty(t, res.DBLatency)

	// Verify each subsystem reports a real state; no availability percentages.
	names := make([]string, len(res.Subsystems))
	for i, sub := range res.Subsystems {
		names[i] = sub.Name
		assert.NotEmpty(t, sub.Status)
		assert.Empty(t, sub.Availability, "no invented per-subsystem uptime")
	}
	assert.Contains(t, names, "API & Web Gateway")
	assert.Contains(t, names, "Database Persistence Engine")
	assert.Contains(t, names, "Telemetry & GPS Ingestion")
}

func TestStatusHandler_JSONMeasuredUptime(t *testing.T) {
	app := newRegisterTestApp(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.NoError(t, availability.Record(ctx, app.DB, availability.ComponentOverall, true, time.Millisecond))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/status", nil)
	rr := httptest.NewRecorder()
	app.StatusJSON(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res StatusPageData
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&res))

	assert.True(t, res.UptimeMeasured)
	assert.Equal(t, 3, res.UptimeSamples)
	assert.Equal(t, 100.0, res.UptimePercentage)
	assert.True(t, res.HistoryAvailable)
	require.Len(t, res.HistoricalDays, 30)

	withData := 0
	for _, day := range res.HistoricalDays {
		if day.HasData {
			withData++
			assert.Equal(t, 3, day.Samples)
			assert.Equal(t, 100.0, day.UptimePercent)
		}
	}
	assert.Equal(t, 1, withData, "all three samples fall in one UTC day bucket")
}

func TestStatusHandler_JSONViaAcceptHeader(t *testing.T) {
	app := newRegisterTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()

	app.Status(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var res StatusPageData
	err := json.NewDecoder(rr.Body).Decode(&res)
	require.NoError(t, err)
	assert.Equal(t, "Operational", res.OverallStatus)
}
