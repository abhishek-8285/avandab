package application_test

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
	"transport-app/internal/shared"
	tripapp "transport-app/internal/trip/application"
)

func newBackhaulAppTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_backhaul_app_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations", "../../../db/migrations"} {
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

func TestBackhaulUseCase(t *testing.T) {
	db := newBackhaulAppTestDB(t)
	svc := service.NewBackhaulService(db)
	uc := tripapp.NewBackhaulUseCase(svc)
	ctx := context.Background()

	tenant := shared.TenantID("tenant-uc-test")
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-uc-test', 'UC Corp', 'uc-test')`)
	require.NoError(t, err)

	vehID := "veh-uc-01"
	_, err = db.Exec(`
		INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ($1, $2, 'MH12AB0001', 'V-001', 'truck', 9000, 'diesel', '2028-01-01', '2028-01-01', '2028-01-01', 'available')`,
		vehID, string(tenant))
	require.NoError(t, err)

	drvID := "drv-uc-01"
	_, err = db.Exec(`
		INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, status)
		VALUES ($1, $2, 'DRV-UC-01', 'Ramesh', 'Patil', '+919876543299', 'available')`,
		drvID, string(tenant))
	require.NoError(t, err)

	routeID := "route-uc-01"
	_, err = db.Exec(`
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ($1, 'Origin', 'Dest', 100, 2, 5000)`,
		routeID)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO route_locations (route_id, source_lat, source_lng, dest_lat, dest_lng)
		VALUES ($1, 18.5204, 73.8567, 19.0760, 72.8777)`,
		routeID)
	require.NoError(t, err)

	tripID := "trip-uc-01"
	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, driver_id, vehicle_id, route_id, departure_time, arrival_time, status)
		VALUES ($1, $2, 'TRIP-UC-01', $3, $4, $5, $6, $7, 'delivered')`,
		tripID, string(tenant), drvID, vehID, routeID, time.Now().Add(-3*time.Hour), time.Now())
	require.NoError(t, err)

	// Booking
	custID := "cust-uc-01"
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ($1, $2, 'UC Customer', '+919999900000')`,
		custID, string(tenant))
	require.NoError(t, err)

	bkID := "bk-uc-01"
	_, err = db.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-UC-01', $3, $4, 'truck', 5000, 12000, $5, 'pending')`,
		bkID, string(tenant), custID, routeID, time.Now().Add(1*time.Hour))
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Pickup Place', 19.1000, 72.8900, 'Drop Place', 18.5204, 73.8567)`,
		bkID, string(tenant))
	require.NoError(t, err)

	// Use case query
	matches, err := uc.FindMatches(ctx, tripapp.FindBackhaulMatchesQuery{
		TripID:   tripID,
		TenantID: tenant,
		RadiusKm: 50,
	})
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, bkID, matches[0].BookingID)

	// Use case command
	offer, err := uc.CreateOffer(ctx, tripapp.CreateBackhaulOfferCommand{
		TripID:      tripID,
		TenantID:    tenant,
		BookingID:   bkID,
		OfferedRate: 12000,
	})
	require.NoError(t, err)
	assert.Equal(t, "offered", offer.Status)
	assert.Equal(t, drvID, offer.DriverID)
}
