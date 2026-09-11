package service_test

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

	"transport-app/internal/service"
)

func newBackhaulTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_backhaul_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestHaversineDistanceKm(t *testing.T) {
	// Identical points
	assert.Equal(t, 0.0, service.HaversineDistanceKm(19.0760, 72.8777, 19.0760, 72.8777))

	// Mumbai to Thane (~18-20 km)
	distMumbaiThane := service.HaversineDistanceKm(19.0760, 72.8777, 19.2183, 72.9781)
	assert.True(t, distMumbaiThane > 15 && distMumbaiThane < 25, "Expected ~19km, got %f", distMumbaiThane)

	// Mumbai to Delhi (~1140-1160 km)
	distMumbaiDelhi := service.HaversineDistanceKm(19.0760, 72.8777, 28.7041, 77.1025)
	assert.True(t, distMumbaiDelhi > 1100 && distMumbaiDelhi < 1200, "Expected ~1150km, got %f", distMumbaiDelhi)
}

func TestBackhaulMatchingEngine(t *testing.T) {
	db := newBackhaulTestDB(t)
	svc := service.NewBackhaulService(db)
	ctx := context.Background()

	tenantA := "tenant-alpha"
	tenantB := "tenant-beta"

	_, err := db.Exec(`
		INSERT OR IGNORE INTO tenants (id, name, slug) VALUES 
		('tenant-alpha', 'Alpha Logistics', 'alpha'),
		('tenant-beta', 'Beta Freight', 'beta');
	`)
	require.NoError(t, err)

	// Setup Base Facility in Delhi
	facilityDelhiID := "fac-delhi-01"
	_, err = db.Exec(`
		INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type, latitude, longitude, is_active)
		VALUES ($1, $2, 'DEL-HUB', 'Delhi Central Depot', 'hub', 28.7041, 77.1025, 1)`,
		facilityDelhiID, tenantA)
	require.NoError(t, err)

	// Setup Vehicle stationed at Delhi depot (capacity 10000kg, truck)
	vehicleID := "veh-truck-01"
	_, err = db.Exec(`
		INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, facility_id)
		VALUES ($1, $2, 'DL01AB1234', 'TRK-1234', 'truck', 10000, 'diesel', '2028-01-01', '2028-01-01', '2028-01-01', 'available', $3)`,
		vehicleID, tenantA, facilityDelhiID)
	require.NoError(t, err)

	// Driver
	driverID := "drv-01"
	_, err = db.Exec(`
		INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, status)
		VALUES ($1, $2, 'DRV001', 'Rajesh', 'Kumar', '+919876543210', 'available')`,
		driverID, tenantA)
	require.NoError(t, err)

	// Outbound Route: Delhi to Mumbai (~1400km)
	routeOutbound := "route-del-mum"
	_, err = db.Exec(`
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ($1, 'Delhi', 'Mumbai', 1400, 28, 45000)`,
		routeOutbound)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO route_locations (route_id, source_lat, source_lng, source_name, dest_lat, dest_lng, dest_name)
		VALUES ($1, 28.7041, 77.1025, 'Delhi Hub', 19.0760, 72.8777, 'Mumbai Port')`,
		routeOutbound)
	require.NoError(t, err)

	// Customer in tenantA
	customerID := "cust-01"
	_, err = db.Exec(`
		INSERT INTO customers (id, tenant_id, name, phone)
		VALUES ($1, $2, 'Reliance Retail', '+919999988888')`,
		customerID, tenantA)
	require.NoError(t, err)

	// Completed / Delivering Trip: delivering in Mumbai Port
	tripID := "trip-mum-deliv"
	departure := time.Now().Add(-30 * time.Hour)
	arrival := time.Now().Add(1 * time.Hour) // within 4h lookahead window
	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, driver_id, vehicle_id, route_id, departure_time, arrival_time, status)
		VALUES ($1, $2, 'TRIP-DEL-MUM-01', $3, $4, $5, $6, $7, 'in_transit')`,
		tripID, tenantA, driverID, vehicleID, routeOutbound, departure, arrival)
	require.NoError(t, err)

	// Trip stops for final stop in Mumbai Port
	_, err = db.Exec(`
		INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status)
		VALUES 
		('stop-01', $1, $2, 1, 'pickup', 'Delhi Hub', 'Delhi Depot', 28.7041, 77.1025, 'completed'),
		('stop-02', $1, $2, 2, 'drop', 'Mumbai Port', 'JNPT Mumbai', 19.0760, 72.8777, 'arrived')`,
		tenantA, tripID)
	require.NoError(t, err)

	// ── Setup Candidate Bookings ──
	now := time.Now()

	// Booking 1: Thane (19km from Mumbai) to Delhi (homeward corridor) -> MATCH
	b1ID := "bk-thane-delhi"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-001', $3, $4, 'truck', 7500, 38000, $5, 'pending')`,
		b1ID, tenantA, customerID, routeOutbound, now.Add(3*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Thane MIDC', 19.2183, 72.9781, 'Delhi Okhla', 28.5355, 77.2732)`,
		b1ID, tenantA)
	require.NoError(t, err)

	// Booking 2: Pune (~118km from Mumbai) to Delhi -> only matches if radius >= 120km
	b2ID := "bk-pune-delhi"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-002', $3, $4, 'truck', 6000, 35000, $5, 'confirmed')`,
		b2ID, tenantA, customerID, routeOutbound, now.Add(5*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Pune Chakan', 18.5204, 73.8567, 'Delhi NCR', 28.7041, 77.1025)`,
		b2ID, tenantA)
	require.NoError(t, err)

	// Booking 3: Thane to Goa (15.2993, 74.1240) -> AWAY from Delhi base! (should be excluded by homeward corridor check)
	b3ID := "bk-thane-goa"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-003', $3, $4, 'truck', 5000, 20000, $5, 'pending')`,
		b3ID, tenantA, customerID, routeOutbound, now.Add(4*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Thane Port', 19.2183, 72.9781, 'Goa Port', 15.2993, 74.1240)`,
		b3ID, tenantA)
	require.NoError(t, err)

	// Booking 4: Thane to Delhi, but cargo weight exceeds truck capacity (12000kg > 10000kg)
	b4ID := "bk-thane-heavy"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-004', $3, $4, 'truck', 12000, 42000, $5, 'pending')`,
		b4ID, tenantA, customerID, routeOutbound, now.Add(2*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Thane Heavy', 19.2183, 72.9781, 'Delhi', 28.7041, 77.1025)`,
		b4ID, tenantA)
	require.NoError(t, err)

	// Booking 5: Incompatible vehicle type ('tempo' vs 'truck')
	b5ID := "bk-thane-tempo"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-005', $3, $4, 'tempo', 2000, 15000, $5, 'pending')`,
		b5ID, tenantA, customerID, routeOutbound, now.Add(2*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Thane Tempo', 19.2183, 72.9781, 'Delhi', 28.7041, 77.1025)`,
		b5ID, tenantA)
	require.NoError(t, err)

	// Booking 6: Tenant isolation (same route & timing, but in tenantB)
	b6ID := "bk-tenant-b"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-006', $3, $4, 'truck', 5000, 39000, $5, 'pending')`,
		b6ID, tenantB, customerID, routeOutbound, now.Add(2*time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Thane TenantB', 19.2183, 72.9781, 'Delhi', 28.7041, 77.1025)`,
		b6ID, tenantB)
	require.NoError(t, err)

	// ── TEST 1: Default Radius (50 km) Matching ──
	matches, err := svc.FindBackhaulMatches(ctx, tenantA, tripID, 50.0)
	require.NoError(t, err)
	// Only BK-001 should match (BK-002 is ~118km away > 50km, BK-003 is away-from-base, BK-004 is overweight, BK-005 is tempo, BK-006 is tenantB)
	require.Len(t, matches, 1)
	assert.Equal(t, b1ID, matches[0].BookingID)
	assert.True(t, matches[0].DeadheadKm > 15 && matches[0].DeadheadKm < 25)
	assert.True(t, matches[0].FreightMargin > 35000)
	assert.True(t, matches[0].CorridorSavingsPct > 90)

	// ── TEST 2: Expanded Radius (130 km) ──
	matches130, err := svc.FindBackhaulMatches(ctx, tenantA, tripID, 130.0)
	require.NoError(t, err)
	// BK-001 and BK-002 should match
	require.Len(t, matches130, 2)
	assert.Equal(t, b1ID, matches130[0].BookingID) // lowest deadhead km (~19km vs ~118km)
	assert.Equal(t, b2ID, matches130[1].BookingID)

	// ── TEST 3: Multi-Tenant Isolation ──
	// Searching from tenantB should yield 0 matches for tripID (since trip belongs to tenantA)
	_, err = svc.FindBackhaulMatches(ctx, tenantB, tripID, 50.0)
	assert.Error(t, err, "Trip belonging to tenantA must not be accessible to tenantB")

	// ── TEST 4: Dispatch Offer Creation & Idempotency ──
	offerReq := service.BackhaulOfferRequest{
		TripID:      tripID,
		BookingID:   b1ID,
		OfferedRate: 38000.0,
	}
	offerResp, err := svc.CreateBackhaulOffer(ctx, tenantA, offerReq)
	require.NoError(t, err)
	assert.NotEmpty(t, offerResp.OfferID)
	assert.Equal(t, "offered", offerResp.Status)
	assert.Equal(t, driverID, offerResp.DriverID)
	assert.Equal(t, vehicleID, offerResp.VehicleID)
	assert.Equal(t, 38000.0, offerResp.OfferedRate)

	// Idempotent re-offer returns same active offer
	offerResp2, err := svc.CreateBackhaulOffer(ctx, tenantA, offerReq)
	require.NoError(t, err)
	assert.Equal(t, offerResp.OfferID, offerResp2.OfferID)

	// ── TEST 5: Blocked Vehicle Safety Guard ──
	// When vehicle is blocked, matching returns 0 matches
	_, err = db.Exec(`UPDATE vehicles SET blocked = 1 WHERE id = $1`, vehicleID)
	require.NoError(t, err)

	blockedMatches, err := svc.FindBackhaulMatches(ctx, tenantA, tripID, 50.0)
	require.NoError(t, err)
	assert.Empty(t, blockedMatches, "Blocked vehicle must return zero matches")

	// Offer creation must also fail
	_, err = svc.CreateBackhaulOffer(ctx, tenantA, offerReq)
	assert.Error(t, err, "Must reject offer creation for blocked vehicle")

	// Unblock vehicle
	_, err = db.Exec(`UPDATE vehicles SET blocked = 0 WHERE id = $1`, vehicleID)
	require.NoError(t, err)

	// ── TEST 6: Maintenance Work Order Guard ──
	// When open work order exists, vehicle is excluded
	_, err = db.Exec(`
		INSERT INTO work_orders (id, tenant_id, vehicle_id, title, status)
		VALUES ('wo-01', $1, $2, 'Brake overhaul', 'in_progress')`,
		tenantA, vehicleID)
	require.NoError(t, err)

	maintMatches, err := svc.FindBackhaulMatches(ctx, tenantA, tripID, 50.0)
	require.NoError(t, err)
	assert.Empty(t, maintMatches, "Vehicle with active work order must return zero matches")

	_, err = svc.CreateBackhaulOffer(ctx, tenantA, offerReq)
	assert.Error(t, err, "Must reject offer creation for vehicle under maintenance")

	// Close work order
	_, err = db.Exec(`UPDATE work_orders SET status = 'done' WHERE id = 'wo-01'`)
	require.NoError(t, err)

	// Matches reappear
	restoredMatches, err := svc.FindBackhaulMatches(ctx, tenantA, tripID, 50.0)
	require.NoError(t, err)
	assert.Len(t, restoredMatches, 1)
}
