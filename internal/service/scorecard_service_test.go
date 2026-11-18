package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// scorecardTestNow is the fixed clock for deterministic decay math.
func scorecardTestNow() time.Time {
	return time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
}

func scorecardTestServices(t *testing.T, db *sql.DB) *service.Services {
	t.Helper()
	svcs := auditTestServices(t, db)
	svcs.Scorecard.WithClock(scorecardTestNow)
	return svcs
}

// seedScorecardDriver inserts a bare driver row.
func seedScorecardDriver(t *testing.T, db *sql.DB, id, code, firstName, lastName string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES (?, ?, ?, ?, '9900000000', 'SC-0001', '2028-01-01', 'available')`,
		id, code, firstName, lastName)
	require.NoError(t, err)
}

// seedBehaviour inserts one driver_behaviour_events row (weight denormalized).
func seedBehaviour(t *testing.T, db *sql.DB, id, driverID, eventType, severity string, weight float64, at time.Time, metadata string) {
	t.Helper()
	if metadata == "" {
		metadata = "{}"
	}
	_, err := db.Exec(`INSERT INTO driver_behaviour_events
		(id, driver_id, event_type, severity, weight, metadata, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, driverID, eventType, severity, weight, metadata, auditTimeStr(at))
	require.NoError(t, err)
}

func driverScoreRow(t *testing.T, db *sql.DB, driverID string) (float64, string) {
	t.Helper()
	var score sql.NullFloat64
	var tier sql.NullString
	require.NoError(t, db.QueryRow(
		`SELECT score, tier FROM drivers WHERE id = ?`, driverID).Scan(&score, &tier))
	if !score.Valid {
		return 0, ""
	}
	return score.Float64, tier.String
}

