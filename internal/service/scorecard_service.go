package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"transport-app/internal/domain"
	"transport-app/internal/fuel"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// BehaviourEvent is one scorecard input row. Weights are denormalized into
// the driver_behaviour_events row at write time (copied from company_config
// scorecard.weight.<type>), so historical scores never shift when weights
// change (Spec 03 §4.1).
type BehaviourEvent struct {
	DriverID   string
	TripID     string
	VehicleID  string
	EventType  string
	Severity   string
	OccurredAt time.Time
	Metadata   string
}

// defaultBehaviourWeights are the compiled defaults for the seven event types
// (Spec 03 §4.1). company_config overrides these at write time.
var defaultBehaviourWeights = map[string]float64{
	"speeding":             8,
	"harsh_braking":        6,
	"harsh_accel":          6,
	"idling":               3,
	"night_driving":        2,
	"fuel_theft_suspicion": 25,
	"odometer_rollback":    20,
}

// fraudEventTypes trigger the hard cap (Spec 03 §4.2) until resolved by an
// admin via the scorecard driver detail page.
var fraudEventTypes = map[string]bool{
	"fuel_theft_suspicion": true,
	"odometer_rollback":    true,
}

// scorecardConfig is a snapshot of the tier/window/fraud thresholds for one
// computation (Spec 03 §4.2, §9).
type scorecardConfig struct {
	windowDays      int
	tierA           float64
	tierB           float64
	minEvents       int
	fraudCap        float64
	fraudCapEnabled bool
}

// behaviourEventRow is one driver_behaviour_events row loaded for scoring.
type behaviourEventRow struct {
	id         string
	eventType  string
	severity   string
	weight     float64
	occurredAt time.Time
	resolved   bool
}

// behaviourEventMeta is the JSON stored in driver_behaviour_events.metadata.
// A fraud event is "resolved" when the admin clears it (Spec 03 §4.2 hard cap).
type behaviourEventMeta struct {
	Resolved   bool   `json:"resolved"`
	ResolvedBy string `json:"resolved_by"`
	ResolvedAt string `json:"resolved_at"`
}

// DriverScore is the result of one 30-day rolling recompute (Spec 03 §4.2).
type DriverScore struct {
	DriverID         string
	Score            float64
	Tier             string
	PeriodStart      time.Time
	PeriodEnd        time.Time
	EventCounts      map[string]int
	InsufficientData bool
}

// ScorecardStats is the /scorecard header row summary.
type ScorecardStats struct {
	TotalDrivers int
	TierA        int
	TierB        int
	TierC        int
	AvgScore     float64
}

// LeaderboardRow is one ranked driver on the /scorecard leaderboard.
type LeaderboardRow struct {
	DriverID         string
	DriverCode       string
	DriverName       string
	Score            float64
	Tier             string
	EventCount       int
	InsufficientData bool
	Sparkline        string // inline SVG, lightweight (Spec 03 §6.3 gotcha 8)
}

// EventBreakdown is the per-event-type count + weighted penalty for the
// driver detail page.
type EventBreakdown struct {
	EventType string
	Count     int
	Penalty   float64
}

// ScorePoint is one row of driver_scores history (the audit trail).
type ScorePoint struct {
	Score      float64
	Tier       string
	PeriodEnd  time.Time
	ComputedAt time.Time
}

// FraudEventView is one fraud-cap event shown on the driver detail page with
// an admin resolve action.
type FraudEventView struct {
	ID         string
	EventType  string
	OccurredAt time.Time
	Resolved   bool
}

// DriverDetail is the full /scorecard/drivers/{id} page payload.
type DriverDetail struct {
	DriverID         string
	DriverCode       string
	DriverName       string
	Score            float64
	Tier             string
	InsufficientData bool
	EventCount       int
	Breakdown        []EventBreakdown
	History          []ScorePoint
	FraudEvents      []FraudEventView
}

