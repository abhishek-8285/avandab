package application

// Focused tests for the registration-number uniqueness and create validation
// invariants of the vehicle slice. Migration 00135 makes plates unique per
// tenant (UNIQUE(tenant_id, registration_number)), not globally; these tests
// run the real use case through the SQL repository + UnitOfWork wiring the
// vehicle handler uses, so the DB constraint and the validation ordering are
// both pinned.

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
	"transport-app/internal/vehicle/domain/aggregate"
)

const vehicleRegistrationTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL CHECK (vehicle_type IN ('truck', 'mini_truck', 'bus', 'van', 'pickup', 'tempo')),
    capacity INTEGER NOT NULL,
    fuel_type TEXT NOT NULL CHECK (fuel_type IN ('diesel', 'petrol', 'gas', 'electric', 'cng')),
    insurance_expiry DATETIME NOT NULL,
    fitness_expiry DATETIME NOT NULL,
    permit_expiry DATETIME NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('available', 'running', 'maintenance', 'inactive', 'blocked')),
    current_mileage REAL,
    blocked INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT,
    rc_expiry DATETIME,
    odometer REAL NOT NULL DEFAULT 0.0,
    puc_expiry DATETIME,
    tenant_id TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    fleet_class TEXT NOT NULL DEFAULT 'CV' CHECK (fleet_class IN ('CV', 'PV', 'FS', 'OFS')),
    ownership TEXT NOT NULL DEFAULT 'O' CHECK (ownership IN ('O', 'C', 'F', 'M')),
    fleet_number TEXT,
    description TEXT,
    manufacturer TEXT,
    manuf_country TEXT,
    model TEXT,
    constr_year_month TEXT,
    acquisition_value REAL,
    acquisition_currency TEXT NOT NULL DEFAULT 'INR',
    acquisition_date DATETIME,
    purchase_vendor TEXT,
    valid_from DATETIME,
    valid_to DATETIME,
    facility_id TEXT,
    maint_plant TEXT,
    planning_plant TEXT,
    company_code TEXT,
    business_area TEXT,
    cost_center TEXT,
    asset_no TEXT,
    fleet_object_no TEXT,
    chassis_no TEXT,
    vehicle_category TEXT,
    engine_number TEXT,
    engine_power TEXT,
    engine_capacity TEXT,
    cylinder_count INTEGER,
    max_speed REAL,
    weight REAL,
    weight_unit TEXT NOT NULL DEFAULT 'TO' CHECK (weight_unit IN ('TO', 'KG')),
    load_volume REAL,
    volume_unit TEXT,
    secondary_fuel TEXT,
    usage_indicator TEXT,
    standard_kmpl REAL,
    UNIQUE (tenant_id, registration_number)
);
CREATE TABLE vehicle_measuring_points (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  vehicle_id TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT 'M',
  kind TEXT NOT NULL,
  meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
  unit TEXT NOT NULL DEFAULT 'KM',
  decimal_places INTEGER NOT NULL DEFAULT 0,
  annual_estimate REAL,
  count_backwards INTEGER NOT NULL DEFAULT 0,
  is_counter INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE vehicle_measurements (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  point_id TEXT NOT NULL,
  doc_number TEXT,
  counter_reading REAL NOT NULL,
  difference_reading REAL,
  total_counter_reading REAL,
  measured_at DATETIME,
  read_by TEXT,
  remarks TEXT,
  recorded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  recorded_by TEXT
);
CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    published_at DATETIME
);
`

func openVehicleRegistrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", "-", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(vehicleRegistrationTestSchema)
	require.NoError(t, err)
	return db
}

func newRegistrationTestCommand(tenantID, registrationNumber string) CreateVehicleCommand {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	return CreateVehicleCommand{
		TenantID:           shared.TenantID(tenantID),
		RegistrationNumber: registrationNumber,
		VehicleNumber:      "VN-" + registrationNumber,
		VehicleType:        aggregate.VehicleTypeTruck,
		Capacity:           5000,
		FuelType:           aggregate.FuelTypeDiesel,
		InsuranceExpiry:    now.AddDate(1, 0, 0),
		FitnessExpiry:      now.AddDate(1, 0, 0),
		PermitExpiry:       now.AddDate(1, 0, 0),
		Odometer:           100,
	}
}

func TestCreateVehicle_RegistrationNumberUniquenessIsPerTenant(t *testing.T) {
	db := openVehicleRegistrationTestDB(t)
	uc := NewCreateVehicleUseCase(uow.NewSQLUnitOfWork(db), id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	_, err := uc.Execute(ctx, newRegistrationTestCommand("tenant-a", "MH01AB1234"))
	require.NoError(t, err)

	// Same plate in the same tenant is rejected by the
	// UNIQUE(tenant_id, registration_number) constraint (migration 00135).
	_, err = uc.Execute(ctx, newRegistrationTestCommand("tenant-a", "MH01AB1234"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UNIQUE")

	// The same plate in another tenant is allowed: plates are per-tenant.
	_, err = uc.Execute(ctx, newRegistrationTestCommand("tenant-b", "MH01AB1234"))
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vehicles WHERE registration_number = ?`, "MH01AB1234").Scan(&count))
	assert.Equal(t, 2, count)
	var tenants int
	require.NoError(t, db.QueryRow(`SELECT COUNT(DISTINCT tenant_id) FROM vehicles WHERE registration_number = ?`, "MH01AB1234").Scan(&tenants))
	assert.Equal(t, 2, tenants)
}

func TestCreateVehicle_ValidationFailsBeforePersistence(t *testing.T) {
	db := openVehicleRegistrationTestDB(t)
	uc := NewCreateVehicleUseCase(uow.NewSQLUnitOfWork(db), id.NewUUIDGenerator(), clock.NewRealClock())
	ctx := context.Background()

	t.Run("missing registration and vehicle number", func(t *testing.T) {
		cmd := newRegistrationTestCommand("tenant-a", "")
		cmd.VehicleNumber = ""
		_, err := uc.Execute(ctx, cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "registration number and vehicle number are required")
	})

	t.Run("invalid fuel type fails closed", func(t *testing.T) {
		cmd := newRegistrationTestCommand("tenant-a", "MH01AB9999")
		cmd.FuelType = aggregate.FuelType("rocket")
		_, err := uc.Execute(ctx, cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid fuel type")
	})

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vehicles`).Scan(&count))
	assert.Zero(t, count, "validation errors must never persist a vehicle")
}
