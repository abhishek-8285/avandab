package sustainability_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/sustainability"
	esgapp "transport-app/internal/sustainability/application"
)

func setupESGTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_esg_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	return db
}

func TestESG_FormulaAndZeroDivisionSafety(t *testing.T) {
	// 1. Primary Method (Fuel Consumed > 0)
	co2e, intensity, meth := sustainability.CalculateEmissions(200.0, 15.0, 50.0, sustainability.EmissionNormBS6)
	assert.Equal(t, sustainability.MethodologyFuelPrimary, meth)
	assert.Equal(t, 50.0*2.68, co2e)
	assert.InDelta(t, (50.0*2.68)/(200.0*15.0), intensity, 0.0001)

	// 2. CNG Fuel Primary Method
	co2eCNG, _, methCNG := sustainability.CalculateEmissions(300.0, 10.0, 40.0, sustainability.EmissionNormCNG)
	assert.Equal(t, sustainability.MethodologyFuelPrimary, methCNG)
	assert.Equal(t, 40.0*2.75, co2eCNG)

	// 3. Fallback Activity Method (BS6: 0.092 kg/tkm)
	co2eBS6, intensityBS6, methBS6 := sustainability.CalculateEmissions(500.0, 20.0, 0, sustainability.EmissionNormBS6)
	assert.Equal(t, sustainability.MethodologyDistanceActivity, methBS6)
	expectedTKM := 500.0 * 20.0
	assert.Equal(t, expectedTKM*0.092, co2eBS6)
	assert.InDelta(t, 0.092, intensityBS6, 0.0001)

	// 4. Fallback Activity Method (EV: 0 kg/tkm direct)
	co2eEV, intensityEV, methEV := sustainability.CalculateEmissions(400.0, 10.0, 0, sustainability.EmissionNormEV)
	assert.Equal(t, sustainability.MethodologyDistanceActivity, methEV)
	assert.Equal(t, 0.0, co2eEV)
	assert.Equal(t, 0.0, intensityEV)

	// 5. Zero-Division Safety Guards
	// Distance = 0, Payload = 0 -> Must NOT be NaN or Inf
	co2eZero, intensityZero, _ := sustainability.CalculateEmissions(0, 0, 0, sustainability.EmissionNormBS6)
	assert.False(t, math.IsNaN(co2eZero))
	assert.False(t, math.IsInf(co2eZero, 0))
	assert.False(t, math.IsNaN(intensityZero))
	assert.False(t, math.IsInf(intensityZero, 0))
	assert.Equal(t, 0.0, co2eZero)
	assert.Equal(t, 0.0, intensityZero)

	// Negative values clamped safely
	co2eNeg, intensityNeg, _ := sustainability.CalculateEmissions(-100, -5, -20, sustainability.EmissionNormBS6)
	assert.False(t, math.IsNaN(co2eNeg))
	assert.Equal(t, 0.0, co2eNeg)
	assert.Equal(t, 0.0, intensityNeg)
}

func TestESG_TripCalculationAndCertificate(t *testing.T) {
	db := setupESGTestDB(t)
	tenantID := "tenant-esg-1"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Green Express', 'green-exp')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, tank_capacity_litres)
		VALUES ('veh-esg-1', $1, 'DL01EV9999', 'DL01EV9999', 'truck', 15000, 'electric', 0)`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('route-esg-1', $1, 'Delhi', 'Jaipur', 270.0, 5.0, 5000.0)`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('cust-1', $1, 'Cust Corp', '9999999999')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, pickup_date, cargo_weight, price, status)
		VALUES ('book-esg-1', $1, 'BK-001', 'cust-1', 'route-esg-1', 'truck', datetime('now'), 12000.0, 15000.0, 'confirmed')`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trips (id, tenant_id, trip_number, route_id, booking_id, vehicle_id, departure_time, status)
		VALUES ('trip-esg-1', $1, 'TRIP-ESG-001', 'route-esg-1', 'book-esg-1', 'veh-esg-1', datetime('now'), 'completed')`, tenantID)
	require.NoError(t, err)

	repo := sustainability.NewSQLESGRepository(db)
	useCase := esgapp.NewESGUsecase(repo)

	ctx := context.Background()

	// 1. Calculate trip ESG
	metrics, err := useCase.CalculateTripESG(ctx, tenantID, "trip-esg-1")
	require.NoError(t, err)
	assert.NotEmpty(t, metrics.ID)
	assert.Equal(t, 270.0, metrics.DistanceKM)
	assert.Equal(t, 12.0, metrics.PayloadTonnes)
	assert.Equal(t, sustainability.EmissionNormEV, metrics.EmissionNorm)
	assert.Equal(t, sustainability.MethodologyDistanceActivity, metrics.Methodology)
	assert.Equal(t, 0.0, metrics.CO2eKG)

	// 2. Retrieve Carbon Certificate
	cert, err := useCase.GetTripCarbonCertificate(ctx, tenantID, "trip-esg-1")
	require.NoError(t, err)
	assert.Equal(t, "trip-esg-1", cert.TripID)
	assert.Equal(t, "TRIP-ESG-001", cert.TripNumber)
	assert.Equal(t, "DL01EV9999", cert.VehicleNumber)
	assert.Equal(t, 3240.0, cert.CargoTKM) // 270 km * 12 tonnes = 3240 tkm
	assert.Equal(t, 0.0, cert.CO2eKG)
}

