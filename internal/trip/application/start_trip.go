package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	driverDomain "transport-app/internal/driver/domain"
	driverAgg "transport-app/internal/driver/domain/aggregate"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
	vehicleDomain "transport-app/internal/vehicle/domain"
	vehicleAgg "transport-app/internal/vehicle/domain/aggregate"
)

// StartTripCommand contains parameters to transition a trip to started.
type StartTripCommand struct {
	TripID         aggregate.TripID
	TenantID       shared.TenantID
	StartOdometer  *float64
	GateFacilityID string
}

// StartTripUseCase orchestrates starting a trip.
type StartTripUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

// NewStartTripUseCase creates a new StartTripUseCase.
func NewStartTripUseCase(uow ports.UnitOfWork, clock ports.Clock) *StartTripUseCase {
	return &StartTripUseCase{uow: uow, clock: clock}
}

// Execute performs the transition.
func (uc *StartTripUseCase) Execute(ctx context.Context, cmd StartTripCommand) error {
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}

		// A trip must have an assigned driver before it can move. Vehicle
		// assignment remains optional at dispatch start to accommodate
		// spot/contractual carrier dispatches where vehicles are assigned at
		// the gate register. When a vehicle is assigned, full compliance gate
		// checks (RC, insurance, fitness, PUC, blacklists) are strictly enforced below.
		if t.DriverID == nil || *t.DriverID == "" {
			return errors.New("driver must be assigned before trip can start")
		}

		// Re-validate full compliance gate before allowing trip to start (Spec 05 §5)
		if t.DriverID != nil && *t.DriverID != "" {
			if err := uc.checkDriverCompliance(txCtx, *t.DriverID, cmd.TenantID); err != nil {
				return err
			}
		}
		if t.VehicleID != nil && *t.VehicleID != "" {
			if err := uc.checkVehicleCompliance(txCtx, *t.VehicleID, cmd.TenantID); err != nil {
				return err
			}
		}

		if err := t.Start(uc.clock.Now()); err != nil {
			return err
		}

		if cmd.StartOdometer != nil {
			if err := t.RecordStartReading(*cmd.StartOdometer, cmd.GateFacilityID, uc.clock.Now()); err != nil {
				return err
			}
		} else if t.VehicleID != nil && *t.VehicleID != "" {
			// Fallback to vehicle's current odometer if available
			vehicleRepo, ok := txCtx.Repositories().Vehicles().(vehicleDomain.VehicleRepository)
			if ok {
				if v, err := vehicleRepo.Find(txCtx, vehicleAgg.VehicleID(*t.VehicleID), cmd.TenantID); err == nil && v != nil && v.Odometer > 0 {
					_ = t.RecordStartReading(v.Odometer, cmd.GateFacilityID, uc.clock.Now())
				}
			}
			if cmd.GateFacilityID != "" && t.GateFacilityID == "" {
				t.GateFacilityID = cmd.GateFacilityID
			}
		} else if cmd.GateFacilityID != "" {
			t.GateFacilityID = cmd.GateFacilityID
		}

		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionStart, string(t.ID), nil, nil)
		return nil
	})
}

func (uc *StartTripUseCase) checkDriverCompliance(ctx ports.TxContext, driverID string, tenantID shared.TenantID) error {
	driverRepo, ok := ctx.Repositories().Drivers().(driverDomain.DriverRepository)
	if !ok {
		return errors.New("failed to retrieve driver repository")
	}
	d, err := driverRepo.Find(ctx, driverAgg.DriverID(driverID), tenantID)
	if err != nil {
		return fmt.Errorf("driver %s not found: %w", driverID, err)
	}
	if d.Status == driverAgg.DriverInactive || d.Status == driverAgg.DriverLeave {
		return fmt.Errorf("driver %s is not assignable (status: %s)", driverID, d.Status)
	}
	now := uc.clock.Now().Truncate(24 * time.Hour)
	if !d.LicenseExpiry.IsZero() && d.LicenseExpiry.Before(now) {
		if isExempt(ctx, "driver", driverID, "license") {
			return nil
		}
		return fmt.Errorf("Dispatch blocked: driver license expired (compliance)")
	}
	return nil
}

func (uc *StartTripUseCase) checkVehicleCompliance(ctx ports.TxContext, vehicleID string, tenantID shared.TenantID) error {
	vehicleRepo, ok := ctx.Repositories().Vehicles().(vehicleDomain.VehicleRepository)
	if !ok {
		return errors.New("failed to retrieve vehicle repository")
	}
	v, err := vehicleRepo.Find(ctx, vehicleAgg.VehicleID(vehicleID), tenantID)
	if err != nil {
		return fmt.Errorf("vehicle %s not found: %w", vehicleID, err)
	}
	if v.Status == vehicleAgg.VehicleInactive || v.Status == vehicleAgg.VehicleMaintenance {
		return fmt.Errorf("vehicle %s is not assignable (status: %s)", vehicleID, v.Status)
	}
	now := uc.clock.Now().Truncate(24 * time.Hour)
	for _, expiry := range []struct {
		name string
		when time.Time
	}{{"insurance", v.InsuranceExpiry}, {"fitness", v.FitnessExpiry}, {"permit", v.PermitExpiry}} {
		if !expiry.when.IsZero() && expiry.when.Before(now) {
			if isExempt(ctx, "vehicle", vehicleID, expiry.name) {
				continue
			}
			return fmt.Errorf("Dispatch blocked: vehicle %s expired (compliance)", expiry.name)
		}
	}

	// PUC check
	if puc := getPUCExpiry(ctx, vehicleID); puc != nil && !puc.IsZero() && puc.Before(now) {
		if !isExempt(ctx, "vehicle", vehicleID, "puc") {
			return fmt.Errorf("Dispatch blocked: vehicle PUC expired (compliance)")
		}
	}

	return nil
}
