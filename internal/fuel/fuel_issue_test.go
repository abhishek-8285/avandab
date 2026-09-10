package fuel

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupFuelIssueTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:fuel_issue_%d?mode=memory&cache=shared", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE tenants (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE vehicles (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_number TEXT NOT NULL,
			registration_number TEXT NOT NULL,
			fleet_class TEXT NOT NULL DEFAULT 'CV',
			status TEXT NOT NULL DEFAULT 'available',
			blocked INTEGER NOT NULL DEFAULT 0,
			blocked_reason TEXT,
			valid_to DATETIME,
			odometer REAL NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		);
		CREATE TABLE vehicle_measuring_points (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			vehicle_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			meas_position TEXT NOT NULL DEFAULT 'DISTANCE',
			unit TEXT NOT NULL DEFAULT 'KM',
			created_at DATETIME NOT NULL DEFAULT (datetime('now'))
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
			recorded_at DATETIME NOT NULL DEFAULT (datetime('now')),
			recorded_by TEXT
		);
		CREATE TABLE fuel_issues (
			id                TEXT PRIMARY KEY,
			tenant_id         TEXT NOT NULL,
			issue_number      TEXT,
			fuel_station_id   TEXT NOT NULL,
			pump_point_id     TEXT,
			vehicle_id        TEXT NOT NULL,
			driver_id         TEXT,
			trip_id           TEXT,
			fuel_type         TEXT NOT NULL DEFAULT 'diesel',
			opening_reading   REAL NOT NULL DEFAULT 0,
			closing_reading   REAL NOT NULL DEFAULT 0,
			litres_issued     REAL NOT NULL CHECK (litres_issued > 0),
			vehicle_odometer  REAL,
			rate_per_litre    REAL,
			total_cost        REAL,
			remarks           TEXT NOT NULL DEFAULT '',
			issued_at         DATETIME NOT NULL DEFAULT (datetime('now')),
			created_by        TEXT NOT NULL DEFAULT '',
			created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
			updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
		);
	`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO tenants (id, name) VALUES ('tenant-A', 'Tenant A'), ('tenant-B', 'Tenant B')`)
	require.NoError(t, err)

	return db
}