func scoreRowsFor(t *testing.T, db *sql.DB, driverID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_scores WHERE driver_id = ?`, driverID).Scan(&n))
	return n
}

// TestScorecard_ScoreFormula verifies the Spec 03 §4.2 formula: 3 speeding
// events (weight 8, severity medium) at 0/10/20 days ago →
// 12 + 8 + 4 = 24 penalty → score 76, tier B.
func TestScorecard_ScoreFormula(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	seedScorecardDriver(t, db, "d1", "D-001", "Ravi", "Kumar")
	seedBehaviour(t, db, "b1", "d1", "speeding", "medium", 8, scorecardTestNow(), "")
	seedBehaviour(t, db, "b2", "d1", "speeding", "medium", 8, scorecardTestNow().Add(-10*24*time.Hour), "")
	seedBehaviour(t, db, "b3", "d1", "speeding", "medium", 8, scorecardTestNow().Add(-20*24*time.Hour), "")

	ds, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)

	assert.InDelta(t, 76.0, ds.Score, 0.01)
	assert.Equal(t, "B", ds.Tier)
	assert.False(t, ds.InsufficientData)
	assert.Equal(t, 3, ds.EventCounts["speeding"])

	score, tier := driverScoreRow(t, db, "d1")
	assert.InDelta(t, 76.0, score, 0.01)
	assert.Equal(t, "B", tier)
	// Every recompute appends a history row (audit trail, gotcha 1).
	assert.Equal(t, 1, scoreRowsFor(t, db, "d1"))

	// Driver detail shows the same breakdown.
	detail, err := svcs.Scorecard.DriverDetail(ctx, "d1")
	require.NoError(t, err)
	require.Len(t, detail.Breakdown, 1)
	assert.Equal(t, "speeding", detail.Breakdown[0].EventType)
	assert.Equal(t, 3, detail.Breakdown[0].Count)
	assert.InDelta(t, 24.0, detail.Breakdown[0].Penalty, 0.01)
	assert.Equal(t, 1, len(detail.History))
}

// TestScorecard_ColdStart verifies the insufficient_data semantics: 0 events
// → score 100/tier A flagged; 1 event → score computed but still flagged;
// 3+ events → flag cleared (Spec 03 §4.2).
func TestScorecard_ColdStart(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	seedScorecardDriver(t, db, "d-zero", "D-000", "Zero", "Events")
	seedScorecardDriver(t, db, "d-one", "D-001", "One", "Event")
	seedScorecardDriver(t, db, "d-three", "D-003", "Three", "Events")

	seedBehaviour(t, db, "b1", "d-one", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "b2", "d-three", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "b3", "d-three", "idling", "low", 3, scorecardTestNow().Add(-5*24*time.Hour), "")
	seedBehaviour(t, db, "b4", "d-three", "idling", "low", 3, scorecardTestNow().Add(-10*24*time.Hour), "")

	dsZero, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d-zero")
	require.NoError(t, err)
	assert.Equal(t, 100.0, dsZero.Score)
	assert.Equal(t, "A", dsZero.Tier)
	assert.True(t, dsZero.InsufficientData)

	dsOne, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d-one")
	require.NoError(t, err)
	assert.InDelta(t, 97.0, dsOne.Score, 0.01) // 100 - 3×1.0×1.0
	assert.True(t, dsOne.InsufficientData)

	dsThree, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d-three")
	require.NoError(t, err)
	assert.False(t, dsThree.InsufficientData)

	// Leaderboard flags agree.
	rows, _, err := svcs.Scorecard.Leaderboard(ctx, "1", 10)
	require.NoError(t, err)
	flags := map[string]bool{}
	for _, r := range rows {
		flags[r.DriverID] = r.InsufficientData
	}
	assert.True(t, flags["d-zero"])
	assert.True(t, flags["d-one"])
	assert.False(t, flags["d-three"])
}

// TestScorecard_FraudCap verifies the hard cap: an unresolved
// fuel_theft_suspicion caps the score at 69 (tier C); resolving the event
// lifts the cap on the next recompute (Spec 03 §4.2, §11 item 6).
func TestScorecard_FraudCap(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	seedScorecardDriver(t, db, "d1", "D-001", "Fraud", "Driver")
	// Raw score without cap: 100 - 25×1.0×1.0 = 75.
	seedBehaviour(t, db, "b-theft", "d1", "fuel_theft_suspicion", "low", 25, scorecardTestNow(), "")

	ds, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)
	assert.Equal(t, 69.0, ds.Score)
	assert.Equal(t, "C", ds.Tier)

	// Resolve → cap lifts on the next recompute.
	require.NoError(t, svcs.Scorecard.ResolveFraudEvent(ctx, "b-theft", "admin-1"))

	var meta string
	require.NoError(t, db.QueryRow(
		`SELECT metadata FROM driver_behaviour_events WHERE id = 'b-theft'`).Scan(&meta))
	var m struct {
		Resolved bool `json:"resolved"`
	}
	require.NoError(t, json.Unmarshal([]byte(meta), &m))
	assert.True(t, m.Resolved)

	ds2, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)
	// Resolved fraud events are excluded from the score entirely
	// (Spec 03 §11 item 6) → no penalty, no cap.
	assert.Equal(t, 100.0, ds2.Score)
	assert.Equal(t, "A", ds2.Tier)

	// Audit trail captured the resolution.
	var auditCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'fraud_event_resolved' AND record_id = 'b-theft'`).
		Scan(&auditCount))
	assert.Equal(t, 1, auditCount)
}

// TestScorecard_FraudCapDisabled verifies scorecard.fraud_cap_enabled=false
// removes the cap entirely.
func TestScorecard_FraudCapDisabled(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	seedScorecardDriver(t, db, "d1", "D-001", "Fraud", "Driver")
	seedBehaviour(t, db, "b-theft", "d1", "fuel_theft_suspicion", "low", 25, scorecardTestNow(), "")

	_, err := db.Exec(`INSERT OR REPLACE INTO company_config (tenant_id, key, value)
		VALUES ('1', 'scorecard.fraud_cap_enabled', 'false')`)
	require.NoError(t, err)

	ds, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)
	assert.InDelta(t, 75.0, ds.Score, 0.01) // cap off → raw score stands
	assert.Equal(t, "B", ds.Tier)
}

