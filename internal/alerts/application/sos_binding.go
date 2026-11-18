package application

// Trip/vehicle binding for driver SOS, moved out of internal/handlers/sos.go
// (handlers-end-state ADR, Phase 2). The HTTP layer must stay at zero raw SQL;
// this file owns the tenant-scoped lookups that turn untrusted client-supplied
// trip/vehicle IDs into the IDs a SOS is actually attributed to.

import (
	"context"
	"errors"

	"transport-app/internal/shared"
)

// Binding rejections. The handler renders each as an HTTP 400 with the error
// text as the message; keeping them here preserves the exact legacy wording.
var (
	ErrSOSTripNotActive   = errors.New("trip_id is not an active trip for this driver")
	ErrSOSVehicleMismatch = errors.New("vehicle_id does not match the trip")
	ErrSOSVehicleForeign  = errors.New("vehicle_id is not in this tenant")
)

// SOSTargets is the resolved, trusted tenant/trip/vehicle a SOS is attributed to.
type SOSTargets struct {
	TenantID  string
	TripID    string
	VehicleID string
}

// ResolveSOSTargets binds untrusted client trip/vehicle IDs to the authenticated
// driver and tenant, applies the emergency tenant fallback, and fills the
// active trip/vehicle when either was omitted. A non-nil error is one of the
// ErrSOS* binding errors, which the handler renders as a 400.
func (s *SOSService) ResolveSOSTargets(ctx context.Context, driverID, tripID, vehicleID string) (SOSTargets, error) {
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = s.DriverTenant(ctx, driverID)
	}
	if s.db == nil {
		// No way to validate: never emit caller-provided IDs.
		return SOSTargets{TenantID: tenantID}, nil
	}

	if tenantID == "" {
		// No way to attribute the IDs — never emit caller-provided ones.
		tripID, vehicleID = "", ""
	} else if tripID != "" {
		var tripVehicleID string
		if err := s.db.QueryRowContext(ctx, `
			SELECT vehicle_id FROM trips WHERE id = $1 AND driver_id = $2 AND tenant_id = $3`,
			tripID, driverID, tenantID).Scan(&tripVehicleID); err != nil {
			return SOSTargets{}, ErrSOSTripNotActive
		}
		if vehicleID != "" && vehicleID != tripVehicleID {
			return SOSTargets{}, ErrSOSVehicleMismatch
		}
		vehicleID = tripVehicleID
	} else if vehicleID != "" {
		var one int
		if err := s.db.QueryRowContext(ctx, `
			SELECT 1 FROM vehicles WHERE id = $1 AND tenant_id = $2`,
			vehicleID, tenantID).Scan(&one); err != nil {
			return SOSTargets{}, ErrSOSVehicleForeign
		}
	}

	if tripID == "" || vehicleID == "" {
		t, v := s.ActiveTrip(ctx, driverID)
		if tripID == "" {
			tripID = t
		}
		if vehicleID == "" {
			vehicleID = v
		}
	}
	return SOSTargets{TenantID: tenantID, TripID: tripID, VehicleID: vehicleID}, nil
}
