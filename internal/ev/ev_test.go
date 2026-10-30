package ev_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/ev"
)

func newTestEVDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_ev_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	// Seed tenants
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-a', 'Tenant A', 'tenant-a'),
		('tenant-b', 'Tenant B', 'tenant-b')`)
	require.NoError(t, err)

	// Seed vehicles
	_, err = db.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		('ev-001', 'tenant-a', 'KA-01-EV-1001', 'EV-TRK-01', 'truck', 15.0, 'electric', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 12500.0),
		('ev-002', 'tenant-a', 'KA-01-EV-1002', 'EV-VAN-02', 'van', 5.0, 'electric', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 8400.0),
		('ev-b01', 'tenant-b', 'MH-02-EV-9001', 'EV-MUM-01', 'truck', 20.0, 'electric', date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 5000.0)`)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEVProfile_ConfigureAndFetch(t *testing.T) {
	db := newTestEVDB(t)
	repo := ev.NewSQLRepository(db)
	svc := ev.NewService(repo)
	ctx := context.Background()

	// 1. Configure EV Profile
	profile, err := svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh:    60.0,
		MaxRangeKM:            280.0,
		ChargingStandard:      ev.ChargingStandardCCS2,
		MinSafeSoC:            20.0,
		NominalEfficiencyWhKM: 210.0,
	})
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "ev-001", profile.VehicleID)
	assert.Equal(t, 60.0, profile.BatteryCapacityKWh)
	assert.Equal(t, 280.0, profile.MaxRangeKM)
	assert.Equal(t, 20.0, profile.MinSafeSoC)

	// 2. Fetch Profile
	fetched, err := svc.GetProfile(ctx, "tenant-a", "ev-001")
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, profile.BatteryCapacityKWh, fetched.BatteryCapacityKWh)

	// 3. Validation failure
	_, err = svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh: -5.0,
		MaxRangeKM:         200.0,
	})
	assert.Error(t, err)
}

func TestEVTelemetry_IngestAndDynamicCalculations(t *testing.T) {
	db := newTestEVDB(t)
	repo := ev.NewSQLRepository(db)
	svc := ev.NewService(repo)
	ctx := context.Background()

	// Configure profile first
	_, err := svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh: 80.0,
		MaxRangeKM:         300.0,
		ChargingStandard:   ev.ChargingStandardCCS2,
		MinSafeSoC:         15.0,
	})
	require.NoError(t, err)

	// Ingest telemetry without explicit remaining range and charging power
	temp := 32.5
	telem, err := svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:      "ev-001",
		SoCPercent:     75.0,
		SoHPercent:     98.0,
		BatteryTempC:   &temp,
		ChargingStatus: ev.ChargingStatusDischarging,
		VoltageV:       400.0,
		CurrentDrawA:   25.0,
	})
	require.NoError(t, err)
	require.NotNil(t, telem)

	// Remaining range should be dynamically computed: 75% of 300 km = 225 km
	assert.Equal(t, 225.0, telem.RemainingRangeKM)
	// Power should be dynamically computed: 400V * 25A / 1000 = 10 kW
	assert.Equal(t, 10.0, telem.ChargingPowerKW)

	// Verify latest telemetry in repo
	latest, err := svc.GetLatestTelemetry(ctx, "tenant-a", "ev-001")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, 75.0, latest.SoCPercent)
	assert.Equal(t, 98.0, latest.SoHPercent)
}

func TestEVChargingSession_AutoLifecycle(t *testing.T) {
	db := newTestEVDB(t)
	repo := ev.NewSQLRepository(db)
	svc := ev.NewService(repo)
	ctx := context.Background()

	// Configure profile
	_, err := svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh: 100.0,
		MaxRangeKM:         400.0,
		ChargingStandard:   ev.ChargingStandardCCS2,
		MinSafeSoC:         15.0,
	})
	require.NoError(t, err)

	// 1. Vehicle plugs in at 20% SoC with fast charger
	_, err = svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:       "ev-001",
		SoCPercent:      20.0,
		ChargingStatus:  ev.ChargingStatusFastCharging,
		ChargingPowerKW: 50.0,
	})
	require.NoError(t, err)

	// Verify session opened
	sessions, total, err := svc.ListChargingSessions(ctx, "tenant-a", "ev-001", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, ev.SessionStatusInProgress, sessions[0].Status)
	assert.Equal(t, ev.ChargerTypeDCFast, sessions[0].ChargerType)
	assert.Equal(t, 20.0, sessions[0].StartSoC)
	assert.Equal(t, 50.0, sessions[0].PeakPowerKW)

	// 2. Charging progresses to 70% with peak power jump to 65kW
	_, err = svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:       "ev-001",
		SoCPercent:      70.0,
		ChargingStatus:  ev.ChargingStatusFastCharging,
		ChargingPowerKW: 65.0,
	})
	require.NoError(t, err)

	sessions, _, err = svc.ListChargingSessions(ctx, "tenant-a", "ev-001", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 65.0, sessions[0].PeakPowerKW)
	// Energy added so far: (70 - 20)/100 * 100 kWh = 50 kWh
	assert.Equal(t, 50.0, sessions[0].EnergyAddedKWh)

	// 3. Charging complete and vehicle begins discharging at 90% SoC
	_, err = svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:      "ev-001",
		SoCPercent:     90.0,
		ChargingStatus: ev.ChargingStatusDischarging,
	})
	require.NoError(t, err)

	sessions, _, err = svc.ListChargingSessions(ctx, "tenant-a", "ev-001", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, ev.SessionStatusCompleted, sessions[0].Status)
	assert.NotNil(t, sessions[0].EndSoC)
	assert.Equal(t, 90.0, *sessions[0].EndSoC)
	// Final energy added: (90 - 20)/100 * 100 = 70 kWh
	assert.Equal(t, 70.0, sessions[0].EnergyAddedKWh)
	// Estimated cost: 70 kWh * 14.50 INR/kWh = 1015.0 INR
	assert.Equal(t, 1015.0, sessions[0].EstimatedCost)
}

