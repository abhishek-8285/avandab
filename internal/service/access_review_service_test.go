package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccessReview_LifecycleCertifyRevoke(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_review_1"

	require.NoError(t, execTenant(t, svcs, tenant))

	due := time.Now().UTC().Add(-time.Hour)
	id, err := svcs.AccessReviews.OpenDueReview(ctx, tenant, "u-1", "dispatcher", "2026-H2", due)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	// Re-open is idempotent: same id, no reset.
	again, err := svcs.AccessReviews.OpenDueReview(ctx, tenant, "u-1", "dispatcher", "2026-H2", due)
	require.NoError(t, err)
	require.Equal(t, id, again)

	r, err := svcs.AccessReviews.GetAccessReview(ctx, tenant, id)
	require.NoError(t, err)
	require.Equal(t, AccessReviewStatusPending, r.Status)
	require.Equal(t, "2026-H2", r.Period)

	// Foreign tenant reads as not-found (fail-closed).
	_, err = svcs.AccessReviews.GetAccessReview(ctx, "other_tenant", id)
	require.ErrorIs(t, err, sql.ErrNoRows)

	// Pending → certified.
	require.NoError(t, svcs.AccessReviews.CertifyAccessReview(ctx, tenant, id, "boss-1"))
	r, _ = svcs.AccessReviews.GetAccessReview(ctx, tenant, id)
	require.Equal(t, AccessReviewStatusCertified, r.Status)
	require.True(t, r.ReviewedAt.Valid)
	require.Equal(t, "boss-1", r.ReviewedBy.String)

	// Certified rows reject re-certify.
	require.ErrorIs(t, svcs.AccessReviews.CertifyAccessReview(ctx, tenant, id, "boss-1"), sql.ErrNoRows)

	// Certified → revoked with note.
	require.NoError(t, svcs.AccessReviews.RevokeAccessReview(ctx, tenant, id, "boss-1", "left the org"))
	r, _ = svcs.AccessReviews.GetAccessReview(ctx, tenant, id)
	require.Equal(t, AccessReviewStatusRevoked, r.Status)
	require.Equal(t, "left the org", r.Notes)

	// Revoked is terminal.
	require.ErrorIs(t, svcs.AccessReviews.RevokeAccessReview(ctx, tenant, id, "boss-1", "again"), sql.ErrNoRows)
}

func TestAccessReview_PendingRevokesDirectly(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_review_1b"

	require.NoError(t, execTenant(t, svcs, tenant))

	id, err := svcs.AccessReviews.OpenDueReview(ctx, tenant, "u-9", "driver", "2026-H2", time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)

	// Pending → revoked without a prior certify; empty note keeps default.
	require.NoError(t, svcs.AccessReviews.RevokeAccessReview(ctx, tenant, id, "boss-1", ""))
	r, _ := svcs.AccessReviews.GetAccessReview(ctx, tenant, id)
	require.Equal(t, AccessReviewStatusRevoked, r.Status)
	require.Equal(t, "", r.Notes)
}

// Due watchlist = pending AND due_at <= now. Explicit past/future due dates;
// the clock itself stays inside ListDueAccessReviews (breach-overdue pattern).
func TestAccessReview_DueWatchlist(t *testing.T) {
	svcs := newPrivacyTestServices(t)
	ctx := context.Background()
	const tenant = "tenant_review_2"

	require.NoError(t, execTenant(t, svcs, tenant))

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(72 * time.Hour)

	overdueID, err := svcs.AccessReviews.OpenDueReview(ctx, tenant, "u-due", "dispatcher", "2026-H1", past)
	require.NoError(t, err)
	_, err = svcs.AccessReviews.OpenDueReview(ctx, tenant, "u-future", "dispatcher", "2026-H1", future)
	require.NoError(t, err)

	due, err := svcs.AccessReviews.ListDueAccessReviews(ctx, tenant)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, overdueID, due[0].ID)

	// Certifying clears the watchlist.
	require.NoError(t, svcs.AccessReviews.CertifyAccessReview(ctx, tenant, overdueID, "boss-1"))
	due, err = svcs.AccessReviews.ListDueAccessReviews(ctx, tenant)
	require.NoError(t, err)
	require.Empty(t, due)
}
