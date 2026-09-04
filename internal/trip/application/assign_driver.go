package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	driverDomain "transport-app/internal/driver/domain"
	driverAgg "transport-app/internal/driver/domain/aggregate"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
)

type AssignDriverCommand struct {
	TripID              aggregate.TripID
	DriverID            string
	TenantID            shared.TenantID
	OverrideMaintenance bool
	OverrideReason      string
}

type AssignDriverUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

func NewAssignDriverUseCase(uow ports.UnitOfWork, clock ports.Clock) *AssignDriverUseCase {
	return &AssignDriverUseCase{uow: uow, clock: clock}
}

func (uc *AssignDriverUseCase) Execute(ctx context.Context, cmd AssignDriverCommand) error {
	if cmd.DriverID == "" {
		return errors.New("driver ID is required")
	}
	return uc.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		repo, ok := txCtx.Repositories().Trips().(domain.TripRepository)
		if !ok {
			return errors.New("failed to retrieve trip repository")
		}
		t, err := repo.Find(txCtx, cmd.TripID, cmd.TenantID)
		if err != nil {
			return err
		}
		if err := uc.checkDriverCompliance(txCtx, cmd.DriverID, cmd.TenantID); err != nil {
			// Compliance override: allow if override flag with reason >=10 chars (Spec 21 §5 EnforceWithOverride)
			if cmd.OverrideMaintenance && len(strings.TrimSpace(cmd.OverrideReason)) >= 10 {
				reasonJSON := fmt.Sprintf(`{"driver_id":%q,"reason":%q,"blocked_by":%q}`, cmd.DriverID, cmd.OverrideReason, err.Error())
				logAudit(txCtx, "assign_driver_override", string(cmd.TripID), nil, &reasonJSON)
				// dispatch_overrides schema is owned by migration 00073 — no
				// runtime DDL. A failed audit insert fails the override loudly:
				// a compliance override without an audit trail must not land.
				if dbGetter, ok := txCtx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
					// Attribute the override to the trip's own tenant —
					// never the bootstrap tenant (cross-org audit leak).
					tenant := cmd.TenantID
					if tenant == "" {
						tenant = t.TenantID
					}
					if tenant == "" {
						return fmt.Errorf("cannot record dispatch override audit: tenant unknown")
					}
					var vehicleID string
					if t.VehicleID != nil {
						vehicleID = *t.VehicleID
					}
					overriddenBy := ""
					if uid := getUserID(txCtx); uid != nil {
						overriddenBy = string(*uid)
					}
					blockedBy := "license_expiry"
					if strings.Contains(strings.ToLower(err.Error()), "license") {
						blockedBy = "license_expiry"
					}
					if _, ierr := dbGetter.DB().ExecContext(txCtx, `INSERT INTO dispatch_overrides (id, tenant_id, trip_id, vehicle_id, driver_id, blocked_by, reason, overridden_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
						fmt.Sprintf("ovr-%d", time.Now().UnixNano()), string(tenant), string(cmd.TripID), vehicleID, cmd.DriverID, blockedBy, cmd.OverrideReason, overriddenBy); ierr != nil {
						return fmt.Errorf("record dispatch override audit: %w", ierr)
					}
				}
			} else {
				return err
			}
		}
		// When trip already carries a vehicle, ensure that vehicle is not blocked for maintenance
		if t.VehicleID != nil && *t.VehicleID != "" {
			if maintRepo, ok := txCtx.Repositories().Maintenance().(*maintsql.MaintenanceRepository); ok {
				blocked, reason, err := maintRepo.IsMaintenanceBlocked(txCtx, *t.VehicleID)
				if err == nil && blocked {
					if !cmd.OverrideMaintenance {
						return errors.New(reason)
					}
					reasonJSON := fmt.Sprintf(`{"vehicle_id":%q,"driver_id":%q,"reason":%q}`, *t.VehicleID, cmd.DriverID, cmd.OverrideReason)
					logAudit(txCtx, "assign_driver_override", string(cmd.TripID), nil, &reasonJSON)
				}
			}
		}
		// Overlap check against the trip's own window: a driver running an
		// earlier trip that ends before this one departs is assignable.
		conflicts, err := repo.CheckDriverConflict(txCtx, cmd.DriverID, cmd.TenantID, string(cmd.TripID), t.DepartureTime, t.ArrivalTime)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("driver %s has conflicting trips: %s", cmd.DriverID, conflicts[0].TripNumber)
		}
		if err := t.AssignDriver(cmd.DriverID, uc.clock.Now()); err != nil {
			return err
		}
		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionAssign, string(t.ID), nil, nil)
		return nil
	})
}

func (uc *AssignDriverUseCase) checkDriverCompliance(ctx ports.TxContext, driverID string, tenantID shared.TenantID) error {
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
			recordComplianceCheck(ctx, "driver", driverID, "license", "warning", "bypassed by exemption")
			return nil
		}
		recordComplianceCheck(ctx, "driver", driverID, "license", "expired", "driver license expired")
		return fmt.Errorf("Dispatch blocked: driver license expired (compliance)")
	}
	return nil
}

func isExempt(ctx ports.TxContext, entityType, entityID, docType string) bool {
	dbGetter, ok := ctx.Repositories().AuditLogs().(repository.DBGetter)
	if !ok || dbGetter == nil || dbGetter.DB() == nil {
		return false
	}
	var id string
	err := dbGetter.DB().QueryRowContext(ctx, `
		SELECT id FROM compliance_exemptions
		WHERE entity_type = ? AND entity_id = ? AND (doc_type = ? OR doc_type = 'all' OR (? = 'permit' AND doc_type = 'rc') OR (? = 'rc' AND doc_type = 'permit'))
		  AND exempt_until > CURRENT_TIMESTAMP
		LIMIT 1`, entityType, entityID, docType, docType, docType).Scan(&id)
	return err == nil && id != ""
}

func recordComplianceCheck(ctx ports.TxContext, entityType, entityID, checkType, status, details string) {
	dbGetter, ok := ctx.Repositories().AuditLogs().(repository.DBGetter)
	if !ok || dbGetter == nil || dbGetter.DB() == nil {
		return
	}
	id := fmt.Sprintf("chk-%d", time.Now().UnixNano())
	_, _ = dbGetter.DB().ExecContext(ctx, `
		INSERT INTO compliance_checks (id, entity_type, entity_id, check_type, status, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, entityType, entityID, checkType, status, details)
}
