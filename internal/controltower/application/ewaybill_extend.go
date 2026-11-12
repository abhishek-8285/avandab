package application

// Command Center inline E-Way Bill extend use-case, extracted from
// internal/handlers/console.go (handlers-end-state ADR, Phase 2; Spec 22
// §2.3/§10 Step 4). The handler keeps the JSON body decode + 1..24h
// validation, HTTP status mapping and the response JSON; this file keeps the
// tenant-scoped e-way-bill lookup, the EWB_EXTEND_ENABLED provider seam, the
// validity update, the EXTENDED eway_bill_events row (best-effort) and the
// audit_logs write.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/shared"
)

// ExtendEwayBillCommand carries the console inline-extend inputs.
type ExtendEwayBillCommand struct {
	EWBID    string
	TenantID string
	ActorID  string
	Hours    int
}

// ExtendEwayBillUseCase extends an E-Way Bill from the Command Center inline
// action. It is stateless and cheap to construct per request.
type ExtendEwayBillUseCase struct {
	db      *sql.DB
	client  intEWB.Client
	enabled bool // EWB_EXTEND_ENABLED: consult the real provider adapter
}

// NewExtendEwayBillUseCase wires the use-case. A nil client with the flag on
// behaves like the legacy handler (local expiry shift only).
func NewExtendEwayBillUseCase(db *sql.DB, client intEWB.Client, extendEnabled bool) *ExtendEwayBillUseCase {
	return &ExtendEwayBillUseCase{db: db, client: client, enabled: extendEnabled}
}

// Extend resolves the tenant-scoped EWB, shifts/sharpens its validity (the
// provider adapter result wins when EWB_EXTEND_ENABLED and the call succeeds),
// updates the row, appends the EXTENDED event and writes the audit row.
// Unknown ids and foreign-tenant ids return ErrConsoleNotFound (handler 404);
// event/audit write failures stay best-effort exactly like the legacy code.
func (uc *ExtendEwayBillUseCase) Extend(ctx context.Context, cmd ExtendEwayBillCommand) (time.Time, error) {
	if uc == nil || uc.db == nil {
		return time.Time{}, ErrConsoleNotFound
	}
	var tripID sql.NullString
	var validUntil time.Time
	err := uc.db.QueryRowContext(ctx, `
		SELECT e.trip_id, e.valid_until
		FROM eway_bills e JOIN trips t ON e.trip_id = t.id
		WHERE e.id = $1 AND t.tenant_id = $2`, cmd.EWBID, cmd.TenantID).
		Scan(&tripID, &validUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrConsoleNotFound
	}
	if err != nil {
		return time.Time{}, err
	}

	newExpiry := validUntil.Add(time.Duration(cmd.Hours) * time.Hour)
	if uc.enabled && uc.client != nil {
		var ewbNumber string
		if nErr := uc.db.QueryRowContext(ctx,
			`SELECT ewb_number FROM eway_bills WHERE id = $1`, cmd.EWBID).Scan(&ewbNumber); nErr == nil {
			if resp, extErr := uc.client.Extend(ctx, ewbNumber, intEWB.ExtendRequest{
				EwbNumber: ewbNumber,
				Reason:    "console_inline_extend",
			}); extErr == nil && !resp.ValidUpto.IsZero() {
				newExpiry = resp.ValidUpto
			}
		}
	}

	if _, uErr := uc.db.ExecContext(ctx,
		`UPDATE eway_bills SET valid_until = $1 WHERE id = $2`,
		newExpiry, cmd.EWBID); uErr != nil {
		return time.Time{}, uErr
	}
	if _, eErr := uc.db.ExecContext(ctx, `
		INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by)
		SELECT $1, ewb_number, trip_id, 'EXTENDED', $2, $3 FROM eway_bills WHERE id = $4`,
		uuid.NewString(), fmt.Sprintf(`{"hours":%d}`, cmd.Hours), cmd.ActorID, cmd.EWBID); eErr != nil {
		slog.Warn("eway_bill_events insert failed", "ewb", cmd.EWBID, "error", eErr)
	}

	_ = WriteAuditLog(ctx, uc.db, cmd.ActorID, string(shared.TenantOrPlatform(ctx)), "ewaybill.extend", "eway_bills", cmd.EWBID, map[string]any{
		"new_expiry":       newExpiry.UTC().Format(time.RFC3339),
		"valid_upto_hours": cmd.Hours,
	})

	return newExpiry, nil
}

// WriteAuditLog records one audit_logs row (id, user, action, table, record,
// new_values JSON, tenant, created_at). Callers treat failures as
// best-effort - the legacy helper swallowed them. Shared by the Command
// Center inline actions and the vertical-slice presentation packages that
// previously called handlers.WriteAuditLog.
func WriteAuditLog(ctx context.Context, db *sql.DB, userID, tenantID, action, table, recordID string, newValues any) error {
	blob, err := json.Marshal(newValues)
	if err != nil {
		return err
	}
	if db == nil {
		return nil
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, tenant_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)`,
		uuid.NewString(), userID, action, table, recordID, string(blob), tenantID)
	return err
}
