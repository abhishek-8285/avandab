package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"transport-app/internal/shared"
)

// MirrorPODArtifactsCommand mirrors a stop POD into the legacy trips columns
// and stop_pod_attachments rows for cross-compatibility with older pages.
type MirrorPODArtifactsCommand struct {
	TenantID     shared.TenantID
	TripID       string
	StopID       string
	PODURL       string
	SignatureURL string
	Notes        string
}

// MirrorPODArtifactsUseCase keeps the legacy POD denormalisation in step with
// the trip aggregate.
type MirrorPODArtifactsUseCase struct {
	db *sql.DB
}

// NewMirrorPODArtifactsUseCase wires the use-case against the database.
func NewMirrorPODArtifactsUseCase(db *sql.DB) *MirrorPODArtifactsUseCase {
	return &MirrorPODArtifactsUseCase{db: db}
}

// Execute performs the legacy writes sequentially. No transaction: the legacy
// handler ran them as independent best-effort statements, and joined errors
// are returned only for logging.
func (uc *MirrorPODArtifactsUseCase) Execute(ctx context.Context, cmd MirrorPODArtifactsCommand) error {
	if uc.db == nil {
		return nil
	}
	var errs []error
	if _, err := uc.db.ExecContext(ctx, `
			UPDATE trips
			SET pod_url = COALESCE(NULLIF($1, ''), pod_url),
			    pod_photo_url = COALESCE(NULLIF($2, ''), pod_photo_url),
			    pod_signature_url = COALESCE(NULLIF($3, ''), pod_signature_url),
			    pod_notes = COALESCE(NULLIF($4, ''), pod_notes),
			    pod_captured_at = CURRENT_TIMESTAMP
			WHERE (id = $5 OR trip_number = $6) AND tenant_id = $7`,
		cmd.PODURL, cmd.PODURL, cmd.SignatureURL, cmd.Notes, cmd.TripID, cmd.TripID, string(cmd.TenantID),
	); err != nil {
		errs = append(errs, fmt.Errorf("mirror pod onto trip: %w", err))
	}
	if cmd.PODURL != "" && cmd.StopID != "" {
		if _, err := uc.db.ExecContext(ctx, `
				INSERT INTO stop_pod_attachments (id, tenant_id, stop_id, trip_id, file_url, file_type, created_at)
				VALUES ($1, $2, $3, $4, $5, 'photo', CURRENT_TIMESTAMP)`,
			uuid.NewString(), string(cmd.TenantID), cmd.StopID, cmd.TripID, cmd.PODURL,
		); err != nil {
			errs = append(errs, fmt.Errorf("mirror pod attachment: %w", err))
		}
	}
	if cmd.SignatureURL != "" && cmd.StopID != "" {
		if _, err := uc.db.ExecContext(ctx, `
				INSERT INTO stop_pod_attachments (id, tenant_id, stop_id, trip_id, file_url, file_type, created_at)
				VALUES ($1, $2, $3, $4, $5, 'signature', CURRENT_TIMESTAMP)`,
			uuid.NewString(), string(cmd.TenantID), cmd.StopID, cmd.TripID, cmd.SignatureURL,
		); err != nil {
			errs = append(errs, fmt.Errorf("mirror pod signature attachment: %w", err))
		}
	}
	return errors.Join(errs...)
}
