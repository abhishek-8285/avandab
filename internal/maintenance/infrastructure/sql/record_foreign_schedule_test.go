package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/shared"
)

// TestInsertRecord_RejectsForeignSchedule: a maintenance record stamped by
// tenant A must never reset tenant B's service schedule (which lifts B's
// dispatch block).
func TestInsertRecord_RejectsForeignSchedule(t *testing.T) {
	db := maintTenantTestDB(t)
	_, err := db.Exec(`CREATE TABLE maintenance_schedules (id TEXT PRIMARY KEY, vehicle_id TEXT, service_type TEXT, last_done_km REAL, last_done_at DATETIME, active INTEGER DEFAULT 1, updated_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id) VALUES ('veh-a', 'tenant-A'), ('veh-b', 'tenant-B')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, active) VALUES ('sch-b', 'veh-b', 'oil', 1)`)
	require.NoError(t, err)
	r := NewMaintenanceRepository(db)
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-A"))
	scheduleID := "sch-b"

	err = r.InsertRecord(ctx, domain.Record{
		ID: "rec-a", VehicleID: "veh-a", ScheduleID: &scheduleID,
		ServiceType: "oil", PerformedAt: time.Now(), OdometerKM: func() *float64 { v := 50000.0; return &v }(),
	})
	require.Error(t, err, "foreign schedule must not be stamped")

	var lastKM float64
	require.NoError(t, db.QueryRow(`SELECT COALESCE(last_done_km, 0) FROM maintenance_schedules WHERE id='sch-b'`).Scan(&lastKM))
	assert.Zero(t, lastKM)

	// Matching by vehicle+service only must also stay inside the tenant.
	err = r.InsertRecord(ctx, domain.Record{
		ID: "rec-a2", VehicleID: "veh-a", ServiceType: "oil", PerformedAt: time.Now(), OdometerKM: func() *float64 { v := 50000.0; return &v }(),
	})
	require.NoError(t, err, "vehicle+service matching is best-effort when no local schedule exists")
	require.NoError(t, db.QueryRow(`SELECT COALESCE(last_done_km, 0) FROM maintenance_schedules WHERE id='sch-b'`).Scan(&lastKM))
	assert.Zero(t, lastKM, "foreign schedule must stay untouched")
}
