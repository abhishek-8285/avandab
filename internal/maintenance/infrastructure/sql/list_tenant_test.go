package sql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Fleetwide maintenance lists must isolate orgs: schedules/DTCs/records of
// one tenant never surface in another's maintenance board.
func TestMaintLists_TenantIsolation(t *testing.T) {
	db := maintTenantTestDB(t)
	repo := NewMaintenanceRepository(db)
	ctx := context.Background()

	_, err := db.Exec(`CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT, slug TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE maintenance_schedules (id TEXT PRIMARY KEY, vehicle_id TEXT, service_type TEXT,
		interval_km REAL, interval_days INTEGER, last_done_km REAL, last_done_at DATETIME,
		due_km REAL, due_at DATETIME, active INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE dtc_events (id TEXT PRIMARY KEY, vehicle_id TEXT, trip_id TEXT, dtc_code TEXT,
		severity TEXT, description TEXT, raw_payload TEXT, occurred_at DATETIME, resolved_at DATETIME, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-A','A','a'),('tenant-B','B','b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id) VALUES ('va','tenant-A'),('vb','tenant-B')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO maintenance_schedules (id, vehicle_id, service_type, due_at, active)
		VALUES ('sa','va','oil_change',datetime('now','+5 days'),1),
		       ('sb','vb','brake',datetime('now','+5 days'),1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dtc_events (id, vehicle_id, dtc_code, severity, occurred_at)
		VALUES ('da','va','P0001','warning',datetime('now')),('db','vb','P0002','warning',datetime('now'))`)
	require.NoError(t, err)

	scheds, err := repo.ListActiveSchedulesForTenant(ctx, "", "tenant-A")
	require.NoError(t, err)
	require.Len(t, scheds, 1)
	assert.Equal(t, "sa", scheds[0].ID)

	dtcs, err := repo.ListDtcEventsForTenant(ctx, "", "tenant-A", 10)
	require.NoError(t, err)
	require.Len(t, dtcs, 1)
	assert.Equal(t, "da", dtcs[0].ID)

	// Legacy unfiltered path still returns everything (worker attribution).
	all, err := repo.ListActiveSchedules(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 2)
}