// TestScorecard_BonusForPayout verifies the Spec 03 §7 bonus scale:
// tier A → 5%, tier C → 0%, unknown score → 0, cold start → 0.
func TestScorecard_BonusForPayout(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Tier A driver: 3 low-severity idling events → 91 (A), ≥ min_events.
	seedScorecardDriver(t, db, "d-a", "D-A", "Alpha", "Driver")
	seedBehaviour(t, db, "a1", "d-a", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "a2", "d-a", "idling", "low", 3, scorecardTestNow().Add(-2*24*time.Hour), "")
	seedBehaviour(t, db, "a3", "d-a", "idling", "low", 3, scorecardTestNow().Add(-4*24*time.Hour), "")

	// Tier C driver: 5 speeding today → score 20 (C).
	seedScorecardDriver(t, db, "d-c", "D-C", "Charlie", "Driver")
	for i := 0; i < 5; i++ {
		seedBehaviour(t, db, "c"+string(rune('0'+i)), "d-c", "speeding", "high", 8, scorecardTestNow(), "")
	}

	// Unknown score: never recomputed.
	seedScorecardDriver(t, db, "d-x", "D-X", "Unknown", "Driver")

	// Cold start: only 1 event in window → score known but insufficient.
	seedScorecardDriver(t, db, "d-cold", "D-CL", "Cold", "Driver")
	seedBehaviour(t, db, "cold1", "d-cold", "idling", "low", 3, scorecardTestNow(), "")

	for _, d := range []string{"d-a", "d-c", "d-cold"} {
		_, err := svcs.Scorecard.RecomputeDriverScore(ctx, d)
		require.NoError(t, err)
	}

	assert.InDelta(t, 250.0, svcs.Scorecard.BonusForPayout(ctx, "d-a", 5000), 0.01) // 5%
	assert.Equal(t, 0.0, svcs.Scorecard.BonusForPayout(ctx, "d-c", 5000))           // C
	assert.Equal(t, 0.0, svcs.Scorecard.BonusForPayout(ctx, "d-x", 5000))           // unknown
	assert.Equal(t, 0.0, svcs.Scorecard.BonusForPayout(ctx, "d-cold", 5000))        // insufficient
	assert.InDelta(t, 100.0, svcs.Scorecard.BonusForPayout(ctx, "d-a", 2000), 0.01) // 5% of 2000
}

