package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/auth"
	legacydomain "transport-app/internal/domain"
	"transport-app/internal/shared"
)

// AuditLogger is the audit-log facade used for compliance-override records.
// The legacy audit service (*service.AuditLogService) satisfies it. A nil
// logger means the raw audit_logs fallback, mirroring the legacy handler
// behaviour when no audit service was wired.
type AuditLogger interface {
	LogAction(ctx context.Context, userID *legacydomain.UserID, action, tableName, recordID string, oldValues, newValues *string) error
}

// RecordDispatchOverrideCommand is one compliance override to persist.
type RecordDispatchOverrideCommand struct {
	TenantID     shared.TenantID // the trip's tenant, never the bootstrap tenant
	TripID       string
	VehicleID    string
	DriverID     string
	BlockedBy    string // compliance blocker code (license_expiry, insurance_expiry, ...)
	Reason       string // operator's override justification (>= 10 chars)
	OverriddenBy string // acting user id
}

// RecordDispatchOverrideUseCase persists the dispatch_overrides audit row and
// the matching audit-log entry for a dispatch compliance override (Spec 21 §5).
type RecordDispatchOverrideUseCase struct {
	db    *sql.DB
	audit AuditLogger
}

// NewRecordDispatchOverrideUseCase wires the use-case. Both dependencies are
// optional: a nil db skips persistence, a nil audit logger selects the raw
// audit_logs fallback.
func NewRecordDispatchOverrideUseCase(db *sql.DB, audit AuditLogger) *RecordDispatchOverrideUseCase {
	return &RecordDispatchOverrideUseCase{db: db, audit: audit}
}

// Execute records the override and its audit trail. The legacy handler
// discarded every write error (the override still proceeded), so this method
// is best-effort: it attempts each write and returns joined errors for logging
// without changing the dispatch response.
func (uc *RecordDispatchOverrideUseCase) Execute(ctx context.Context, cmd RecordDispatchOverrideCommand) error {
	var errs []error
	if uc.db != nil {
		// dispatch_overrides is migration-owned (00073); no runtime DDL.
		if _, err := uc.db.ExecContext(ctx, `INSERT INTO dispatch_overrides (id, tenant_id, trip_id, vehicle_id, driver_id, blocked_by, reason, overridden_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			uuid.NewString(), string(cmd.TenantID), cmd.TripID, cmd.VehicleID, cmd.DriverID, cmd.BlockedBy, cmd.Reason, cmd.OverriddenBy, time.Now().UTC().Format("2006-01-02 15:04:05")); err != nil {
			errs = append(errs, fmt.Errorf("record dispatch override: %w", err))
		}
	}
	if uc.audit != nil {
		uid := legacydomain.UserID(cmd.OverriddenBy)
		reason := cmd.Reason
		if err := uc.audit.LogAction(ctx, &uid, "dispatch_override", "trips", cmd.TripID, nil, &reason); err != nil {
			errs = append(errs, fmt.Errorf("audit dispatch override: %w", err))
		}
	} else if uc.db != nil {
		// Raw fallback matches the legacy inline INSERT: same tenant resolution
		// (TenantOrPlatform) and request IP attribution.
		auditID := uuid.NewString()
		var ipVal *string
		if ip, ok := ctx.Value(auth.ContextIP).(string); ok && ip != "" {
			ipVal = &ip
		}
		if _, err := uc.db.ExecContext(ctx, `INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, ip_address, tenant_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)`,
			auditID, cmd.OverriddenBy, "dispatch_override", "trips", cmd.TripID, cmd.Reason, ipVal, string(shared.TenantOrPlatform(ctx))); err != nil {
			errs = append(errs, fmt.Errorf("audit dispatch override fallback: %w", err))
		}
	}
	return errors.Join(errs...)
}
