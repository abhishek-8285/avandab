package coldchain_test

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

	"transport-app/internal/coldchain"
)

func newTestColdChainDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_cc_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))

	// Seed tenants
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('tenant-cc-a', 'ColdChain Tenant A', 'tenant-cc-a'),
		('tenant-cc-b', 'ColdChain Tenant B', 'tenant-cc-b')`)
	require.NoError(t, err)

	// Seed vehicles
	_, err = db.Exec(`INSERT OR IGNORE INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, current_mileage) VALUES
		('veh-ref-1', 'tenant-cc-a', 'DL-01-REF-1001', 'REF-001', 'truck', 15.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 24000.0),
		('veh-ref-2', 'tenant-cc-a', 'DL-01-REF-1002', 'REF-002', 'truck', 12.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 18000.0),
		('veh-ref-b', 'tenant-cc-b', 'MH-02-REF-9999', 'REF-B01', 'truck', 20.0, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 5000.0)`)
	require.NoError(t, err)

	// Seed routes & trips
	_, err = db.Exec(`INSERT OR IGNORE INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES
		('route-cc-1', 'Delhi Hub', 'Chandigarh Pharma Depot', 250.0, 5.0, 5000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT OR IGNORE INTO trips (id, tenant_id, vehicle_id, trip_number, route_id, departure_time, status, created_at, updated_at) VALUES
		('trip-cc-1', 'tenant-cc-a', 'veh-ref-1', 'TRIP-CC-001', 'route-cc-1', datetime('now', '-5 hours'), 'completed', datetime('now', '-5 hours'), datetime('now'))`)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestColdChain_ProfileConfigureAndFetch(t *testing.T) {
	db := newTestColdChainDB(t)
	repo := coldchain.NewSQLRepository(db)
	svc := coldchain.NewService(repo)
	ctx := context.Background()

	// 1. Configure reefer profile
	prof, err := svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel:    "Thermo King SLXi",
		NumZones:           1,
		Zone1Type:          coldchain.ZoneTypeChilled,
		Zone1TargetTempC:   4.0,
		Zone1MinTempC:      2.0,
		Zone1MaxTempC:      8.0,
		HumidityMonitoring: true,
		DefrostMode:        "AUTO",
	})
	require.NoError(t, err)
	require.NotNil(t, prof)
	assert.Equal(t, "veh-ref-1", prof.VehicleID)
	assert.Equal(t, 4.0, prof.Zone1TargetTempC)
	assert.Equal(t, 2.0, prof.Zone1MinTempC)
	assert.Equal(t, 8.0, prof.Zone1MaxTempC)
	assert.True(t, prof.HumidityMonitoring)

	// 2. Fetch profile
	fetched, err := svc.GetProfile(ctx, "tenant-cc-a", "veh-ref-1")
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Equal(t, "Thermo King SLXi", fetched.ReeferMakeModel)

	// 3. Validation error: min > max
	_, err = svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		Zone1MinTempC: 10.0,
		Zone1MaxTempC: 5.0,
	})
	assert.Error(t, err)
}

func TestColdChain_TelemetryAndExcursionDetection(t *testing.T) {
	db := newTestColdChainDB(t)
	repo := coldchain.NewSQLRepository(db)
	svc := coldchain.NewService(repo)
	ctx := context.Background()

	// Configure chilled profile (2°C to 8°C)
	_, err := svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel:  "Carrier Vector 1550",
		Zone1Type:        coldchain.ZoneTypeChilled,
		Zone1TargetTempC: 4.0,
		Zone1MinTempC:    2.0,
		Zone1MaxTempC:    8.0,
	})
	require.NoError(t, err)

	// 1. In-band telemetry at 4.2°C -> No excursion
	telem, err := svc.IngestTelemetry(ctx, "tenant-cc-a", coldchain.IngestReeferTelemetryInput{
		VehicleID:         "veh-ref-1",
		ReeferPowerStatus: coldchain.PowerStatusOn,
		ReeferMode:        coldchain.ModeCooling,
		Zone1TempC:        4.2,
		Zone1SetpointC:    4.0,
		DoorStatus:        coldchain.DoorClosed,
	})
	require.NoError(t, err)
	assert.False(t, telem.HasActiveExcursion)

	excursions, total, err := svc.ListExcursions(ctx, "tenant-cc-a", "veh-ref-1", "", 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, excursions)

	// 2. Over-temp excursion at 11.5°C (> 8°C + 3°C -> CRITICAL)
	telem, err = svc.IngestTelemetry(ctx, "tenant-cc-a", coldchain.IngestReeferTelemetryInput{
		VehicleID:         "veh-ref-1",
		ReeferPowerStatus: coldchain.PowerStatusOn,
		ReeferMode:        coldchain.ModeCooling,
		Zone1TempC:        11.5,
		Zone1SetpointC:    4.0,
		DoorStatus:        coldchain.DoorClosed,
	})
	require.NoError(t, err)
	assert.True(t, telem.HasActiveExcursion)

	excursions, total, err = svc.ListExcursions(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ExcursionOpen, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, coldchain.ExcursionOverTemp, excursions[0].ExcursionType)
	assert.Equal(t, coldchain.SeverityCritical, excursions[0].Severity)
	assert.Equal(t, 11.5, excursions[0].PeakTempC)

	// 3. Temperature cools back down to 5.0°C -> Auto-resolved!
	telem, err = svc.IngestTelemetry(ctx, "tenant-cc-a", coldchain.IngestReeferTelemetryInput{
		VehicleID:         "veh-ref-1",
		ReeferPowerStatus: coldchain.PowerStatusOn,
		ReeferMode:        coldchain.ModeCooling,
		Zone1TempC:        5.0,
		Zone1SetpointC:    4.0,
		DoorStatus:        coldchain.DoorClosed,
	})
	require.NoError(t, err)
	assert.False(t, telem.HasActiveExcursion)

	excursions, total, err = svc.ListExcursions(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ExcursionResolved, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, coldchain.ExcursionResolved, excursions[0].Status)
	assert.NotNil(t, excursions[0].EndTime)
}

func TestColdChain_AcknowledgeAndResolveExcursion(t *testing.T) {
	db := newTestColdChainDB(t)
	repo := coldchain.NewSQLRepository(db)
	svc := coldchain.NewService(repo)
	ctx := context.Background()

	_, err := svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		Zone1MinTempC: 2.0,
		Zone1MaxTempC: 8.0,
	})
	require.NoError(t, err)

	// Trigger breach
	_, err = svc.IngestTelemetry(ctx, "tenant-cc-a", coldchain.IngestReeferTelemetryInput{
		VehicleID:  "veh-ref-1",
		Zone1TempC: 9.2,
	})
	require.NoError(t, err)

	excursions, total, err := svc.ListExcursions(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ExcursionOpen, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	excID := excursions[0].ID

	// 1. Acknowledge excursion
	ack, err := svc.AcknowledgeExcursion(ctx, "tenant-cc-a", excID, "Dispatcher Ramesh")
	require.NoError(t, err)
	assert.Equal(t, coldchain.ExcursionAcknowledged, ack.Status)
	assert.Equal(t, "Dispatcher Ramesh", ack.ResolvedBy)

	// 2. Resolve excursion with corrective action
	res, err := svc.ResolveExcursion(ctx, "tenant-cc-a", excID, coldchain.ResolveExcursionInput{
		ResolutionNote: "Driver verified reefer diesel fuel valve was open; unit resumed normal cooling cycle",
		ResolvedBy:     "Ops Manager Priya",
	})
	require.NoError(t, err)
	assert.Equal(t, coldchain.ExcursionResolved, res.Status)
	assert.Equal(t, "Ops Manager Priya", res.ResolvedBy)
	assert.NotEmpty(t, res.ResolutionNote)
}

func TestColdChain_CalculateMKT_AndComplianceCertification(t *testing.T) {
	db := newTestColdChainDB(t)
	repo := coldchain.NewSQLRepository(db)
	svc := coldchain.NewService(repo)
	ctx := context.Background()

	// Configure reefer profile
	_, err := svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel:  "Carrier Vector 1550",
		Zone1Type:        coldchain.ZoneTypeChilled,
		Zone1TargetTempC: 4.0,
		Zone1MinTempC:    2.0,
		Zone1MaxTempC:    8.0,
	})
	require.NoError(t, err)

	// 1. MKT mathematical test: constant 4°C readings must yield 4°C MKT
	constantTemps := []float64{4.0, 4.0, 4.0, 4.0, 4.0}
	mkt := coldchain.CalculateMKT(constantTemps)
	assert.InDelta(t, 4.0, mkt, 0.05)

	// Varying readings with slight excursion: [4.0, 5.0, 4.5, 7.5, 5.2]
	varyingTemps := []float64{4.0, 5.0, 4.5, 7.5, 5.2}
	mktVarying := coldchain.CalculateMKT(varyingTemps)
	assert.True(t, mktVarying > 4.0 && mktVarying < 8.0)

	// 2. Certify compliant trip
	startTime := time.Now().Add(-6 * time.Hour)
	endTime := time.Now()
	summary, err := svc.CertifyTripCompliance(ctx, "tenant-cc-a", coldchain.CertifyTripComplianceInput{
		TripID:      "trip-cc-1",
		VehicleID:   "veh-ref-1",
		CertifiedBy: "QA Auditor Dr. Mehta",
	}, varyingTemps, 120, 2, startTime, endTime)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, coldchain.ComplianceCompliant, summary.ComplianceStatus)
	assert.Equal(t, "QA Auditor Dr. Mehta", summary.CertifiedBy)
	assert.NotNil(t, summary.CertifiedAt)
	assert.Equal(t, 4.0, summary.Zone1MinTempC)
	assert.Equal(t, 7.5, summary.Zone1MaxTempC)

	// 3. Verify fleet metrics
	metrics, err := svc.GetFleetMetrics(ctx, "tenant-cc-a")
	require.NoError(t, err)
	require.NotNil(t, metrics)
	assert.Equal(t, 1, metrics.TotalReefers)
	assert.Equal(t, 100.0, metrics.FleetComplianceRatePct)

	// 4. List vehicles
	vehicles, err := svc.ListReeferVehicles(ctx, "tenant-cc-a")
	require.NoError(t, err)
	assert.Len(t, vehicles, 1)
	assert.Equal(t, "veh-ref-1", vehicles[0].VehicleID)
}

func TestColdChain_TenantIsolation(t *testing.T) {
	db := newTestColdChainDB(t)
	repo := coldchain.NewSQLRepository(db)
	svc := coldchain.NewService(repo)
	ctx := context.Background()

	// Tenant A setup
	_, err := svc.ConfigureProfile(ctx, "tenant-cc-a", "veh-ref-1", coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel: "Carrier Transicold",
		Zone1MinTempC:   2.0,
		Zone1MaxTempC:   8.0,
	})
	require.NoError(t, err)

	// Tenant B cannot access Tenant A's profile
	profB, err := svc.GetProfile(ctx, "tenant-cc-b", "veh-ref-1")
	require.NoError(t, err)
	assert.Nil(t, profB)

	// Tenant B list is empty
	listB, err := svc.ListReeferVehicles(ctx, "tenant-cc-b")
	require.NoError(t, err)
	assert.Empty(t, listB)
}