// TestScorecard_Leaderboard verifies ranked ordering + insufficient flags.
func TestScorecard_Leaderboard(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// d-a: 0 events → 100 (cold start). d-b: 1 speeding today → 88 (cold).
	// d-c: 3 speeding 0/10/20d → 76. d-d: 5 speeding today → 20.
	// d-e: 7 low idling (hours ago) → ~79.
	seedScorecardDriver(t, db, "d-a", "D-A", "Alpha", "Driver")
	seedScorecardDriver(t, db, "d-b", "D-B", "Bravo", "Driver")
	seedScorecardDriver(t, db, "d-c", "D-C", "Charlie", "Driver")
	seedScorecardDriver(t, db, "d-d", "D-D", "Delta", "Driver")
	seedScorecardDriver(t, db, "d-e", "D-E", "Echo", "Driver")

	seedBehaviour(t, db, "b1", "d-b", "speeding", "medium", 8, scorecardTestNow(), "")
	seedBehaviour(t, db, "c1", "d-c", "speeding", "medium", 8, scorecardTestNow(), "")
	seedBehaviour(t, db, "c2", "d-c", "speeding", "medium", 8, scorecardTestNow().Add(-10*24*time.Hour), "")
	seedBehaviour(t, db, "c3", "d-c", "speeding", "medium", 8, scorecardTestNow().Add(-20*24*time.Hour), "")
	for i := 0; i < 5; i++ {
		seedBehaviour(t, db, "d"+string(rune('0'+i)), "d-d", "speeding", "high", 8, scorecardTestNow(), "")
	}
	for i := 0; i < 7; i++ {
		seedBehaviour(t, db, "e"+string(rune('0'+i)), "d-e", "idling", "low", 3, scorecardTestNow().Add(-time.Duration(i)*time.Hour), "")
	}

	for _, d := range []string{"d-a", "d-b", "d-c", "d-d", "d-e"} {
		_, err := svcs.Scorecard.RecomputeDriverScore(ctx, d)
		require.NoError(t, err)
	}
	// Second recompute gives d-e a 2-point history → sparkline renders.
	_, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d-e")
	require.NoError(t, err)

	rows, stats, err := svcs.Scorecard.Leaderboard(ctx, "1", 10)
	require.NoError(t, err)
	require.Len(t, rows, 5)

	// Ranked: 100 (d-a), 88 (d-b), 79 (d-e), 76 (d-c), 40 (d-d).
	wantOrder := []string{"d-a", "d-b", "d-e", "d-c", "d-d"}
	for i, id := range wantOrder {
		assert.Equal(t, id, rows[i].DriverID, "rank %d", i+1)
	}
	// Scores descending.
	for i := 1; i < len(rows); i++ {
		assert.GreaterOrEqual(t, rows[i-1].Score, rows[i].Score)
	}
	// Insufficient: d-a (0 events), d-b (1). Others ≥ 3.
	flags := map[string]bool{}
	for _, r := range rows {
		flags[r.DriverID] = r.InsufficientData
	}
	assert.True(t, flags["d-a"])
	assert.True(t, flags["d-b"])
	assert.False(t, flags["d-c"])
	assert.False(t, flags["d-d"])
	assert.False(t, flags["d-e"])
	// Cold-start rows carry a 100 score but no misleading tier flag on the row.
	assert.Equal(t, "A", rows[0].Tier)
	assert.Equal(t, 5, stats.TotalDrivers)
	assert.InDelta(t, (100+88+79.1+76+20)/5.0, stats.AvgScore, 0.1)
	// Sparkline present only when history exists.
	assert.Equal(t, "", rows[0].Sparkline) // d-a never recomputed → no history
	assert.NotEmpty(t, rows[2].Sparkline)  // d-e has history
}

// TestScorecard_NightlySweep verifies RecomputeAllDrivers updates every
// driver with events in the window and skips drivers without events.
func TestScorecard_NightlySweep(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	seedScorecardDriver(t, db, "d1", "D-001", "One", "Driver")
	seedScorecardDriver(t, db, "d2", "D-002", "Two", "Driver")
	seedScorecardDriver(t, db, "d-idle", "D-003", "Idle", "Driver")

	seedBehaviour(t, db, "b1", "d1", "speeding", "high", 8, scorecardTestNow(), "")
	seedBehaviour(t, db, "b2", "d1", "speeding", "high", 8, scorecardTestNow().Add(-3*24*time.Hour), "")
	seedBehaviour(t, db, "b3", "d2", "idling", "low", 3, scorecardTestNow(), "")

	require.NoError(t, svcs.Scorecard.RecomputeAllDrivers(ctx))

	score1, tier1 := driverScoreRow(t, db, "d1")
	// 16 (today, high) + 16×0.9 (3 days ago) = 30.4 → 69.6.
	assert.InDelta(t, 69.6, score1, 0.01)
	assert.Equal(t, "C", tier1)
	score2, _ := driverScoreRow(t, db, "d2")
	assert.InDelta(t, 97.0, score2, 0.01)
	_, tierIdle := driverScoreRow(t, db, "d-idle")
	assert.Equal(t, "", tierIdle) // no events → untouched
	assert.Equal(t, 1, scoreRowsFor(t, db, "d1"))
}

