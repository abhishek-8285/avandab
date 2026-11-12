package application

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Driver-vehicle preferred assignment characterization (handlers-end-state ADR,
// Phase 2). The two TestPreferredAssignment_* cases are the legacy
// internal/handlers/assignment_test.go cases, moved here with their assertions
// unchanged; the remaining cases pin tenant isolation, the active-assignment
// uniqueness invariant and the not-found/validation contract of the new
// service seam.

func newAssignmentTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_assign_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	cwd, _ := os.Getwd()
	migDir := ""
	for _, cand := range []string{"../../../db/migrations", "../../db/migrations", "../db/migrations", "db/migrations"} {
		if _, statErr := os.Stat(cand); statErr == nil {
			migDir = cand
			break
		}
	}
	require.NotEmpty(t, migDir, "db/migrations not found from %s", cwd)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedDriverForAssign(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	seedDriverInTenant(t, db, "tenant-1", id)
}

func seedDriverInTenant(t *testing.T, db *sql.DB, tenantID, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES (?, ?, 'Rahul', 'Kumar', '9876543210', 'DL-12345', date('now','+1 year'), 'available', ?)`,
		id, "DRV-"+id, tenantID)
	require.NoError(t, err)
}

func seedVehicleForAssign(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	seedVehicleInTenant(t, db, "tenant-1", id)
}

func seedVehicleInTenant(t *testing.T, db *sql.DB, tenantID, id string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, tenant_id)
		VALUES (?, ?, ?, 'truck', 5000, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), ?)`,
		id, "REG-"+id, "MH-01-"+id, tenantID)
	require.NoError(t, err)
}

func TestPreferredAssignment_AssignAndUnassign(t *testing.T) {
	db := newAssignmentTestDB(t)
	seedDriverForAssign(t, db, "drv-1")
	seedDriverForAssign(t, db, "drv-2")
	seedVehicleForAssign(t, db, "veh-102")
	seedVehicleForAssign(t, db, "veh-103")
	ctx := context.Background()
	svc := NewAssignmentService(db)

	// Assign veh-102 to drv-1.
	require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-1", VehicleID: "veh-102", AssignedBy: "tester"}))
	a, err := svc.ActiveByDriver(ctx, "tenant-1", "drv-1")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "veh-102", a.VehicleID)

	// Vehicle side mirrors.
	b, err := svc.ActiveByVehicle(ctx, "tenant-1", "veh-102")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, "drv-1", b.DriverID)

	// Reassign same vehicle to drv-2 should move it (1:1 active).
	require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-2", VehicleID: "veh-102", AssignedBy: "tester"}))
	a2, _ := svc.ActiveByDriver(ctx, "tenant-1", "drv-2")
	require.NotNil(t, a2)
	assert.Equal(t, "veh-102", a2.VehicleID)
	// drv-1 now has no active.
	a1, _ := svc.ActiveByDriver(ctx, "tenant-1", "drv-1")
	assert.Nil(t, a1, "previous driver should be unassigned")

	// Assign different vehicle to drv-2 should replace.
	require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-2", VehicleID: "veh-103", AssignedBy: "tester"}))
	a3, _ := svc.ActiveByDriver(ctx, "tenant-1", "drv-2")
	require.NotNil(t, a3)
	assert.Equal(t, "veh-103", a3.VehicleID)
	// old vehicle veh-102 now free.
	b2, _ := svc.ActiveByVehicle(ctx, "tenant-1", "veh-102")
	assert.Nil(t, b2)

	// History preserved: 3 rows, 2 unassigned.
	var total, active int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1'`).Scan(&total))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1' AND unassigned_at IS NULL`).Scan(&active))
	assert.Equal(t, 3, total)
	assert.Equal(t, 1, active)

	// Unassign.
	require.NoError(t, svc.UnassignDriver(ctx, "tenant-1", "drv-2"))
	a4, _ := svc.ActiveByDriver(ctx, "tenant-1", "drv-2")
	assert.Nil(t, a4)
}

func TestPreferredAssignment_TenantIsolation(t *testing.T) {
	db := newAssignmentTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2','T2','t2')`)
	seedDriverForAssign(t, db, "drv-iso")
	seedVehicleForAssign(t, db, "veh-iso")
	ctx := context.Background()
	svc := NewAssignmentService(db)
	require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-iso", VehicleID: "veh-iso", AssignedBy: "tester"}))
	// Other tenant sees nothing.
	a, _ := svc.ActiveByDriver(ctx, "tenant-2", "drv-iso")
	assert.Nil(t, a)
	b, _ := svc.ActiveByVehicle(ctx, "tenant-2", "veh-iso")
	assert.Nil(t, b)
}

