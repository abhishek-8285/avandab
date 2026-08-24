package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/alerts/channels"
	"transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
)

func radarLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newRadarFixture(t *testing.T) (*ComplianceRadarService, *sql.DB) {
	t.Helper()
	db := newVerifyTestDB(t)
	svc := NewComplianceRadarService(db, nil, radarLogger()) // nil engine: Radar works, emit skipped
	return svc, db
}

// newTestAlertEngine wires a real pipeline engine (no channels) onto the
// SAME database as the service under test so emitted alerts land in rows
// the assertions can read.
func newTestAlertEngine(t *testing.T, db *sql.DB) *pipeline.Engine {
	t.Helper()
	repo := alertsqlite.NewAlertRepository(db)
	return pipeline.NewEngine(repo, map[string]channels.Provider{}, radarLogger())
}

func seedVehicleDoc(t *testing.T, db *sql.DB, id string, expiresInDays int) {
	t.Helper()
	mustRadar(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity,
		insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('veh-' || ?, 'MH-12-' || ?, ? || '-NUM', 'truck', 10,
		date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'available', '1')`, id, id, id)
	mustRadar(t, db, `INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, expiry_date, status)
		VALUES (?, 'veh-' || ?, 'insurance', '/f/' || ?, ?, 'verified')`,
		id, id, id, dateOffset(expiresInDays))
}

func seedEwayBill(t *testing.T, db *sql.DB, id string, expiresInHours float64, tenantID string) {
	t.Helper()
	mustRadar(t, db, `INSERT OR IGNORE INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-ewb', 'Pune', 'Nagpur', 700, 12, 20000)`)
	mustRadar(t, db, `INSERT INTO trips (id, trip_number, route_id, departure_time, tenant_id, status)
		VALUES ('tr-ewb-' || ?, 'TR-EWB-' || ?, 'rt-ewb', datetime('now'), ?, 'started')`, id, id, tenantID)
	validUntil := time.Now().Add(time.Duration(expiresInHours * float64(time.Hour)))
	mustRadar(t, db, `INSERT INTO eway_bills (id, trip_id, ewb_number, generation_date, valid_until, status)
		VALUES (?, 'tr-ewb-' || ?, 'EWB-' || ?, datetime('now'), ?, 'active')`,
		id, id, id, validUntil.UTC().Format("2006-01-02 15:04:05"))
}

func mustRadar(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Exec(q, args...)
	require.NoError(t, err, q)
}

func dateOffset(days int) string {
	return time.Now().AddDate(0, 0, days).Format("2006-01-02")
}

func TestComplianceRadar_DocumentBuckets(t *testing.T) {
	svc, db := newRadarFixture(t)
	seedVehicleDoc(t, db, "doc-in30", 20) // inside 30d window
	seedVehicleDoc(t, db, "doc-out", 45)  // outside every window
	radar, err := svc.Radar(context.Background(), "1")
	require.NoError(t, err)
	t.Logf("DUMP %s", (func() string { b, _ := json.Marshal(radar); return string(b) })())

	var found bool
	for _, e := range radar.ExpiringSoon {
		if e["id"] == "veh-doc-in30" {
			found = true
			assert.Equal(t, "insurance", e["kind"])
			assert.GreaterOrEqual(t, e["days_left"], 17)
			assert.LessOrEqual(t, e["days_left"], 21)
		}
	}
	assert.True(t, found, "doc at 20d must appear in radar")
	for _, e := range radar.ExpiringSoon {
		assert.NotEqual(t, "doc-out", e["id"], "45d doc must stay silent")
	}
}

func TestComplianceRadar_EwayBillBucketsAndTenantIsolation(t *testing.T) {
	svc, db := newRadarFixture(t)
	seedEwayBill(t, db, "ewb6", 6, "1")      // inside 12h warn
	seedEwayBill(t, db, "ewb48", 48, "1")    // outside windows
	seedEwayBill(t, db, "ewb-tb", 6, "tn-b") // other tenant
	radar, err := svc.Radar(context.Background(), "1")
	require.NoError(t, err)

	var saw6 bool
	for _, e := range radar.EwaybillExpiring {
		if e["id"] == "ewb6" {
			saw6 = true
		}
		assert.NotEqual(t, "ewb48", e["id"])
		assert.NotEqual(t, "ewb-tb", e["id"], "tenant B EWB must not leak into tenant A radar")
	}
	assert.True(t, saw6, "EWB at 6h must appear")
}

// TestComplianceRadar_SweepEmitsThroughPipeline — full pipeline path:
// buckets produce alert rows with correct ranks and dedup keys.
func TestComplianceRadar_SweepEmitsThroughPipeline(t *testing.T) {
	db := newVerifyTestDB(t)
	engine := newTestAlertEngine(t, db)
	svc := NewComplianceRadarService(db, engine, radarLogger())

	seedVehicleDoc(t, db, "vdoc-7d", 5)   // 7d bucket
	seedVehicleDoc(t, db, "vdoc-30d", 15) // 30d bucket
	seedVehicleDoc(t, db, "vdoc-silent", 60)
	seedEwayBill(t, db, "ewbcrit", 2, "1") // rank 1
	seedEwayBill(t, db, "ewbwarn", 9, "1") // rank 2

	require.NoError(t, svc.Sweep(context.Background()))

	countByType := func(prefix string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM alerts WHERE alert_type LIKE ?`, prefix+"%").Scan(&n))
		return n
	}
	assert.Equal(t, 1, countByType("doc_expiry_7d"), "5d doc lands in the 7d bucket")
	assert.Equal(t, 1, countByType("doc_expiry_30d"), "15d doc lands in the 30d bucket")
	assert.Equal(t, 0, countByType("doc_expiry_1d"), "nothing near expiry in this fixture")
	assert.Equal(t, 1, countByType("ewb_expiry_4h"), "2h EWB is critical bucket")
	assert.Equal(t, 1, countByType("ewb_expiry_12h"), "9h EWB is warn bucket")

	// Rank + dedup key sanity on one critical row.
	var rank int
	var dedupKey string
	require.NoError(t, db.QueryRow(`SELECT severity_rank, dedup_key FROM alerts WHERE alert_type='ewb_expiry_4h' LIMIT 1`).Scan(&rank, &dedupKey))
	assert.Equal(t, 1, rank, "≤4h EWB must be rank-1 critical")
	assert.Contains(t, dedupKey, "compliance:ewb_expiry_4h:")
}
