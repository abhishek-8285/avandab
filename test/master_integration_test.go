package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"transport-app/internal/shared"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// PHASE 1: TESTING THE 4 CRITICAL SYNC RULES (INTEGRATION TESTS)
// ============================================================================

// SYNC-1.1: Driver License Expired Hard-Block
func TestSync1_1_DriverLicenseExpired(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	pastExpiry := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	d, err := svcs.Drivers.CreateDriver(
		ctx,
		"Rahul", "Kumar", "999-1001", "rahul@test.com", "Addr 1",
		"DL-EXP-99", pastExpiry, 5, nil, nil, nil,
	)
	// CreateDriver checks license expiry and returns error if already expired
	require.Error(t, err)
	require.Contains(t, err.Error(), "license has already expired")

	_ = d
}

// SYNC-1.2 & 1.3: Vehicle RC Expired Hard-Block at Compliance & Document Renewal
func TestSync1_2_1_3_VehicleComplianceAndRenewal(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	pastExpiry := time.Now().AddDate(0, 0, -10).Format("2006-01-02")

	// Create vehicle with expired insurance - creation succeeds (blocking is at dispatch)
	v1, err := svcs.Vehicles.CreateVehicle(
		ctx,
		"MH-12-EXP-9999", "TRK-EXP", domain.VehicleTypeTruck, 10000, domain.FuelTypeDiesel,
		pastExpiry, futureExpiry, futureExpiry, "1000",
	)
	require.NoError(t, err)

	// SYNC-1.2: Compliance check on expired vehicle -> Blocked=true
	compRes, err := svcs.Compliance.ValidateVehicleCompliance(ctx, v1.ID)
	require.NoError(t, err)
	// Vehicle with expired insurance should not be valid for dispatch
	assert.False(t, compRes.Valid)

	// Create valid vehicle
	v, err := svcs.Vehicles.CreateVehicle(
		ctx,
		"MH-12-VAL-1111", "TRK-VAL", domain.VehicleTypeTruck, 15000, domain.FuelTypeDiesel,
		futureExpiry, futureExpiry, futureExpiry, "1000",
	)
	require.NoError(t, err)

	// SYNC-1.3: Valid vehicle compliance -> Valid, not blocked
	res, err := svcs.Compliance.ValidateVehicleCompliance(ctx, v.ID)
	require.NoError(t, err)
	assert.True(t, res.Valid)
	assert.False(t, res.Blocked)
}

// SYNC-1.4: Valid Assignment
func TestSync1_4_ValidAssignment(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")

	d, err := svcs.Drivers.CreateDriver(
		ctx,
		"Amit", "Patel", "999-2002", "amit@test.com", "Addr 2",
		"DL-OK-2002", futureExpiry, 3, nil, nil, nil,
	)
	require.NoError(t, err)

	v, err := svcs.Vehicles.CreateVehicle(
		ctx,
		"MH-12-OK-2002", "TRK-2002", domain.VehicleTypeTruck, 12000, domain.FuelTypeDiesel,
		futureExpiry, futureExpiry, futureExpiry, "1000",
	)
	require.NoError(t, err)

	err = svcs.Compliance.EnforceDispatchCompliance(ctx, &d.ID, &v.ID)
	assert.NoError(t, err)
}

// SYNC-2.1: Invalid State Transition
func TestSync2_1_InvalidStateTransition(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	cust, _ := svcs.Customers.CreateCustomer(ctx, "Test Cust", "Corp", "999-3001", "", "", "", "")
	rt, _ := svcs.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 3.5, 4500, "")

	bk, _ := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  cust.ID,
		RouteID:     rt.ID,
		PickupDate:  futureExpiry,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       4500,
	})

	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID:     &bk.ID,
		RouteID:       rt.ID,
		DepartureTime: futureExpiry,
		Remarks:       "Draft trip",
	})
	require.NoError(t, err)

	// Attempting deliver on draft trip should fail state transition
	_, err = svcs.Trips.DeliverTripWithPOD(ctx, trp.ID, "https://avandab.com/pod.png")
	assert.Error(t, err)
}