func TestPreferredAssignment_NotFoundAndValidation(t *testing.T) {
	db := newAssignmentTestDB(t)
	ctx := context.Background()
	svc := NewAssignmentService(db)

	// Unknown driver/vehicle reads stay (nil, nil), like the legacy helpers.
	a, err := svc.ActiveByDriver(ctx, "tenant-1", "drv-missing")
	require.NoError(t, err)
	assert.Nil(t, a)
	b, err := svc.ActiveByVehicle(ctx, "tenant-1", "veh-missing")
	require.NoError(t, err)
	assert.Nil(t, b)

	// Empty ids short-circuit with sql.ErrNoRows (legacy handler contract).
	require.ErrorIs(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", VehicleID: "veh-missing"}), sql.ErrNoRows)
	require.ErrorIs(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-missing"}), sql.ErrNoRows)

	// Unknown driver (vehicle exists in the tenant).
	seedVehicleForAssign(t, db, "veh-known")
	err = svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-missing", VehicleID: "veh-known", AssignedBy: "tester"})
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Unknown vehicle (driver exists in the tenant).
	seedDriverForAssign(t, db, "drv-known")
	err = svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-known", VehicleID: "veh-missing", AssignedBy: "tester"})
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Rejected assignments must not write history rows.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-1'`).Scan(&count))
	assert.Zero(t, count)
}

func TestPreferredAssignment_TenantScopedWrites(t *testing.T) {
	db := newAssignmentTestDB(t)
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-2','T2','t2')`)
	seedDriverForAssign(t, db, "drv-1")
	seedVehicleForAssign(t, db, "veh-1")
	ctx := context.Background()
	svc := NewAssignmentService(db)
	require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: "drv-1", VehicleID: "veh-1", AssignedBy: "tester"}))

	// Writes issued for another tenant are no-ops on the tenant-1 active row.
	require.NoError(t, svc.UnassignDriver(ctx, "tenant-2", "drv-1"))
	require.NoError(t, svc.UnassignVehicle(ctx, "tenant-2", "veh-1"))
	a, err := svc.ActiveByDriver(ctx, "tenant-1", "drv-1")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "veh-1", a.VehicleID)

	// A cross-tenant assignment is rejected by the tenant-scoped existence
	// checks (the driver/vehicle rows belong to tenant-1 only).
	err = svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-2", DriverID: "drv-1", VehicleID: "veh-1", AssignedBy: "intruder"})
	require.ErrorIs(t, err, sql.ErrNoRows)
	a, _ = svc.ActiveByDriver(ctx, "tenant-1", "drv-1")
	require.NotNil(t, a)

	var foreign int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_preferred_vehicles WHERE tenant_id='tenant-2'`).Scan(&foreign))
	assert.Zero(t, foreign)
}

func TestPreferredAssignment_ActiveUniquenessAndLegacyRowShape(t *testing.T) {
	db := newAssignmentTestDB(t)
	seedDriverForAssign(t, db, "drv-1")
	seedDriverForAssign(t, db, "drv-2")
	seedVehicleForAssign(t, db, "veh-1")
	seedVehicleForAssign(t, db, "veh-2")
	ctx := context.Background()
	svc := NewAssignmentService(db)
	assign := func(driverID, vehicleID string) {
		t.Helper()
		require.NoError(t, svc.AssignVehicleToDriver(ctx, AssignVehicleToDriverCommand{TenantID: "tenant-1", DriverID: driverID, VehicleID: vehicleID, AssignedBy: "tester"}))
	}
	assign("drv-1", "veh-1")
	assign("drv-1", "veh-2") // driver reassigned to the other vehicle
	assign("drv-2", "veh-1") // vehicle reassigned to the other driver

	// At most one active assignment per driver and per vehicle (1:1 active).
	rows, err := db.Query(`SELECT driver_id FROM driver_preferred_vehicles WHERE tenant_id='tenant-1' AND unassigned_at IS NULL GROUP BY driver_id HAVING COUNT(*) > 1`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	assert.False(t, rows.Next(), "a driver must never have two active assignments")
	rowsV, err := db.Query(`SELECT vehicle_id FROM driver_preferred_vehicles WHERE tenant_id='tenant-1' AND unassigned_at IS NULL GROUP BY vehicle_id HAVING COUNT(*) > 1`)
	require.NoError(t, err)
	defer func() { _ = rowsV.Close() }()
	assert.False(t, rowsV.Next(), "a vehicle must never have two active assignments")

	// The active row keeps the legacy shape: "dva-" id, is_primary=1 and the
	// actor recorded in assigned_by.
	var id, assignedBy string
	var isPrimary int
	require.NoError(t, db.QueryRow(`SELECT id, is_primary, assigned_by FROM driver_preferred_vehicles WHERE tenant_id='tenant-1' AND driver_id='drv-2' AND unassigned_at IS NULL`).Scan(&id, &isPrimary, &assignedBy))
	assert.True(t, strings.HasPrefix(id, "dva-"), "legacy assignment id prefix preserved, got %q", id)
	assert.Equal(t, 1, isPrimary)
	assert.Equal(t, "tester", assignedBy)
}
