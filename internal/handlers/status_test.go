package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/apm"
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
	assert.Contains(t, body, "99.98%")
	assert.Contains(t, body, "API &amp; Web Gateway")
	assert.Contains(t, body, "Database Persistence Engine")
	assert.Contains(t, body, "Telemetry &amp; GPS Ingestion")
	assert.Contains(t, body, "Guaranteed 99.9% Production SLA")
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
	assert.Equal(t, 99.98, res.UptimePercentage)
	assert.Equal(t, "99.9%", res.SLATarget)
	assert.Len(t, res.Subsystems, 6)
	assert.Len(t, res.HistoricalDays, 30)

	// Verify each subsystem is populated
	names := make([]string, len(res.Subsystems))
	for i, sub := range res.Subsystems {
		names[i] = sub.Name
		assert.NotEmpty(t, sub.Status)
		assert.NotEmpty(t, sub.Availability)
	}
	assert.Contains(t, names, "API & Web Gateway")
	assert.Contains(t, names, "Database Persistence Engine")
	assert.Contains(t, names, "Telemetry & GPS Ingestion")
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
