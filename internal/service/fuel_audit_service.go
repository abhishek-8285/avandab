package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"transport-app/internal/fuel"
	"transport-app/internal/repository"
)

// FuelAuditClaim is the service-layer view of one audited (or pending) fuel
// claim with its checks A/B/C breakdown (Spec 03 §3).
type FuelAuditClaim struct {
	ExpenseID       string
	TripID          string
	TripNumber      string
	DriverID        string
	DriverName      string
	VehicleID       string
	VehicleReg      string
	Category        string
	Amount          float64
	FuelLitres      *float64
	Status          string
	AuditStatus     string
	ClaimedLitres   float64
	ExpectedLevel   *float64
	ExpectedOdo     *float64
	ExpectedBest    float64
	VarianceLitres  float64
	VariancePct     float64
	Result          string
	KmplUsed        float64
	OdometerDeltaKm float64
	TankCapacity    float64
	LevelDeltaPct   float64
	ChecksJSON      string
	CreatedAt       time.Time
	ReviewedBy      *string
	ReviewedAt      *time.Time
	ReviewNote      *string
	RefillEvents    []fuelRefillEvent
}

// fuelRefillEvent is one refill_detected fuel_event row feeding Check A.
type fuelRefillEvent struct {
	ID              string
	EstimatedLitres float64
	Confidence      float64
	OccurredAt      time.Time
	OdometerBefore  float64
	OdometerAfter   float64
}

// FuelAuditStats holds the /fuel/audit dashboard summary.
type FuelAuditStats struct {
	PendingCount     int
	NeedsReviewCount int
	PassedCount      int
	FailedCount      int
	AvgVariancePct   float64
	EnforceMode      bool
}

// auditCheck is one entry of fuel_claim_audits.checks (JSON).
type auditCheck struct {
	Type      string  `json:"type"`
	Expected  float64 `json:"expected_litres"`
	Available bool    `json:"available"`
}

// fuelAuditConfig is a snapshot of the audit thresholds for one pass.
type fuelAuditConfig struct {
	claimTolerancePct   float64
	crosscheckMarginPct float64
	kmplDefault         float64
}

// FuelAuditService evaluates pending fuel claims against the telemetry
// evidence gathered by the FuelEngine (Spec 03 §3). Raw SQL via
// repository.DBGetter — no sqlc regen (Spec 03 §1.2 rule 1).
type FuelAuditService struct {
	baseService
	config *fuel.ConfigReader
}

// auditClaimRow is the pending-claim input for one audit.
type auditClaimRow struct {
	expenseID      string
	tripID         string
	driverID       string
	amount         float64
	createdAt      time.Time
	fuelLitres     *float64
	vehicleID      string
	tripStart      time.Time
	tripStartValid bool
	status         string
}

// vehicleAuditMeta carries the vehicle-level inputs for checks A and B.
type vehicleAuditMeta struct {
	sensorFitted bool
	tankCapacity float64
	kmpl         float64
	regNumber    string
}

// loadAuditConfig reads the audit thresholds from company_config.
func (s *FuelAuditService) loadAuditConfig(ctx context.Context) (fuelAuditConfig, error) {
	t := tenantIDFor(ctx)
	cfg := fuelAuditConfig{
		claimTolerancePct:   fuel.DefaultClaimTolerancePct,
		crosscheckMarginPct: fuel.DefaultClaimCrosscheckPct,
		kmplDefault:         fuel.DefaultKmpl,
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigClaimTolerancePct, cfg.claimTolerancePct); err == nil && v >= 0 {
		cfg.claimTolerancePct = v
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigClaimCrosscheckPct, cfg.crosscheckMarginPct); err == nil && v >= 0 {
		cfg.crosscheckMarginPct = v
	}
	if v, err := s.config.GetFloat(ctx, t, fuel.ConfigKmplDefault, cfg.kmplDefault); err == nil && v > 0 {
		cfg.kmplDefault = v
	}
	return cfg, nil
}

// AuditPendingClaims runs one audit pass over all pending fuel claims
// (Spec 03 §3.2 step 2). Returns the number of claims audited.
func (s *FuelAuditService) AuditPendingClaims(ctx context.Context) (int, error) {
	cfg, err := s.loadAuditConfig(ctx)
	if err != nil {
		return 0, fmt.Errorf("fuel audit: load config: %w", err)
	}
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return 0, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	claims, err := s.pendingFuelClaims(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("fuel audit: load pending claims: %w", err)
	}

	audited := 0
	for _, c := range claims {
		if err := s.auditOne(ctx, db, cfg, c); err != nil {
			s.log.Error("fuel audit claim failed", "expense_id", c.expenseID, "error", err)
			continue
		}
		audited++
	}
	return audited, nil
}