// SYNC-2.2 & 2.3: e-POD Happy Path & Missing POD File Validation
func TestSync2_2_2_3_EPODDeliveryAndValidation(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	cust, _ := svcs.Customers.CreateCustomer(ctx, "Pod Cust", "Corp", "999-4001", "", "", "", "")
	rt, _ := svcs.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 3.5, 4500, "")

	bk, _ := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  cust.ID,
		RouteID:     rt.ID,
		PickupDate:  futureExpiry,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       4500,
	})

	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID:     &bk.ID,
		RouteID:       rt.ID,
		DepartureTime: futureExpiry,
		Remarks:       "Trip for delivery",
	})
	require.NoError(t, err)

	// Transition through workflow to InTransit
	_, _ = svcs.Trips.ScheduleTrip(ctx, trp.ID)
	d, _ := svcs.Drivers.CreateDriver(ctx, "Drv", "Pod", "999-4002", "", "", "LIC-POD", futureExpiry, 2, nil, nil, nil)
	v, _ := svcs.Vehicles.CreateVehicle(ctx, "MH-12-POD-1", "TRK-POD", domain.VehicleTypeTruck, 10000, domain.FuelTypeDiesel, futureExpiry, futureExpiry, futureExpiry, "")
	_, _ = svcs.Trips.AssignDriver(ctx, trp.ID, d.ID)
	_, _ = svcs.Trips.AssignVehicle(ctx, trp.ID, v.ID)
	_, _ = svcs.Trips.StartTrip(ctx, trp.ID)

	// SYNC-2.3: Missing e-POD File -> Error
	_, err = svcs.Trips.DeliverTripWithPOD(ctx, trp.ID, "")
	assert.Error(t, err)

	// SYNC-2.2: e-POD Happy Path -> Delivered
	podURL := "https://avandab.com/uploads/pod/trp-pod.png"
	deliveredTrip, err := svcs.Trips.DeliverTripWithPOD(ctx, trp.ID, podURL)
	require.NoError(t, err)
	assert.Equal(t, domain.TripDelivered, deliveredTrip.Status)
}

// SYNC-3.1 & 3.2: Telemetry Exception Alerting (Fuel Theft & GPS Deviation)
func TestSync3_TelemetryAlerting(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID1 := domain.TripID("trp-tele-1")
	drvID1 := domain.DriverID("drv-tele-1")

	// SYNC-3.1: Fuel Theft Detection (15L drop with ignition OFF)
	dpFuel := service.TelemetryDataPoint{
		VehicleID: domain.VehicleID("vh-tele-1"),
		TripID:    &tripID1,
		DriverID:  &drvID1,
		Latitude:  19.076, Longitude: 72.877,
		Speed: 0, FuelLevel: 35.0, IgnitionOn: false,
		PlannedRouteLat: 19.076, PlannedRouteLng: 72.877,
	}
	alertsFuel, _ := svcs.Telemetry.ProcessTelemetryStream(ctx, dpFuel, 50.0)
	assert.NotEmpty(t, alertsFuel)
	assert.Equal(t, "theft_suspicion", alertsFuel[0].AlertType)

	// SYNC-3.2: GPS Deviation (>5km)
	tripID2 := domain.TripID("trp-tele-2")
	drvID2 := domain.DriverID("drv-tele-2")
	dpGPS := service.TelemetryDataPoint{
		VehicleID: domain.VehicleID("vh-tele-2"),
		TripID:    &tripID2,
		DriverID:  &drvID2,
		Latitude:  19.000, Longitude: 72.000,
		Speed: 50, FuelLevel: 50.0, IgnitionOn: true,
		PlannedRouteLat: 19.100, PlannedRouteLng: 73.000,
	}
	alertsGPS, _ := svcs.Telemetry.ProcessTelemetryStream(ctx, dpGPS, 50.0)
	assert.NotEmpty(t, alertsGPS)
	assert.Equal(t, "gps_deviation", alertsGPS[0].AlertType)

	// SYNC-3.3: Normal Telemetry (100 logs -> 0 alerts)
	tripIDN := domain.TripID("trp-normal")
	drvIDN := domain.DriverID("drv-normal")
	dpNormal := service.TelemetryDataPoint{
		VehicleID: domain.VehicleID("vh-normal"),
		TripID:    &tripIDN, DriverID: &drvIDN,
		Latitude: 19.076, Longitude: 72.877,
		Speed: 50, FuelLevel: 49.9, IgnitionOn: true,
		PlannedRouteLat: 19.076, PlannedRouteLng: 72.877,
	}
	for i := 0; i < 100; i++ {
		normal, _ := svcs.Telemetry.ProcessTelemetryStream(ctx, dpNormal, 50.0)
		assert.Empty(t, normal)
	}
}

