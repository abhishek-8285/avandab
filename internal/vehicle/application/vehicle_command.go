package application

// AV command writes, extracted from internal/handlers/vehicles.go
// (handlers-end-state ADR, Phase 2). The legacy INSERT plus the actuation
// status UPDATEs are preserved verbatim: the command row is the audit trail (
// status acknowledged), the vehicle-status change is keyed by BOTH vehicle id
// and tenant, and every status-update error is ignored because the command was
// already logged (the handler still redirects).

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// ErrInvalidVehicleCommand reports a command outside the migration-00137
// CHECK list. The handler maps it to the legacy 400 "invalid command type".
var ErrInvalidVehicleCommand = errors.New("invalid command type")

// vehicleCommandTypes mirrors the vehicle_commands.command_type CHECK list
// from migration 00137.
var vehicleCommandTypes = map[string]bool{
	"E_STOP":          true,
	"HOLD_POSITION":   true,
	"RESUME_MISSION":  true,
	"SET_SPEED_LIMIT": true,
	"REROUTE":         true,
	"RETURN_TO_BASE":  true,
}

// IsValidVehicleCommand reports whether the AV cockpit accepts the command.
func IsValidVehicleCommand(commandType string) bool {
	return vehicleCommandTypes[commandType]
}

// VehicleCommandService records teleoperation commands and applies their
// vehicle-status actuation. It is stateless and cheap to construct per request.
type VehicleCommandService struct {
	db *sql.DB
}

// NewVehicleCommandService wires the command writer against the database.
func NewVehicleCommandService(db *sql.DB) *VehicleCommandService {
	return &VehicleCommandService{db: db}
}

// SendVehicleCommand is one operator command to one vehicle.
type SendVehicleCommand struct {
	TenantID    shared.TenantID
	VehicleID   string
	CommandType string
	IssuedBy    string
}

// Send records the command (legacy id prefix "cmd-"), then applies the
// actuation status: E_STOP blocks with the operator-facing reason,
// RESUME_MISSION runs and clears the block reason, HOLD_POSITION parks the
// vehicle as available. Other command types leave vehicle status untouched.
func (s *VehicleCommandService) Send(ctx context.Context, cmd SendVehicleCommand) error {
	if !IsValidVehicleCommand(cmd.CommandType) {
		return ErrInvalidVehicleCommand
	}
	if s.db == nil {
		return errors.New("vehicle command store unavailable")
	}
	commandID := "cmd-" + uuid.NewString()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vehicle_commands (id, tenant_id, vehicle_id, command_type, status, issued_by)
		VALUES ($1, $2, $3, $4, 'acknowledged', $5)`,
		commandID, string(cmd.TenantID), cmd.VehicleID, cmd.CommandType, cmd.IssuedBy)
	if err != nil {
		return err
	}

	// Actuation feedback is best-effort in the legacy handler: the command was
	// already logged, so a failed status update never fails the request.
	switch cmd.CommandType {
	case "E_STOP":
		_, _ = s.db.ExecContext(ctx, `UPDATE vehicles SET status = 'blocked', blocked_reason = 'AV E-STOP Triggered' WHERE id = $1 AND tenant_id = $2`, cmd.VehicleID, string(cmd.TenantID))
	case "RESUME_MISSION":
		_, _ = s.db.ExecContext(ctx, `UPDATE vehicles SET status = 'running', blocked_reason = NULL WHERE id = $1 AND tenant_id = $2`, cmd.VehicleID, string(cmd.TenantID))
	case "HOLD_POSITION":
		_, _ = s.db.ExecContext(ctx, `UPDATE vehicles SET status = 'available' WHERE id = $1 AND tenant_id = $2`, cmd.VehicleID, string(cmd.TenantID))
	}
	return nil
}