// RunAudit is the manual backfill trigger (POST /fuel/audit/run).
func (s *FuelAuditService) RunAudit(ctx context.Context) (int, error) {
	return s.AuditPendingClaims(ctx)
}

// pendingFuelClaims loads claims awaiting audit: category=fuel,
// audit_status=pending, still pending approval.
func (s *FuelAuditService) pendingFuelClaims(ctx context.Context, db *sql.DB) ([]auditClaimRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT de.id, COALESCE(de.trip_id,''), COALESCE(de.driver_id,''), de.amount,
		       de.created_at, de.fuel_litres,
		       COALESCE(t.vehicle_id,''), COALESCE(t.departure_time, ''),
		       COALESCE(de.status,'pending')
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		WHERE de.category = 'fuel'
		  AND COALESCE(de.audit_status,'pending') = 'pending'
		  AND COALESCE(de.status,'pending') = 'pending'
		  AND de.tenant_id = ?
		ORDER BY de.created_at ASC`, tenantIDFor(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auditClaimRow
	for rows.Next() {
		var c auditClaimRow
		var tripStart string
		var litres sql.NullFloat64
		if err := rows.Scan(&c.expenseID, &c.tripID, &c.driverID, &c.amount,
			&c.createdAt, &litres, &c.vehicleID, &tripStart, &c.status); err != nil {
			return nil, err
		}
		if litres.Valid {
			c.fuelLitres = &litres.Float64
		}
		if tripStart != "" {
			if ts, ok := parseDBTime(tripStart); ok {
				c.tripStart = ts
				c.tripStartValid = true
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// auditOne evaluates a single claim and persists the audit row + status
// atomically (Spec 03 §1.2 rule 5 — UoW).
func (s *FuelAuditService) auditOne(ctx context.Context, db *sql.DB, cfg fuelAuditConfig, c auditClaimRow) error {
	if c.vehicleID == "" {
		// No vehicle on the trip — neither check is possible (Spec 03 §3.2).
		return nil
	}
	meta, err := s.vehicleMeta(ctx, db, c.vehicleID)
	if err != nil {
		return err
	}

	windowStart := s.claimWindowStart(ctx, db, c)

	// Check A — level-based: Σ refill_detected litres in the window.
	refills, err := s.refillsInWindow(ctx, db, c.vehicleID, windowStart, c.createdAt)
	if err != nil {
		return err
	}
	expectedLevel := 0.0
	for _, r := range refills {
		expectedLevel += r.EstimatedLitres
	}
	levelDeltaPct := 0.0
	if meta.tankCapacity > 0 {
		levelDeltaPct = expectedLevel / meta.tankCapacity * 100.0
	}

	// Check B — odometer-based: odometer delta / kmpl.
	odoDelta, err := s.odometerDelta(ctx, db, c, windowStart)
	if err != nil {
		return err
	}
	kmpl := meta.kmpl
	if kmpl <= 0 {
		kmpl = cfg.kmplDefault
	}
	expectedOdo := 0.0
	if odoDelta > 0 {
		expectedOdo = odoDelta / kmpl
	}

	// Availability: a check with no evidence stands down so the other check
	// (or the tolerance verdict) decides — zero refills or zero movement is
	// "no signal", not a measured zero (Spec 03 §3.2 "if only one available,
	// it stands").
	levelAvailable := meta.sensorFitted && meta.tankCapacity > 0 && len(refills) > 0
	odoAvailable := odoDelta > 0

	checks := []auditCheck{
		{Type: "level", Expected: expectedLevel, Available: levelAvailable},
		{Type: "odometer", Expected: expectedOdo, Available: odoAvailable},
	}
	checksJSON, _ := json.Marshal(checks)

	// Check C — cross-check: both available checks must agree.
	crosscheckOK := true
	if levelAvailable && odoAvailable {
		denom := math.Max(expectedLevel, expectedOdo)
		if denom <= 0 {
			denom = 1
		}
		crosscheckOK = math.Abs(expectedLevel-expectedOdo)/denom*100.0 <= cfg.crosscheckMarginPct
	}

	// Verdict against the best expected value.
	expectedBest := expectedOdo
	if levelAvailable {
		if odoAvailable {
			expectedBest = (expectedLevel + expectedOdo) / 2.0
		} else {
			expectedBest = expectedLevel
		}
	}

	claimed := 0.0
	if c.fuelLitres != nil {
		claimed = *c.fuelLitres
	}
	result, varianceLitres, variancePct := auditVerdict(claimed, expectedBest, cfg.claimTolerancePct, crosscheckOK)

	expenseID := c.expenseID
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		db := txDB(txCtx, db)
		_, err := db.ExecContext(txCtx, `
			INSERT INTO fuel_claim_audits
			 (id, expense_id, trip_id, vehicle_id, driver_id, litres_claimed,
			  litres_expected_level, litres_expected_odo, level_delta_pct,
			  odometer_delta_km, tank_capacity_litres, kmpl_used,
			  variance_litres, variance_pct, result, checks, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(expense_id) DO UPDATE SET
			  litres_claimed = excluded.litres_claimed,
			  litres_expected_level = excluded.litres_expected_level,
			  litres_expected_odo = excluded.litres_expected_odo,
			  level_delta_pct = excluded.level_delta_pct,
			  odometer_delta_km = excluded.odometer_delta_km,
			  tank_capacity_litres = excluded.tank_capacity_litres,
			  kmpl_used = excluded.kmpl_used,
			  variance_litres = excluded.variance_litres,
			  variance_pct = excluded.variance_pct,
			  result = excluded.result,
			  checks = excluded.checks,
			  created_at = excluded.created_at`,
			generateID(), expenseID, orNull(c.tripID), orNull(c.vehicleID), orNull(c.driverID), claimed,
			orNullFloat(expectedLevel, levelAvailable), orNullFloat(expectedOdo, odoAvailable), levelDeltaPct,
			odoDelta, meta.tankCapacity, kmpl,
			varianceLitres, variancePct, result, string(checksJSON), c.createdAt)
		if err != nil {
			return err
		}

		_, err = db.ExecContext(txCtx,
			`UPDATE driver_expenses SET audit_status = ? WHERE id = ? AND tenant_id = ?`, result, expenseID, tenantIDFor(txCtx))
		return err
	})
}

// claimWindowStart returns the audit window start: the trip start, or the
// previous fuel claim on the same trip, whichever is later (Spec 03 §11
// item 1 — refills accumulate per trip; each claim audits its own slice).
func (s *FuelAuditService) claimWindowStart(ctx context.Context, db *sql.DB, c auditClaimRow) time.Time {
	start := time.Time{}
	if c.tripStartValid {
		start = c.tripStart
	}
	var prev string
	err := db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM driver_expenses
		 WHERE trip_id = ? AND category = 'fuel' AND id != ? AND datetime(created_at) < datetime(?)`,
		c.tripID, c.expenseID, fuelTimeStr(c.createdAt)).Scan(&prev)
	if err == nil && prev != "" {
		if t, ok := parseDBTime(prev); ok && t.After(start) {
			start = t
		}
	}
	if start.IsZero() {
		// No trip start and no previous claim: fall back to 30 days so the
		// vehicle's refill history in the window is bounded but meaningful.
		return c.createdAt.Add(-30 * 24 * time.Hour)
	}
	return start
}

