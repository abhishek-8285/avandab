package application

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/trip/domain/aggregate"
)

// fixedClock returns a caller-controlled time, e.g. 02:00 IST (20:30 UTC).
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// #11: at 02:00 IST an expiry stamped at the previous UTC midnight is already
// in the past, but UTC-truncated "now" made it look like today -> dispatch
// silently unblocked an expired document.
func TestAssignVehicle_ISTEarlyMorning_ExpiredUTCMidnight_Rejected(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	ist := time.FixedZone("IST", 5*3600+30*60)
	clk := fixedClock{t: time.Date(2026, 11, 17, 2, 0, 0, 0, ist)} // 20:30 UTC Nov 16

	tripID := uuid.NewString()
	vehID := "veh-clock-utc"
	driverID := "drv-clock-utc"

	seedTestDriver(t, db, driverID)
	seedTestTrip(t, db, tripID)
	seedTestVehicle(t, db, vehID, false)

	assignDriverUC := NewAssignDriverUseCase(unitOfWork, clk)
	require.NoError(t, assignDriverUC.Execute(context.Background(), AssignDriverCommand{
		TripID: aggregate.TripID(tripID), DriverID: driverID,
		TenantID: shared.TenantID("tenant-1"),
	}))

	// RC expired at the previous UTC midnight.
	_, err := db.Exec(`UPDATE vehicles SET rc_expiry = '2026-11-16 00:00:00' WHERE id = ?`, vehID)
	require.NoError(t, err)

	err = NewAssignVehicleUseCase(unitOfWork, clk).Execute(context.Background(), AssignVehicleCommand{
		TripID: aggregate.TripID(tripID), VehicleID: vehID,
		TenantID: shared.TenantID("tenant-1"),
	})
	require.Error(t, err, "expired-at-UTC-midnight RC must block at 02:00 IST")
	assert.Contains(t, err.Error(), "RC expired")
}

// failingRand forces crypto/rand.Int to fail.
type failingRand struct{}

func (failingRand) Read([]byte) (int, error) { return 0, errors.New("forced rand failure") }

// fixedIDGen avoids uuid.New() (which also consumes crypto/rand) so only the
// OTP rand.Int path sees the injected failure.
type fixedIDGen struct{}

func (fixedIDGen) GenerateUUID() string                 { return "fixed-uuid" }
func (fixedIDGen) GenerateDisplayID(prefix string) string { return prefix + "-fixed" }

// #19: rand.Int error used to be discarded, then otpN.Int64() nil-deref panicked.
func TestCreateTrip_RandFailure_ReturnsError_NoPanic(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()
	_, err := db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('rt-rand', 'tenant-1', 'A', 'B', 10, 1, 100)`)
	require.NoError(t, err)

	old := rand.Reader
	rand.Reader = failingRand{}
	defer func() { rand.Reader = old }()

	uc := NewCreateTripUseCase(unitOfWork, fixedIDGen{}, clk)
	var tripID aggregate.TripID
	require.NotPanics(t, func() {
		tripID, err = uc.Execute(context.Background(), CreateTripCommand{
			TenantID:      shared.TenantID("tenant-1"),
			RouteID:       "rt-rand",
			DepartureTime: time.Now().Add(time.Hour),
		})
	})
	require.Error(t, err, "rand failure must be returned, not nil-dereferenced")
	_ = tripID
}