// ScorecardService computes the 30-day rolling weighted score per driver
// (Spec 03 §4) and feeds the settlement performance bonus (Spec 03 §7).
// Raw SQL via repository.DBGetter — no sqlc regen (Spec 03 §1.2 rule 1).
type ScorecardService struct {
	baseService
	config *fuel.ConfigReader
	now    func() time.Time

	// featureGate reports whether the scorecard feature is on for an org.
	// Nil means ungated (tests, tooling). Production wires the features
	// registry so one org disabling scorecard stops only its own recomputes.
	featureGate func(tenantID string) bool
}

// NewScorecardService builds the service over the store's raw DB. It is only
// constructed when the store exposes one (real SQLRepository); test fakes
// leave Services.Scorecard nil.
func NewScorecardService(bs baseService, db *sql.DB) *ScorecardService {
	return &ScorecardService{baseService: bs, config: fuel.NewConfigReader(db), now: time.Now}
}

// WithClock overrides the time source (deterministic tests for decay).
func (s *ScorecardService) WithClock(now func() time.Time) *ScorecardService {
	if now != nil {
		s.now = now
	}
	return s
}

// WithFeatureGate scopes recomputes to orgs with the feature on.
// Chain after construction; safe to omit (ungated).
func (s *ScorecardService) WithFeatureGate(gate func(tenantID string) bool) *ScorecardService {
	s.featureGate = gate
	return s
}

// driverTenant resolves a driver's org for per-org config and gating.
// Empty when the driver is unknown.
func (s *ScorecardService) driverTenant(ctx context.Context, driverID string) string {
	var tenant string
	_ = s.scoreDB().QueryRowContext(ctx, `SELECT tenant_id FROM drivers WHERE id = ?`, driverID).Scan(&tenant)
	return tenant
}

// WriteBehaviourEvent inserts a driver_behaviour_events row with the weight
// denormalized from company_config at write time (Spec 03 §4.1). Called by
// the alerting spec for speeding/harsh_braking/harsh_accel/idling/night_driving
// events; the fuel engine writes its own rows directly with the same schema.
func (s *ScorecardService) WriteBehaviourEvent(ctx context.Context, evt BehaviourEvent) error {
	if evt.EventType == "" {
		return fmt.Errorf("scorecard: behaviour event requires event_type")
	}
	if evt.DriverID == "" {
		return fmt.Errorf("scorecard: behaviour event requires driver_id")
	}
	if evt.Severity == "" {
		evt.Severity = "medium"
	}
	if evt.Metadata == "" {
		evt.Metadata = "{}"
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = time.Now()
	}

	weight := defaultBehaviourWeights[evt.EventType]
	if w, err := s.config.GetFloat(ctx, s.driverTenant(ctx, evt.DriverID), fuel.ConfigScorecardWeight+evt.EventType, weight); err == nil {
		weight = w
	}

	db := s.scoreDB()
	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_behaviour_events
		    (id, driver_id, trip_id, vehicle_id, event_type, severity, weight, metadata, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		generateID(), evt.DriverID, orNull(evt.TripID), orNull(evt.VehicleID),
		evt.EventType, evt.Severity, weight, evt.Metadata, fuelTimeStr(evt.OccurredAt))
	return err
}

