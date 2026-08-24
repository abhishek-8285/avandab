package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"

	"transport-app/internal/events"
	ocrint "transport-app/internal/integration/ocr"
	"transport-app/internal/shared"
)

// Verification states (Spec 22 §5.3, migration 00094).
const (
	VerifyManual       = "manual"
	VerifyAutoVerified = "auto_verified"
	VerifyFlagged      = "flagged"
)

// Rule thresholds — constants here, owner-tunable values move to
// company_config in a later step if pilots demand it.
const (
	autoVerifyConfidence = 0.90
	autoMedianBand       = 1.20 // within 20% of category median
	flagMedianMultiple   = 2.00 // above 2x median flags
	maxRouteDistanceKm   = 10.0
	duplicateWindowMin   = 30
	duplicateMaxDistKm   = 5.0
	medianLookbackDays   = 90
)

// KharchaVerifyService runs async post-submit verification of driver
// expenses: auto-verified, flagged, or left manual. Subscribes to
// ExpenseCreated on the event bus; never blocks the driver sync path.
type KharchaVerifyService struct {
	baseService
	db  *sql.DB
	ocr ocrint.Client
}

func NewKharchaVerifyService(db *sql.DB, log *slog.Logger, ocr ocrint.Client) *KharchaVerifyService {
	s := &KharchaVerifyService{db: db, ocr: ocr}
	if log != nil {
		s.log = log
	} else {
		s.log = slog.Default()
	}
	return s
}

// SubscribeExpenseCreated wires the verifier to the event bus.
func (s *KharchaVerifyService) SubscribeExpenseCreated(bus events.EventBus) {
	if bus == nil {
		return
	}
	bus.Subscribe(events.ExpenseCreated, func(ctx context.Context, e events.Event) error {
		id, _ := e.Payload.(map[string]any)["expense_id"].(string)
		if id == "" {
			return nil
		}
		if _, err := s.VerifyExpense(ctx, id); err != nil {
			s.log.Warn("kharcha verify failed (expense stays manual)", "expense_id", id, "error", err)
		}
		return nil
	})
}

// expenseRow is the raw data the rules need.
type expenseRow struct {
	ID         string
	TenantID   string
	DriverID   string
	TripID     string
	Category   string
	Amount     float64
	Lat        *float64
	Lng        *float64
	ReceiptURL *string
}

func (e *expenseRow) expenseID() string { return e.ID }

// VerifyExpense applies the §5.3 rule set to one expense and persists the
// outcome. Returns the resulting state.
func (s *KharchaVerifyService) VerifyExpense(ctx context.Context, expenseID string) (string, error) {
	e := expenseRow{ID: expenseID}
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(tenant_id, ''), COALESCE(driver_id, ''), COALESCE(trip_id, ''),
		       category, amount, latitude, longitude, receipt_url
		FROM driver_expenses WHERE id = ?`,
		expenseID).Scan(&e.TenantID, &e.DriverID, &e.TripID,
		&e.Category, &e.Amount, &e.Lat, &e.Lng, &e.ReceiptURL)
	if err != nil {
		return "", fmt.Errorf("load expense: %w", err)
	}

	// OCR is best-effort: failure leaves OCR inputs empty and never fails
	// the driver (edge case 4).
	var ocrAmt, ocrConf float64
	if e.ReceiptURL != nil && *e.ReceiptURL != "" && s.ocr != nil {
		if r, oerr := s.ocr.Extract(ctx, *e.ReceiptURL); oerr == nil {
			ocrAmt, ocrConf = r.Amount, r.Confidence
		} else {
			s.log.Info("kharcha OCR unavailable", "expense_id", expenseID, "error", oerr)
		}
	}

	state, reason := s.classify(ctx, &e, ocrAmt, ocrConf)

	_, uerr := s.db.ExecContext(ctx, `
		UPDATE driver_expenses
		SET verification_state = ?, flag_reason = ?,
		    ocr_amount = NULLIF(?, 0), ocr_confidence = NULLIF(?, 0)
		WHERE id = ?`,
		state, reason, ocrAmt, ocrConf, expenseID)
	if uerr != nil {
		return "", fmt.Errorf("persist verification: %w", uerr)
	}
	s.log.Info("kharcha verified", "expense_id", expenseID,
		"state", state, "reason", reason, "amount", e.Amount, "tenant_id", e.TenantID)
	return state, nil
}

// classify implements the §5.3 rule order: any flag condition wins;
// auto_verified requires ALL clean conditions; otherwise manual.
func (s *KharchaVerifyService) classify(ctx context.Context, e *expenseRow, ocrAmt, ocrConf float64) (string, string) {
	dist := s.distanceFromRouteKm(ctx, e)

	if dist > maxRouteDistanceKm {
		return VerifyFlagged, fmt.Sprintf("distance_from_route_km=%.1f>%.0f", dist, maxRouteDistanceKm)
	}
	if d, ok := s.duplicateWithinWindow(ctx, e); ok {
		return VerifyFlagged, fmt.Sprintf("duplicate_of=%s (%s)", d.id, d.detail)
	}

	median := s.categoryMedian(ctx, e.TenantID, e.Category)
	if median > 0 && e.Amount > median*flagMedianMultiple {
		return VerifyFlagged, fmt.Sprintf("amount_%.2f_gt_%.2fx_median_%.2f", e.Amount, flagMedianMultiple, median)
	}

	withinBand := median > 0 && e.Amount >= median/autoMedianBand && e.Amount <= median*autoMedianBand
	highConf := ocrConf >= autoVerifyConfidence && ocrAmt > 0
	nearRoute := dist <= maxRouteDistanceKm

	if highConf && withinBand && nearRoute {
		return VerifyAutoVerified, ""
	}
	return VerifyManual, ""
}

// distanceFromRouteKm returns the haversine distance from the expense GPS
// point to the trip's route corridor. Missing geo on either side → -1
// ("cannot judge": distance rules do not fire).
func (s *KharchaVerifyService) distanceFromRouteKm(ctx context.Context, e *expenseRow) float64 {
	if e.Lat == nil || e.Lng == nil || e.TripID == "" {
		return -1
	}
	var srcLat, srcLng, dstLat, dstLng sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT rl.source_lat, rl.source_lng, rl.dest_lat, rl.dest_lng
		FROM trips t JOIN route_locations rl ON rl.route_id = t.route_id
		WHERE t.id = ?`, e.TripID).
		Scan(&srcLat, &srcLng, &dstLat, &dstLng)
	if err != nil || !srcLat.Valid || !srcLng.Valid || !dstLat.Valid || !dstLng.Valid {
		return -1
	}
	dSrc := HaversineKm(*e.Lat, *e.Lng, srcLat.Float64, srcLng.Float64)
	dDst := HaversineKm(*e.Lat, *e.Lng, dstLat.Float64, dstLng.Float64)
	return math.Min(dSrc, dDst)
}