func TestEVFleetMetrics_AndCarbonAvoided(t *testing.T) {
	db := newTestEVDB(t)
	repo := ev.NewSQLRepository(db)
	svc := ev.NewService(repo)
	ctx := context.Background()

	// Configure profiles and telemetry for 2 vehicles
	_, err := svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh: 80.0,
		MaxRangeKM:         300.0,
		MinSafeSoC:         20.0,
	})
	require.NoError(t, err)

	// ev-001 has 12% SoC (below safe threshold 20% -> Low Battery Alert)
	_, err = svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:      "ev-001",
		SoCPercent:     12.0,
		ChargingStatus: ev.ChargingStatusDischarging,
	})
	require.NoError(t, err)

	// ev-002 is actively charging at 60%
	_, err = svc.IngestTelemetry(ctx, "tenant-a", ev.IngestEVTelemetryInput{
		VehicleID:       "ev-002",
		SoCPercent:      60.0,
		ChargingStatus:  ev.ChargingStatusCharging,
		ChargingPowerKW: 22.0,
	})
	require.NoError(t, err)

	// Record trip green energy summary
	summary, err := svc.RecordEnergySummary(ctx, "tenant-a", ev.RecordEnergySummaryInput{
		VehicleID:         "ev-001",
		DistanceKM:        200.0,
		EnergyConsumedKWh: 40.0,
	})
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 200.0, summary.DistanceKM)
	// CO2 avoided: 200 km * 0.55 kg/km = 110.0 kg
	assert.Equal(t, 110.0, summary.CO2AvoidedKG)
	// Efficiency: 40 kWh * 1000 / 200 km = 200 Wh/km
	assert.Equal(t, 200.0, summary.EfficiencyWhKM)

	// Fetch Fleet Metrics
	metrics, err := svc.GetFleetMetrics(ctx, "tenant-a")
	require.NoError(t, err)
	require.NotNil(t, metrics)
	assert.Equal(t, 2, metrics.TotalEVs)
	assert.Equal(t, 1, metrics.ActiveChargingCount)
	assert.Equal(t, 1, metrics.LowBatteryAlerts) // ev-001 has 12% < 20%
	assert.Equal(t, 200.0, metrics.TotalGreenDistanceKM)
	assert.Equal(t, 110.0, metrics.TotalCO2AvoidedKG)

	// Check EV vehicles list
	vehList, err := svc.ListEVVehicles(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Len(t, vehList, 2)
	for _, v := range vehList {
		if v.VehicleID == "ev-001" {
			assert.True(t, v.IsLowBattery)
		}
	}
}

func TestEVTenantIsolation(t *testing.T) {
	db := newTestEVDB(t)
	repo := ev.NewSQLRepository(db)
	svc := ev.NewService(repo)
	ctx := context.Background()

	// Tenant A setup
	_, err := svc.ConfigureProfile(ctx, "tenant-a", "ev-001", ev.ConfigureEVProfileInput{
		BatteryCapacityKWh: 60.0,
		MaxRangeKM:         250.0,
	})
	require.NoError(t, err)

	// Tenant B cannot see Tenant A's profile
	profB, err := svc.GetProfile(ctx, "tenant-b", "ev-001")
	require.NoError(t, err)
	assert.Nil(t, profB)

	// Tenant B EV list only contains ev-b01
	bList, err := svc.ListEVVehicles(ctx, "tenant-b")
	require.NoError(t, err)
	assert.Len(t, bList, 1)
	assert.Equal(t, "ev-b01", bList[0].VehicleID)
}
