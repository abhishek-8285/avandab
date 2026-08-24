package service

import (
	"context"
	"database/sql"
	"fmt"

	"transport-app/internal/shared"
)

// Pilot KPI targets (Spec 22 §10 Step 12): settlement cycle <10min,
// kharcha app-submitted ≥90%, disputed settlements <5%, driver WAU
// ≥80%, EWB expiry caught 100%. Computed from existing tables —
// no new event plumbing beyond the command_center usage rows.
type KPIService struct {
	db *sql.DB
}

func NewKPIService(db *sql.DB) *KPIService { return &KPIService{db: db} }

// PilotKPIs is the §10-S12 wire shape.
type PilotKPIs struct {
	WindowDays            int      `json:"window_days"`
	SettlementCycleMin    *float64 `json:"settlement_cycle_minutes"` // avg created→paid, target <10
	KharchaAppSubmitted   *float64 `json:"kharcha_app_submitted_pct"`
	DisputedSettlements   *float64 `json:"disputed_settlements_pct"`
	DriverWAU             *float64 `json:"driver_wau_pct"`
	EwbExpiryCaught       *float64 `json:"ewb_expiry_caught_pct"`
	ConsoleOpens          int      `json:"console_opens"`
	PanelActions          int      `json:"panel_actions"`
	SamplesBelowThreshold bool     `json:"-"`
}

// PilotKPIs computes the five pilot metrics for one tenant over a window.
// Nil pointer = no samples in the window (reported as null, not zero —
// zero would fake success).
func (s *KPIService) PilotKPIs(ctx context.Context, tenantID string, days int) (*PilotKPIs, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	if days <= 0 || days > 90 {
		days = 14
	}
	out := &PilotKPIs{WindowDays: days}
	cutoff := fmt.Sprintf("datetime('now', '-%d days')", days)

	// 1. Settlement cycle: mean(created_at → paid_at) for paid settlements.
	var cycle sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT AVG((julianday(paid_at) - julianday(created_at)) * 24 * 60)
		FROM driver_settlements
		WHERE status = 'paid' AND paid_at IS NOT NULL
		  AND created_at >= `+cutoff+`
		  AND trip_id IN (SELECT id FROM trips WHERE tenant_id = ?)`, tenantID).Scan(&cycle)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("settlement cycle: %w", err)
	}
	if cycle.Valid {
		v := cycle.Float64
		out.SettlementCycleMin = &v
	}

	// 2. Kharcha app-submitted: mobile claims carry an idempotency key;
	// web/manual entry does not (00076 offline-sync contract).
	var appSub, totalExp int
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN idempotency_key IS NOT NULL THEN 1 ELSE 0 END), 0), COUNT(*)
		FROM driver_expenses
		WHERE created_at >= `+cutoff+` AND COALESCE(tenant_id,'') = ?`, tenantID).
		Scan(&appSub, &totalExp)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("kharcha submitted: %w", err)
	}
	if totalExp > 0 {
		v := float64(appSub) / float64(totalExp) * 100
		out.KharchaAppSubmitted = &v
	}

	// 3. Disputed settlements share.
	var disputed, settledTotal int
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN ds.status = 'disputed' THEN 1 ELSE 0 END), 0), COUNT(*)
		FROM driver_settlements ds
		JOIN trips t ON t.id = ds.trip_id
		WHERE ds.created_at >= `+cutoff+` AND t.tenant_id = ?`, tenantID).
		Scan(&disputed, &settledTotal)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("disputed share: %w", err)
	}
	if settledTotal > 0 {
		v := float64(disputed) / float64(settledTotal) * 100
		out.DisputedSettlements = &v
	}

	// 4. Driver weekly activity: distinct drivers with any expense claim
	// or trip departure in the last 7 days, over all known drivers.
	var activeDrivers, allDrivers int
	err = s.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(DISTINCT driver_id) FROM (
		     SELECT driver_id FROM driver_expenses
		       WHERE created_at >= datetime('now', '-7 days') AND COALESCE(tenant_id,'') = ?
		     UNION
		     SELECT ds.driver_id FROM driver_settlements ds
		       JOIN trips t ON t.id = ds.trip_id
		       WHERE t.tenant_id = ? AND t.departure_time >= datetime('now', '-7 days')
		   ) act),
		  (SELECT COUNT(*) FROM drivers WHERE COALESCE(tenant_id,'') = ?)`,
		tenantID, tenantID, tenantID).Scan(&activeDrivers, &allDrivers)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("driver wau: %w", err)
	}
	if allDrivers > 0 {
		v := float64(activeDrivers) / float64(allDrivers) * 100
		out.DriverWAU = &v
	}

	// 5. EWB expiry caught: expired bills that produced at least one
	// radar ewb_expiry_* alert before expiry.
	var caught, expired int
	err = s.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM eway_bills e
		    JOIN alerts a ON a.entity_type = 'ewaybill'
		      AND (a.entity_id = e.id OR a.entity_id = e.ewb_number)
		      AND a.alert_type LIKE 'ewb_expiry_%'
		    WHERE e.valid_until < datetime('now')
		      AND e.trip_id IN (SELECT id FROM trips WHERE tenant_id = ?)),
		  (SELECT COUNT(*) FROM eway_bills e
		    WHERE e.valid_until < datetime('now')
		      AND e.trip_id IN (SELECT id FROM trips WHERE tenant_id = ?))`,
		tenantID, tenantID).Scan(&caught, &expired)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("ewb caught: %w", err)
	}
	if expired > 0 {
		v := float64(caught) / float64(expired) * 100
		out.EwbExpiryCaught = &v
	}

	// Usage counters from the command_center experiment_events stream.
	_ = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM experiment_events
		WHERE experiment = 'command_center' AND event = 'console_open'
		  AND created_at >= `+cutoff+` AND tenant_id = ?`, tenantID).Scan(&out.ConsoleOpens)
	_ = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM experiment_events
		WHERE experiment = 'command_center' AND event = 'panel_action'
		  AND created_at >= `+cutoff+` AND tenant_id = ?`, tenantID).Scan(&out.PanelActions)

	out.SamplesBelowThreshold = kpisOnTarget(out)
	return out, nil
}

// kpisOnTarget reports whether every sampled metric meets its §10-S12
// target; unsampled metrics do not fail the flag.
func kpisOnTarget(k *PilotKPIs) bool {
	if k.SettlementCycleMin != nil && *k.SettlementCycleMin >= 10 {
		return false
	}
	if k.KharchaAppSubmitted != nil && *k.KharchaAppSubmitted < 90 {
		return false
	}
	if k.DisputedSettlements != nil && *k.DisputedSettlements >= 5 {
		return false
	}
	if k.DriverWAU != nil && *k.DriverWAU < 80 {
		return false
	}
	if k.EwbExpiryCaught != nil && *k.EwbExpiryCaught < 100 {
		return false
	}
	return true
}

// RecordConsoleUsage persists one command_center usage event into the
// experiment_events stream (00039). Best-effort by design: analytics
// must never break the page that produced it.
func (s *KPIService) RecordConsoleUsage(ctx context.Context, tenantID, userID, event string) {
	if s.db == nil || userID == "" || event == "" {
		return
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO experiment_events
		  (id, tenant_id, user_id, experiment, variant, event, meta, created_at)
		VALUES ('cce-' || lower(hex(randomblob(8))), ?, ?, 'command_center', 'console', ?, '{}',
		        strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
		tenantID, userID, event)
}
