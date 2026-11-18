package sql

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func maintGuardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:maint_guard_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL,
			maintenance_override_by TEXT, maintenance_override_at DATETIME, maintenance_override_reason TEXT,
			updated_at DATETIME);
		CREATE TABLE dtc_events (id TEXT PRIMARY KEY, vehicle_id TEXT NOT NULL, resolved_at DATETIME);
		INSERT INTO vehicles (id, tenant_id) VALUES ('veh-a', 'tenant-a'), ('veh-b', 'tenant-b');
		INSERT INTO dtc_events (id, vehicle_id) VALUES ('dtc-a', 'veh-a'), ('dtc-b', 'veh-b');`)
	require.NoError(t, err)
	return db
}

func TestOverrideMaintenance_TenantScoped(t *testing.T) {
	db := maintGuardTestDB(t)
	r := NewMaintenanceRepository(db)
	ctx := context.Background()

	require.Error(t, r.OverrideMaintenance(ctx, "tenant-b", "veh-a", "actor", "reason"),
		"another tenant's vehicle must not be overridden")

	var by sql.NullString
	require.NoError(t, db.QueryRow(`SELECT maintenance_override_by FROM vehicles WHERE id='veh-a'`).Scan(&by))
	require.False(t, by.Valid, "victim vehicle must stay untouched")

	require.NoError(t, r.OverrideMaintenance(ctx, "tenant-a", "veh-a", "actor", "reason"))
	require.NoError(t, db.QueryRow(`SELECT maintenance_override_by FROM vehicles WHERE id='veh-a'`).Scan(&by))
	require.True(t, by.Valid)
}

func TestResolveDtcEvent_TenantScoped(t *testing.T) {
	db := maintGuardTestDB(t)
	r := NewMaintenanceRepository(db)
	ctx := context.Background()

	require.Error(t, r.ResolveDtcEvent(ctx, "tenant-b", "dtc-a"), "another tenant's DTC must not resolve")

	var resolved sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT resolved_at FROM dtc_events WHERE id='dtc-a'`).Scan(&resolved))
	require.False(t, resolved.Valid, "victim DTC must stay open")

	require.NoError(t, r.ResolveDtcEvent(ctx, "tenant-a", "dtc-a"))
	require.NoError(t, db.QueryRow(`SELECT resolved_at FROM dtc_events WHERE id='dtc-a'`).Scan(&resolved))
	require.True(t, resolved.Valid)
}
