package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupComplianceTestDB(t *testing.T) (*sql.DB, *service.Services, *events.InMemoryBus) {
	t.Helper()
	name := fmt.Sprintf("test_compliance_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(dbConn, "../../db/migrations"))
	_, _ = dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	_, _ = dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
			('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
			('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
			('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
			('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
			('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
			('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
			('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
			('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
			('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
			('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
			('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
			('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
			('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
			('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
			('acme','Acme','acme'), ('beta','Beta','beta')`)
	t.Cleanup(func() { _ = dbConn.Close() })

	repo := sqlite.NewRepository(dbConn)
	cfg := &config.Config{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewInMemoryBus()

	svcs := service.NewServices(repo, cfg, log, bus)
	return dbConn, svcs, bus
}

func TestComplianceService_DriverLicenseGate(t *testing.T) {
	dbConn, svcs, bus := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")

	var blockedEvents []events.Event
	bus.Subscribe("ComplianceBlocked", func(ctx context.Context, e events.Event) error {
		blockedEvents = append(blockedEvents, e)
		return nil
	})

	now := time.Now()
	// Insert driver with expired license directly
	drvID := "drv-expired-101"
	expiredDate := now.AddDate(0, 0, -5).Format("2006-01-02")
	_, err := dbConn.Exec(`
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES (?, 'DRV-EXP', 'Rahul', 'Kumar', '+919876543210', 'DL-EXPIRED', ?, 'available', 'tenant-1')`,
		drvID, expiredDate)
	require.NoError(t, err)

	// Verify CheckDispatchCompliance blocks
	res, err := svcs.Compliance.CheckDispatchCompliance(ctx, drvID, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dispatch blocked: driver license expired (compliance)")
	require.True(t, res.Blocked)
	require.False(t, res.Valid)

	// Verify event was published
	require.Len(t, blockedEvents, 1)
	require.Equal(t, "compliance", blockedEvents[0].Payload.(map[string]any)["source"])
	require.Equal(t, "compliance_blocked", blockedEvents[0].Payload.(map[string]any)["alert_type"])

	// Verify compliance_checks row written
	var count int
	err = dbConn.QueryRow("SELECT COUNT(*) FROM compliance_checks WHERE entity_id = ? AND status = 'expired'", drvID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// 2. Grant temporary exemption
	err = svcs.Compliance.CreateExemption(ctx, service.ComplianceExemption{
		EntityType:  "driver",
		EntityID:    drvID,
		DocType:     "license",
		Reason:      "Renewal in progress at RTO",
		ExemptUntil: now.Add(48 * time.Hour),
		CreatedBy:   "admin-1",
	})
	require.NoError(t, err)

	// Now check again -> should pass with warning
	res2, err2 := svcs.Compliance.CheckDispatchCompliance(ctx, drvID, "")
	require.NoError(t, err2)
	require.True(t, res2.Valid)
	require.False(t, res2.Blocked)
	require.NotEmpty(t, res2.Alerts)
}

func TestComplianceService_VehiclePUCAnd7DayWarning(t *testing.T) {
	dbConn, svcs, _ := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	now := time.Now()

	vehID := "veh-puc-101"
	pastPUC := now.AddDate(0, 0, -2).Format("2006-01-02")
	futureDate := now.AddDate(1, 0, 0).Format("2006-01-02")

	_, err := dbConn.Exec(`
		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id)
		VALUES (?, 'TRUCK-101', 'MH12AB1234', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`,
		vehID, futureDate, futureDate, futureDate, pastPUC)
	require.NoError(t, err)

	// CheckDispatchCompliance -> should block on PUC
	res, err := svcs.Compliance.CheckDispatchCompliance(ctx, "", vehID)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "puc expired")
	require.True(t, res.Blocked)

	// Set PUC expiry to 5 days in the future (within 7-day warning window)
	futurePUC := now.AddDate(0, 0, 5).Format("2006-01-02")
	_, err = dbConn.Exec("UPDATE vehicles SET puc_expiry = ? WHERE id = ?", futurePUC, vehID)
	require.NoError(t, err)

	res2, err2 := svcs.Compliance.CheckDispatchCompliance(ctx, "", vehID)
	require.NoError(t, err2)
	require.True(t, res2.Valid)
	require.False(t, res2.Blocked)
	require.NotEmpty(t, res2.Alerts)

	// Verify compliance_checks row written with warning
	var warnCount int
	err = dbConn.QueryRow("SELECT COUNT(*) FROM compliance_checks WHERE entity_id = ? AND status = 'warning'", vehID).Scan(&warnCount)
	require.NoError(t, err)
	require.GreaterOrEqual(t, warnCount, 1)
}

func TestTripService_ComplianceGateIntegration(t *testing.T) {
	dbConn, svcs, _ := setupComplianceTestDB(t)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	now := time.Now()

	// Seed test route, booking, trip, driver, vehicle
	routeID := "rt-comp-1"
	_, err := dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES (?, 'Mumbai', 'Pune', 150, 4, 5000, 'tenant-1')`, routeID)
	require.NoError(t, err)

	tripID := "trp-comp-1"
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id) VALUES (?, 'TRP-100', ?, datetime('now'), 'scheduled', 'tenant-1')`, tripID, routeID)
	require.NoError(t, err)

	drvID := "drv-comp-1"
	validDate := now.AddDate(1, 0, 0).Format("2006-01-02")
	_, err = dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES (?, 'DRV-1', 'Amit', 'Singh', '+919222222222', 'DL-1', ?, 'available', 'tenant-1')`, drvID, validDate)
	require.NoError(t, err)

	vehID := "veh-comp-1"
	_, err = dbConn.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, tenant_id) VALUES (?, 'TRUCK-1', 'MH14XY9999', 'truck', 10.0, 'diesel', ?, ?, ?, ?, 'available', 'tenant-1')`, vehID, validDate, validDate, validDate, validDate)
	require.NoError(t, err)

	// Assign Driver & Vehicle when valid
	_, err = svcs.Trips.AssignDriver(ctx, domain.TripID(tripID), domain.DriverID(drvID))
	require.NoError(t, err)

	_, err = svcs.Trips.AssignVehicle(ctx, domain.TripID(tripID), domain.VehicleID(vehID))
	require.NoError(t, err)

	// Now simulate driver's license expiring before trip starts
	_, err = dbConn.Exec("UPDATE drivers SET license_expiry = ? WHERE id = ?", now.AddDate(0, 0, -1).Format("2006-01-02"), drvID)
	require.NoError(t, err)

	// Attempt StartTrip -> must be blocked by compliance gate!
	_, err = svcs.Trips.StartTrip(ctx, domain.TripID(tripID))
	require.Error(t, err)
	require.Contains(t, err.Error(), "Dispatch blocked: driver license expired (compliance)")

	// Grant exemption -> StartTrip succeeds
	err = svcs.Compliance.CreateExemption(ctx, service.ComplianceExemption{
		EntityType:  "driver",
		EntityID:    drvID,
		DocType:     "license",
		Reason:      "Emergency dispatch authorization",
		ExemptUntil: now.Add(24 * time.Hour),
		CreatedBy:   "admin-1",
	})
	require.NoError(t, err)

	startedTrp, err := svcs.Trips.StartTrip(ctx, domain.TripID(tripID))
	require.NoError(t, err)
	require.Equal(t, domain.TripStarted, startedTrp.Status)
}