// SYNC-4.1 & 4.2: Financial Settlement Net Payout Engine
func TestSync4_FinancialSettlement(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	grossFare := 10000.0
	approvedFuel := 2000.0
	deductions := 500.0

	// SYNC-4.1: Net Payout = 10000 - 2000 - 500 = 7500
	stl, err := svcs.Settlements.CreateSettlementForTrip(ctx, "trp-stl-0", grossFare, approvedFuel, deductions)
	require.NoError(t, err)
	assert.Equal(t, 7500.0, stl.NetPayout)
	assert.Equal(t, "pending", stl.Status)

	// SYNC-4.2: ProcessFinancialSettlement triggers payout (needs a real delivered trip)
	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	cust, _ := svcs.Customers.CreateCustomer(ctx, "Fin Cust", "Corp", "999-6001", "", "", "", "")
	rt, _ := svcs.Routes.CreateRoute(ctx, "Delhi", "Agra", 200, 3.0, grossFare, "")
	bk, _ := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID: cust.ID, RouteID: rt.ID, PickupDate: futureExpiry,
		VehicleType: domain.VehicleTypeTruck, Passengers: 1, Price: grossFare,
	})
	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID: &bk.ID, RouteID: rt.ID, DepartureTime: futureExpiry,
	})
	require.NoError(t, err)
	_, _ = svcs.Trips.ScheduleTrip(ctx, trp.ID)
	d, _ := svcs.Drivers.CreateDriver(ctx, "Fin", "Drv", "999-6002", "", "", "LIC-FIN", futureExpiry, 3, nil, nil, nil)
	v, _ := svcs.Vehicles.CreateVehicle(ctx, "MH-12-FIN-1", "TRK-FIN", domain.VehicleTypeTruck, 10000, domain.FuelTypeDiesel, futureExpiry, futureExpiry, futureExpiry, "")
	_, _ = svcs.Trips.AssignDriver(ctx, trp.ID, d.ID)
	_, _ = svcs.Trips.AssignVehicle(ctx, trp.ID, v.ID)
	_, _ = svcs.Trips.StartTrip(ctx, trp.ID)
	_, _ = svcs.Trips.DeliverTripWithPOD(ctx, trp.ID, "https://avandab.com/pod/fin.png")

	stl2, err := svcs.Settlements.ProcessFinancialSettlement(ctx, trp.ID, "PAY-REF-001")
	require.NoError(t, err)
	assert.Equal(t, "paid", stl2.Status)
	assert.NotNil(t, stl2.PaymentRef)
}

// ============================================================================
// PHASE 2: API, SECURITY & RBAC TESTS
// ============================================================================

func TestSec1_2_AuthCookies(t *testing.T) {
	store := auth.NewSessionStore("test-secret-32bytes-long-enough!", false)

	// SEC-1: Missing Cookie
	req1 := httptest.NewRequest("GET", "/api/v1/invoices", nil)
	_, ok1 := store.ValidateSession(req1)
	assert.False(t, ok1)

	// SEC-2: Tampered Cookie
	req2 := httptest.NewRequest("GET", "/api/v1/invoices", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: "tampered.cookie.signature"})
	_, ok2 := store.ValidateSession(req2)
	assert.False(t, ok2)
}

func TestSec3_5_RBACPermissions(t *testing.T) {
	// SEC-3: Dispatcher (Role 2) read permission on trips -> Allowed
	assert.True(t, auth.HasPermission(2, "trips", "read"))

	// SEC-4: Dispatcher (Role 2) access to invoices -> Forbidden (finance-only resource)
	assert.False(t, auth.HasPermission(2, "invoices", "read"))

	// SEC-5: Accountant (Role 3) write permission on payments -> Allowed
	assert.True(t, auth.HasPermission(3, "payments", "write"))
}