// RecomputeDriverScore computes the 30-day rolling score for one driver and
// persists it atomically: one driver_scores history row + the denormalized
// drivers.score / drivers.tier (Spec 03 §4.2, §4.3).
func (s *ScorecardService) RecomputeDriverScore(ctx context.Context, driverID string) (DriverScore, error) {
	tenant := s.driverTenant(ctx, driverID)
	if s.featureGate != nil && !s.featureGate(tenant) {
		// Gated-off org (or unknown driver): skip quietly, sweep continues.
		return DriverScore{}, nil
	}
	cfg := s.loadScorecardConfigFor(ctx, tenant)
	now := s.now()
	periodStart := now.Add(-time.Duration(cfg.windowDays) * 24 * time.Hour)

	events, err := s.eventsInWindow(ctx, driverID, periodStart)
	if err != nil {
		return DriverScore{}, err
	}

	score, counts, insufficient := computeScore(now, cfg, events)
	tier := tierFor(score, cfg)
	if cfg.fraudCapEnabled && hasUnresolvedFraud(events) {
		score = math.Min(score, cfg.fraudCap)
		tier = "C"
	}

	ds := DriverScore{
		DriverID:         driverID,
		Score:            score,
		Tier:             tier,
		PeriodStart:      periodStart,
		PeriodEnd:        now,
		EventCounts:      counts,
		InsufficientData: insufficient,
	}

	if s.txManager == nil {
		return ds, fmt.Errorf("scorecard: transaction manager unavailable")
	}
	err = s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		db := txDB(txCtx, s.scoreDB())
		countsJSON, _ := json.Marshal(counts)
		if _, err := db.ExecContext(txCtx,
			`INSERT INTO driver_scores
			    (id, driver_id, score, tier, period_start, period_end, event_counts, computed_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			generateID(), driverID, score, tier, fuelTimeStr(periodStart), fuelTimeStr(now),
			string(countsJSON), fuelTimeStr(now)); err != nil {
			return err
		}
		_, err := db.ExecContext(txCtx,
			`UPDATE drivers SET score = ?, tier = ?, updated_at = ? WHERE id = ?`,
			score, tier, fuelTimeStr(now), driverID)
		return err
	})
	if err != nil {
		return DriverScore{}, fmt.Errorf("scorecard: persist score for %s: %w", driverID, err)
	}

	if s.log != nil {
		s.log.Info("scorecard recomputed", "driver_id", driverID, "score", score, "tier", tier,
			"events", len(events), "insufficient", insufficient)
	}
	return ds, nil
}

// RecomputeAllDrivers recomputes scores for every driver with at least one
// behaviour event in the window. Called by the nightly sweep ticker (Spec 03
// §4.3). A failing driver is logged and skipped so the sweep always finishes.
func (s *ScorecardService) RecomputeAllDrivers(ctx context.Context) error {
	cfg := s.loadScorecardConfig(ctx)
	periodStart := s.now().Add(-time.Duration(cfg.windowDays) * 24 * time.Hour)

	rows, err := s.scoreDB().QueryContext(ctx,
		`SELECT DISTINCT driver_id FROM driver_behaviour_events
		 WHERE driver_id IS NOT NULL AND driver_id != '' AND datetime(occurred_at) >= datetime(?)`,
		fuelTimeStr(periodStart))
	if err != nil {
		return err
	}
	defer rows.Close()

	var drivers []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return err
		}
		drivers = append(drivers, d)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var firstErr error
	for _, d := range drivers {
		if _, err := s.RecomputeDriverScore(ctx, d); err != nil {
			if s.log != nil {
				s.log.Error("scorecard nightly sweep: driver failed", "driver_id", d, "error", err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// Spec 03 §4.3 — decay leg: reset drivers that previously had a score
	// but have NO events in the current window to 100/A (all penalties expired).
	noEventRows, err := s.scoreDB().QueryContext(ctx,
		`SELECT id FROM drivers
		 WHERE score IS NOT NULL
		   AND id NOT IN (
		       SELECT DISTINCT driver_id FROM driver_behaviour_events
		       WHERE driver_id IS NOT NULL AND driver_id != '' AND datetime(occurred_at) >= datetime(?)
		   )`, fuelTimeStr(periodStart))
	if err != nil {
		return err
	}
	defer noEventRows.Close()
	var decayDrivers []string
	for noEventRows.Next() {
		var d string
		if err := noEventRows.Scan(&d); err != nil {
			return err
		}
		decayDrivers = append(decayDrivers, d)
	}
	if err := noEventRows.Err(); err != nil {
		return err
	}
	// Recompute each — they'll produce score=100, tier=A (0 events → 0 penalty).
	for _, d := range decayDrivers {
		if _, err := s.RecomputeDriverScore(ctx, d); err != nil {
			if s.log != nil {
				s.log.Error("scorecard nightly sweep (decay): driver failed", "driver_id", d, "error", err)
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Leaderboard returns all drivers ranked by their latest score (Spec 03 §6.3).
// Cold-start drivers (fewer than scorecard.min_events events in the window)
// carry the insufficient_data flag instead of a misleading tier. The sparkline
// is the latest score history as a lightweight inline SVG.
func (s *ScorecardService) Leaderboard(ctx context.Context, tenantID string, limit int) ([]LeaderboardRow, ScorecardStats, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	cfg := s.loadScorecardConfigFor(ctx, tenantID)
	periodStart := s.now().Add(-time.Duration(cfg.windowDays) * 24 * time.Hour)
	db := s.scoreDB()

	rows, err := db.QueryContext(ctx,
		`SELECT d.id, d.driver_id, d.first_name || ' ' || d.last_name,
		        COALESCE(d.score, 100), COALESCE(d.tier, 'A'), COALESCE(d.tenant_id, '')
		 FROM drivers d
		 WHERE d.tenant_id = ?
		 ORDER BY COALESCE(d.score, 100) DESC, d.first_name ASC, d.last_name ASC
		 LIMIT ?`, tenantID, limit)
	if err != nil {
		return nil, ScorecardStats{}, err
	}
	defer rows.Close()

	var out []LeaderboardRow
	var ids []string
	for rows.Next() {
		var r LeaderboardRow
		var tenant string
		if err := rows.Scan(&r.DriverID, &r.DriverCode, &r.DriverName, &r.Score, &r.Tier, &tenant); err != nil {
			return nil, ScorecardStats{}, err
		}
		out = append(out, r)
		ids = append(ids, r.DriverID)
	}
	if err := rows.Err(); err != nil {
		return nil, ScorecardStats{}, err
	}

	// Event counts in the window, one grouped query (cold start detection).
	counts := map[string]int{}
	if len(ids) > 0 {
		crows, err := db.QueryContext(ctx,
			`SELECT driver_id, COUNT(*) FROM driver_behaviour_events
			 WHERE driver_id IN (`+placeholders(len(ids))+`) AND datetime(occurred_at) >= datetime(?)
			 GROUP BY driver_id`,
			append(strsToAny(ids), fuelTimeStr(periodStart))...)
		if err != nil {
			return nil, ScorecardStats{}, err
		}
		defer crows.Close()
		for crows.Next() {
			var d string
			var n int
			if err := crows.Scan(&d, &n); err != nil {
				return nil, ScorecardStats{}, err
			}
			counts[d] = n
		}
		if err := crows.Err(); err != nil {
			return nil, ScorecardStats{}, err
		}
	}

	// Sparkline history: latest 14 scores per driver, oldest first.
	history := map[string][]float64{}
	if len(ids) > 0 {
		hrows, err := db.QueryContext(ctx,
			`SELECT driver_id, score FROM driver_scores
			 WHERE driver_id IN (`+placeholders(len(ids))+`)
			 ORDER BY driver_id ASC, period_end ASC`,
			strsToAny(ids)...)
		if err != nil {
			return nil, ScorecardStats{}, err
		}
		defer hrows.Close()
		for hrows.Next() {
			var d string
			var sc float64
			if err := hrows.Scan(&d, &sc); err != nil {
				return nil, ScorecardStats{}, err
			}
			hist := history[d]
			if len(hist) >= 14 {
				hist = hist[1:]
			}
			history[d] = append(hist, sc)
		}
		if err := hrows.Err(); err != nil {
			return nil, ScorecardStats{}, err
		}
	}

	var stats ScorecardStats
	for i := range out {
		r := &out[i]
		r.EventCount = counts[r.DriverID]
		r.InsufficientData = r.EventCount < cfg.minEvents
		r.Sparkline = sparklineSVG(history[r.DriverID])
		stats.TotalDrivers++
		stats.AvgScore += r.Score
		switch r.Tier {
		case "A":
			stats.TierA++
		case "B":
			stats.TierB++
		default:
			stats.TierC++
		}
	}
	if stats.TotalDrivers > 0 {
		stats.AvgScore /= float64(stats.TotalDrivers)
	}
	return out, stats, nil
}

// DriverDetail returns the score history + event breakdown for one driver
// (Spec 03 §6.3 driver detail page).
func (s *ScorecardService) DriverDetail(ctx context.Context, driverID string) (DriverDetail, error) {
	cfg := s.loadScorecardConfigFor(ctx, s.driverTenant(ctx, driverID))
	now := s.now()
	periodStart := now.Add(-time.Duration(cfg.windowDays) * 24 * time.Hour)
	db := s.scoreDB()

	var d DriverDetail
	var driverCode string
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(driver_id, ''), first_name || ' ' || last_name
		 FROM drivers WHERE id = ?`, driverID).Scan(&driverCode, &d.DriverName)
	if err != nil {
		return DriverDetail{}, fmt.Errorf("scorecard: driver %s: %w", driverID, err)
	}
	d.DriverID = driverID
	d.DriverCode = driverCode

	events, err := s.eventsInWindow(ctx, driverID, periodStart)
	if err != nil {
		return DriverDetail{}, err
	}
	scoreVal, counts, insufficient := computeScore(now, cfg, events)
	if cfg.fraudCapEnabled && hasUnresolvedFraud(events) {
		scoreVal = math.Min(scoreVal, cfg.fraudCap)
	}
	d.Score = scoreVal
	d.Tier = tierFor(scoreVal, cfg)
	if cfg.fraudCapEnabled && hasUnresolvedFraud(events) {
		d.Tier = "C"
	}
	d.InsufficientData = insufficient
	d.EventCount = len(events)

	var types []string
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		d.Breakdown = append(d.Breakdown, EventBreakdown{EventType: t, Count: counts[t], Penalty: scorecardPenalty(events, t, now, float64(cfg.windowDays))})
	}

	// Score history — the full audit trail, newest first (gotcha 1: never
	// delete old driver_scores rows).
	hrows, err := db.QueryContext(ctx,
		`SELECT score, tier, period_end, computed_at FROM driver_scores
		 WHERE driver_id = ? ORDER BY period_end DESC LIMIT 30`, driverID)
	if err != nil {
		return DriverDetail{}, err
	}
	defer hrows.Close()
	for hrows.Next() {
		var p ScorePoint
		var pe, ca string
		if err := hrows.Scan(&p.Score, &p.Tier, &pe, &ca); err != nil {
			return DriverDetail{}, err
		}
		if t, ok := parseDBTime(pe); ok {
			p.PeriodEnd = t
		}
		if t, ok := parseDBTime(ca); ok {
			p.ComputedAt = t
		}
		d.History = append(d.History, p)
	}
	if err := hrows.Err(); err != nil {
		return DriverDetail{}, err
	}

	// Unresolved fraud-cap events with admin resolve actions.
	frows, err := db.QueryContext(ctx,
		`SELECT id, event_type, occurred_at FROM driver_behaviour_events
		 WHERE driver_id = ? AND event_type IN ('fuel_theft_suspicion', 'odometer_rollback')
		   AND datetime(occurred_at) >= datetime(?)
		 ORDER BY occurred_at DESC`, driverID, fuelTimeStr(periodStart))
	if err != nil {
		return DriverDetail{}, err
	}
	defer frows.Close()
	for frows.Next() {
		var f FraudEventView
		var occ string
		if err := frows.Scan(&f.ID, &f.EventType, &occ); err != nil {
			return DriverDetail{}, err
		}
		if t, ok := parseDBTime(occ); ok {
			f.OccurredAt = t
		}
		f.Resolved = false
		// The dedicated query below fills resolution state.
		d.FraudEvents = append(d.FraudEvents, f)
	}
	if err := frows.Err(); err != nil {
		return DriverDetail{}, err
	}
	for i := range d.FraudEvents {
		if resolved, err := s.eventResolved(ctx, d.FraudEvents[i].ID); err == nil {
			d.FraudEvents[i].Resolved = resolved
		}
	}

	return d, nil
}

