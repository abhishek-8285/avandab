package application

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/trip/domain/aggregate"
)

func newTripTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_trip_app_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	// 00103 enforces tenant_id FK via triggers — seed test tenant used by helpers.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedTestTrip(t *testing.T, db *sql.DB, tripID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, status, departure_time, arrival_time, tenant_id)
		VALUES (?, ?, 'b-1', 'r-1', 'scheduled', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'tenant-1')`,
		tripID, "TRIP-"+tripID)
	require.NoError(t, err)
}

func seedTestVehicle(t *testing.T, db *sql.DB, vehicleID string, maintenanceDue bool) {
	t.Helper()
	var dueVal *string
	if maintenanceDue {
		today := time.Now().UTC().Format("2006-01-02")
		dueVal = &today
	}
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, status, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES (?, ?, ?, 'truck', 15, 'available', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), ?, 'tenant-1')`,
		vehicleID, "REG-"+vehicleID, "MH-01-"+vehicleID, dueVal)
	require.NoError(t, err)
}

func seedTestDriver(t *testing.T, db *sql.DB, driverID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES (?, ?, 'Rajesh', 'Kumar', '9876543210', 'DL-12345', date('now','+1 year'), 'available', 'tenant-1')`,
		driverID, "DRV-"+driverID)
	require.NoError(t, err)
}

func TestAssignVehicle_MaintenanceBlock_And_Override(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()

	tripID := uuid.NewString()
	vehID := "veh-blocked"
	driverID := "drv-v-1"

	seedTestDriver(t, db, driverID)
	seedTestTrip(t, db, tripID)
	seedTestVehicle(t, db, vehID, true) // Maintenance is due!

	// Assign driver first so domain aggregate invariant is satisfied
	assignDriverUC := NewAssignDriverUseCase(unitOfWork, clk)
	err := assignDriverUC.Execute(context.Background(), AssignDriverCommand{
		TripID:   aggregate.TripID(tripID),
		DriverID: driverID,
		TenantID: shared.TenantID("tenant-1"),
	})
	require.NoError(t, err)

	uc := NewAssignVehicleUseCase(unitOfWork, clk)

	// 1. Attempt assignment without override -> must fail with maintenance blocker error
	err = uc.Execute(context.Background(), AssignVehicleCommand{
		TripID:              aggregate.TripID(tripID),
		VehicleID:           vehID,
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance")

	// 2. Attempt assignment WITH override -> must succeed
	err = uc.Execute(context.Background(), AssignVehicleCommand{
		TripID:              aggregate.TripID(tripID),
		VehicleID:           vehID,
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: true,
		OverrideReason:      "Urgent emergency dispatch",
	})
	require.NoError(t, err)

	// Verify vehicle assigned on trip
	var assignedVeh sql.NullString
	err = db.QueryRow("SELECT vehicle_id FROM trips WHERE id = ?", tripID).Scan(&assignedVeh)
	require.NoError(t, err)
	assert.Equal(t, vehID, assignedVeh.String)

	// Verify assign_vehicle_override audit log recorded
	var auditCount int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'assign_vehicle_override' AND record_id = ?", tripID).Scan(&auditCount)
	require.NoError(t, err)
	assert.Equal(t, 1, auditCount)
}

func TestAssignDriver_TripVehicle_MaintenanceBlock_And_Override(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()

	tripID := uuid.NewString()
	vehID := "veh-blocked-d"
	driverID := "drv-d-1"

	seedTestDriver(t, db, driverID)
	seedTestTrip(t, db, tripID)
	seedTestVehicle(t, db, vehID, true)

	// Assign vehicle directly to trip in database
	_, err := db.Exec("UPDATE trips SET vehicle_id = ? WHERE id = ?", vehID, tripID)
	require.NoError(t, err)

	uc := NewAssignDriverUseCase(unitOfWork, clk)

	// 1. Assign driver without override -> must fail because trip has maintenance-blocked vehicle
	err = uc.Execute(context.Background(), AssignDriverCommand{
		TripID:              aggregate.TripID(tripID),
		DriverID:            driverID,
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: false,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maintenance")

	// 2. Assign driver WITH override -> succeeds and writes audit log
	err = uc.Execute(context.Background(), AssignDriverCommand{
		TripID:              aggregate.TripID(tripID),
		DriverID:            driverID,
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: true,
		OverrideReason:      "Driver assigned under manager approval",
	})
	require.NoError(t, err)

	var auditCount int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'assign_driver_override' AND record_id = ?", tripID).Scan(&auditCount)
	require.NoError(t, err)
	assert.Equal(t, 1, auditCount)
}
