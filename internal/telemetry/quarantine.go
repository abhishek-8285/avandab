package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"transport-app/internal/repository"
)

// Quarantine reasons.
const (
	QuarantineReasonUnknownDevice     = "unknown_device"
	QuarantineReasonRetiredDevice     = "retired_device"
	QuarantineReasonQuarantinedDevice = "quarantined_device"
	QuarantineReasonUnassignedDevice  = "unassigned_device"
)

// QuarantineStatus constants matching the CHECK constraint in migration 00040.
const (
	QuarantineStatusOpen     = "open"
	QuarantineStatusResolved = "resolved"
	QuarantineStatusRejected = "rejected"
)

// QuarantineEntry represents a row in device_quarantine.
type QuarantineEntry struct {
	ID         string
	TenantID   string
	IMEI       string
	Source     string
	RawPayload string
	Reason     string
	Status     string
	ResolvedBy *string
	ResolvedAt *time.Time
	CreatedAt  time.Time
}

// QuarantineStore provides raw-SQL persistence for device_quarantine.
type QuarantineStore struct {
	db *sql.DB
}

// NewQuarantineStore constructs a QuarantineStore.
func NewQuarantineStore(db *sql.DB) *QuarantineStore {
	return &QuarantineStore{db: db}
}

func (s *QuarantineStore) dbFromContext(ctx context.Context) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return s.db
}

// Quarantine inserts a quarantine entry for a frame that cannot be processed.
// The raw payload is stored verbatim for admin review.
func (s *QuarantineStore) Quarantine(ctx context.Context, entry QuarantineEntry) error {
	db := s.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO device_quarantine
        (id, tenant_id, imei, source, raw_payload, reason, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ID, entry.TenantID, entry.IMEI, entry.Source,
		entry.RawPayload, entry.Reason, entry.Status, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("quarantine insert: %w", err)
	}
	return nil
}

// ListOpen returns open quarantine entries, newest first.
func (s *QuarantineStore) ListOpen(ctx context.Context, tenantID string, limit int) ([]QuarantineEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, imei, source, raw_payload, reason, status,
		        resolved_by, resolved_at, created_at
		 FROM device_quarantine
		 WHERE tenant_id = $1 AND status = $2
		 ORDER BY created_at DESC LIMIT $3`,
		tenantID, QuarantineStatusOpen, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []QuarantineEntry
	for rows.Next() {
		var e QuarantineEntry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.IMEI, &e.Source,
			&e.RawPayload, &e.Reason, &e.Status, &e.ResolvedBy,
			&e.ResolvedAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Resolve marks a quarantine entry as resolved or rejected. Uses the
// transaction from the context when running inside a UnitOfWork.
func (s *QuarantineStore) Resolve(ctx context.Context, id, status, resolvedBy string) error {
	db := s.dbFromContext(ctx)
	_, err := db.ExecContext(ctx,
		`UPDATE device_quarantine
		 SET status = $1, resolved_by = $2, resolved_at = CURRENT_TIMESTAMP
		 WHERE id = $3 AND status = $4`,
		status, resolvedBy, id, QuarantineStatusOpen)
	return err
}