// BonusForPayout returns the performance bonus for a settlement based on the
// driver's tier (Spec 03 §7): tier A → bonus_a_pct (5%), B → bonus_b_pct (2%),
// C → bonus_c_pct (0%). Returns 0 when the score is unknown or the driver has
// fewer than scorecard.min_events events (cold start — no misleading reward).
// The bonus is computed on the pre-clamp net payout (gotcha 3).
func (s *ScorecardService) BonusForPayout(ctx context.Context, driverID string, netPayout float64) float64 {
	cfg := s.loadScorecardConfigFor(ctx, s.driverTenant(ctx, driverID))
	db := s.scoreDB()

	var score sql.NullFloat64
	var tier sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT score, tier FROM drivers WHERE id = ?`, driverID).Scan(&score, &tier); err != nil {
		return 0
	}
	if !score.Valid {
		return 0
	}
	if !tier.Valid || tier.String == "" {
		return 0
	}

	// Cold start: no bonus until the driver has enough events in the window.
	var n int
	periodStart := s.now().Add(-time.Duration(cfg.windowDays) * 24 * time.Hour)
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_behaviour_events
		 WHERE driver_id = ? AND datetime(occurred_at) >= datetime(?)`,
		driverID, fuelTimeStr(periodStart)).Scan(&n); err != nil {
		return 0
	}
	if n < cfg.minEvents {
		return 0
	}

	var pct float64
	switch tier.String {
	case "A":
		pct = s.pct(ctx, fuel.ConfigScorecardBonusA, fuel.DefaultScorecardBonusA)
	case "B":
		pct = s.pct(ctx, fuel.ConfigScorecardBonusB, fuel.DefaultScorecardBonusB)
	default:
		pct = s.pct(ctx, fuel.ConfigScorecardBonusC, fuel.DefaultScorecardBonusC)
	}
	return netPayout * pct / 100.0
}

