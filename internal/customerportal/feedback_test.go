package customerportal

// Characterization + extraction tests for the shipper feedback write path
// moved out of internal/handlers/customer_portal.go (handlers-end-state ADR,
// Phase 2): customer_users-scoped trip ownership, 404-vs-403 discrimination,
// legacy missing-table fallbacks, insert row shape and error wrapping.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackService_CreateScopedRowShape(t *testing.T) {
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)
	svc := NewFeedbackService(db)

	id, err := svc.Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 5, Comment: "great service"})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	var gotTenant, gotTrip, gotCustomer, gotComment, gotCreated string
	var gotRating int
	require.NoError(t, db.QueryRow(`SELECT tenant_id, trip_id, customer_id, rating, comment, created_at FROM trip_feedback WHERE id = ?`, id).
		Scan(&gotTenant, &gotTrip, &gotCustomer, &gotRating, &gotComment, &gotCreated))
	assert.Equal(t, "tenant-1", gotTenant)
	assert.Equal(t, "t-1", gotTrip)
	assert.Equal(t, "c-1", gotCustomer)
	assert.Equal(t, 5, gotRating)
	assert.Equal(t, "great service", gotComment)
	assert.Len(t, gotCreated, 19, "legacy UTC format YYYY-MM-DD HH:MM:SS")
}

func TestFeedbackService_OwnershipRules(t *testing.T) {
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)
	svc := NewFeedbackService(db)
	ctx := context.Background()

	// Owned trip works.
	_, err := svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 4})
	require.NoError(t, err)

	// Same tenant, another customer's trip -> forbidden (403 in handler).
	_, err = svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-2", Rating: 4})
	assert.ErrorIs(t, err, ErrFeedbackForbidden)

	// Other tenant's trip -> not found (404), never leaked as forbidden.
	_, err = svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-3", Rating: 4})
	assert.ErrorIs(t, err, ErrFeedbackTripNotFound)

	// Unknown trip -> not found.
	_, err = svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-ghost", Rating: 4})
	assert.ErrorIs(t, err, ErrFeedbackTripNotFound)

	// User without a customer_users link -> forbidden for an existing trip.
	_, err = svc.Create(ctx, "tenant-1", "u-9", FeedbackRequest{TripID: "t-1", Rating: 4})
	assert.ErrorIs(t, err, ErrFeedbackForbidden)

	assert.Equal(t, int64(1), countPortalRows(t, db, `SELECT COUNT(*) FROM trip_feedback`), "rejected attempts must not insert")
}

func TestFeedbackService_NoDuplicateGuard(t *testing.T) {
	// FROZEN LEGACY BEHAVIOR: the handler/service never checked for an existing
	// feedback row, so a shipper could submit twice. Preserved verbatim; a
	// future duplicate rule is a deliberate product change, not this migration.
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)
	svc := NewFeedbackService(db)
	ctx := context.Background()

	_, err := svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 5, Comment: "first"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 3, Comment: "second"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), countPortalRows(t, db, `SELECT COUNT(*) FROM trip_feedback WHERE trip_id = 't-1'`))
}

func TestFeedbackService_LegacyLazyTableCreate(t *testing.T) {
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)
	mustExecPortal(t, db, `DROP TABLE trip_feedback`)

	id, err := NewFeedbackService(db).Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 5, Comment: "lazy"})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Equal(t, int64(1), countPortalRows(t, db, `SELECT COUNT(*) FROM trip_feedback WHERE id = ?`, id), "legacy lazy CREATE TABLE + retry insert")
}

func TestFeedbackService_LegacyUnscopedFallbackWhenTablesMissing(t *testing.T) {
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)
	mustExecPortal(t, db, `DROP TABLE customer_users`)

	// The scoped query errors with "no such table"; the legacy fallback derives
	// the customer without customer_users scoping and still inserts.
	_, err := NewFeedbackService(db).Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 5})
	require.NoError(t, err)
	assert.Equal(t, int64(1), countPortalRows(t, db, `SELECT COUNT(*) FROM trip_feedback`))

	// Fallback with no derivable customer -> the legacy unlinked 404 sentinel.
	_, err = NewFeedbackService(db).Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-ghost", Rating: 5})
	assert.ErrorIs(t, err, ErrFeedbackCustomerUnlinked)
}

func TestFeedbackService_InsertErrorWrapped(t *testing.T) {
	db := newPortalTestDB(t)
	seedPortalFeedbackFixture(t, db)

	// Rating 0 bypasses the handler's 1..5 validation and trips the DB CHECK;
	// the handler keeps the legacy "failed to save feedback: <err>" message.
	_, err := NewFeedbackService(db).Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 0})
	require.Error(t, err)
	var insErr *FeedbackInsertError
	require.True(t, errors.As(err, &insErr), "insert failures must be distinguishable from lookup failures")
	assert.Contains(t, insErr.Err.Error(), "CHECK")
	assert.Zero(t, countPortalRows(t, db, `SELECT COUNT(*) FROM trip_feedback`))
}

func TestFeedbackService_NilDB(t *testing.T) {
	_, err := NewFeedbackService(nil).Create(context.Background(), "tenant-1", "u-1", FeedbackRequest{TripID: "t-1", Rating: 5})
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrFeedbackTripNotFound))
	assert.False(t, errors.Is(err, ErrFeedbackForbidden))
}

func countPortalRows(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}