func TestESG_PeriodicSnapshotAndBRSRReport(t *testing.T) {
	db := setupESGTestDB(t)
	tenantID := "tenant-esg-brsr"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ($1, 'Eco Fleet', 'eco-fleet')`, tenantID)
	require.NoError(t, err)

	// Create 2 vehicles (1 EV, 1 Diesel BS-VI)
	_, err = db.Exec(`
		INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, tank_capacity_litres)
		VALUES 
		('v-ev', $1, 'DL01EV1111', 'DL01EV1111', 'truck', 10000, 'electric', 0),
		('v-d', $1, 'DL01DL2222', 'DL01DL2222', 'truck', 20000, 'diesel', 300.0)`, tenantID)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r-1', $1, 'Depot A', 'Depot B', 500.0, 10.0, 10000.0)`, tenantID)
	require.NoError(t, err)

	// Create Trip 1 (Diesel with fuel consumed)
	now := time.Now().UTC()
	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, route_id, vehicle_id, departure_time, completed_at, fuel_consumed_liters, status)
		VALUES ('t-1', $1, 'TRIP-D-01', 'r-1', 'v-d', $2, $2, 100.0, 'completed')`,
		tenantID, now.Format(time.RFC3339))
	require.NoError(t, err)

	// Create Trip 2 (EV)
	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, route_id, vehicle_id, departure_time, completed_at, status)
		VALUES ('t-2', $1, 'TRIP-EV-02', 'r-1', 'v-ev', $2, $2, 'completed')`,
		tenantID, now.Format(time.RFC3339))
	require.NoError(t, err)

	repo := sustainability.NewSQLESGRepository(db)
	useCase := esgapp.NewESGUsecase(repo)
	ctx := context.Background()

	// Calculate both trips
	_, err = useCase.CalculateTripESG(ctx, tenantID, "t-1")
	require.NoError(t, err)
	_, err = useCase.CalculateTripESG(ctx, tenantID, "t-2")
	require.NoError(t, err)

	// Generate snapshot for current month
	startDate := now.Add(-24 * time.Hour).Format("2006-01-02")
	endDate := now.Add(24 * time.Hour).Format("2006-01-02")

	snap, err := useCase.GenerateSnapshot(ctx, tenantID, sustainability.GenerateSnapshotRequest{
		PeriodStart: startDate,
		PeriodEnd:   endDate,
	}, "esg_officer")
	require.NoError(t, err)

	assert.Equal(t, 2, snap.TotalTrips)
	assert.Equal(t, 1000.0, snap.TotalDistanceKM)
	assert.Equal(t, 100.0, snap.TotalFuelLitres)
	assert.Equal(t, 268.0, snap.TotalCO2eKG) // 100L * 2.68
	assert.Equal(t, 500.0, snap.EVDistanceKM)
	assert.Equal(t, "esg_officer", snap.CreatedBy)

	// Verify BRSR report
	brsr, err := useCase.GetBRSRReport(ctx, tenantID, now.Format("2006"))
	require.NoError(t, err)
	assert.InDelta(t, 0.27, brsr.Scope1EmissionsTonne, 0.01)
	assert.Equal(t, 1000.0, brsr.TotalDistanceKM)
	assert.InDelta(t, 50.0, brsr.GreenFleetSharePct, 0.1) // 500km / 1000km = 50%
}

func TestESG_TenantIsolation(t *testing.T) {
	db := setupESGTestDB(t)
	tenant1 := "tenant-esg-a"
	tenant2 := "tenant-esg-b"

	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES 
		($1, 'Tenant A', 'tenant-a'),
		($2, 'Tenant B', 'tenant-b')`, tenant1, tenant2)
	require.NoError(t, err)

	repo := sustainability.NewSQLESGRepository(db)
	useCase := esgapp.NewESGUsecase(repo)
	ctx := context.Background()

	// Tenant 1 creates a snapshot
	_, err = useCase.GenerateSnapshot(ctx, tenant1, sustainability.GenerateSnapshotRequest{
		PeriodStart: "2026-08-01",
		PeriodEnd:   "2026-08-31",
	}, "admin")
	require.NoError(t, err)

	// Tenant 1 should have 1 snapshot
	list1, err := useCase.ListSnapshots(ctx, tenant1)
	require.NoError(t, err)
	assert.Len(t, list1, 1)

	// Tenant 2 should have 0 snapshots
	list2, err := useCase.ListSnapshots(ctx, tenant2)
	require.NoError(t, err)
	assert.Len(t, list2, 0)
}
