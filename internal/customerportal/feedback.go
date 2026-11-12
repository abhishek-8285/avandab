package customerportal

// Shipper feedback write path, extracted from
// internal/handlers/customer_portal.go (handlers-end-state ADR, Phase 2).
// The handler keeps JSON/form parsing, the trip_id/rating/comment validation
// and the response shapes; this file keeps the customer_users-scoped trip
// ownership lookup (404 vs 403 discrimination), the legacy missing-table
// fallbacks and the trip_feedback insert.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrFeedbackTripNotFound reports a trip id that does not exist in the tenant.
	ErrFeedbackTripNotFound = errors.New("feedback: trip not found")
	// ErrFeedbackForbidden reports a tenant trip that is not linked to the shipper.
	ErrFeedbackForbidden = errors.New("feedback: trip not in your shipments")
	// ErrFeedbackCustomerUnlinked reports the legacy missing-table fallback with
	// no derivable customer id.
	ErrFeedbackCustomerUnlinked = errors.New("feedback: trip not found or customer not linked")
)

// FeedbackInsertError wraps a trip_feedback insert failure so the handler can
// keep the legacy "failed to save feedback: <err>" message and log line.
type FeedbackInsertError struct {
	Err error
}

// Error implements error.
func (e *FeedbackInsertError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying database error.
func (e *FeedbackInsertError) Unwrap() error { return e.Err }

// FeedbackRequest carries the validated feedback fields.
type FeedbackRequest struct {
	TripID  string
	Rating  int
	Comment string
}

// FeedbackService writes shipper feedback. It is stateless and cheap to
// construct per request.
type FeedbackService struct {
	db *sql.DB
}

// NewFeedbackService wires the feedback write path against the database.
func NewFeedbackService(db *sql.DB) *FeedbackService {
	return &FeedbackService{db: db}
}

// Create resolves the shipper-owned customer id for the trip and inserts the
// feedback row, returning its id. Typed errors preserve the handler's
// 404/403/500 mapping; the legacy "no such table" fallbacks (unscoped
// customer derivation + lazy trip_feedback create) are preserved verbatim.
func (s *FeedbackService) Create(ctx context.Context, tenantID, userID string, req FeedbackRequest) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("feedback: database unavailable")
	}

	// Resolve customer_id for this shipper via scoped query:
	// SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	var customerID string
	err := s.db.QueryRowContext(ctx, `
		SELECT b.customer_id
		FROM trips t
		JOIN bookings b ON b.id = t.booking_id
		WHERE t.id = $1 AND t.tenant_id = $2 AND b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=$3)
	`, req.TripID, tenantID, userID).Scan(&customerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Distinguish access denied from not found.
			var exists int
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trips WHERE id = $1 AND tenant_id = $2`, req.TripID, tenantID).Scan(&exists)
			if exists == 0 {
				return "", ErrFeedbackTripNotFound
			}
			return "", ErrFeedbackForbidden
		}
		if strings.Contains(err.Error(), "no such table") {
			// Fallback: derive customer_id without scope when tables are missing.
			_ = s.db.QueryRowContext(ctx, `
				SELECT b.customer_id FROM trips t JOIN bookings b ON b.id = t.booking_id WHERE t.id = $1 AND t.tenant_id = $2
			`, req.TripID, tenantID).Scan(&customerID)
			if customerID == "" {
				return "", ErrFeedbackCustomerUnlinked
			}
		} else {
			return "", err
		}
	}

	feedbackID := uuid.NewString()
	insert := func() error {
		_, insertErr := s.db.ExecContext(ctx, `
			INSERT INTO trip_feedback (id, tenant_id, trip_id, customer_id, rating, comment, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, feedbackID, tenantID, req.TripID, customerID, req.Rating, req.Comment, time.Now().UTC().Format("2006-01-02 15:04:05"))
		return insertErr
	}
	insertErr := insert()
	if insertErr != nil {
		if strings.Contains(insertErr.Error(), "no such table") {
			// Auto-create minimal table for dev/test when migration 00073 is absent.
			_, _ = s.db.ExecContext(ctx, `
				CREATE TABLE IF NOT EXISTS trip_feedback (
					id TEXT PRIMARY KEY,
					tenant_id TEXT NOT NULL DEFAULT '1',
					trip_id TEXT NOT NULL REFERENCES trips(id),
					customer_id TEXT NOT NULL,
					rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
					comment TEXT NOT NULL DEFAULT '',
					created_at TEXT NOT NULL DEFAULT (CURRENT_TIMESTAMP)
				)
			`)
			insertErr = insert()
		}
		if insertErr != nil {
			return "", &FeedbackInsertError{Err: insertErr}
		}
	}
	return feedbackID, nil
}
