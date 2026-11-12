package domain_test

import (
	"testing"

	"transport-app/internal/dispatch/domain"
)

func TestDispatchErrors(t *testing.T) {
	errs := []error{
		domain.ErrTenantRequired,
		domain.ErrTargetRequired,
		domain.ErrRunNotFound,
		domain.ErrNoStops,
		domain.ErrNoVehicles,
		domain.ErrStopNotFound,
		domain.ErrRunCommitted,
		domain.ErrInvalidStopType,
		domain.ErrBookingUnknown,
		domain.ErrRouteNotFound,
		domain.ErrInvalidOp,
		domain.ErrInvalidSeq,
	}

	for _, err := range errs {
		if err == nil || err.Error() == "" {
			t.Fatalf("expected non-empty domain error, got: %v", err)
		}
	}
}
