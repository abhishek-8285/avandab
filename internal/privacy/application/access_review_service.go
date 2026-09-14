package application

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"transport-app/internal/repository"
	"transport-app/internal/shared/ports"
)

// Periodic access re-certification ledger (UN-style 6/12-month access
// reviews). One row per (tenant, user, period): open a pending row for the
// review period (e.g. '2026-H2'), then certify (access still needed) or
// revoke (access withdrawn + note). Due dates are reviewer-chosen per row —
// passed as explicit params, so no clock seam is needed; the due watchlist
// derives from stored due_at in queries. Reads are tenant-scoped
// fail-closed: a foreign id reads as not-found, never as another org's row.
// Routes reuse privacy:manage (00154 backfill, roles 1 + 6): reviewing who
// holds access is the same governance surface as breach reporting, and
// users:manage is platform-only (org_admin must run its own reviews).
const (
	AccessReviewStatusPending   = "pending"
	AccessReviewStatusCertified = "certified"
	AccessReviewStatusRevoked   = "revoked"
)

// accessReviewTimeLayout is the cross-engine DATETIME literal (same pattern
// as the breach overdue cutoff): lexicographic compare == time compare.
const accessReviewTimeLayout = "2006-01-02 15:04:05"

// AccessReview is one row of access_reviews (00155).
type AccessReview struct {
	ID         string
	TenantID   string
	UserID     string
	RoleName   string
	Period     string
	Status     string
	ReviewedBy sql.NullString
	ReviewedAt sql.NullTime
	DueAt      time.Time
	Notes      string
}

// AccessReviewService administers the re-certification ledger. Auto-opening
// reviews on a schedule (cron sweeping users every 6/12 months) is an
// explicit non-goal of this slice — opening starts with a reviewer filing
// the period.
type AccessReviewService struct {
	db    *sql.DB
	idGen ports.IDGenerator
}

// NewAccessReviewService wires the ledger over the caller's DB.
func NewAccessReviewService(db *sql.DB, idGen ports.IDGenerator) *AccessReviewService {
	return &AccessReviewService{db: db, idGen: idGen}
}

func scanAccessReview(row *sql.Row, rows *sql.Rows, r *AccessReview) error {
	if rows != nil {
		return rows.Scan(&r.ID, &r.TenantID, &r.UserID, &r.RoleName, &r.Period,
			&r.Status, &r.ReviewedBy, &r.ReviewedAt, &r.DueAt, &r.Notes)
	}
	return row.Scan(&r.ID, &r.TenantID, &r.UserID, &r.RoleName, &r.Period,
		&r.Status, &r.ReviewedBy, &r.ReviewedAt, &r.DueAt, &r.Notes)
}

const accessReviewColumns = `id, tenant_id, user_id, role_name, period,
	status, reviewed_by, reviewed_at, due_at, notes`

// OpenDueReview files (or re-opens idempotently) the pending row for one
// user + period. Re-open is a no-op returning the existing id: it never
// resets a certified/revoked decision — file a new period to re-review.
func (s *AccessReviewService) OpenDueReview(ctx context.Context, tenantID, userID, roleName, period string, dueAt time.Time) (string, error) {
	if userID == "" {
		return "", errors.New("user_id is required")
	}
	if period == "" {
		return "", errors.New("period is required")
	}
	if dueAt.IsZero() {
		return "", errors.New("due_at is required")
	}
	id := "rev_" + s.idGen.GenerateUUID()
	_, err := repository.ExecTx(ctx, s.db, `
		INSERT INTO access_reviews (id, tenant_id, user_id, role_name, period, due_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, user_id, period) DO NOTHING`,
		id, tenantID, userID, roleName, period, dueAt.UTC().Format(accessReviewTimeLayout))
	if err != nil {
		return "", err
	}
	var got string
	if err := repository.QueryRowTx(ctx, s.db, `
		SELECT id FROM access_reviews
		WHERE tenant_id = $1 AND user_id = $2 AND period = $3`,
		tenantID, userID, period).Scan(&got); err != nil {
		return "", err
	}
	return got, nil
}

// GetAccessReview reads one review inside the caller's tenant (fail-closed).
func (s *AccessReviewService) GetAccessReview(ctx context.Context, tenantID, id string) (*AccessReview, error) {
	var r AccessReview
	err := scanAccessReview(repository.QueryRowTx(ctx, s.db,
		`SELECT `+accessReviewColumns+` FROM access_reviews WHERE tenant_id = $1 AND id = $2`,
		tenantID, id), nil, &r)
	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	return &r, err
}

// CertifyAccessReview records the reviewer keeping the grant (pending only;
// revoked/certified rows reject — file a new period to re-review).
func (s *AccessReviewService) CertifyAccessReview(ctx context.Context, tenantID, id, reviewedBy string) error {
	if reviewedBy == "" {
		return errors.New("reviewed_by is required")
	}
	res, err := repository.ExecTx(ctx, s.db, `
		UPDATE access_reviews
		SET reviewed_by = $3, reviewed_at = CURRENT_TIMESTAMP,
		    status = 'certified', updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2 AND status = 'pending'`,
		tenantID, id, reviewedBy)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RevokeAccessReview records the reviewer withdrawing the grant, from pending
// or certified alike (empty note keeps the existing notes).
func (s *AccessReviewService) RevokeAccessReview(ctx context.Context, tenantID, id, reviewedBy, note string) error {
	if reviewedBy == "" {
		return errors.New("reviewed_by is required")
	}
	res, err := repository.ExecTx(ctx, s.db, `
		UPDATE access_reviews
		SET reviewed_by = $3, reviewed_at = CURRENT_TIMESTAMP,
		    status = 'revoked',
		    notes = CASE WHEN $4 = '' THEN notes ELSE $4 END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE tenant_id = $1 AND id = $2 AND status IN ('pending', 'certified')`,
		tenantID, id, reviewedBy, note)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListDueAccessReviews returns the tenant's pending reviews past due — the
// re-certification watchlist. Cutoff computed in Go and passed as a param
// (same cross-engine pattern as the breach overdue query).
func (s *AccessReviewService) ListDueAccessReviews(ctx context.Context, tenantID string) ([]AccessReview, error) {
	cutoff := time.Now().UTC().Format(accessReviewTimeLayout)
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+accessReviewColumns+` FROM access_reviews
		WHERE tenant_id = $1 AND status = 'pending' AND due_at <= $2
		ORDER BY due_at ASC`, tenantID, cutoff)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []AccessReview
	for rows.Next() {
		var r AccessReview
		if err := scanAccessReview(nil, rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
