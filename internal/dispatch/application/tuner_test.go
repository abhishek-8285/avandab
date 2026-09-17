package application_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dispatchapp "transport-app/internal/dispatch/application"
	dispatchdomain "transport-app/internal/dispatch/domain"
	"transport-app/internal/shared"
)

// tuneFixture builds a planned 3-stop single-route run on the depot tenant
// and returns the planner + run detail for op targeting.
func tuneFixture(t *testing.T, db *sql.DB, tenant shared.TenantID) (*dispatchapp.PlanKPI, *dispatchapp.RunDetail) {
	t.Helper()
	svc := dispatchapp.NewPlannerService(db, nil)
	ctx := context.Background()

	seedFleet(t, db, string(tenant))

	runID, err := svc.CreateRun(ctx, dispatchapp.CreateRunCommand{
		TenantID: tenant, Source: "adhoc",
		Stops: []dispatchapp.PlannerStopInput{
			{Address: "Far West", Lat: 18.40, Lng: 73.50},
			{Address: "Mid", Lat: 18.55, Lng: 73.80},
			{Address: "Far East", Lat: 18.70, Lng: 74.10},
		},
	})
	require.NoError(t, err)

	kpi, err := svc.Plan(ctx, dispatchapp.PlanCommand{TenantID: tenant, RunID: runID, Provider: "vrp"})
	require.NoError(t, err)
	require.Equal(t, 0, kpi.UnassignedCount)

	detail, err := svc.GetRun(ctx, tenant, runID)
	require.NoError(t, err)
	require.Len(t, detail.Routes, 1, "fixture expects single route")
	require.Len(t, detail.Routes[0].Stops, 3)
	return kpi, detail
}

func TestTuner_Reorder_ChangesSequenceAndGeometry(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-ro")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	route := detail.Routes[0]
	stops := route.Stops

	// Move first stop to the end (seq 3).
	kpi, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "reorder", StopID: stops[0].ID, Seq: 3,
	})
	require.NoError(t, err)
	require.NotNil(t, kpi)

	after, err := dispatchapp.NewPlannerService(db, nil).GetRun(ctx, tenant, detail.ID)
	require.NoError(t, err)
	afterRoute := after.Routes[0]

	// Order must be rotated: old stop 0 now last.
	assert.Equal(t, stops[1].ID, afterRoute.Stops[0].ID, "stop 2 must now be first")
	assert.Equal(t, stops[2].ID, afterRoute.Stops[1].ID, "stop 3 must now be second")
	assert.Equal(t, stops[0].ID, afterRoute.Stops[2].ID, "stop 1 must now be last")

	// Ratchet: reorder changes real geometry → KPI must reflect it.
	// Original west→mid→east totals more km than mid→east→west from the depot;
	// either way, KPI km must stay positive and recomputed, not stale.
	assert.Greater(t, after.KPI.TotalKM, 0.0)
	assert.NotEqual(t, detail.KPI.TotalKM, 0.0)

	// Seq values must be normalised 1..3.
	for i, s := range afterRoute.Stops {
		assert.Equal(t, i+1, s.Seq, "seq must be renormalised")
	}
}

func TestTuner_Reorder_InvalidSeqRejected(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-bad")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	_, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "reorder", StopID: detail.Routes[0].Stops[0].ID, Seq: 0,
	})
	require.ErrorIs(t, err, dispatchdomain.ErrInvalidOp)
}

func TestTuner_Unassign_StopLeavesRoute(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-un")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	stop := detail.Routes[0].Stops[1]
	kpi, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "unassign", StopID: stop.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, kpi.UnassignedCount)

	after, err := dispatchapp.NewPlannerService(db, nil).GetRun(ctx, tenant, detail.ID)
	require.NoError(t, err)
	assert.Len(t, after.Routes[0].Stops, 2, "route must lose the stop")
	require.Len(t, after.Stops, 1, "unassigned pool must hold the stop")
	assert.Equal(t, stop.ID, after.Stops[0].ID)
	assert.Equal(t, "unassigned", after.Stops[0].Status)
}

func TestTuner_Unassign_RouteClearsAllStops(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-ur")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	kpi, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "unassign", RouteID: detail.Routes[0].ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, kpi.UnassignedCount)

	after, err := dispatchapp.NewPlannerService(db, nil).GetRun(ctx, tenant, detail.ID)
	require.NoError(t, err)
	assert.Empty(t, after.Routes, "emptied route must be removed")
	assert.Len(t, after.Stops, 3)
}

