package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/ports"
	"transport-app/internal/trip/domain"
	"transport-app/internal/trip/domain/aggregate"
	vehicleDomain "transport-app/internal/vehicle/domain"
	vehicleAgg "transport-app/internal/vehicle/domain/aggregate"
)

type AssignVehicleCommand struct {
	TripID              aggregate.TripID
	VehicleID           string
	TenantID            shared.TenantID
	OverrideMaintenance bool
	OverrideReason      string
}

type AssignVehicleUseCase struct {
	uow   ports.UnitOfWork
	clock ports.Clock
}

func NewAssignVehicleUseCase(uow ports.UnitOfWork, clock ports.Clock) *AssignVehicleUseCase {
	return &AssignVehicleUseCase{uow: uow, clock: clock}
}

func (uc *AssignVehicleUseCase) Execute(ctx context.Context, cmd AssignVehicleCommand) error {
	if cmd.VehicleID == "" {
		return errors.New("vehicle ID is required")
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
		// Attribute any compliance-override audit to the trip's own tenant
		// when the caller didn't specify one — never the bootstrap tenant.
		if cmd.TenantID == "" {
			cmd.TenantID = t.TenantID
		}
		if err := uc.checkVehicleCompliance(txCtx, cmd); err != nil {
			return err
		}
		conflicts, err := repo.CheckVehicleConflict(txCtx, cmd.VehicleID, cmd.TenantID, string(cmd.TripID), t.DepartureTime, t.ArrivalTime)
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("vehicle %s has conflicting trips: %s", cmd.VehicleID, conflicts[0].TripNumber)
		}
		if err := t.AssignVehicle(cmd.VehicleID, uc.clock.Now()); err != nil {
			return err
		}
		if err := repo.Save(txCtx, t); err != nil {
			return err
		}
		logAudit(txCtx, ActionAssign, string(t.ID), nil, nil)
		return nil
	})
}

