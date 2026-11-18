package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"transport-app/internal/trip/domain"
)

// A booking can own a cancelled first trip plus a live replacement. Cancelling
// the booking must find the replacement, not just the oldest row.
func TestFindActiveByBookingID_ReturnsReplacement(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-active", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

	first := newTestTripAgg("tr-first", "1", "TR-ACT-1", ptr("bk-active"), "route-active", now.Add(time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, first))
	require.NoError(t, first.Cancel(now))
	require.NoError(t, repo.Save(ctx, first))

	replacement := newTestTripAgg("tr-replacement", "1", "TR-ACT-2", ptr("bk-active"), "route-active", now.Add(2*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, replacement))

	finder, ok := repo.(domain.ActiveTripRepository)
	require.True(t, ok, "repository must expose active-trip lookup")
	trips, err := finder.FindActiveByBookingID(ctx, "bk-active", "1")
	require.NoError(t, err)
	require.Len(t, trips, 1)
	require.Equal(t, "tr-replacement", string(trips[0].ID))
}

func ptr(s string) *string { return &s }
