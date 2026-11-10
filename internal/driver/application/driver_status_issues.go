package application

// Driver mobile status + issue endpoints, extracted from
// internal/handlers/drivers.go (handlers-end-state ADR, Phase 2). Legacy
// tenant-unscoped identity lookups, status transitions with the
// deactivated_at KYC clock and issue insert/list semantics are preserved.

import (
	"context"

	"github.com/google/uuid"
)

// DriverIssue is one driver_issues row rendered by
// GET /api/v1/drivers/me/issues (field names are the legacy JSON shape).
type DriverIssue struct {
	ID        string `json:"id"`
	TripID    string `json:"trip_id,omitempty"`
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	PhotoURL  string `json:"photo_url,omitempty"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// UpdateMyStatus flips the acting driver's duty status. Only
// available/leave/inactive are accepted by the handler; on_trip stays
// system-controlled by the trip lifecycle and is rejected there.
//
// The identity lookup keeps its legacy tenant-unscoped rule, and the status
// write keeps the migration-00185 semantics: inactive stamps deactivated_at
// once, available/leave clear it, on_trip (impossible through this path)
// leaves it untouched. kyc_purged_at is never modified.
func (s *DriverSelfService) UpdateMyStatus(ctx context.Context, userID, status string) (string, error) {
	driverID, err := s.ResolveActorDriverID(ctx, userID)
	if err != nil {
		return "", err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE drivers
		 SET status = $1,
		     updated_at = CURRENT_TIMESTAMP,
		     deactivated_at = CASE
		         WHEN $1 = 'inactive' THEN COALESCE(deactivated_at, CURRENT_TIMESTAMP)
		         ELSE NULL END
		 WHERE id = $2`,
		status, driverID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrDriverNotFound
	}
	return driverID, nil
}

// ReportIssueCommand is the validated issue payload (the handler parses the
// multipart form, applies category/severity defaults and uploads the photo).
type ReportIssueCommand struct {
	DriverID string
	TripID   string
	Category string
	Severity string
	Message  string
	PhotoURL string
}

// ReportIssue inserts a driver issue, keeping the legacy twin INSERT (with and
// without a trip id) and the NULL photo_url representation.
func (s *DriverSelfService) ReportIssue(ctx context.Context, cmd ReportIssueCommand) (string, error) {
	if s.db == nil {
		return "", ErrDriverNotFound
	}
	issueID := uuid.NewString()
	var err error
	if cmd.TripID != "" {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO driver_issues (id, driver_id, trip_id, category, severity, message, photo_url)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			issueID, cmd.DriverID, cmd.TripID, cmd.Category, cmd.Severity, cmd.Message, nullableString(cmd.PhotoURL))
	} else {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO driver_issues (id, driver_id, category, severity, message, photo_url)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			issueID, cmd.DriverID, cmd.Category, cmd.Severity, cmd.Message, nullableString(cmd.PhotoURL))
	}
	if err != nil {
		return "", err
	}
	return issueID, nil
}

// ListMyIssues returns the acting driver's 20 newest issues. The legacy
// identity lookup is tenant-unscoped, and per-row scan failures are skipped.
func (s *DriverSelfService) ListMyIssues(ctx context.Context, userID string) ([]DriverIssue, error) {
	driverID, err := s.ResolveActorDriverID(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(trip_id,''), category, severity, message, COALESCE(photo_url,''), status, created_at
		FROM driver_issues WHERE driver_id = $1 ORDER BY created_at DESC LIMIT 20`, driverID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	issues := []DriverIssue{}
	for rows.Next() {
		var issue DriverIssue
		if err := rows.Scan(&issue.ID, &issue.TripID, &issue.Category, &issue.Severity, &issue.Message,
			&issue.PhotoURL, &issue.Status, &issue.CreatedAt); err == nil {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

// nullableString maps an empty string to NULL, matching the legacy
// nullIfEmpty helper used for driver_issues.photo_url.
func nullableString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