func TestFuelIssue_StationValidation(t *testing.T) {
	db := setupFuelIssueTestDB(t)
	repo := NewSQLFuelIssueRepository(db)
	ctx := context.Background()

	// 1. Missing station
	_, err := repo.RecordFuelIssue(ctx, "tenant-A", "user-1", RecordFuelIssueRequest{
		FuelStationID:  "nonexistent-station",
		VehicleID:      "veh-1",
		OpeningReading: 100,
		ClosingReading: 200,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fuel station nonexistent-station not found")

	// 2. Expired station (valid_to in past)
	pastDate := time.Now().Add(-48 * time.Hour)
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class, valid_to)
		VALUES ('station-exp', 'tenant-A', 'FS-01', 'REG-FS-01', 'FS', ?)`, pastDate)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class)
		VALUES ('veh-1', 'tenant-A', 'TR-01', 'REG-TR-01', 'CV')`)
	require.NoError(t, err)

	_, err = repo.RecordFuelIssue(ctx, "tenant-A", "user-1", RecordFuelIssueRequest{
		FuelStationID:  "station-exp",
		VehicleID:      "veh-1",
		OpeningReading: 100,
		ClosingReading: 200,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fuel station station-exp validity expired")
}

func TestFuelIssue_VehicleComplianceValidation(t *testing.T) {
	db := setupFuelIssueTestDB(t)
	repo := NewSQLFuelIssueRepository(db)
	ctx := context.Background()

	futureDate := time.Now().Add(365 * 24 * time.Hour)
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class, valid_to)
		VALUES ('station-ok', 'tenant-A', 'FS-01', 'REG-FS-01', 'FS', ?)`, futureDate)
	require.NoError(t, err)

	// Blocked receiving vehicle
	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class, blocked, blocked_reason)
		VALUES ('veh-blocked', 'tenant-A', 'TR-02', 'REG-TR-02', 'CV', 1, 'Insurance expired')`)
	require.NoError(t, err)

	_, err = repo.RecordFuelIssue(ctx, "tenant-A", "user-1", RecordFuelIssueRequest{
		FuelStationID:  "station-ok",
		VehicleID:      "veh-blocked",
		OpeningReading: 100,
		ClosingReading: 200,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dispatch/fuel issue blocked: Insurance expired")
}

func TestFuelIssue_ReadingMonotonicityAndCalculation(t *testing.T) {
	db := setupFuelIssueTestDB(t)
	repo := NewSQLFuelIssueRepository(db)
	ctx := context.Background()

	futureDate := time.Now().Add(365 * 24 * time.Hour)
	_, err := db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class, valid_to)
		VALUES ('station-ok', 'tenant-A', 'FS-01', 'REG-FS-01', 'FS', ?)`, futureDate)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, fleet_class, odometer)
		VALUES ('veh-ok', 'tenant-A', 'TR-03', 'REG-TR-03', 'CV', 50000)`)
	require.NoError(t, err)

	// Closing <= Opening
	_, err = repo.RecordFuelIssue(ctx, "tenant-A", "user-1", RecordFuelIssueRequest{
		FuelStationID:  "station-ok",
		VehicleID:      "veh-ok",
		OpeningReading: 200,
		ClosingReading: 150,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closing_reading must be greater than opening_reading")

	// Setup measuring point on pump
	_, err = db.Exec(`INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, unit)
		VALUES ('pump-1', 'tenant-A', 'station-ok', 'PUMP', 'L')`)
	require.NoError(t, err)

	// Setup topup point on truck
	_, err = db.Exec(`INSERT INTO vehicle_measuring_points (id, tenant_id, vehicle_id, kind, unit)
		VALUES ('topup-1', 'tenant-A', 'veh-ok', 'FUEL_TOPUP', 'L')`)
	require.NoError(t, err)

	// Successful fuel issue
	pumpID := "pump-1"
	odo := 50250.0
	rate := 92.50
	issueNum := "SLIP-2026-001"
	res, err := repo.RecordFuelIssue(ctx, "tenant-A", "operator-1", RecordFuelIssueRequest{
		IssueNumber:     &issueNum,
		FuelStationID:   "station-ok",
		PumpPointID:     &pumpID,
		VehicleID:       "veh-ok",
		OpeningReading:  10000.0,
		ClosingReading:  10120.0,
		VehicleOdometer: &odo,
		RatePerLitre:    &rate,
		Remarks:         "Routine tank fill",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, 120.0, res.LitresIssued)
	assert.InDelta(t, 11100.0, *res.TotalCost, 0.01) // 120 * 92.50
	assert.Equal(t, "operator-1", res.CreatedBy)

	// Verify vehicle odometer was updated
	var updatedOdo float64
	err = db.QueryRow(`SELECT odometer FROM vehicles WHERE id = 'veh-ok'`).Scan(&updatedOdo)
	require.NoError(t, err)
	assert.Equal(t, 50250.0, updatedOdo)

	// Verify pump measurement document was created
	var pumpCounter, pumpDiff float64
	err = db.QueryRow(`SELECT counter_reading, difference_reading FROM vehicle_measurements WHERE point_id = 'pump-1'`).Scan(&pumpCounter, &pumpDiff)
	require.NoError(t, err)
	assert.Equal(t, 10120.0, pumpCounter)
	assert.Equal(t, 120.0, pumpDiff)

	// Verify vehicle top-up measurement document was created
	var topupCounter, topupDiff float64
	err = db.QueryRow(`SELECT counter_reading, difference_reading FROM vehicle_measurements WHERE point_id = 'topup-1'`).Scan(&topupCounter, &topupDiff)
	require.NoError(t, err)
	assert.Equal(t, 120.0, topupCounter)
	assert.Equal(t, 120.0, topupDiff)

	// Verify List and Get with Tenant Isolation
	list, err := repo.ListFuelIssues(ctx, "tenant-A", FuelIssueFilter{})
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// Tenant B sees nothing
	ghostList, err := repo.ListFuelIssues(ctx, "tenant-B", FuelIssueFilter{})
	require.NoError(t, err)
	assert.Empty(t, ghostList)

	ghostGet, err := repo.GetFuelIssue(ctx, "tenant-B", res.ID)
	require.NoError(t, err)
	assert.Nil(t, ghostGet)
}
