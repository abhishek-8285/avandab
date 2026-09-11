package application

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newTimelineTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_timeline_%d", time.Now().UnixNano())
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

func TestMaskPhoneNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"+91 9876543210", "+91 98*** **210"},
		{"+919876543210", "+91 98*** **210"},
		{"9876543210", "98*** **210"},
		{"123456", "12****56"},
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := MaskPhoneNumber(tc.input)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestHaversineDistanceMeters(t *testing.T) {
	// Mumbai (19.0760, 72.8777) to Pune (18.5204, 73.8567) is ~120 km
	dist := HaversineDistanceMeters(19.0760, 72.8777, 18.5204, 73.8567)
	assert.True(t, dist > 100000 && dist < 140000, "expected ~120km, got %f", dist)

	// Same point
	distSame := HaversineDistanceMeters(19.0760, 72.8777, 19.0760, 72.8777)
	assert.True(t, distSame < 1.0)
}

func TestTimelineUseCase_BuildTimeline_FullLifecycle(t *testing.T) {
	db := newTimelineTestDB(t)
	ctx := context.Background()

	// Seed prerequisite data
	_, err := db.Exec(`INSERT OR IGNORE INTO users (id, name, email, password_hash, status, tenant_id)
		VALUES ('u-1', 'Admin', 'admin@fleet.local', 'h', 'active', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, maintenance_due, tenant_id)
		VALUES ('v-1', 'MH-12-AB-1234', 'MH-12-AB-1234', 'truck', 10, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'), 0, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('d-1', 'DRV-101', 'Ramesh', 'Kumar', '+919876543210', 'DL-12345', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r-1', 'Bhiwandi Depot', 'Pune Hub', 150.0, 3.5, 5000.0)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO customers (id, name, email, phone, tenant_id)
		VALUES ('c-1', 'Acme Logistics', 'acme@test.com', '9999999999', '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id, created_at)
		VALUES ('b-1', 'BKG-001', 'c-1', datetime('now', '-4 hours'), 'r-1', 'truck', 5000.0, 'completed', '1', datetime('now', '-4 hours'))`)
	require.NoError(t, err)

	// Create trip
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id,
		departure_time, arrival_time, status, tenant_id, started_at, in_transit_at, completed_at, pod_scan_value)
		VALUES ('trip-1', 'TRP-101', 'b-1', 'd-1', 'v-1', 'r-1',
		datetime('now', '-3 hours'), datetime('now', '+1 hour'), 'completed', '1',
		datetime('now', '-3 hours'), datetime('now', '-2 hours'), datetime('now', '-30 minutes'), 'SCAN-QR-998877')`)
	require.NoError(t, err)

	// Create trip stops
	_, err = db.Exec(`INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, latitude, longitude, actual_arrival, status)
		VALUES ('ts-1', '1', 'trip-1', 1, 'pickup', 'Bhiwandi Depot', 'Depot Address', 19.2967, 73.0628, datetime('now', '-3 hours'), 'completed')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, latitude, longitude, actual_arrival, status, pod_verified_at)
		VALUES ('ts-2', '1', 'trip-1', 2, 'drop', 'Pune Hub', 'Hub Address', 18.5204, 73.8567, datetime('now', '-30 minutes'), 'completed', datetime('now', '-30 minutes'))`)
	require.NoError(t, err)

	// FASTag toll crossing
	_, err = db.Exec(`INSERT INTO fastag_tags (id, tenant_id, tag_id, vehicle_id, vehicle_number, issuer, tag_class, balance, status)
		VALUES ('tag-1', '1', 'TAG-101', 'v-1', 'MH-12-AB-1234', 'ICICI', 'VC4', 1000.0, 'ACTIVE')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_id, trip_id, plaza_name, amount, txn_timestamp, status)
		VALUES ('ft-1', '1', 'TAG-101', 'v-1', 'trip-1', 'Khalapur Toll Plaza', 240.0, datetime('now', '-2 hours'), 'SUCCESS')`)
	require.NoError(t, err)

	// Geofence event
	_, err = db.Exec(`INSERT INTO geofence_events (id, tenant_id, vehicle_id, trip_id, zone_kind, event_type, created_at)
		VALUES ('ge-1', '1', 'v-1', 'trip-1', 'Expressway Corridor', 'entering', datetime('now', '-2 hours', '-15 minutes'))`)
	require.NoError(t, err)

	// Telemetry positions
	_, err = db.Exec(`INSERT INTO telemetry_positions (id, tenant_id, imei, device_time, latitude, longitude, trip_id, vehicle_id)
		VALUES ('pos-1', '1', 'IMEI123', datetime('now', '-3 hours'), 19.2967, 73.0628, 'trip-1', 'v-1'),
		       ('pos-2', '1', 'IMEI123', datetime('now', '-2 hours'), 18.9000, 73.4000, 'trip-1', 'v-1'),
		       ('pos-3', '1', 'IMEI123', datetime('now', '-30 minutes'), 18.5204, 73.8567, 'trip-1', 'v-1')`)
	require.NoError(t, err)

	uc := NewTimelineUseCase(db, nil)
	timeline, err := uc.BuildTimeline(ctx, "trip-1", "1")
	require.NoError(t, err)
	require.NotNil(t, timeline)

	// Verify timeline properties
	assert.Equal(t, "trip-1", timeline.TripID)
	assert.Equal(t, "TRP-101", timeline.TripNumber)
	assert.Equal(t, "completed", timeline.Status)
	assert.Equal(t, "MH-12-AB-1234", timeline.VehicleLabel)
	assert.Equal(t, "Ramesh Kumar", timeline.DriverName)
	assert.Equal(t, "+91 98*** **210", timeline.DriverPhone) // Masked!

	// Verify Milestones
	require.GreaterOrEqual(t, len(timeline.Milestones), 5)

	// 1. Order Created
	assert.Equal(t, string(MilestoneOrderCreated), timeline.Milestones[0].Type)
	assert.Equal(t, "completed", timeline.Milestones[0].Status)
	assert.NotNil(t, timeline.Milestones[0].Timestamp)

	// 2. Vehicle Dispatched
	assert.Equal(t, string(MilestoneVehicleDispatched), timeline.Milestones[1].Type)
	assert.Equal(t, "completed", timeline.Milestones[1].Status)

	// Check for FASTag checkpoint
	hasToll := false
	for _, m := range timeline.Milestones {
		if m.Type == string(MilestoneInTransitCheckpoint) && m.Title == "Toll Plaza Crossed (Khalapur Toll Plaza)" {
			hasToll = true
			assert.Equal(t, "completed", m.Status)
		}
	}
	assert.True(t, hasToll, "expected FASTag milestone to be derived")

	// Check for Out for Delivery and POD Delivered
	lastMilestone := timeline.Milestones[len(timeline.Milestones)-1]
	assert.Equal(t, string(MilestoneDeliveredPOD), lastMilestone.Type)
	assert.Equal(t, "completed", lastMilestone.Status)
	assert.Contains(t, lastMilestone.Title, "Delivered & e-POD Verified")

	// Verify Polyline
	assert.Equal(t, 3, len(timeline.Polyline))
}