func TestSec8_AuditLoggingWithClientIP(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/drivers", nil)
	req.Header.Set("CF-Connecting-IP", "203.0.113.199")
	assert.Equal(t, "203.0.113.199", auth.ClientIP(req))
}

// ============================================================================
// PHASE 3: EDGE CASES, CONCURRENCY & GOTCHAS
// ============================================================================

// 1. Double e-POD Idempotency Race Condition Test
func TestEdgeCase1_DoubleEPODRaceCondition(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	futureExpiry := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	cust, _ := svcs.Customers.CreateCustomer(ctx, "Race Cust", "Corp", "999-5001", "", "", "", "")
	rt, _ := svcs.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 3.5, 4500, "")

	bk, _ := svcs.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
		CustomerID:  cust.ID,
		RouteID:     rt.ID,
		PickupDate:  futureExpiry,
		VehicleType: domain.VehicleTypeTruck,
		Passengers:  1,
		Price:       4500,
	})

	trp, err := svcs.Trips.CreateTrip(ctx, service.CreateTripRequest{
		BookingID:     &bk.ID,
		RouteID:       rt.ID,
		DepartureTime: futureExpiry,
		Remarks:       "Trip for race condition test",
	})
	require.NoError(t, err)

	_, _ = svcs.Trips.ScheduleTrip(ctx, trp.ID)
	d, _ := svcs.Drivers.CreateDriver(ctx, "Drv", "Race", "999-5002", "", "", "LIC-RACE", futureExpiry, 2, nil, nil, nil)
	v, _ := svcs.Vehicles.CreateVehicle(ctx, "MH-12-RACE-1", "TRK-RACE", domain.VehicleTypeTruck, 10000, domain.FuelTypeDiesel, futureExpiry, futureExpiry, futureExpiry, "")
	_, _ = svcs.Trips.AssignDriver(ctx, trp.ID, d.ID)
	_, _ = svcs.Trips.AssignVehicle(ctx, trp.ID, v.ID)
	_, _ = svcs.Trips.StartTrip(ctx, trp.ID)

	podURL := "https://avandab.com/uploads/pod/race-test.pdf"

	var wg sync.WaitGroup
	results := make([]error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, results[index] = svcs.Trips.DeliverTripWithPOD(ctx, trp.ID, podURL)
		}(i)
	}
	wg.Wait()

	// Exactly 1 request succeeds, remaining fail cleanly
	successCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount)
}

// 2. Large Telemetry Batch Ingestion Test
func TestEdgeCase2_LargeTelemetryBatchIngestion(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	tripID := domain.TripID("trp-batch")
	drvID := domain.DriverID("drv-batch")
	dpBatch := service.TelemetryDataPoint{
		VehicleID: domain.VehicleID("vh-batch"),
		TripID:    &tripID, DriverID: &drvID,
		Latitude: 19.076, Longitude: 72.877,
		Speed: 50, FuelLevel: 49.9, IgnitionOn: true,
		PlannedRouteLat: 19.076, PlannedRouteLng: 72.877,
	}
	for i := 0; i < 500; i++ {
		alerts, _ := svcs.Telemetry.ProcessTelemetryStream(ctx, dpBatch, 50.0)
		assert.Empty(t, alerts)
	}
}

// 3. Reverse Route Distance & Pricing Test
func TestEdgeCase3_ReverseRoutePricing(t *testing.T) {
	db := NewTestDB(t)
	svcs := NewTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Create route with reverse pricing Pune -> Mumbai
	rt, err := svcs.Routes.CreateRoute(ctx, "Mumbai", "Pune", 150, 3.5, 4500, "Highway NH48")
	require.NoError(t, err)

	revDist := 160.0
	revFare := 5200.0
	rt.ReverseDistance = &revDist
	rt.ReverseStandardFare = &revFare

	dist, fare, isRev := rt.GetDistanceAndFare("Pune", "Mumbai")
	assert.Equal(t, 160.0, dist)
	assert.Equal(t, 5200.0, fare)
	assert.True(t, isRev)
}
