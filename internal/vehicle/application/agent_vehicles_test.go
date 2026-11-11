package application

// Focused test for the agent vehicle availability facade (Phase 3):
// tenant isolation + maintenance-block filter (due/DTC/work order/override).

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
)

func TestAgentVehicleQueries_AvailableDropsMaintenanceBlocked(t *testing.T) {
	name := fmt.Sprintf("agent_veh_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE vehicles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, registration_number TEXT, vehicle_type TEXT, capacity INTEGER, status TEXT,
		 maintenance_due DATETIME, maintenance_override_by TEXT, maintenance_override_at DATETIME, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE dtc_events (id TEXT PRIMARY KEY, vehicle_id TEXT, dtc_code TEXT, severity TEXT, resolved_at DATETIME, occurred_at DATETIME);
		CREATE TABLE work_orders (id TEXT PRIMARY KEY, vehicle_id TEXT, status TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_type, capacity, status, maintenance_due) VALUES
		('v-ok','tenant-a','REG-OK','truck',10,'available',NULL),
		('v-due','tenant-a','REG-DUE','truck',10,'available','2026-01-01 00:00:00'),
		('v-dtc','tenant-a','REG-DTC','truck',10,'available',NULL),
		('v-wo','tenant-a','REG-WO','truck',10,'available',NULL),
		('v-b','tenant-b','REG-B','truck',10,'available',NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO dtc_events (id, vehicle_id, dtc_code, severity) VALUES ('e1','v-dtc','P0300','critical')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO work_orders (id, vehicle_id, status) VALUES ('wo1','v-wo','in_progress')`)
	require.NoError(t, err)

	rows, err := NewAgentVehicleQueries(db).Available(context.Background(), shared.TenantID("tenant-a"))
	require.NoError(t, err)
	require.Len(t, rows, 1, "due date, critical DTC and active work order all block")
	assert.Equal(t, "v-ok", rows[0].ID)
	assert.Equal(t, "REG-OK", rows[0].Number)
}