func (uc *AssignVehicleUseCase) checkVehicleCompliance(ctx ports.TxContext, cmd AssignVehicleCommand) error {
	vehicleRepo, ok := ctx.Repositories().Vehicles().(vehicleDomain.VehicleRepository)
	if !ok {
		return errors.New("failed to retrieve vehicle repository")
	}
	v, err := vehicleRepo.Find(ctx, vehicleAgg.VehicleID(cmd.VehicleID), cmd.TenantID)
	if err != nil {
		return fmt.Errorf("vehicle %s not found: %w", cmd.VehicleID, err)
	}
	if v.Status == vehicleAgg.VehicleInactive || v.Status == vehicleAgg.VehicleMaintenance {
		return fmt.Errorf("vehicle %s is not assignable (status: %s)", cmd.VehicleID, v.Status)
	}
	now := uc.clock.Now().Truncate(24 * time.Hour)
	for _, expiry := range []struct {
		name string
		when time.Time
	}{{"insurance", v.InsuranceExpiry}, {"fitness", v.FitnessExpiry}, {"permit", v.PermitExpiry}} {
		if !expiry.when.IsZero() && expiry.when.Before(now) {
			if isExempt(ctx, "vehicle", cmd.VehicleID, expiry.name) {
				recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, expiry.name, "warning", "bypassed by exemption")
				continue
			}
			if cmd.OverrideMaintenance && len(strings.TrimSpace(cmd.OverrideReason)) >= 10 {
				reasonJSON := fmt.Sprintf(`{"vehicle_id":%q,"blocked_by":%q,"reason":%q}`, cmd.VehicleID, expiry.name, cmd.OverrideReason)
				logAudit(ctx, "assign_vehicle_override", string(cmd.TripID), nil, &reasonJSON)
				// dispatch_overrides schema is owned by migration 00073 — no
				// runtime DDL. A failed audit insert fails the override loudly:
				// a compliance override without an audit trail must not land.
				if dbGetter, ok := ctx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
					tenant := cmd.TenantID
					if tenant == "" {
						return fmt.Errorf("cannot record dispatch override audit: tenant unknown")
					}
					overriddenBy := ""
					if uid := getUserID(ctx); uid != nil {
						overriddenBy = string(*uid)
					}
					blockedBy := expiry.name + "_expiry"
					if _, ierr := dbGetter.DB().ExecContext(ctx, `INSERT INTO dispatch_overrides (id, tenant_id, trip_id, vehicle_id, driver_id, blocked_by, reason, overridden_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
						fmt.Sprintf("ovr-%d", time.Now().UnixNano()), string(tenant), string(cmd.TripID), cmd.VehicleID, "", blockedBy, cmd.OverrideReason, overriddenBy); ierr != nil {
						return ierr
					}
				}
				recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, expiry.name, "warning", "bypassed by override")
				continue
			}
			recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, expiry.name, "expired", fmt.Sprintf("vehicle %s expired", expiry.name))
			return fmt.Errorf("Dispatch blocked: vehicle %s expired (compliance)", expiry.name)
		} else if !expiry.when.IsZero() && expiry.when.Before(now.Add(7*24*time.Hour)) {
			recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, expiry.name, "warning", fmt.Sprintf("vehicle %s expires in <7 days", expiry.name))
		}
	}

	// PUC expiry check (Spec 05 §5)
	if puc := getPUCExpiry(ctx, cmd.VehicleID); puc != nil && !puc.IsZero() {
		if puc.Before(now) {
			if !isExempt(ctx, "vehicle", cmd.VehicleID, "puc") {
				if cmd.OverrideMaintenance && len(strings.TrimSpace(cmd.OverrideReason)) >= 10 {
					reasonJSON := fmt.Sprintf(`{"vehicle_id":%q,"blocked_by":"puc","reason":%q}`, cmd.VehicleID, cmd.OverrideReason)
					logAudit(ctx, "assign_vehicle_override", string(cmd.TripID), nil, &reasonJSON)
					if dbGetter, ok := ctx.Repositories().AuditLogs().(repository.DBGetter); ok && dbGetter.DB() != nil {
						tenant := cmd.TenantID
						if tenant == "" {
							return fmt.Errorf("cannot record dispatch override audit: tenant unknown")
						}
						_, _ = dbGetter.DB().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dispatch_overrides (
                            id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '1', trip_id TEXT NOT NULL, vehicle_id TEXT, driver_id TEXT, blocked_by TEXT NOT NULL, reason TEXT NOT NULL, overridden_by TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT (datetime('now'))
                        )`)
						overriddenBy := ""
						if uid := getUserID(ctx); uid != nil {
							overriddenBy = string(*uid)
						}
						_, _ = dbGetter.DB().ExecContext(ctx, `INSERT INTO dispatch_overrides (id, tenant_id, trip_id, vehicle_id, driver_id, blocked_by, reason, overridden_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
							fmt.Sprintf("ovr-%d", time.Now().UnixNano()), string(tenant), string(cmd.TripID), cmd.VehicleID, "", "puc_expiry", cmd.OverrideReason, overriddenBy)
					}
					recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, "puc", "warning", "bypassed by override")
				} else {
					recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, "puc", "expired", "vehicle PUC expired")
					return fmt.Errorf("Dispatch blocked: vehicle PUC expired (compliance)")
				}
			} else {
				recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, "puc", "warning", "bypassed by exemption")
			}
		} else if puc.Before(now.Add(7 * 24 * time.Hour)) {
			recordComplianceCheck(ctx, "vehicle", cmd.VehicleID, "puc", "warning", "vehicle PUC expires in <7 days")
		}
	}

	// Maintenance block check (Spec 04 §6, §12)
	if maintRepo, ok := ctx.Repositories().Maintenance().(*maintsql.MaintenanceRepository); ok {
		blocked, reason, err := maintRepo.IsMaintenanceBlocked(ctx, cmd.VehicleID)
		if err == nil && blocked {
			if !cmd.OverrideMaintenance {
				return errors.New(reason)
			}
			reasonJSON := fmt.Sprintf(`{"vehicle_id":%q,"reason":%q}`, cmd.VehicleID, cmd.OverrideReason)
			logAudit(ctx, "assign_vehicle_override", string(cmd.TripID), nil, &reasonJSON)
		}
	}

	return nil
}

func getPUCExpiry(ctx ports.TxContext, vehicleID string) *time.Time {
	dbGetter, ok := ctx.Repositories().AuditLogs().(repository.DBGetter)
	if !ok || dbGetter == nil || dbGetter.DB() == nil {
		return nil
	}
	var t sql.NullTime
	_ = dbGetter.DB().QueryRowContext(ctx, `SELECT puc_expiry FROM vehicles WHERE id = ?`, vehicleID).Scan(&t)
	if t.Valid && !t.Time.IsZero() {
		return &t.Time
	}
	return nil
}