// TestSettlement_PersistenceAndBonus verifies the Spec 03 §0.1 fix: the
// settlement row is actually INSERTed with the performance bonus, and
// re-generating for the same trip returns the existing row (UNIQUE trip_id).
func TestSettlement_PersistenceAndBonus(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	// The settlement service resolves the driver via GetTripByID, which is
	// tenant-scoped — a bare background ctx fails closed (no tenant).
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	// Seed route/vehicle/driver/trip (pattern from the fuel audit tests).
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r1', 'Delhi', 'Jaipur', 0, 5, 8000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
		 insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ('v1', 'KA01AB1234', 'KA01AB1234', 'truck', 2000, 'diesel',
		        '2027-01-01', '2027-01-01', '2027-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('d1', 'D-001', 'Ravi', 'Kumar', '9988776655', 'KA-12345', '2028-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, driver_id, vehicle_id)
		VALUES ('t1', 'TRIP-001', 'r1', ?, 'in_transit', 'd1', 'v1')`,
		auditTimeStr(time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	// Tier A driver: 3 low-severity idling events → 91, bonus eligible.
	seedBehaviour(t, db, "a1", "d1", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "a2", "d1", "idling", "low", 3, scorecardTestNow().Add(-2*24*time.Hour), "")
	seedBehaviour(t, db, "a3", "d1", "idling", "low", 3, scorecardTestNow().Add(-4*24*time.Hour), "")
	_, err = svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)

	settlement, err := svcs.Settlements.CreateSettlementForTrip(ctx, domain.TripID("t1"), 5000.0, 0.0, 0.0)
	require.NoError(t, err)
	assert.InDelta(t, 250.0, settlement.PerformanceBonus, 0.01) // 5% of 5000
	assert.InDelta(t, 5250.0, settlement.NetPayout, 0.01)

	// The row actually exists now (Spec 03 §0.1 fix).
	var bonus, netPayout float64
	var status string
	require.NoError(t, db.QueryRow(
		`SELECT performance_bonus, net_payout, status FROM driver_settlements WHERE trip_id = 't1'`).
		Scan(&bonus, &netPayout, &status))
	assert.InDelta(t, 250.0, bonus, 0.01)
	assert.InDelta(t, 5250.0, netPayout, 0.01)
	assert.Equal(t, "pending", status)

	// Re-call with the same trip → existing row, no duplicate.
	again, err := svcs.Settlements.CreateSettlementForTrip(ctx, domain.TripID("t1"), 5000.0, 0.0, 0.0)
	require.NoError(t, err)
	assert.Equal(t, settlement.ID, again.ID)
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM driver_settlements WHERE trip_id = 't1'`).Scan(&count))
	assert.Equal(t, 1, count)
}

// TestSettlement_NegativePayoutClamp verifies the bonus is computed on the
// pre-clamp payout and the final net payout is clamped at 0 (gotcha 3).
func TestSettlement_NegativePayoutClamp(t *testing.T) {
	db := auditTestDB(t)
	svcs := scorecardTestServices(t, db)
	// Tenant-scoped ctx so GetTripByID resolves the driver (see above).
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	seedScorecardDriver(t, db, "d1", "D-001", "Ravi", "Kumar")
	seedBehaviour(t, db, "a1", "d1", "idling", "low", 3, scorecardTestNow(), "")
	seedBehaviour(t, db, "a2", "d1", "idling", "low", 3, scorecardTestNow().Add(-2*24*time.Hour), "")
	seedBehaviour(t, db, "a3", "d1", "idling", "low", 3, scorecardTestNow().Add(-4*24*time.Hour), "")
	_, err := svcs.Scorecard.RecomputeDriverScore(ctx, "d1")
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r1', 'Delhi', 'Jaipur', 0, 5, 8000)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type,
		 insurance_expiry, fitness_expiry, permit_expiry, status)
		VALUES ('v1', 'KA01AB1234', 'KA01AB1234', 'truck', 2000, 'diesel',
		        '2027-01-01', '2027-01-01', '2027-01-01', 'available')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, driver_id, vehicle_id)
		VALUES ('t1', 'TRIP-001', 'r1', ?, 'in_transit', 'd1', 'v1')`,
		auditTimeStr(time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC)))
	require.NoError(t, err)

	// fare 1000, advances 2000 → pre-bonus net -1000 → bonus -50 → clamp 0.
	s, err := svcs.Settlements.CreateSettlementForTrip(ctx, domain.TripID("t1"), 1000.0, 2000.0, 0.0)
	require.NoError(t, err)
	assert.InDelta(t, -50.0, s.PerformanceBonus, 0.01)
	assert.Equal(t, 0.0, s.NetPayout)
}
