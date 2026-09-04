package sql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A driver finishing an earlier trip must be assignable to a later,
// non-overlapping trip. The old status-only check blocked this forever.
func TestTripRepository_CheckDriverConflict_TimeWindow(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	// Existing trip: departs +2h, arrives +4h, driver assigned.
	agg := newTestTripAgg("tr-win-1", "1", "TR-WIN-01", nil, "route-1", now.Add(2*time.Hour), "Window", now)
	arr := now.Add(4 * time.Hour)
	agg.ArrivalTime = &arr
	require.NoError(t, agg.Schedule(now))
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))

	// Later window (+10h..+12h): no overlap → assignable.
	laterEnd := now.Add(12 * time.Hour)
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now.Add(10*time.Hour), &laterEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)

	// Overlapping window (+3h..+5h): conflict.
	overEnd := now.Add(5 * time.Hour)
	conflicts, err = repo.CheckDriverConflict(ctx, "drv-1", "1", "", now.Add(3*time.Hour), &overEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-win-1", conflicts[0].ID)

	// Earlier window ending before departure (+0h..+1h): no overlap.
	earlyEnd := now.Add(1 * time.Hour)
	conflicts, err = repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, &earlyEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
}

// Open-ended trips (no arrival recorded) conservatively conflict with any
// later window — an unfinished trip still holds its driver.
func TestTripRepository_CheckDriverConflict_OpenEnded(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	agg := newTestTripAgg("tr-open-1", "1", "TR-OPEN-01", nil, "route-1", now.Add(2*time.Hour), "Open", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))

	laterEnd := now.Add(72 * time.Hour)
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now.Add(48*time.Hour), &laterEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
}

// Same window semantics for vehicles.
func TestTripRepository_CheckVehicleConflict_TimeWindow(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	agg := newTestTripAgg("tr-vwin-1", "1", "TR-VWIN-01", nil, "route-1", now.Add(2*time.Hour), "VWindow", now)
	arr := now.Add(4 * time.Hour)
	agg.ArrivalTime = &arr
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, agg.AssignVehicle("veh-1", now))
	require.NoError(t, repo.Save(ctx, agg))

	laterEnd := now.Add(12 * time.Hour)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now.Add(10*time.Hour), &laterEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)

	overEnd := now.Add(5 * time.Hour)
	conflicts, err = repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now.Add(3*time.Hour), &overEnd)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-vwin-1", conflicts[0].ID)
}
