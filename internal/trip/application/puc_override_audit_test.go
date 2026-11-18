package application

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/trip/domain/aggregate"
)

// PUC compliance overrides must be audited in dispatch_overrides (schema owned
// by migration 00073) with strict error checks, like the RC/insurance path.
// A failed audit INSERT must fail the whole override loudly — never be swallowed
// by a `_, _ =` (which used to also mask a runtime CREATE TABLE with a
// hardcoded tenant default).
func TestAssignVehicle_PUCOverride_AuditInsertFailure_BlocksOverride(t *testing.T) {
	db := newTripTestDB(t)
	unitOfWork := uow.NewSQLUnitOfWork(db)
	clk := clock.NewRealClock()

	tripID := uuid.NewString()
	vehID := "veh-puc-audit"
	driverID := "drv-puc-audit"

	seedTestDriver(t, db, driverID)
	seedTestTrip(t, db, tripID)
	seedTestVehicle(t, db, vehID, false)
	_, err := db.Exec(`UPDATE vehicles SET puc_expiry = date('now','-1 day') WHERE id = ?`, vehID)
	require.NoError(t, err)

	_, err = db.Exec(`CREATE TRIGGER fail_override_insert BEFORE INSERT ON dispatch_overrides
		BEGIN SELECT RAISE(ABORT, 'forced override audit failure'); END`)
	require.NoError(t, err)

	require.NoError(t, NewAssignDriverUseCase(unitOfWork, clk).Execute(context.Background(), AssignDriverCommand{
		TripID:   aggregate.TripID(tripID),
		DriverID: driverID,
		TenantID: shared.TenantID("tenant-1"),
	}))

	err = NewAssignVehicleUseCase(unitOfWork, clk).Execute(context.Background(), AssignVehicleCommand{
		TripID:              aggregate.TripID(tripID),
		VehicleID:           vehID,
		TenantID:            shared.TenantID("tenant-1"),
		OverrideMaintenance: true,
		OverrideReason:      "urgent dispatch under manager approval",
	})
	require.Error(t, err, "failed audit insert must surface and block the override")
	assert.Contains(t, err.Error(), "forced override audit failure")

	var overrides int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM dispatch_overrides WHERE trip_id = ?`, tripID).Scan(&overrides))
	assert.Equal(t, 0, overrides, "no override row may land when its audit insert fails")

	var assigned sql.NullString
	require.NoError(t, db.QueryRow(`SELECT vehicle_id FROM trips WHERE id = ?`, tripID).Scan(&assigned))
	assert.False(t, assigned.Valid, "override must not take effect; got vehicle %q", assigned.String)
}
