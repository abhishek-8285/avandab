package application

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"transport-app/internal/dispatch/domain"
	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

func TestService_RejectsIncompleteAssignmentCommands(t *testing.T) {
	svc := NewService(nil, nil)
	err := svc.AssignDriver(context.Background(), AssignDriverCommand{TripID: aggregate.TripID("trip-1"), DriverID: "drv-1"})
	require.ErrorIs(t, err, domain.ErrTenantRequired)
	err = svc.AssignVehicle(context.Background(), AssignVehicleCommand{TenantID: shared.TenantID("t1"), TripID: aggregate.TripID("trip-1")})
	require.ErrorIs(t, err, domain.ErrTargetRequired)
}
