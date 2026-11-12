package application

import (
	"context"
	"database/sql"
	"log/slog"

	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

// BlockVehicleForBreakdownCommand records a vehicle breakdown reported in the
// trip-close dialog (SOP close, ZMOTM_MMS p.8).
type BlockVehicleForBreakdownCommand struct {
	TenantID shared.TenantID
	TripID   aggregate.TripID
	// VehicleID is the vehicle recorded on the trip at close; empty resolves it
	// from the trip row (the same fallback the handler used).
	VehicleID string
	Note      string
}

// BreakdownResult reports the resolved vehicle and the description used for
// both the blocked_reason and the ops-alert body.
type BreakdownResult struct {
	VehicleID   string
	Description string
}

// BlockVehicleForBreakdownUseCase flags the trip's vehicle as
// maintenance-blocked after a breakdown at trip close.
type BlockVehicleForBreakdownUseCase struct {
	db *sql.DB
}

// NewBlockVehicleForBreakdownUseCase wires the use-case against the database.
func NewBlockVehicleForBreakdownUseCase(db *sql.DB) *BlockVehicleForBreakdownUseCase {
	return &BlockVehicleForBreakdownUseCase{db: db}
}

// Execute resolves the vehicle and updates it. Best-effort by design: the
// legacy handler ignored write errors, so the close and the ops alert never
// depended on the vehicle write.
func (uc *BlockVehicleForBreakdownUseCase) Execute(ctx context.Context, cmd BlockVehicleForBreakdownCommand) BreakdownResult {
	vehicleID := cmd.VehicleID
	if vehicleID == "" && uc.db != nil {
		var v sql.NullString
		if err := uc.db.QueryRowContext(ctx, `SELECT vehicle_id FROM trips WHERE id = $1 AND tenant_id = $2`, string(cmd.TripID), string(cmd.TenantID)).Scan(&v); err != nil {
			slog.WarnContext(ctx, "trip close breakdown vehicle lookup failed", slog.String("trip_id", string(cmd.TripID)), slog.Any("error", err))
		}
		if v.Valid {
			vehicleID = v.String
		}
	}
	desc := "Breakdown reported at trip close"
	if cmd.Note != "" {
		desc += ": " + cmd.Note
	}
	if vehicleID != "" {
		desc += " (vehicle " + vehicleID + ")"
		if uc.db != nil {
			if _, err := uc.db.ExecContext(ctx, `UPDATE vehicles SET status = 'maintenance', blocked = 1, blocked_reason = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND tenant_id = $3`,
				desc, vehicleID, string(cmd.TenantID)); err != nil {
				slog.WarnContext(ctx, "trip close breakdown vehicle block failed",
					slog.String("trip_id", string(cmd.TripID)), slog.String("vehicle_id", vehicleID), slog.Any("error", err))
			}
		}
	}
	return BreakdownResult{VehicleID: vehicleID, Description: desc}
}
