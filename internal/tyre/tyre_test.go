package tyre_test

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

	"transport-app/internal/tyre"
)

func newTestTyreDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_tyre_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	// Seed test tenants and vehicles
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-a', 'Tenant A', 'tenant-a'),
		('tenant-b', 'Tenant B', 'tenant-b')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		('veh-1', 'tenant-a', 'KA-01-AA-1111', 'V-001', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 50000.0),
		('veh-2', 'tenant-a', 'KA-01-AA-2222', 'V-002', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 75000.0),
		('veh-b', 'tenant-b', 'MH-02-BB-9999', 'VB-001', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 10000.0)`)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTyre_CRUDAndTenantIsolation(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	// 1. Create Tyre in tenant-a
	t1, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "AP-1001",
		Brand:        "Apollo",
		Model:        "EnduRace RA",
		Size:         "295/80R22.5",
		TyreType:     tyre.TyreTypeTubeless,
		PurchaseCost: 22000.0,
		VendorName:   "Apollo Tyres Hub",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, t1.ID)
	assert.Equal(t, tyre.TyreStatusInStock, t1.Status)
	assert.Equal(t, 15.0, t1.CurrentTreadDepthMM)
	assert.Equal(t, 22000.0, t1.PurchaseCost)

	// 2. Duplicate serial number in same tenant must fail
	_, err = svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "AP-1001",
		Brand:        "Apollo",
		Size:         "295/80R22.5",
	})
	assert.ErrorIs(t, err, tyre.ErrDuplicateSerial)

	// 3. Same serial number in DIFFERENT tenant is allowed (multi-tenant scope)
	tB, err := svc.CreateTyre(ctx, "tenant-b", tyre.CreateTyreInput{
		SerialNumber: "AP-1001",
		Brand:        "Apollo",
		Size:         "295/80R22.5",
		PurchaseCost: 21500.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "tenant-b", tB.TenantID)

	// 4. Cross-tenant isolation: tenant-a cannot read tenant-b tyre
	_, err = svc.GetTyre(ctx, "tenant-a", tB.ID)
	assert.ErrorIs(t, err, tyre.ErrTyreNotFound)

	// 5. List with filters
	list, total, err := svc.ListTyres(ctx, tyre.TyreFilter{TenantID: "tenant-a"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, list, 1)
	assert.Equal(t, "AP-1001", list[0].SerialNumber)
}

func TestTyre_MountDismountAndCPKM(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	// Purchase tyre for ₹20,000
	t1, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "MRF-2001",
		Brand:        "MRF",
		Model:        "Super Lug",
		Size:         "10.00R20",
		TyreType:     tyre.TyreTypeRadial,
		PurchaseCost: 20000.0,
	})
	require.NoError(t, err)

	// Mount tyre at 'FL' on 'veh-1' at 50,000 km
	mountDate := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	mounted, err := svc.MountTyre(ctx, "tenant-a", t1.ID, tyre.MountTyreInput{
		VehicleID:       "veh-1",
		AxlePosition:    "FL",
		MountOdometerKM: 50000.0,
		MountDate:       &mountDate,
		PerformedBy:     "Ramesh (Master Mechanic)",
	})
	require.NoError(t, err)
	assert.Equal(t, tyre.TyreStatusMounted, mounted.Status)
	assert.Equal(t, "FL", *mounted.AxlePosition)
	assert.Equal(t, "veh-1", *mounted.VehicleID)

	// Cannot mount another tyre on 'FL' while occupied
	t2, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "CEAT-3001",
		Brand:        "CEAT",
		Size:         "10.00R20",
		PurchaseCost: 19000.0,
	})
	require.NoError(t, err)

	_, err = svc.MountTyre(ctx, "tenant-a", t2.ID, tyre.MountTyreInput{
		VehicleID:       "veh-1",
		AxlePosition:    "FL",
		MountOdometerKM: 50000.0,
	})
	assert.ErrorIs(t, err, tyre.ErrAxleOccupied)

	// Check live CPKM while mounted: vehicle mileage is 50,000 in DB -> update vehicle_latest_position to 70,000 km
	_, err = db.Exec(`INSERT INTO vehicle_latest_position (vehicle_id, tenant_id, device_time, latitude, longitude, speed, odometer)
		VALUES ('veh-1', 'tenant-a', datetime('now'), 19.0, 72.0, 45.0, 70000.0)
		ON CONFLICT(vehicle_id) DO UPDATE SET odometer = 70000.0`)
	require.NoError(t, err)

	current, err := svc.GetTyre(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	require.NotNil(t, current.Metrics)
	assert.Equal(t, 20000.0, current.Metrics.CurrentRunKM, "run distance should be 70k - 50k = 20k km")
	// CPKM = 20,000 cost / 20,000 km = ₹1.000 / km
	assert.Equal(t, 1.0, current.Metrics.CPKM)

	// Dismount at 90,000 km (ran 40,000 km total)
	dismountDate := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	dismounted, err := svc.DismountTyre(ctx, "tenant-a", t1.ID, tyre.DismountTyreInput{
		DismountOdometerKM: 90000.0,
		DismountDate:       &dismountDate,
		Reason:             "Regular rotation and inspection",
		PerformedBy:        "Suresh",
	})
	require.NoError(t, err)
	assert.Equal(t, tyre.TyreStatusInStock, dismounted.Status)
	assert.Nil(t, dismounted.VehicleID)
	assert.Nil(t, dismounted.AxlePosition)
	assert.Equal(t, 40000.0, dismounted.TotalKMRun)

	// After dismount, CPKM is ₹20,000 / 40,000 km = ₹0.50 / km
	assert.Equal(t, 0.5, dismounted.Metrics.CPKM)

	// Verify activities audit log
	acts, err := svc.ListActivities(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	require.Len(t, acts, 2)
	assert.Equal(t, tyre.ActivityDismount, acts[0].ActivityType)
	assert.Equal(t, tyre.ActivityMount, acts[1].ActivityType)
}

func TestTyre_Rotation(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	// Mount Tyre 1 on FL and Tyre 2 on FR
	t1, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "ROT-01",
		Brand:        "Bridgestone",
		Size:         "295/80R22.5",
	})
	require.NoError(t, err)
	t2, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "ROT-02",
		Brand:        "Bridgestone",
		Size:         "295/80R22.5",
	})
	require.NoError(t, err)

	_, err = svc.MountTyre(ctx, "tenant-a", t1.ID, tyre.MountTyreInput{
		VehicleID: "veh-1", AxlePosition: "FL", MountOdometerKM: 50000.0,
	})
	require.NoError(t, err)

	_, err = svc.MountTyre(ctx, "tenant-a", t2.ID, tyre.MountTyreInput{
		VehicleID: "veh-1", AxlePosition: "FR", MountOdometerKM: 50000.0,
	})
	require.NoError(t, err)

	// Rotate FL -> R1-LO, FR -> R1-RO
	err = svc.RotateTyres(ctx, "tenant-a", tyre.RotateTyresInput{
		VehicleID: "veh-1",
		Rotations: []tyre.TyreRotationItem{
			{TyreID: t1.ID, FromPosition: "FL", ToPosition: "R1-LO"},
			{TyreID: t2.ID, FromPosition: "FR", ToPosition: "R1-RO"},
		},
		OdometerKM:  65000.0,
		PerformedBy: "Workshop Supervisor",
		Notes:       "15,000 km rotation schedule",
	})
	require.NoError(t, err)

	after1, err := svc.GetTyre(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	assert.Equal(t, "R1-LO", *after1.AxlePosition)

	after2, err := svc.GetTyre(ctx, "tenant-a", t2.ID)
	require.NoError(t, err)
	assert.Equal(t, "R1-RO", *after2.AxlePosition)
}

func TestTyre_InspectionAndWearRate(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	t1, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber:        "INSP-01",
		Brand:               "Michelin",
		Size:                "295/80R22.5",
		InitialTreadDepthMM: floatPtr(15.0),
		MinTreadDepthMM:     floatPtr(3.0),
		PurchaseCost:        30000.0,
	})
	require.NoError(t, err)

	_, err = svc.MountTyre(ctx, "tenant-a", t1.ID, tyre.MountTyreInput{
		VehicleID: "veh-1", AxlePosition: "FL", MountOdometerKM: 10000.0,
	})
	require.NoError(t, err)

	// Record inspection after running 20,000 km: tread went from 15.0 to 11.0 mm (4mm wear over 20,000 km -> 5,000 km/mm)
	psi := 120.0
	insp, err := svc.RecordInspection(ctx, "tenant-a", t1.ID, tyre.RecordInspectionInput{
		OdometerKM:    30000.0,
		TreadDepthMM:  11.0,
		PressurePSI:   &psi,
		Condition:     tyre.ConditionGood,
		InspectorName: "Inspector Dilip",
	})
	require.NoError(t, err)
	assert.Equal(t, 11.0, insp.TreadDepthMM)

	// Check metrics: veh-1 odometer is 50,000, mounted at 10,000 -> 40,000 km run.
	// 4mm tread loss over 40,000 km = 10,000 km/mm.
	checked, err := svc.GetTyre(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	assert.Equal(t, 11.0, checked.CurrentTreadDepthMM)
	require.NotNil(t, checked.Metrics)
	assert.Equal(t, 4.0, checked.Metrics.TreadLossMM)
	assert.Equal(t, 8.0, checked.Metrics.UsableTreadLeftMM)        // 11 - 3 min = 8mm usable
	assert.Equal(t, 10000.0, checked.Metrics.KMPerMM)              // 40k / 4mm = 10,000 km/mm
	assert.Equal(t, 80000.0, checked.Metrics.ProjectedRemainingKM) // 8mm * 10,000 = 80,000 km remaining

	// Record second inspection with low tread: 4.0 mm (warning threshold: <= 3.0 + 1.5 = 4.5mm)
	_, err = svc.RecordInspection(ctx, "tenant-a", t1.ID, tyre.RecordInspectionInput{
		OdometerKM:    65000.0,
		TreadDepthMM:  4.0,
		Condition:     tyre.ConditionUnevenWear,
		ActionTaken:   "Schedule for retreading next week",
		InspectorName: "Inspector Dilip",
	})
	require.NoError(t, err)

	checked2, err := svc.GetTyre(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	assert.Equal(t, "LOW_TREAD", checked2.Metrics.HealthStatus)

	// Critical tread: 2.5 mm <= min (3.0 mm)
	_, err = svc.RecordInspection(ctx, "tenant-a", t1.ID, tyre.RecordInspectionInput{
		OdometerKM:   70000.0,
		TreadDepthMM: 2.5,
		Condition:    tyre.ConditionWornOut,
	})
	require.NoError(t, err)

	checked3, err := svc.GetTyre(ctx, "tenant-a", t1.ID)
	require.NoError(t, err)
	assert.Equal(t, "CRITICAL_TREAD", checked3.Metrics.HealthStatus)
}

func TestTyre_RetreadAndScrapLifecycle(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	t1, err := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "RET-01",
		Brand:        "JK Tyre",
		Size:         "295/80R22.5",
		PurchaseCost: 18000.0,
		MaxRetreads:  intPtr(2),
	})
	require.NoError(t, err)

	// Send 1st retread
	retread1, err := svc.RetreadTyre(ctx, "tenant-a", t1.ID, tyre.RetreadTyreInput{
		Action:      "SEND",
		VendorName:  "Elgi Rubber Retreading",
		PerformedBy: "Workshop Supervisor",
	})
	require.NoError(t, err)
	assert.Equal(t, tyre.TyreStatusUnderRetread, retread1.Status)

	// Receive 1st retread (costs ₹4,500, restores tread to 14.0mm)
	received1, err := svc.RetreadTyre(ctx, "tenant-a", t1.ID, tyre.RetreadTyreInput{
		Action:          "RECEIVE",
		VendorName:      "Elgi Rubber Retreading",
		Cost:            4500.0,
		NewTreadDepthMM: floatPtr(14.0),
		PerformedBy:     "Workshop Supervisor",
	})
	require.NoError(t, err)
	assert.Equal(t, tyre.TyreStatusInStock, received1.Status)
	assert.Equal(t, 1, received1.RetreadCount)
	assert.Equal(t, 4500.0, received1.TotalRetreadCost)
	assert.Equal(t, 14.0, received1.CurrentTreadDepthMM)

	// 2nd retread (reaches max_retreads = 2)
	_, err = svc.RetreadTyre(ctx, "tenant-a", t1.ID, tyre.RetreadTyreInput{Action: "SEND"})
	require.NoError(t, err)
	received2, err := svc.RetreadTyre(ctx, "tenant-a", t1.ID, tyre.RetreadTyreInput{
		Action: "RECEIVE", Cost: 4800.0,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, received2.RetreadCount)
	assert.Equal(t, 9300.0, received2.TotalRetreadCost)

	// 3rd retread must be BLOCKED because max_retreads is 2
	_, err = svc.RetreadTyre(ctx, "tenant-a", t1.ID, tyre.RetreadTyreInput{Action: "SEND"})
	assert.ErrorIs(t, err, tyre.ErrMaxRetreadsReached)

	// Scrap the tyre: recovery ₹1,500
	scrapped, err := svc.ScrapTyre(ctx, "tenant-a", t1.ID, tyre.ScrapTyreInput{
		Reason:         "Max retreads reached and casing worn",
		RecoveryAmount: 1500.0,
		PerformedBy:    "Store Manager",
	})
	require.NoError(t, err)
	assert.Equal(t, tyre.TyreStatusScrapped, scrapped.Status)
	assert.Equal(t, 1500.0, scrapped.ScrapRecoveryAmount)
	assert.NotNil(t, scrapped.ScrapDate)
}

func TestTyre_FleetSummary(t *testing.T) {
	db := newTestTyreDB(t)
	repo := tyre.NewSQLRepository(db)
	svc := tyre.NewService(repo)
	ctx := context.Background()

	// Create 3 tyres: 1 mounted, 1 in stock, 1 scrapped
	t1, _ := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "SUM-01", Brand: "Apollo", Size: "295/80R22.5", PurchaseCost: 20000.0,
	})
	_, _ = svc.MountTyre(ctx, "tenant-a", t1.ID, tyre.MountTyreInput{
		VehicleID: "veh-1", AxlePosition: "FL", MountOdometerKM: 10000.0,
	})

	_, _ = svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "SUM-02", Brand: "MRF", Size: "295/80R22.5", PurchaseCost: 22000.0,
	})

	t3, _ := svc.CreateTyre(ctx, "tenant-a", tyre.CreateTyreInput{
		SerialNumber: "SUM-03", Brand: "CEAT", Size: "295/80R22.5", PurchaseCost: 18000.0,
	})
	_, _ = svc.ScrapTyre(ctx, "tenant-a", t3.ID, tyre.ScrapTyreInput{Reason: "Sidewall burst"})

	sum, err := svc.GetFleetSummary(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, 3, sum.TotalTyres)
	assert.Equal(t, 1, sum.MountedCount)
	assert.Equal(t, 1, sum.InStockCount)
	assert.Equal(t, 1, sum.ScrappedCount)
	assert.Equal(t, 60000.0, sum.TotalInvestmentCost) // 20k + 22k + 18k
}

func floatPtr(f float64) *float64 { return &f }
func intPtr(i int) *int           { return &i }
