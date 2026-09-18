package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/domain"
	"transport-app/internal/shared"
	tripdomain "transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
	tripsql "transport-app/internal/trip/infrastructure/persistence/sql"
)

func TestLegacyTripUpdatesPreserveLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name       string
		completed  bool
		statusOnly bool
	}{
		{name: "delivered_detail_edit"},
		{name: "completed_detail_edit", completed: true},
		{name: "status_only", statusOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := sql.Open("sqlite", ":memory:")
			require.NoError(t, err)
			conn.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			ctx := shared.ContextWithTenantID(context.Background(), "trip-edit-tenant")
			tenantID := shared.TenantIDFromContext(ctx)
			_, err = conn.ExecContext(ctx, `
CREATE TABLE drivers (id TEXT PRIMARY KEY, driver_id TEXT, first_name TEXT, last_name TEXT);
CREATE TABLE vehicles (id TEXT PRIMARY KEY, registration_number TEXT, vehicle_number TEXT);
CREATE TABLE routes (id TEXT PRIMARY KEY, source TEXT, destination TEXT);
CREATE TABLE trips (
	id TEXT PRIMARY KEY, trip_number TEXT NOT NULL UNIQUE, booking_id TEXT,
	driver_id TEXT, vehicle_id TEXT, route_id TEXT NOT NULL,
	departure_time DATETIME NOT NULL, arrival_time DATETIME,
	status TEXT NOT NULL, remarks TEXT, tenant_id TEXT NOT NULL,
	version INTEGER NOT NULL DEFAULT 1,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	started_at DATETIME, reached_pickup_at DATETIME, in_transit_at DATETIME,
	delivered_at DATETIME, completed_at DATETIME, close_odometer REAL,
	start_odometer REAL, gate_facility_id TEXT, idempotency_key TEXT
);
CREATE TABLE outbox_events (
	id TEXT PRIMARY KEY, aggregate_id TEXT NOT NULL, aggregate_type TEXT NOT NULL,
	event_type TEXT NOT NULL, payload TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, published_at DATETIME
);`)
			require.NoError(t, err)

			now := time.Date(2026, 9, 17, 6, 7, 8, 123456789, time.UTC)
			agg := aggregate.NewTripAggregate("trip-edit", tenantID, "TR-EDIT", nil, "route-original", now, "Original details", now)
			agg.SetIdempotencyKey("trip-edit-create")
			domainRepo := tripsql.NewTripRepository(conn)
			require.NoError(t, domainRepo.Save(ctx, agg))
			require.NoError(t, agg.Schedule(now))
			require.NoError(t, agg.AssignDriver("driver-original", now))
			require.NoError(t, agg.AssignVehicle("vehicle-original", now))
			require.NoError(t, agg.Start(now.Add(time.Hour)))
			require.NoError(t, agg.RecordStartReading(120000.25, "gate-original", now.Add(time.Hour)))
			require.NoError(t, agg.ReachPickup(now.Add(2*time.Hour)))
			require.NoError(t, agg.StartTransit(now.Add(3*time.Hour)))
			require.NoError(t, agg.Deliver(now.Add(4*time.Hour)))
			if tc.completed {
				require.NoError(t, agg.Complete(now.Add(5*time.Hour)))
				require.NoError(t, agg.RecordCloseReading(120321.75, now.Add(5*time.Hour)))
			}
			require.NoError(t, domainRepo.Save(ctx, agg))

			checkLifecycle := func(got tripdomain.TripReadModel, seed bool) {
				t.Helper()
				for _, field := range []struct {
					name string
					want any
					got  any
				}{
					{"started_at", agg.StartedAt, got.StartedAt},
					{"reached_pickup_at", agg.ReachedPickupAt, got.ReachedPickupAt},
					{"in_transit_at", agg.InTransitAt, got.InTransitAt},
					{"delivered_at", agg.DeliveredAt, got.DeliveredAt},
					{"completed_at", agg.CompletedAt, got.CompletedAt},
					{"close_odometer", agg.CloseOdometer, got.CloseOdometer},
					{"start_odometer", agg.StartOdometer, got.StartOdometer},
					{"gate_facility_id", agg.GateFacilityID, got.GateFacilityID},
				} {
					if seed {
						require.Equal(t, field.want, field.got, "production seed: %s", field.name)
					} else {
						assert.Equal(t, field.want, field.got, "%s must survive legacy update", field.name)
					}
				}
			}
			before, err := domainRepo.GetReadModel(ctx, agg.ID, tenantID)
			require.NoError(t, err)
			checkLifecycle(before, true)

			legacyRepo := NewRepository(conn)
			legacy, err := legacyRepo.GetTripByID(ctx, domain.TripID(agg.ID))
			require.NoError(t, err)
			edit := legacy.Trip
			if tc.statusOnly {
				edit.Status = domain.TripCompleted
				updated, err := legacyRepo.UpdateTripStatus(ctx, edit.ID, edit.Status)
				require.NoError(t, err)
				assert.Equal(t, edit.Status, updated.Status)
			} else {
				remarks := "Corrected dispatch details"
				arrival := now.Add(4*time.Hour + 30*time.Minute)
				edit.Remarks = &remarks
				edit.DepartureTime = now.Add(30 * time.Minute)
				edit.ArrivalTime = &arrival
				updated, err := legacyRepo.UpdateTrip(ctx, edit)
				require.NoError(t, err)
				assert.Equal(t, edit.Remarks, updated.Remarks)
				assert.Equal(t, edit.DepartureTime, updated.DepartureTime)
				assert.Equal(t, edit.ArrivalTime, updated.ArrivalTime)
				assert.Equal(t, edit.Status, updated.Status)
			}

			after, err := domainRepo.GetReadModel(ctx, agg.ID, tenantID)
			require.NoError(t, err)
			assert.Equal(t, *edit.Remarks, after.Remarks)
			assert.Equal(t, edit.DepartureTime, after.DepartureTime)
			assert.Equal(t, edit.ArrivalTime, after.ArrivalTime)
			assert.Equal(t, string(edit.Status), after.Status)
			assert.Equal(t, before.TripNumber, after.TripNumber)
			assert.Equal(t, before.BookingID, after.BookingID)
			assert.Equal(t, before.DriverID, after.DriverID)
			assert.Equal(t, before.VehicleID, after.VehicleID)
			assert.Equal(t, before.RouteID, after.RouteID)
			assert.Equal(t, before.CreatedAt, after.CreatedAt)
			checkLifecycle(after, false)

			var version int64
			var key string
			err = conn.QueryRowContext(ctx, `SELECT version, idempotency_key FROM trips WHERE id = ? AND tenant_id = ?`, agg.ID, tenantID).Scan(&version, &key)
			require.NoError(t, err)
			assert.Equal(t, agg.Version+1, version)
			assert.Equal(t, "trip-edit-create", key)
		})
	}
}