// refillsInWindow returns the refill_detected events for the vehicle inside
// the claim window (Spec 03 §3.2 Check A — never the vehicle's lifetime).
func (s *FuelAuditService) refillsInWindow(ctx context.Context, db *sql.DB, vehicleID string, from, to time.Time) ([]fuelRefillEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(estimated_litres,0), COALESCE(confidence,0),
		       occurred_at, COALESCE(odometer_before,0), COALESCE(odometer_after,0)
		FROM fuel_events
		WHERE vehicle_id = ? AND event_type = 'refill_detected'
		  AND datetime(occurred_at) > datetime(?) AND datetime(occurred_at) <= datetime(?)
		ORDER BY occurred_at ASC`,
		vehicleID, fuelTimeStr(from), fuelTimeStr(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []fuelRefillEvent
	for rows.Next() {
		var r fuelRefillEvent
		if err := rows.Scan(&r.ID, &r.EstimatedLitres, &r.Confidence, &r.OccurredAt,
			&r.OdometerBefore, &r.OdometerAfter); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// odometerDelta computes the distance driven in the claim window: from
// telemetry_snapshots bounds when present, else from the route's recorded
// distance (Spec 03 §3.2 Check B / gotcha 3).
func (s *FuelAuditService) odometerDelta(ctx context.Context, db *sql.DB, c auditClaimRow, windowStart time.Time) (float64, error) {
	var lo, hi sql.NullFloat64
	err := db.QueryRowContext(ctx, `
		SELECT MIN(odometer), MAX(odometer)
		FROM telemetry_snapshots
		WHERE vehicle_id = ? AND datetime(timestamp) > datetime(?) AND datetime(timestamp) <= datetime(?)
		  AND odometer > 0`,
		c.vehicleID, fuelTimeStr(windowStart), fuelTimeStr(c.createdAt)).Scan(&lo, &hi)
	if err != nil {
		return 0, err
	}
	if lo.Valid && hi.Valid {
		delta := hi.Float64 - lo.Float64
		if delta >= 0 {
			return delta, nil
		}
		return 0, nil
	}

	// Fallback: route distance for the trip (recorded plan, not telemetry).
	if c.tripID != "" {
		var dist sql.NullFloat64
		if err := db.QueryRowContext(ctx, `
			SELECT r.distance FROM trips t
			JOIN routes r ON r.id = t.route_id
			WHERE t.id = ?`, c.tripID).Scan(&dist); err == nil && dist.Valid && dist.Float64 > 0 {
			return dist.Float64, nil
		}
	}
	return 0, nil
}

// vehicleMeta loads the fuel-sensor, capacity and kmpl inputs for a vehicle.
func (s *FuelAuditService) vehicleMeta(ctx context.Context, db *sql.DB, vehicleID string) (vehicleAuditMeta, error) {
	var m vehicleAuditMeta
	var sensor sql.NullInt64
	var cap, kmpl sql.NullFloat64
	err := db.QueryRowContext(ctx,
		`SELECT COALESCE(fuel_sensor_fitted,0), tank_capacity_litres,
		        current_mileage, registration_number
		 FROM vehicles WHERE id = ?`, vehicleID).Scan(&sensor, &cap, &kmpl, &m.regNumber)
	if err != nil {
		return m, fmt.Errorf("fuel audit: vehicle %s: %w", vehicleID, err)
	}
	m.sensorFitted = sensor.Valid && sensor.Int64 != 0
	if cap.Valid {
		m.tankCapacity = cap.Float64
	}
	if kmpl.Valid {
		m.kmpl = kmpl.Float64
	}
	return m, nil
}

// auditVerdict maps the claimed-vs-expected variance to passed/needs_review
// (Spec 03 §3.2 verdict: |variance| within tolerance → passed).
func auditVerdict(claimed, expectedBest, tolerancePct float64, crosscheckOK bool) (result string, varianceLitres, variancePct float64) {
	varianceLitres = claimed - expectedBest
	if expectedBest <= 0 {
		variancePct = 0
		if claimed > 0 {
			variancePct = 100
		}
	} else {
		variancePct = math.Abs(varianceLitres) / expectedBest * 100.0
	}
	if !crosscheckOK || variancePct > tolerancePct {
		return "needs_review", varianceLitres, variancePct
	}
	return "passed", varianceLitres, variancePct
}

// ReviewClaim applies an admin verdict to a claim (Spec 03 §3.2 step 5):
// sets the audit row's result/reviewer trail and flips the expense status.
func (s *FuelAuditService) ReviewClaim(ctx context.Context, expenseID, verdict, note, userID string) error {
	if verdict != "passed" && verdict != "failed" {
		return fmt.Errorf("invalid verdict: %s (expected passed|failed)", verdict)
	}
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	now := time.Now()
	return s.txManager.WithTransaction(ctx, func(txCtx context.Context) error {
		db := txDB(txCtx, db)
		res, err := db.ExecContext(txCtx, `
			UPDATE fuel_claim_audits
			SET result = ?, reviewed_by = ?, reviewed_at = ?, review_note = ?
			WHERE expense_id = ?`,
			verdict, orNull(userID), now, orNull(note), expenseID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("claim audit row not found")
		}
		_, err = db.ExecContext(txCtx,
			`UPDATE driver_expenses SET audit_status = ? WHERE id = ? AND tenant_id = ?`, verdict, expenseID, tenantIDFor(txCtx))
		return err
	})
}

// ListAuditClaims returns claims joined with their latest audit row
// (dashboard + queue). All fuel claims, newest first.
func (s *FuelAuditService) ListAuditClaims(ctx context.Context) ([]FuelAuditClaim, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return nil, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	rows, err := db.QueryContext(ctx, `
		SELECT de.id,
		       COALESCE(de.trip_id,''), COALESCE(t.trip_number,''),
		       COALESCE(de.driver_id,''), COALESCE(d.first_name||' '||d.last_name,''),
		       COALESCE(t.vehicle_id,''), COALESCE(v.registration_number,''),
		       COALESCE(de.category, de.expense_type, 'other'), de.amount,
		       de.fuel_litres, COALESCE(de.status,'pending'), COALESCE(de.audit_status,'pending'),
		       fca.litres_expected_level, fca.litres_expected_odo,
		       fca.variance_litres, fca.variance_pct, fca.result, fca.kmpl_used,
		       fca.odometer_delta_km, fca.tank_capacity_litres, fca.level_delta_pct,
		       COALESCE(fca.checks,'[]'), de.created_at,
		       fca.reviewed_by, fca.reviewed_at, fca.review_note
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN fuel_claim_audits fca ON fca.expense_id = de.id
		WHERE de.category = 'fuel' AND de.tenant_id = ?
		ORDER BY de.created_at DESC LIMIT 200`, tenantIDFor(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditClaims(rows)
}

// GetAuditDetail returns one claim with its refill events for the detail view.
func (s *FuelAuditService) GetAuditDetail(ctx context.Context, expenseID string) (FuelAuditClaim, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return FuelAuditClaim{}, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	row := db.QueryRowContext(ctx, `
		SELECT de.id,
		       COALESCE(de.trip_id,''), COALESCE(t.trip_number,''),
		       COALESCE(de.driver_id,''), COALESCE(d.first_name||' '||d.last_name,''),
		       COALESCE(t.vehicle_id,''), COALESCE(v.registration_number,''),
		       COALESCE(de.category, de.expense_type, 'other'), de.amount,
		       de.fuel_litres, COALESCE(de.status,'pending'), COALESCE(de.audit_status,'pending'),
		       fca.litres_expected_level, fca.litres_expected_odo,
		       fca.variance_litres, fca.variance_pct, fca.result, fca.kmpl_used,
		       fca.odometer_delta_km, fca.tank_capacity_litres, fca.level_delta_pct,
		       COALESCE(fca.checks,'[]'), de.created_at,
		       fca.reviewed_by, fca.reviewed_at, fca.review_note
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		LEFT JOIN fuel_claim_audits fca ON fca.expense_id = de.id
		WHERE de.id = ? AND de.tenant_id = ?`, expenseID, tenantIDFor(ctx))
	c, err := scanAuditClaim(row)
	if err != nil {
		return FuelAuditClaim{}, fmt.Errorf("claim not found: %w", err)
	}

	// Refill events feeding Check A — reuse the same claim window the audit
	// pass used (trip start or previous fuel claim on the trip, Spec 03 §11.1).
	if c.VehicleID != "" {
		row := auditClaimRow{
			expenseID: c.ExpenseID,
			tripID:    c.TripID,
			createdAt: c.CreatedAt,
		}
		if c.TripID != "" {
			var dep string
			if err := db.QueryRowContext(ctx,
				`SELECT COALESCE(departure_time, '') FROM trips WHERE id = ?`, c.TripID).Scan(&dep); err == nil && dep != "" {
				if ts, err := time.Parse("2006-01-02 15:04:05", dep); err == nil {
					row.tripStart = ts
					row.tripStartValid = true
				}
			}
		}
		windowStart := s.claimWindowStart(ctx, db, row)
		if refills, err := s.refillsInWindow(ctx, db, c.VehicleID, windowStart, c.CreatedAt); err == nil {
			c.RefillEvents = refills
		}
	}
	return c, nil
}

// GetAuditStats returns the /fuel/audit dashboard summary.
func (s *FuelAuditService) GetAuditStats(ctx context.Context) (FuelAuditStats, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return FuelAuditStats{}, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	var st FuelAuditStats
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses
		 WHERE category = 'fuel' AND COALESCE(audit_status,'pending') = 'pending' AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&st.PendingCount)
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses
		 WHERE category = 'fuel' AND audit_status = 'needs_review' AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&st.NeedsReviewCount)
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses
		 WHERE category = 'fuel' AND audit_status = 'passed' AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&st.PassedCount)
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses
		 WHERE category = 'fuel' AND audit_status = 'failed' AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&st.FailedCount)
	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(AVG(variance_pct),0) FROM fuel_claim_audits`).
		Scan(&st.AvgVariancePct)

	enforce, err := s.EnforceMode(ctx)
	if err != nil {
		return st, err
	}
	st.EnforceMode = enforce
	return st, nil
}

