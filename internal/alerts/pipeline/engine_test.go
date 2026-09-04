package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/alerts/domain"
	sqliterepo "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/events"
	"transport-app/internal/shared"
)

type mockClock struct {
	current time.Time
}

func (m *mockClock) Now() time.Time {
	return m.current
}

func (m *mockClock) Advance(d time.Duration) {
	m.current = m.current.Add(d)
}

func newAlertsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_alerts_pipe_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	// 00103 enforces tenant FK — seed common tenants (including 'tenant-42' used in inbox_enrich_test).
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-42','Tenant 42','tenant-42')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEngine_DedupAndCooldown(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	clk := &mockClock{current: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")

	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "speeding",
			"severity":   "warning",
			"vehicle_id": "v-dedup-1",
			"title":      "Speeding 95 km/h",
			"details":    "Vehicle v-dedup-1 speeding at 95 km/h",
		},
	}

	// 1. First event -> creates open alert with occurrences = 1
	err := engine.ProcessEvent(ctx, ev)
	require.NoError(t, err)

	openAlert, err := repo.FindOpenByDedupKey(ctx, "telemetry:speeding:v-dedup-1")
	require.NoError(t, err)
	require.NotNil(t, openAlert)
	assert.Equal(t, 1, openAlert.Occurrences)
	assert.Equal(t, clk.Now(), openAlert.FirstSeenAt)
	assert.Equal(t, clk.Now(), openAlert.LastSeenAt)

	// 2. Advance time by 60s (within 300s cooldown)
	clk.Advance(60 * time.Second)

	// Second event -> should deduplicate into existing row
	err = engine.ProcessEvent(ctx, ev)
	require.NoError(t, err)

	updatedAlert, err := repo.FindOpenByDedupKey(ctx, "telemetry:speeding:v-dedup-1")
	require.NoError(t, err)
	require.NotNil(t, updatedAlert)
	assert.Equal(t, openAlert.ID, updatedAlert.ID, "must keep same alert ID")
	assert.Equal(t, 2, updatedAlert.Occurrences, "occurrences must increment to 2")
	assert.Equal(t, clk.Now(), updatedAlert.LastSeenAt, "last_seen_at must update to current time")

	// Verify total row count is still 1
	var totalCount int
	err = db.QueryRow("SELECT COUNT(*) FROM alerts WHERE dedup_key = 'telemetry:speeding:v-dedup-1'").Scan(&totalCount)
	require.NoError(t, err)
	assert.Equal(t, 1, totalCount)
}

func TestEngine_CooldownExpiry(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	clk := &mockClock{current: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")

	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "speeding",
			"severity":   "warning",
			"vehicle_id": "v-expiry-1",
			"title":      "Speeding 90 km/h",
		},
	}

	// 1. First event
	err := engine.ProcessEvent(ctx, ev)
	require.NoError(t, err)

	firstAlert, err := repo.FindOpenByDedupKey(ctx, "telemetry:speeding:v-expiry-1")
	require.NoError(t, err)
	require.NotNil(t, firstAlert)

	// 2. Advance time past default 300s cooldown (e.g. 301 seconds)
	clk.Advance(301 * time.Second)

	// Second event -> cooldown expired, creates new alert row
	err = engine.ProcessEvent(ctx, ev)
	require.NoError(t, err)

	// Verify total rows created is 2
	var totalCount int
	err = db.QueryRow("SELECT COUNT(*) FROM alerts WHERE dedup_key = 'telemetry:speeding:v-expiry-1'").Scan(&totalCount)
	require.NoError(t, err)
	assert.Equal(t, 2, totalCount, "cooldown expiry must create a second alert row")
}