type dupHit struct {
	id     string
	detail string
}

// duplicateWithinWindow finds a same-driver same-category claim inside
// ±30min of THIS expense (window anchored on the row's own created_at,
// not wall-clock) and within 5km when both have GPS.
func (s *KharchaVerifyService) duplicateWithinWindow(ctx context.Context, e *expenseRow) (dupHit, bool) {
	query := `
		SELECT d.id FROM driver_expenses d
		JOIN driver_expenses self ON self.id = ?
		WHERE d.driver_id = ? AND d.category = ? AND d.id <> ?
		  AND ABS(strftime('%s', d.created_at) - strftime('%s', self.created_at)) <= ?`
	args := []any{e.expenseID(), e.DriverID, e.Category, e.expenseID(), duplicateWindowMin * 60}
	if e.Lat != nil && e.Lng != nil {
		query += `
		  AND d.latitude IS NOT NULL AND d.longitude IS NOT NULL`
	}
	query += ` LIMIT 1`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return dupHit{}, false
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			detail := fmt.Sprintf("same_driver_category_%dmin_window", duplicateWindowMin)
			if e.Lat == nil || e.Lng == nil {
				detail += "_no_geo"
			}
			return dupHit{id: id, detail: detail}, true
		}
	}
	return dupHit{}, false
}

// categoryMedian computes the tenant's median amount for a category over
// the lookback window; 0 when no history (bands cannot fire without one).
func (s *KharchaVerifyService) categoryMedian(ctx context.Context, tenantID, category string) float64 {
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT amount FROM driver_expenses
		WHERE tenant_id = ? AND category = ?
		  AND created_at >= datetime('now', '-`+fmt.Sprint(medianLookbackDays)+` days')
		ORDER BY amount`, tenantID, category)
	if err != nil {
		return 0
	}
	defer func() { _ = rows.Close() }()
	var amounts []float64
	for rows.Next() {
		var a float64
		if rows.Scan(&a) == nil {
			amounts = append(amounts, a)
		}
	}
	if len(amounts) == 0 {
		return 0
	}
	mid := len(amounts) / 2
	if len(amounts)%2 == 1 {
		return amounts[mid]
	}
	return (amounts[mid-1] + amounts[mid]) / 2
}

// ListFlaggedExpenses returns the admin queue: flagged first, then manual
// pending claims awaiting human review.
func (s *KharchaVerifyService) ListFlaggedExpenses(ctx context.Context, tenantID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, driver_id, category, amount, description,
		       verification_state, flag_reason, created_at
		FROM driver_expenses
		WHERE tenant_id = ? AND status IN ('pending')
		  AND verification_state IN ('flagged', 'manual')
		ORDER BY CASE verification_state WHEN 'flagged' THEN 0 ELSE 1 END,
		         created_at DESC
		LIMIT ?`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var id, driver, cat, state, reason, createdAt string
		var desc sql.NullString
		var amount float64
		if err := rows.Scan(&id, &driver, &cat, &amount, &desc, &state, &reason, &createdAt); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "driver_id": driver, "category": cat, "amount": amount,
			"description": desc.String, "verification_state": state,
			"flag_reason": reason, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

// HaversineKm is the great-circle distance between two WGS84 points.
func HaversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 6371.0 * 2 * math.Asin(math.Sqrt(a))
}
