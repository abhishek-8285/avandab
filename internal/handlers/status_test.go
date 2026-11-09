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
	// No SLA alerts either: honest empty incident history.
	assert.Contains(t, body, "Availability alerting is active")
}

func TestStatusHandler_HTMLPageWithMeasuredUptime(t *testing.T) {
	app := newRegisterTestApp(t)
	app.Config.UptimeSampleInterval = 25 * time.Hour
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-25 * time.Hour), now} {
		_, err := app.DB.Exec("INSERT INTO availability_samples (sampled_at, component, healthy, latency_ms) VALUES ($1, $2, $3, $4)",
			at, availability.ComponentOverall, true, 1)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	app.Status(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "100.00%")
	assert.Contains(t, body, "From 2 samples")
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
	app.Config.UptimeSampleInterval = 25 * time.Hour
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-25 * time.Hour), now} {
		_, err := app.DB.Exec("INSERT INTO availability_samples (sampled_at, component, healthy, latency_ms) VALUES ($1, $2, $3, $4)",
			at, availability.ComponentOverall, true, 1)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/status", nil)
	rr := httptest.NewRecorder()
	app.StatusJSON(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res StatusPageData
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&res))

	assert.True(t, res.UptimeMeasured)
	assert.Equal(t, 2, res.UptimeSamples)
	assert.Equal(t, 100.0, res.UptimePercentage)
	assert.True(t, res.HistoryAvailable)
	require.Len(t, res.HistoricalDays, 30)

	withData := 0
	for _, day := range res.HistoricalDays {
		if day.HasData {
			withData++
			assert.Equal(t, 1, day.Samples)
			assert.Equal(t, 100.0, day.UptimePercent)
		}
	}
	assert.Equal(t, 2, withData, "two samples land in two UTC day buckets")
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

// TestStatusHandler_IncidentHistoryFromSLAAlerts proves the public incident
// history is derived from real availability SLA alerts, deduplicated across
// the per-tenant alert fan-out (one public incident per breach, not per tenant).
func TestStatusHandler_IncidentHistoryFromSLAAlerts(t *testing.T) {
	app := newRegisterTestApp(t)
	now := time.Now().UTC()

	insertAlert := func(id, tenant, status string, firstSeen time.Time, resolvedAt *time.Time) {
		t.Helper()
		_, err := app.DB.Exec(`INSERT INTO alerts (
			id, source, alert_type, severity, status, dedup_key,
			tenant_id, ack_status, severity_rank, money_at_risk,
			title, message, occurrences, first_seen_at, last_seen_at, created_at, updated_at,
			resolved_at, resolved_by
		) VALUES (?, 'availability', 'sla_breach', 'critical', ?, ?, ?,
			'open', 1, 0,
			'30-day uptime 98.50% is below the 99.9% SLA target',
			'Measured platform uptime is below target.', 1,
			?, ?, ?, ?, ?, ?)`,
			id, status, "dk-"+id, tenant, firstSeen, firstSeen, firstSeen, firstSeen, resolvedAt, nil)
		require.NoError(t, err)
	}

	// Same 10-minute breach bucket across two tenants => ONE public incident.
	insertAlert("i-open-a", "tenant-42", "open", now.Add(-2*time.Minute), nil)
	insertAlert("i-open-b", "tenant-7", "open", now.Add(-2*time.Minute), nil)
	// Older resolved breach => second incident with recovery text.
	resolved := now.Add(-40 * time.Minute)
	insertAlert("i-res", "tenant-42", "resolved", now.Add(-60*time.Minute), &resolved)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/status", nil)
	rr := httptest.NewRecorder()
	app.StatusJSON(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var res StatusPageData
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&res))
	require.Len(t, res.Incidents, 2, "tenant fan-out must collapse into one public incident per breach bucket")
	assert.Equal(t, "Investigating", res.Incidents[0].Status)
	assert.Equal(t, "Resolved", res.Incidents[1].Status)
	assert.Contains(t, res.Incidents[1].Description, "Recovered after")

	reqHTML := httptest.NewRequest(http.MethodGet, "/status", nil)
	rrHTML := httptest.NewRecorder()
	app.Status(rrHTML, reqHTML)
	require.Equal(t, http.StatusOK, rrHTML.Code)
	body := rrHTML.Body.String()
	assert.Contains(t, body, "Investigating")
	assert.Contains(t, body, "Resolved")
	assert.Contains(t, body, "30-day uptime 98.50% is below the 99.9% SLA target")
}

func TestStatusHandler_JSONCollectingUptime(t *testing.T) {
	app := newRegisterTestApp(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		require.NoError(t, availability.Record(ctx, app.DB, availability.ComponentOverall, true, time.Millisecond))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/status", nil)
	rr := httptest.NewRecorder()
	app.StatusJSON(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var res StatusPageData
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&res))

	assert.False(t, res.UptimeMeasured, "two back-to-back samples must not read as 30-day availability")
	assert.True(t, res.UptimeCollecting)
	assert.Equal(t, 2, res.UptimeSamples)
	assert.Equal(t, 0.0, res.UptimePercentage)
	assert.False(t, res.HistoryAvailable)
}