func TestEngine_StormBatching_Occurrences(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	clk := &mockClock{current: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")

	ev := events.Event{
		Type: "AlertEvent",
		Payload: map[string]interface{}{
			"source":     "telemetry",
			"alert_type": "gps_deviation",
			"severity":   "warning",
			"vehicle_id": "v-storm-1",
			"title":      "GPS Deviation 6.2 km",
		},
	}

	// Publish 5 rapid events within storm window (e.g., 5 seconds apart)
	for i := 0; i < 5; i++ {
		err := engine.ProcessEvent(ctx, ev)
		require.NoError(t, err)
		clk.Advance(5 * time.Second)
	}

	alert, err := repo.FindOpenByDedupKey(ctx, "telemetry:gps_deviation:v-storm-1")
	require.NoError(t, err)
	require.NotNil(t, alert)
	assert.Equal(t, 5, alert.Occurrences, "5 events in storm window must accumulate to occurrences=5")

	var rowCount int
	err = db.QueryRow("SELECT COUNT(*) FROM alerts WHERE dedup_key = 'telemetry:gps_deviation:v-storm-1'").Scan(&rowCount)
	require.NoError(t, err)
	assert.Equal(t, 1, rowCount, "all 5 events must collapse into a single alert row")
}

func TestTelemetryAlertsRebuild_00059_CanonicalTypes(t *testing.T) {
	db := newAlertsTestDB(t)

	// All 13 canonical alert types from Spec 05 §12 (00059)
	canonicalTypes := []string{
		domain.AlertTypeNightDriving,
		domain.AlertTypeRestrictedZone,
		domain.AlertTypeUnauthorizedMovement,
		domain.AlertTypeOffHoursUse,
		domain.AlertTypeRefill,
		domain.AlertTypeTheftSuspicion,
		domain.AlertTypeAbnormalDrain,
		domain.AlertTypeSiphonConfirmed,
		domain.AlertTypeOdometerRollback,
		domain.AlertTypeSpeeding,
		domain.AlertTypeTempBreach,
		domain.AlertTypeGPSDeviation,
		domain.AlertTypeGeofenceBreach,
	}

	for i, at := range canonicalTypes {
		_, err := db.Exec(`
			INSERT INTO telemetry_alerts (id, alert_type, severity, details)
			VALUES (?, ?, 'warning', 'Test telemetry alert')`,
			fmt.Sprintf("ta-%d", i), at)
		assert.NoError(t, err, "telemetry_alerts table must accept canonical type %s", at)
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM telemetry_alerts").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, len(canonicalTypes), count)
}

// TestEngine_LegacyFuelEventMapping pins the founder-shape → canonical
// mapping: fuel-engine events carry category/event_type/priority/summary
// keys (no alert_type/severity/message) and must still classify into the
// canonical catalog with correct source, instead of empty-typed alerts.
func TestEngine_LegacyFuelEventMapping(t *testing.T) {
	db := newAlertsTestDB(t)
	repo := sqliterepo.NewAlertRepository(db)
	engine := NewEngine(repo, nil, nil)

	clk := &mockClock{current: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)}
	engine.SetClock(clk)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-42")

	cases := []struct {
		eventType     string
		wantAlertType string
	}{
		{"refill_detected", domain.AlertTypeRefill},
		{"drain_theft_suspected", domain.AlertTypeTheftSuspicion},
		{"abnormal_drain", domain.AlertTypeAbnormalDrain},
		{"siphon_confirmed", domain.AlertTypeSiphonConfirmed},
	}

	for _, tc := range cases {
		ev := events.Event{
			Type: "AlertEvent",
			Payload: map[string]interface{}{
				"category":         "fuel",
				"priority":         "high",
				"title":            "Fuel event",
				"summary":          "Legacy shape from fuel engine",
				"vehicle_id":       "veh-legacy-1",
				"event_type":       tc.eventType,
				"trip_id":          "",
				"driver_id":        "drv-1",
				"estimated_litres": 12.5,
			},
		}
		require.NoError(t, engine.ProcessEvent(ctx, ev))

		dedup := fmt.Sprintf("%s:%s:%s", domain.SourceFuel, tc.wantAlertType, "veh-legacy-1")
		alert, err := repo.FindOpenByDedupKey(ctx, dedup)
		require.NoError(t, err, "event_type %s", tc.eventType)
		require.NotNil(t, alert, "event_type %s must create canonical alert", tc.eventType)
		assert.Equal(t, tc.wantAlertType, alert.AlertType)
		assert.Equal(t, domain.SourceFuel, alert.Source)
		assert.Equal(t, "high", alert.Severity, "priority must map to severity")
		assert.Contains(t, alert.Message, "Legacy shape", "summary must map to message")

		_ = repo.Resolve(ctx, alert.ID, "test")
	}
}