// EnforceMode reports whether fuel.audit_enforce='true' (annotate vs enforce).
func (s *FuelAuditService) EnforceMode(ctx context.Context) (bool, error) {
	v, err := s.config.Get(ctx, tenantIDFor(ctx), fuel.ConfigAuditEnforce)
	if err != nil || v == "" {
		return fuel.DefaultAuditEnforce, nil
	}
	return v == "true", nil
}

// --- scan helpers ---

type auditClaimScanner interface {
	Next() bool
	Scan(...interface{}) error
	Close() error
}

type rowScanner interface {
	Scan(...interface{}) error
}

// scanAuditClaim maps one SELECT row (the shared 24-column projection) onto
// FuelAuditClaim. Works for both *sql.Row and *sql.Rows scans.
func scanAuditClaim(s rowScanner) (FuelAuditClaim, error) {
	var c FuelAuditClaim
	var expectedLevel, expectedOdo, kmpl, odoDelta, tankCap, levelDelta sql.NullFloat64
	var varianceL, varianceP sql.NullFloat64
	var resultStr sql.NullString
	var reviewedBy, reviewNote *string
	var reviewedAt *time.Time
	if err := s.Scan(
		&c.ExpenseID, &c.TripID, &c.TripNumber, &c.DriverID, &c.DriverName,
		&c.VehicleID, &c.VehicleReg, &c.Category, &c.Amount, &c.FuelLitres,
		&c.Status, &c.AuditStatus,
		&expectedLevel, &expectedOdo,
		&varianceL, &varianceP, &resultStr, &kmpl,
		&odoDelta, &tankCap, &levelDelta,
		&c.ChecksJSON, &c.CreatedAt,
		&reviewedBy, &reviewedAt, &reviewNote,
	); err != nil {
		return FuelAuditClaim{}, err
	}
	if varianceL.Valid {
		c.VarianceLitres = varianceL.Float64
	}
	if varianceP.Valid {
		c.VariancePct = varianceP.Float64
	}
	c.Result = resultStr.String
	if expectedLevel.Valid {
		c.ExpectedLevel = &expectedLevel.Float64
	}
	if expectedOdo.Valid {
		c.ExpectedOdo = &expectedOdo.Float64
	}
	c.KmplUsed = kmpl.Float64
	c.OdometerDeltaKm = odoDelta.Float64
	c.TankCapacity = tankCap.Float64
	c.LevelDeltaPct = levelDelta.Float64
	c.ReviewedBy = reviewedBy
	c.ReviewedAt = reviewedAt
	c.ReviewNote = reviewNote
	c.ExpectedBest = c.ExpectedBestFromChecks()
	return c, nil
}

