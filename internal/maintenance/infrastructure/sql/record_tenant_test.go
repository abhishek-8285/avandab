package sql

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/maintenance/domain"
)

func maintTenantTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:maint_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE maintenance_records (
		id TEXT PRIMARY KEY, vehicle_id TEXT, schedule_id TEXT, service_type TEXT,
		performed_at DATETIME, odometer_km REAL, cost REAL, vendor TEXT, notes TEXT,
		recorded_by TEXT, tenant_id TEXT)`)
	require.NoError(t, err)
	return db
}

// Workshop records without an attributable tenant must be refused, never
// filed under the bootstrap tenant.
func TestInsertRecord_RequiresTenant(t *testing.T) {
	db := maintTenantTestDB(t)
	r := NewMaintenanceRepository(db)

	err := r.InsertRecord(context.Background(), domain.Record{
		ID: "rec-1", VehicleID: "veh-ghost", ServiceType: "oil", PerformedAt: time.Now(),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without tenant")

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM maintenance_records`).Scan(&count))
	assert.Equal(t, 0, count)
}

// Tenant resolves from the vehicle's own org when context carries none.
func TestInsertRecord_TenantFromVehicle(t *testing.T) {
	db := maintTenantTestDB(t)
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id) VALUES ('veh-1', 'tenant-W')`)
	require.NoError(t, err)
	r := NewMaintenanceRepository(db)

	require.NoError(t, r.InsertRecord(context.Background(), domain.Record{
		ID: "rec-2", VehicleID: "veh-1", ServiceType: "oil", PerformedAt: time.Now(),
	}))

	var tenant string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM maintenance_records WHERE id = 'rec-2'`).Scan(&tenant))
	assert.Equal(t, "tenant-W", tenant)
}
