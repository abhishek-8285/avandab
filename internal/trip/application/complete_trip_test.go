package application

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/trip/domain/aggregate"
)

func seedDeliveredTrip(t *testing.T, db *sql.DB, tripID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, status, departure_time, arrival_time, tenant_id)
		VALUES (?, ?, 'b-1', 'r-1', 'delivered', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'tenant-1')`,
		tripID, "TRIP-"+tripID)
	require.NoError(t, err)
}

func tripCloseState(t *testing.T, db *sql.DB, tripID string) (string, sql.NullFloat64) {
	t.Helper()
	var status string
	var odo sql.NullFloat64
	require.NoError(t, db.QueryRow(
		`SELECT status, close_odometer FROM trips WHERE id = ?`, tripID).Scan(&status, &odo))
	return status, odo
}

func TestCompleteTrip_WithCloseOdometer(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	tripID := "trip-close-1"
	seedDeliveredTrip(t, db, tripID)

	odo := 125400.5
	uc := NewCompleteTripUseCase(unitOfWork, clk)
	require.NoError(t, uc.Execute(ctx, CompleteTripCommand{
		TripID:        aggregate.TripID(tripID),
		TenantID:      shared.TenantID("tenant-1"),
		CloseOdometer: &odo,
	}))

	status, got := tripCloseState(t, db, tripID)
	assert.Equal(t, "completed", status)
	require.True(t, got.Valid, "close_odometer must persist")
	assert.InDelta(t, 125400.5, got.Float64, 0.001)
}

func TestCompleteTrip_WithoutReading(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	tripID := "trip-close-2"
	seedDeliveredTrip(t, db, tripID)

	uc := NewCompleteTripUseCase(unitOfWork, clk)
	require.NoError(t, uc.Execute(ctx, CompleteTripCommand{
		TripID:   aggregate.TripID(tripID),
		TenantID: shared.TenantID("tenant-1"),
	}))

	status, got := tripCloseState(t, db, tripID)
	assert.Equal(t, "completed", status)
	assert.False(t, got.Valid, "close_odometer stays NULL when omitted")
}

func TestCompleteTrip_InvalidCloseOdometer(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	ctx := shared.ContextWithTenantID(context.Background(), shared.TenantID("tenant-1"))

	tripID := "trip-close-3"
	seedDeliveredTrip(t, db, tripID)

	bad := -10.0
	uc := NewCompleteTripUseCase(unitOfWork, clk)
	err := uc.Execute(ctx, CompleteTripCommand{
		TripID:        aggregate.TripID(tripID),
		TenantID:      shared.TenantID("tenant-1"),
		CloseOdometer: &bad,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "positive")

	// Failed close must not complete the trip.
	status, _ := tripCloseState(t, db, tripID)
	assert.Equal(t, "delivered", status)
}
