package handlers

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
)

func newAssignmentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_assign_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	migDir := "../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migDir = "db/migrations"
	}
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedDriverForAssign(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES (?, ?, 'Rahul', 'Kumar', '9876543210', 'DL-12345', date('now','+1 year'), 'available', 'tenant-1')`,
		id, "DRV-"+id)
	require.NoError(t, err)
}

func seedVehicleForAssign(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES (?, ?, ?, 'truck', 5000, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 'tenant-1')`,
		id, "REG-"+id, "MH-01-"+id)
	require.NoError(t, err)
}

func TestPreferredAssignment_AssignAndUnassign(t *testing.T) {
	db := newAssignmentTestDB(t)
	seedDriverForAssign(t, db, "drv-1")
	seedDriverForAssign(t, db, "drv-2")
	seedVehicleForAssign(t, db, "veh-102")
	seedVehicleForAssign(t, db, "veh-103")
	ctx := context.Background()

	// Assign veh-102 to drv-1.
	require.NoError(t, assignVehicleToDriver(ctx, db, "tenant-1", "drv-1", "veh-102", "tester"))
	a, err := getActiveAssignmentByDriver(ctx, db, "drv-1", "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "veh-102", a.VehicleID)

	// Vehicle side mirrors.
	b, err := getActiveAssignmentByVehicle(ctx, db, "veh-102", "tenant-1")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "drv-1", b.DriverID)

	// Reassign same vehicle to drv-2 should move it (1:1 active).
	require.NoError(t, assignVehicleToDriver(ctx, db, "tenant-1", "drv-2", "veh-102", "tester"))
	a2, _ := getActiveAssignmentByDriver(ctx, db, "drv-2", "tenant-1")
	require.NotNil(t, a2)
	assert.Equal(t, "veh-102", a2.VehicleID)
	// drv-1 now has no active.
	a1, _ := getActiveAssignmentByDriver(ctx, db, "drv-1", "tenant-1")
	assert.Nil(t, a1, "previous driver should be unassigned")

	// Assign different vehicle to drv-2 should replace.
	require.NoError(t, assignVehicleToDriver(ctx, db, "tenant-1", "drv-2", "veh-103", "tester"))
	a3, _ := getActiveAssignmentByDriver(ctx, db, "drv-2", "tenant-1")
	require.NotNil(t, a3)
	assert.Equal(t, "veh-103", a3.VehicleID)
	// old vehicle veh-102 now free.
	b2, _ := getActiveAssignmentByVehicle(ctx, db, "veh-102", "tenant-1")
	assert.Nil(t, b2)

	// History preserved: 3 rows, 2 unassigned.
	var total, active int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1'`).Scan(&total))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1' AND unassigned_at IS NULL`).Scan(&active))
	assert.Equal(t, 3, total)
	assert.Equal(t, 1, active)

	// Unassign.
	require.NoError(t, unassignDriver(ctx, db, "tenant-1", "drv-2"))
	a4, _ := getActiveAssignmentByDriver(ctx, db, "drv-2", "tenant-1")
	assert.Nil(t, a4)
}

func TestPreferredAssignment_TenantIsolation(t *testing.T) {
	db := newAssignmentTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2','T2','t2')`)
	seedDriverForAssign(t, db, "drv-iso")
	seedVehicleForAssign(t, db, "veh-iso")
	ctx := context.Background()
	require.NoError(t, assignVehicleToDriver(ctx, db, "tenant-1", "drv-iso", "veh-iso", "tester"))
	// Other tenant sees nothing.
	a, _ := getActiveAssignmentByDriver(ctx, db, "drv-iso", "tenant-2")
	assert.Nil(t, a)
}