// ExpectedBestFromChecks reconstructs the best expected value used for the
// verdict: the average of the available checks, else the single one.
func (c *FuelAuditClaim) ExpectedBestFromChecks() float64 {
	var checks []auditCheck
	if err := json.Unmarshal([]byte(c.ChecksJSON), &checks); err != nil {
		if c.ExpectedOdo != nil {
			return *c.ExpectedOdo
		}
		if c.ExpectedLevel != nil {
			return *c.ExpectedLevel
		}
		return 0
	}
	level, odo := 0.0, 0.0
	haveLevel, haveOdo := false, false
	for _, ch := range checks {
		if !ch.Available {
			continue
		}
		switch ch.Type {
		case "level":
			level, haveLevel = ch.Expected, true
		case "odometer":
			odo, haveOdo = ch.Expected, true
		}
	}
	switch {
	case haveLevel && haveOdo:
		return (level + odo) / 2.0
	case haveLevel:
		return level
	case haveOdo:
		return odo
	default:
		return 0
	}
}

func scanAuditClaims(rows auditClaimScanner) ([]FuelAuditClaim, error) {
	var out []FuelAuditClaim
	for rows.Next() {
		c, err := scanAuditClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Close()
}

// fuelTimeStr formats for SQLite TEXT comparison (same format as the engine).
func fuelTimeStr(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// parseDBTime parses a time value read back from SQLite. DATETIME columns
// are stored by the driver in RFC3339 ("2026-08-19T09:00:00Z") while
// DEFAULT CURRENT_TIMESTAMP yields the space format — accept both.
func parseDBTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func orNull(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func orNullFloat(f float64, valid bool) interface{} {
	if !valid {
		return nil
	}
	return f
}

// txDB returns the transaction from context when present, else the plain DB.
func txDB(ctx context.Context, db *sql.DB) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return db
}

// KmplRow is one row in the per-vehicle efficiency report (Spec 03 §6.1).
type KmplRow struct {
	VehicleID       string
	RegistrationNo  string
	OdometerDeltaKm float64
	RefillLitres    float64
	ComputedKmpl    float64
	ConfiguredKmpl  float64
	VariancePct     float64
	TripCount       int
}

// KmplReport returns per-vehicle efficiency computed from fuel_events and
// telemetry_snapshots over the given date range (Spec 03 §6.1 /fuel/reports/kmpl).
func (s *FuelAuditService) KmplReport(ctx context.Context, from, to time.Time) ([]KmplRow, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return nil, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	cfg, err := s.loadAuditConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Step 1: per-vehicle refill totals from fuel_events in window.
	rows, err := db.QueryContext(ctx, `
		SELECT fe.vehicle_id, COALESCE(SUM(fe.estimated_litres),0) AS refill_litres, COUNT(*) AS refills
		FROM fuel_events fe
		WHERE fe.event_type = 'refill_detected'
		  AND datetime(fe.occurred_at) >= datetime(?)
		  AND datetime(fe.occurred_at) <= datetime(?)
		GROUP BY fe.vehicle_id`,
		fuelTimeStr(from), fuelTimeStr(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type vehicleRefill struct {
		vehicleID    string
		refillLitres float64
	}
	var refills []vehicleRefill
	for rows.Next() {
		var vr vehicleRefill
		var cnt int
		if err := rows.Scan(&vr.vehicleID, &vr.refillLitres, &cnt); err != nil {
			return nil, err
		}
		refills = append(refills, vr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []KmplRow
	for _, vr := range refills {
		// Step 2: odometer delta from telemetry_snapshots.
		var lo, hi sql.NullFloat64
		_ = db.QueryRowContext(ctx, `
			SELECT MIN(odometer), MAX(odometer)
			FROM telemetry_snapshots
			WHERE vehicle_id = ?
			  AND datetime(timestamp) >= datetime(?) AND datetime(timestamp) <= datetime(?)
			  AND odometer > 0`,
			vr.vehicleID, fuelTimeStr(from), fuelTimeStr(to)).Scan(&lo, &hi)

		odoDelta := 0.0
		if lo.Valid && hi.Valid && hi.Float64 > lo.Float64 {
			odoDelta = hi.Float64 - lo.Float64
		}

		// Step 3: vehicle configured kmpl + trip count.
		var reg string
		var configKmpl sql.NullFloat64
		var tripCount int
		_ = db.QueryRowContext(ctx,
			`SELECT registration_number, current_mileage FROM vehicles WHERE id = ?`,
			vr.vehicleID).Scan(&reg, &configKmpl)
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM trips WHERE vehicle_id = ? AND datetime(departure_time) >= datetime(?) AND datetime(departure_time) <= datetime(?)`,
			vr.vehicleID, fuelTimeStr(from), fuelTimeStr(to)).Scan(&tripCount)

		configuredKmpl := cfg.kmplDefault
		if configKmpl.Valid && configKmpl.Float64 > 0 {
			configuredKmpl = configKmpl.Float64
		}

		computedKmpl := 0.0
		if vr.refillLitres > 0 && odoDelta > 0 {
			computedKmpl = odoDelta / vr.refillLitres
		}

		variancePct := 0.0
		if configuredKmpl > 0 {
			variancePct = (computedKmpl - configuredKmpl) / configuredKmpl * 100.0
		}

		out = append(out, KmplRow{
			VehicleID:       vr.vehicleID,
			RegistrationNo:  reg,
			OdometerDeltaKm: odoDelta,
			RefillLitres:    vr.refillLitres,
			ComputedKmpl:    computedKmpl,
			ConfiguredKmpl:  configuredKmpl,
			VariancePct:     variancePct,
			TripCount:       tripCount,
		})
	}
	return out, nil
}