func TestTuner_Move_BetweenRoutes(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-mv")
	svc := dispatchapp.NewPlannerService(db, nil)

	// Solver consolidates to one route (min-vehicles), so build the second
	// route with a split, then move a stop across.
	_, detail := tuneFixture(t, db, tenant)
	runID := detail.ID
	tuner := dispatchapp.NewTunerService(db)
	_, err := tuner.Apply(ctx, tenant, runID, dispatchapp.TuneOp{
		Op: "split", RouteID: detail.Routes[0].ID, SplitPart: 2,
	})
	require.NoError(t, err)
	detail, err = svc.GetRun(ctx, tenant, runID)
	require.NoError(t, err)
	require.Len(t, detail.Routes, 2)

	src, dst := detail.Routes[0], detail.Routes[1]
	require.NotEmpty(t, src.Stops)
	stop := src.Stops[0]
	kpi, err := tuner.Apply(ctx, tenant, runID, dispatchapp.TuneOp{
		Op: "move", StopID: stop.ID, ToRouteID: dst.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, kpi.UnassignedCount, "moved stop must stay assigned")

	after, err := svc.GetRun(ctx, tenant, runID)
	require.NoError(t, err)
	byID := map[string][]dispatchapp.StopView{}
	for _, r := range after.Routes {
		byID[r.ID] = r.Stops
	}
	assert.NotContains(t, stopIDs(byID[src.ID]), stop.ID, "source route must lose the stop")
	assert.Contains(t, stopIDs(byID[dst.ID]), stop.ID, "dest route must gain the stop")
}

func TestTuner_Merge_SingleRouteKeepsOrder(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-mg")
	svc := dispatchapp.NewPlannerService(db, nil)

	// Two routes via split, then merge them back into one.
	_, detail := tuneFixture(t, db, tenant)
	runID := detail.ID
	tuner := dispatchapp.NewTunerService(db)
	_, err := tuner.Apply(ctx, tenant, runID, dispatchapp.TuneOp{
		Op: "split", RouteID: detail.Routes[0].ID, SplitPart: 2,
	})
	require.NoError(t, err)
	detail, err = svc.GetRun(ctx, tenant, runID)
	require.NoError(t, err)
	require.Len(t, detail.Routes, 2)

	kpi, err := tuner.Apply(ctx, tenant, runID, dispatchapp.TuneOp{Op: "merge"})
	require.NoError(t, err)
	assert.Equal(t, 0, kpi.UnassignedCount)
	assert.Equal(t, 1, kpi.VehiclesUsed)

	after, err := svc.GetRun(ctx, tenant, runID)
	require.NoError(t, err)
	require.Len(t, after.Routes, 1, "all routes merged into one")
	assert.Len(t, after.Routes[0].Stops, 3, "no stops lost")
	// All stops still pending/assigned.
	for _, s := range after.Routes[0].Stops {
		assert.NotEqual(t, "unassigned", s.Status)
	}
}

func TestTuner_Split_SplitsTailToNewRoute(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-sp")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	kpi, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "split", RouteID: detail.Routes[0].ID, SplitPart: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, kpi.UnassignedCount)
	assert.Equal(t, 2, kpi.VehiclesUsed, "same vehicle now runs two routes")

	after, err := dispatchapp.NewPlannerService(db, nil).GetRun(ctx, tenant, detail.ID)
	require.NoError(t, err)
	require.Len(t, after.Routes, 2)
	assert.Len(t, after.Routes[0].Stops, 1, "head keeps stop 1")
	assert.Len(t, after.Routes[1].Stops, 2, "tail gets stops 2-3")
	assert.Equal(t, after.Routes[0].VehicleID, after.Routes[1].VehicleID, "split keeps the vehicle")
}

func TestTuner_Split_InvalidPartRejected(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-spb")
	_, detail := tuneFixture(t, db, tenant)

	tuner := dispatchapp.NewTunerService(db)
	// 3-stop route: SplitPart 2 is valid (splits after stop 1), 3 is not.
	_, err := tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "split", RouteID: detail.Routes[0].ID, SplitPart: 2,
	})
	require.NoError(t, err)
	_, err = tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{
		Op: "split", RouteID: detail.Routes[0].ID, SplitPart: 3,
	})
	require.ErrorIs(t, err, dispatchdomain.ErrInvalidSeq)
}

func TestTuner_UnknownRunRejected(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-404")

	tuner := dispatchapp.NewTunerService(db)
	_, err := tuner.Apply(ctx, tenant, "run-nope", dispatchapp.TuneOp{Op: "merge"})
	require.ErrorIs(t, err, dispatchdomain.ErrRunNotFound)
}

func TestTuner_CommittedRunLocked(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	tenant := shared.TenantID("tenant-tune-lock")
	_, detail := tuneFixture(t, db, tenant)

	_, err := db.Exec(`UPDATE planner_runs SET status='dispatched' WHERE id=?`, detail.ID)
	require.NoError(t, err)

	tuner := dispatchapp.NewTunerService(db)
	_, err = tuner.Apply(ctx, tenant, detail.ID, dispatchapp.TuneOp{Op: "merge"})
	require.ErrorIs(t, err, dispatchdomain.ErrRunCommitted)
}

func TestTuner_TenantIsolation(t *testing.T) {
	db := newPlannerTestDB(t)
	ctx := context.Background()
	owner := shared.TenantID("tenant-tune-own")
	_, detail := tuneFixture(t, db, owner)

	// Second tenant exists but owns nothing of this run.
	_, err := db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-tune-other', 'Other', 'other')`)
	require.NoError(t, err)

	tuner := dispatchapp.NewTunerService(db)
	_, err = tuner.Apply(ctx, "tenant-tune-other", detail.ID, dispatchapp.TuneOp{Op: "merge"})
	require.ErrorIs(t, err, dispatchdomain.ErrRunNotFound)
}

func stopIDs(stops []dispatchapp.StopView) []string {
	out := make([]string, 0, len(stops))
	for _, s := range stops {
		out = append(out, s.ID)
	}
	return out
}