// ResolveFraudEvent marks a fuel_theft_suspicion or odometer_rollback event
// resolved, clearing the fraud cap on the next recompute (Spec 03 §4.2, §11
// item 6). Writes an audit log entry (action='fraud_event_resolved') and
// recomputes the affected driver so the cap lifts immediately.
func (s *ScorecardService) ResolveFraudEvent(ctx context.Context, eventID, adminUserID string) error {
	db := s.scoreDB()

	var driverID, eventType, metadata string
	err := db.QueryRowContext(ctx,
		`SELECT driver_id, event_type, metadata FROM driver_behaviour_events WHERE id = ?`,
		eventID).Scan(&driverID, &eventType, &metadata)
	if err != nil {
		return fmt.Errorf("scorecard: behaviour event %s not found", eventID)
	}
	if !fraudEventTypes[eventType] {
		return fmt.Errorf("scorecard: event %s (%s) is not a fraud-cap event", eventID, eventType)
	}

	var meta behaviourEventMeta
	_ = json.Unmarshal([]byte(metadata), &meta)
	if meta.Resolved {
		return fmt.Errorf("scorecard: event %s already resolved", eventID)
	}
	meta.Resolved = true
	meta.ResolvedBy = adminUserID
	meta.ResolvedAt = fuelTimeStr(s.now())
	updated, _ := json.Marshal(meta)

	if s.txManager == nil {
		return fmt.Errorf("scorecard: transaction manager unavailable")
	}
	err = s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		tdb := txDB(txCtx, db)
		if _, err := tdb.ExecContext(txCtx,
			`UPDATE driver_behaviour_events SET metadata = ? WHERE id = ?`,
			string(updated), eventID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.store != nil {
		uid := domain.UserID(adminUserID)
		oldVal := metadata
		newVal := string(updated)
		s.logAudit(ctx, &uid, "fraud_event_resolved", "driver_behaviour_events", eventID, &oldVal, &newVal)
	}

	if driverID != "" {
		if _, err := s.RecomputeDriverScore(ctx, driverID); err != nil {
			return err
		}
	}
	return nil
}

// eventsInWindow loads the driver's behaviour events for the rolling window.
func (s *ScorecardService) eventsInWindow(ctx context.Context, driverID string, periodStart time.Time) ([]behaviourEventRow, error) {
	rows, err := s.scoreDB().QueryContext(ctx,
		`SELECT id, event_type, severity, weight, metadata, occurred_at
		 FROM driver_behaviour_events
		 WHERE driver_id = ? AND datetime(occurred_at) >= datetime(?)
		 ORDER BY occurred_at ASC`, driverID, fuelTimeStr(periodStart))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []behaviourEventRow
	for rows.Next() {
		var e behaviourEventRow
		var meta string
		var occ string
		if err := rows.Scan(&e.id, &e.eventType, &e.severity, &e.weight, &meta, &occ); err != nil {
			return nil, err
		}
		if t, ok := parseDBTime(occ); ok {
			e.occurredAt = t
		}
		var m behaviourEventMeta
		if err := json.Unmarshal([]byte(meta), &m); err == nil {
			e.resolved = m.Resolved
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// eventResolved reports whether one fraud event's metadata marks it resolved.
func (s *ScorecardService) eventResolved(ctx context.Context, eventID string) (bool, error) {
	var meta string
	if err := s.scoreDB().QueryRowContext(ctx,
		`SELECT metadata FROM driver_behaviour_events WHERE id = ?`, eventID).Scan(&meta); err != nil {
		return false, err
	}
	var m behaviourEventMeta
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		return false, nil
	}
	return m.Resolved, nil
}

// loadScorecardConfig reads the scorecard thresholds from company_config.
func (s *ScorecardService) loadScorecardConfig(ctx context.Context) scorecardConfig {
	return s.loadScorecardConfigFor(ctx, string(shared.DefaultTenant))
}

// loadScorecardConfigFor reads thresholds for one org (empty tenant falls
// back to compiled defaults via missed lookups).
func (s *ScorecardService) loadScorecardConfigFor(ctx context.Context, tenant string) scorecardConfig {
	t := tenant
	cfg := scorecardConfig{
		windowDays:      fuel.DefaultScorecardWindowDays,
		tierA:           fuel.DefaultScorecardTierA,
		tierB:           fuel.DefaultScorecardTierB,
		minEvents:       fuel.DefaultScorecardMinEvents,
		fraudCap:        fuel.DefaultScorecardFraudCap,
		fraudCapEnabled: true,
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigScorecardWindowDays, float64(cfg.windowDays)); err == nil && v > 0 {
		cfg.windowDays = int(v)
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigScorecardTierA, cfg.tierA); err == nil && v > 0 {
		cfg.tierA = v
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigScorecardTierB, cfg.tierB); err == nil && v >= 0 {
		cfg.tierB = v
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigScorecardMinEvents, float64(cfg.minEvents)); err == nil && v >= 0 {
		cfg.minEvents = int(v)
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigScorecardFraudCap, cfg.fraudCap); err == nil && v >= 0 {
		cfg.fraudCap = v
	}
	if raw, err := s.config.Get(ctx, t, fuel.ConfigScorecardFraudCapOn); err == nil && raw != "" {
		cfg.fraudCapEnabled = raw != "false"
	}
	return cfg
}

func (s *ScorecardService) pct(ctx context.Context, key string, def float64) float64 {
	v, err := s.config.GetFloat(ctx, string(shared.DefaultTenant), key, def)
	if err != nil {
		return def
	}
	return v
}

// scoreDB returns the raw *sql.DB the service needs.
func (s *ScorecardService) scoreDB() *sql.DB {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		panic("scorecard: store does not support raw DB access")
	}
	return getter.DB()
}

// computeScore applies the Spec 03 §4.2 formula to the in-window events:
//
//	penalty(t) = weight × severity_mult(sev) × decay(days_ago)
//	decay(d)   = (window − d) / window        for 0 ≤ d < window, else 0
//	score      = clamp(100 − Σ penalty, 0, 100)
//
// Resolved fraud events are excluded entirely (Spec 03 §11 item 6). Returns
// the score, per-type counts, and the insufficient_data cold-start flag.
func computeScore(now time.Time, cfg scorecardConfig, events []behaviourEventRow) (score float64, counts map[string]int, insufficient bool) {
	counts = make(map[string]int)
	penaltySum := 0.0
	window := float64(cfg.windowDays)

	for _, e := range events {
		counts[e.eventType]++
		if fraudEventTypes[e.eventType] && e.resolved {
			continue
		}
		days := now.Sub(e.occurredAt).Hours() / 24.0
		if days >= window || days < 0 {
			continue
		}
		decay := (window - days) / window
		if decay <= 0 {
			continue
		}
		penaltySum += e.weight * severityMult(e.severity) * decay
	}

	score = 100 - penaltySum
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	insufficient = total < cfg.minEvents
	return score, counts, insufficient
}

// scorecardPenalty recomputes the weighted penalty for one event type (used
// by the driver detail breakdown view).
func scorecardPenalty(events []behaviourEventRow, eventType string, now time.Time, window float64) float64 {
	p := 0.0
	for _, e := range events {
		if e.eventType != eventType {
			continue
		}
		if fraudEventTypes[e.eventType] && e.resolved {
			continue
		}
		days := now.Sub(e.occurredAt).Hours() / 24.0
		if days >= window || days < 0 {
			continue
		}
		p += e.weight * severityMult(e.severity) * (window - days) / window
	}
	return p
}

// severityMult maps severity to its multiplier (Spec 03 §4.2).
func severityMult(sev string) float64 {
	switch sev {
	case "low":
		return 1.0
	case "medium":
		return 1.5
	case "high":
		return 2.0
	default:
		return 1.5
	}
}

// tierFor maps a score to its tier (Spec 03 §4.2).
func tierFor(score float64, cfg scorecardConfig) string {
	switch {
	case score >= cfg.tierA:
		return "A"
	case score >= cfg.tierB:
		return "B"
	default:
		return "C"
	}
}

// hasUnresolvedFraud reports whether any fraud-cap event in the window is
// still unresolved.
func hasUnresolvedFraud(events []behaviourEventRow) bool {
	for _, e := range events {
		if fraudEventTypes[e.eventType] && !e.resolved {
			return true
		}
	}
	return false
}

// sparklineSVG renders a lightweight inline SVG sparkline (Spec 03 §6.3
// gotcha 8 — no charting library).
func sparklineSVG(scores []float64) string {
	if len(scores) < 2 {
		return ""
	}
	const w, h = 120.0, 32.0
	min, max := scores[0], scores[0]
	for _, sc := range scores {
		if sc < min {
			min = sc
		}
		if sc > max {
			max = sc
		}
	}
	if max == min {
		max = min + 1
	}
	pts := make([]string, len(scores))
	for i, sc := range scores {
		var x float64
		if len(scores) > 1 {
			x = float64(i) / float64(len(scores)-1) * w
		} else {
			x = w / 2.0
		}
		y := h - 2 - (sc-min)/(max-min)*(h-4)
		pts[i] = fmt.Sprintf("%.1f,%.1f", x, y)
	}
	return `<svg width="120" height="32" viewBox="0 0 120 32" class="inline-block align-middle" aria-hidden="true"><polyline fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" points="` +
		strings.Join(pts, " ") + `"/></svg>`
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func strsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// PreferredDriver is one entry in the preferred-driver list for dispatch ordering.
type PreferredDriver struct {
	DriverID   string
	DriverCode string
	DriverName string
	Score      float64
	Tier       string
}

// PreferredDrivers returns tier A and B drivers sorted by score descending
// (Spec 03 §6.4 — preferred-load ordering hook). This is a read-only helper
// for the dispatcher UI; it does NOT gate the assignment use cases.
func (s *ScorecardService) PreferredDrivers(ctx context.Context, limit int) ([]PreferredDriver, error) {
	if limit <= 0 {
		limit = 20
	}
	db := s.scoreDB()
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(driver_id,''), first_name||' '||last_name, score, tier
		FROM drivers
		WHERE score IS NOT NULL AND tier IN ('A', 'B') AND status = 'available'
		ORDER BY score DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PreferredDriver
	for rows.Next() {
		var p PreferredDriver
		if err := rows.Scan(&p.DriverID, &p.DriverCode, &p.DriverName, &p.Score, &p.Tier); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
