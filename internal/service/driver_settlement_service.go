package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/events"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// Common settlement error codes
var (
	ErrSettlementNotFound    = errors.New("settlement_not_found")
	ErrTripNotFound          = errors.New("trip_not_found")
	ErrDriverNotAssigned     = errors.New("driver_not_assigned")
	ErrBookingPriceMissing   = errors.New("booking_price_missing")
	ErrSettlementAlreadyPaid = errors.New("settlement_already_paid")
)

// DriverSettlementRecord represents a driver financial settlement.
type DriverSettlementRecord struct {
	ID               string           `json:"id"`
	TripID           domain.TripID    `json:"trip_id"`
	DriverID         domain.DriverID  `json:"driver_id"`
	GrossFare        float64          `json:"gross_fare"`
	CommissionAmount float64          `json:"commission_amount"`
	AdvancesKharcha  float64          `json:"advances_kharcha"`
	Deductions       float64          `json:"deductions"`
	PerformanceBonus float64          `json:"performance_bonus"`
	TDSRate          float64          `json:"tds_rate"`
	TDSAmount        float64          `json:"tds_amount"`
	NetPayout        float64          `json:"net_payout"`
	RateModel        string           `json:"rate_model"`
	RateBasisJSON    string           `json:"rate_basis_json,omitempty"`
	Status           string           `json:"status"` // pending, processing, paid, disputed
	PaymentRef       *string          `json:"payment_ref,omitempty"`
	PaidAt           *time.Time       `json:"paid_at,omitempty"`
	ConfirmedAt      *time.Time       `json:"confirmed_at,omitempty"`
	DisputedAt       *time.Time       `json:"disputed_at,omitempty"`
	DisputeReason    *string          `json:"dispute_reason,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Lines            []SettlementLine `json:"lines,omitempty"`
}

// SettlementLine represents a single line item component in a settlement breakdown.
type SettlementLine struct {
	ID           string    `json:"id"`
	SettlementID string    `json:"settlement_id"`
	TripID       string    `json:"trip_id"`
	LineType     string    `json:"line_type"` // gross_fare, commission, advances, deduction, tds, adjustment
	Label        string    `json:"label"`
	Amount       float64   `json:"amount"` // Signed: positive for earnings, negative for deductions/tds
	RefID        string    `json:"ref_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// RateResult contains gross fare and commission computation.
type RateResult struct {
	GrossFare  float64
	Commission float64
	RateModel  string
	RateBasis  map[string]interface{}
}

// DriverSettlementService handles driver payout calculation, rate models, TDS, and financial settlements.
type DriverSettlementService struct {
	baseService
	defaultFare       float64
	defaultAdvances   float64
	defaultDeductions float64
	scorecard         *ScorecardService
	opsAlerts         *OpsAlertService
}

const (
	defaultSettlementFare       = 1000.0
	defaultSettlementAdvances   = 200.0
	defaultSettlementDeductions = 50.0
)

// GenerateSettlement computes and PERSISTS a driver settlement with its line items.
func (s *DriverSettlementService) GenerateSettlement(ctx context.Context, tripID string, force bool) (*DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required for settlements")
	}
	db := getter.DB()

	// 1. Load Trip & Driver
	var driverID, bookingID, routeID sql.NullString
	err := db.QueryRowContext(ctx, `SELECT driver_id, booking_id, route_id FROM trips WHERE id = ?`, tripID).
		Scan(&driverID, &bookingID, &routeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("lookup trip: %w", err)
	}
	if !driverID.Valid || driverID.String == "" {
		return nil, ErrDriverNotAssigned
	}

	// 2. Idempotency Check
	if !force {
		existing, err := s.findByTripID(ctx, db, tripID)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	// 3. Compute Gross & Commission via Rate Model
	rateResult, err := s.calculateGrossFare(ctx, db, tripID, bookingID.String, routeID.String)
	if err != nil {
		return nil, err
	}

	// 4. Get Advances (approved kharcha sum) & Deductions
	advances, advanceLines, err := s.getApprovedAdvances(ctx, db, tripID, driverID.String)
	if err != nil {
		return nil, err
	}

	deductions, deductionLines, err := s.getApprovedDeductions(ctx, db, tripID, driverID.String)
	if err != nil {
		return nil, err
	}

	// 5. TDS under Section 194C
	tdsBase := rateResult.GrossFare - rateResult.Commission - advances - deductions
	if tdsBase < 0 {
		tdsBase = 0
	}
	tdsRate, tdsAmount, tdsSection, err := s.calculateTDS(ctx, db, driverID.String, tdsBase)
	if err != nil {
		return nil, err
	}

	// 6. Net Payout
	netPayout := rateResult.GrossFare - rateResult.Commission - advances - deductions - tdsAmount
	bonus := 0.0
	if s.scorecard != nil {
		bonus = s.scorecard.BonusForPayout(ctx, driverID.String, netPayout)
		netPayout += bonus
	}
	if netPayout < 0 {
		netPayout = 0
	}

	// 7-8. Persist driver_settlements header + settlement_lines ATOMICALLY:
	// a crash mid-write must never leave a header without its lines.
	settlementID := "stl-" + uuid.New().String()
	rateBasisJSON, err := json.Marshal(rateResult.RateBasis)
	if err != nil {
		return nil, fmt.Errorf("marshal rate basis: %w", err)
	}
	now := time.Now().UTC()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin settlement tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	insertSettlement := func() error {
		_, e := tx.ExecContext(ctx, `
			INSERT INTO driver_settlements
			    (id, trip_id, driver_id, gross_fare, commission_amount, advances_kharcha,
			     deductions, performance_bonus, tds_rate, tds_amount, net_payout,
			     rate_model, rate_basis_json, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', datetime('now'), datetime('now'))
		`, settlementID, tripID, driverID.String, rateResult.GrossFare, rateResult.Commission,
			advances, deductions, bonus, tdsRate, tdsAmount, netPayout,
			rateResult.RateModel, string(rateBasisJSON))
		return e
	}

	if force {
		var oldID string
		err = tx.QueryRowContext(ctx, `SELECT id FROM driver_settlements WHERE trip_id = ?`, tripID).Scan(&oldID)
		switch {
		case err == nil && oldID != "":
			settlementID = oldID
			if _, err := tx.ExecContext(ctx, `DELETE FROM settlement_lines WHERE settlement_id = ?`, settlementID); err != nil {
				return nil, fmt.Errorf("clear old settlement lines: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE driver_settlements
				SET gross_fare = ?, commission_amount = ?, advances_kharcha = ?, deductions = ?,
				    performance_bonus = ?, tds_rate = ?, tds_amount = ?, net_payout = ?,
				    rate_model = ?, rate_basis_json = ?, updated_at = datetime('now')
				WHERE id = ?
			`, rateResult.GrossFare, rateResult.Commission, advances, deductions,
				bonus, tdsRate, tdsAmount, netPayout, rateResult.RateModel, string(rateBasisJSON), settlementID); err != nil {
				return nil, fmt.Errorf("update settlement: %w", err)
			}
		case errors.Is(err, sql.ErrNoRows):
			if err := insertSettlement(); err != nil {
				return nil, fmt.Errorf("insert settlement: %w", err)
			}
		default:
			return nil, fmt.Errorf("lookup existing settlement: %w", err)
		}
	} else {
		err = insertSettlement()
		if err != nil && strings.Contains(err.Error(), "UNIQUE") {
			// Lost a race against another writer: release our tx before
			// reading their committed row (same-pool lock trap).
			_ = tx.Rollback()
			return s.findByTripID(ctx, db, tripID)
		}
		if err != nil {
			return nil, fmt.Errorf("insert settlement: %w", err)
		}
	}

	lines := s.buildLines(settlementID, tripID, rateResult, advances, advanceLines, deductions, deductionLines, tdsRate, tdsAmount, tdsSection, bonus)
	for _, l := range lines {
		lineID := "stl-ln-" + uuid.New().String()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settlement_lines (id, settlement_id, trip_id, line_type, label, amount, ref_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		`, lineID, settlementID, tripID, l.LineType, l.Label, l.Amount, l.RefID); err != nil {
			return nil, fmt.Errorf("insert settlement line: %w", err)
		}
	}

	// Spec 22 §5.5 — claim the included advance requests so they cannot be
	// double-counted by another trip's settlement.
	if _, err := tx.ExecContext(ctx, `
		UPDATE driver_advance_requests SET settlement_id = ?
		WHERE trip_id = ? AND driver_id = ? AND status = 'approved' AND settlement_id IS NULL
	`, settlementID, tripID, driverID.String); err != nil {
		return nil, fmt.Errorf("attach advance requests: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit settlement: %w", err)
	}

	// 9. Emit SettlementGenerated Event
	if s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type: "SettlementGenerated",
			Payload: map[string]interface{}{
				"SettlementID": settlementID,
				"TripID":       tripID,
				"DriverID":     driverID.String,
				"NetPayout":    netPayout,
				"OccurredAt":   now,
			},
		})
	}

	return s.findByTripID(ctx, db, tripID)
}

func (s *DriverSettlementService) calculateGrossFare(ctx context.Context, db *sql.DB, tripID, bookingID, routeID string) (RateResult, error) {
	rateModel := s.getConfig(ctx, db, "settlement_rate_model")
	if rateModel == "" {
		rateModel = "per_km"
	}

	switch rateModel {
	case "per_km":
		var km float64
		if routeID != "" {
			_ = db.QueryRowContext(ctx, `SELECT distance FROM routes WHERE id = ?`, routeID).Scan(&km)
		}
		if km <= 0 {
			km = 100.0 // Default fallback distance
		}
		ratePerKm := s.getConfigFloat(ctx, db, "settlement_rate_per_km", 11.90)
		gross := km * ratePerKm
		return RateResult{
			GrossFare:  gross,
			Commission: 0,
			RateModel:  "per_km",
			RateBasis:  map[string]interface{}{"km": km, "rate_per_km": ratePerKm},
		}, nil

	case "fixed":
		fixedFare := s.getConfigFloat(ctx, db, "settlement_fixed_fare", 5000.00)
		return RateResult{
			GrossFare:  fixedFare,
			Commission: 0,
			RateModel:  "fixed",
			RateBasis:  map[string]interface{}{"fixed": fixedFare},
		}, nil

	case "commission_pct":
		var fare float64
		if bookingID != "" {
			_ = db.QueryRowContext(ctx, `SELECT price FROM bookings WHERE id = ?`, bookingID).Scan(&fare)
		}
		if fare <= 0 {
			return RateResult{}, ErrBookingPriceMissing
		}
		pct := s.getConfigFloat(ctx, db, "settlement_commission_pct", 5.00) / 100.0
		commission := fare * pct
		gross := fare - commission
		return RateResult{
			GrossFare:  gross,
			Commission: commission,
			RateModel:  "commission_pct",
			RateBasis:  map[string]interface{}{"fare": fare, "commission_pct": pct},
		}, nil

	default:
		return RateResult{
			GrossFare:  defaultSettlementFare,
			Commission: 0,
			RateModel:  "fixed",
			RateBasis:  map[string]interface{}{"fixed": defaultSettlementFare},
		}, nil
	}
}

func (s *DriverSettlementService) getApprovedAdvances(ctx context.Context, db *sql.DB, tripID, driverID string) (float64, []SettlementLine, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, category, amount FROM driver_expenses
		WHERE trip_id = ? AND driver_id = ? AND status = 'approved' AND category IN ('kharcha', 'advance', 'fuel', 'toll')
	`, tripID, driverID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	var total float64
	var lines []SettlementLine
	for rows.Next() {
		var id, cat string
		var amt float64
		if err := rows.Scan(&id, &cat, &amt); err == nil {
			total += amt
			lines = append(lines, SettlementLine{
				LineType: "advances",
				Label:    fmt.Sprintf("Approved expense (%s) #%s", cat, id),
				Amount:   -amt,
				RefID:    id,
			})
		}
	}

	// Spec 22 S7/§5.5 — Paisa-tab advance requests ride the same advances
	// bucket: approved (not yet attached) requests for this trip+driver are
	// deducted here; pending ones never are (edge case 8).
	advReqRows, aerr := db.QueryContext(ctx, `
		SELECT id, amount FROM driver_advance_requests
		WHERE trip_id = ? AND driver_id = ? AND status = 'approved' AND settlement_id IS NULL
	`, tripID, driverID)
	if aerr == nil {
		defer func() { _ = advReqRows.Close() }()
		for advReqRows.Next() {
			var id string
			var amt float64
			if err := advReqRows.Scan(&id, &amt); err == nil {
				total += amt
				lines = append(lines, SettlementLine{
					LineType: "advances",
					Label:    fmt.Sprintf("Advance request #%s", id),
					Amount:   -amt,
					RefID:    id,
				})
			}
		}
	}
	return total, lines, nil
}

func (s *DriverSettlementService) getApprovedDeductions(ctx context.Context, db *sql.DB, tripID, driverID string) (float64, []SettlementLine, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, category, amount FROM driver_expenses
		WHERE trip_id = ? AND driver_id = ? AND status = 'approved' AND category NOT IN ('kharcha', 'advance', 'fuel', 'toll')
	`, tripID, driverID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	var total float64
	var lines []SettlementLine
	for rows.Next() {
		var id, cat string
		var amt float64
		if err := rows.Scan(&id, &cat, &amt); err == nil {
			total += amt
			lines = append(lines, SettlementLine{
				LineType: "deduction",
				Label:    fmt.Sprintf("Approved deduction (%s) #%s", cat, id),
				Amount:   -amt,
				RefID:    id,
			})
		}
	}
	return total, lines, nil
}

func (s *DriverSettlementService) calculateTDS(ctx context.Context, db *sql.DB, driverID string, tdsBase float64) (float64, float64, string, error) {
	var pan sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT pan FROM drivers WHERE id = ?`, driverID).Scan(&pan)

	tdsSection := s.getConfig(ctx, db, "tds_section")
	if tdsSection == "" {
		tdsSection = "194C"
	}

	var rate float64
	if pan.Valid && strings.TrimSpace(pan.String) != "" {
		rate = s.getConfigFloat(ctx, db, "tds_rate_with_pan", 1.00)
	} else {
		rate = s.getConfigFloat(ctx, db, "tds_rate_without_pan", 2.00)
	}

	amount := tdsBase * (rate / 100.0)
	return rate, amount, tdsSection, nil
}

func (s *DriverSettlementService) buildLines(settlementID, tripID string, rate RateResult, advances float64, advLines []SettlementLine, deductions float64, dedLines []SettlementLine, tdsRate, tdsAmount float64, tdsSection string, bonus float64) []SettlementLine {
	var lines []SettlementLine

	// Gross fare
	label := fmt.Sprintf("Trip fare (%s)", rate.RateModel)
	if rate.RateModel == "per_km" {
		if km, ok := rate.RateBasis["km"].(float64); ok {
			label = fmt.Sprintf("Trip fare (per_km %.1f km x %.2f)", km, rate.RateBasis["rate_per_km"])
		}
	}
	lines = append(lines, SettlementLine{
		SettlementID: settlementID,
		TripID:       tripID,
		LineType:     "gross_fare",
		Label:        label,
		Amount:       rate.GrossFare,
	})

	if rate.Commission > 0 {
		pct := 5.0
		if p, ok := rate.RateBasis["commission_pct"].(float64); ok {
			pct = p * 100.0
		}
		lines = append(lines, SettlementLine{
			SettlementID: settlementID,
			TripID:       tripID,
			LineType:     "commission",
			Label:        fmt.Sprintf("Platform commission %.1f%%", pct),
			Amount:       -rate.Commission,
		})
	}

	if advances > 0 {
		if len(advLines) > 0 {
			lines = append(lines, advLines...)
		} else {
			lines = append(lines, SettlementLine{
				SettlementID: settlementID,
				TripID:       tripID,
				LineType:     "advances",
				Label:        "Advances & kharcha",
				Amount:       -advances,
			})
		}
	}

	if deductions > 0 {
		lines = append(lines, dedLines...)
	}

	if tdsAmount > 0 {
		lines = append(lines, SettlementLine{
			SettlementID: settlementID,
			TripID:       tripID,
			LineType:     "tds",
			Label:        fmt.Sprintf("TDS u/s %s @%.0f%%", tdsSection, tdsRate),
			Amount:       -tdsAmount,
		})
	}

	if bonus > 0 {
		lines = append(lines, SettlementLine{
			SettlementID: settlementID,
			TripID:       tripID,
			LineType:     "adjustment",
			Label:        "Performance bonus",
			Amount:       bonus,
		})
	}

	return lines
}

func (s *DriverSettlementService) findByTripID(ctx context.Context, db *sql.DB, tripID string) (*DriverSettlementRecord, error) {
	var rec DriverSettlementRecord
	var created, updated string
	var payRef, paidAt, confAt, dispAt, dispReason sql.NullString
	var rateBasis sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT id, trip_id, driver_id, gross_fare, commission_amount, advances_kharcha,
		       deductions, performance_bonus, tds_rate, tds_amount, net_payout,
		       rate_model, rate_basis_json, status, payment_ref, paid_at,
		       confirmed_at, disputed_at, dispute_reason, created_at, updated_at
		FROM driver_settlements WHERE trip_id = ?
	`, tripID).Scan(
		&rec.ID, &rec.TripID, &rec.DriverID, &rec.GrossFare, &rec.CommissionAmount,
		&rec.AdvancesKharcha, &rec.Deductions, &rec.PerformanceBonus, &rec.TDSRate,
		&rec.TDSAmount, &rec.NetPayout, &rec.RateModel, &rateBasis, &rec.Status,
		&payRef, &paidAt, &confAt, &dispAt, &dispReason, &created, &updated,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSettlementNotFound
		}
		return nil, err
	}

	if payRef.Valid {
		rec.PaymentRef = &payRef.String
	}
	if paidAt.Valid {
		if t, ok := parseDBTime(paidAt.String); ok {
			rec.PaidAt = &t
		}
	}
	if confAt.Valid {
		if t, ok := parseDBTime(confAt.String); ok {
			rec.ConfirmedAt = &t
		}
	}
	if dispAt.Valid {
		if t, ok := parseDBTime(dispAt.String); ok {
			rec.DisputedAt = &t
		}
	}
	if dispReason.Valid {
		rec.DisputeReason = &dispReason.String
	}
	if rateBasis.Valid {
		rec.RateBasisJSON = rateBasis.String
	}
	if t, ok := parseDBTime(created); ok {
		rec.CreatedAt = t
	}
	if t, ok := parseDBTime(updated); ok {
		rec.UpdatedAt = t
	}

	// Fetch settlement lines
	rows, err := db.QueryContext(ctx, `
		SELECT id, settlement_id, trip_id, line_type, label, amount, COALESCE(ref_id,''), created_at
		FROM settlement_lines WHERE settlement_id = ? ORDER BY created_at ASC
	`, rec.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var l SettlementLine
			var lCreated string
			if err := rows.Scan(&l.ID, &l.SettlementID, &l.TripID, &l.LineType, &l.Label, &l.Amount, &l.RefID, &lCreated); err == nil {
				if t, ok := parseDBTime(lCreated); ok {
					l.CreatedAt = t
				}
				rec.Lines = append(rec.Lines, l)
			}
		}
	}

	return &rec, nil
}

// GetSettlement retrieves a settlement by its ID.
func (s *DriverSettlementService) GetSettlement(ctx context.Context, id string) (*DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required")
	}
	db := getter.DB()

	var tripID string
	err := db.QueryRowContext(ctx, `SELECT trip_id FROM driver_settlements WHERE id = ?`, id).Scan(&tripID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSettlementNotFound
		}
		return nil, err
	}

	return s.findByTripID(ctx, db, tripID)
}

// ListSettlements retrieves settlements with optional status and driver filtering.
func (s *DriverSettlementService) ListSettlements(ctx context.Context, status, driverID string, limit, offset int) ([]DriverSettlementRecord, error) {
	return s.listSettlementsFiltered(ctx, status, driverID, "", "", limit, offset)
}

// ListSettlementsDateRange retrieves settlements with optional status,
// driver filtering and a created_at window (YYYY-MM-DD bounds, inclusive).
// The window uses date(substr(created_at,1,10)) because SQLite stores
// timestamps as text in mixed formats (RFC3339 from Go, 'YYYY-MM-DD HH:MM:SS'
// from CURRENT_TIMESTAMP) — only the prefix is stable.
func (s *DriverSettlementService) ListSettlementsDateRange(ctx context.Context, status, driverID, from, to string, limit, offset int) ([]DriverSettlementRecord, error) {
	return s.listSettlementsFiltered(ctx, status, driverID, from, to, limit, offset)
}

func (s *DriverSettlementService) listSettlementsFiltered(ctx context.Context, status, driverID, from, to string, limit, offset int) ([]DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required")
	}
	db := getter.DB()

	if limit <= 0 {
		limit = 50
	}

	var conditions []string
	var args []interface{}
	if status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if driverID != "" {
		conditions = append(conditions, "driver_id = ?")
		args = append(args, driverID)
	}
	if from != "" || to != "" {
		conditions = append(conditions, "(? = '' OR date(substr(created_at,1,10)) >= date(?))")
		args = append(args, from, from)
		conditions = append(conditions, "(? = '' OR date(substr(created_at,1,10)) <= date(?))")
		args = append(args, to, to)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`SELECT trip_id FROM driver_settlements %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DriverSettlementRecord
	for rows.Next() {
		var tripID string
		if err := rows.Scan(&tripID); err == nil {
			if rec, err := s.findByTripID(ctx, db, tripID); err == nil && rec != nil {
				results = append(results, *rec)
			}
		}
	}
	return results, nil
}

// MarkPaid marks a settlement paid with payment reference.
func (s *DriverSettlementService) MarkPaid(ctx context.Context, settlementID, paymentRef string, paidAt time.Time) (*DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required")
	}
	db := getter.DB()

	var currentStatus string
	var tripID, driverID string
	var netPayout float64
	err := db.QueryRowContext(ctx, `SELECT status, trip_id, driver_id, net_payout FROM driver_settlements WHERE id = ?`, settlementID).
		Scan(&currentStatus, &tripID, &driverID, &netPayout)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSettlementNotFound
		}
		return nil, err
	}

	paidAtStr := paidAt.UTC().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `
		UPDATE driver_settlements
		SET status = 'paid', payment_ref = ?, paid_at = ?, updated_at = datetime('now')
		WHERE id = ?
	`, paymentRef, paidAtStr, settlementID)
	if err != nil {
		return nil, err
	}

	// Spec 22 §5.5 — attached advance requests become 'paid' with their
	// settlement so the §5.2 running balance stays consistent.
	_, _ = db.ExecContext(ctx, `
		UPDATE driver_advance_requests SET status = 'paid'
		WHERE settlement_id = ? AND status = 'approved'
	`, settlementID)

	if s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type: events.DriverPayoutSettled,
			Payload: map[string]interface{}{
				"SettlementID": settlementID,
				"TripID":       tripID,
				"DriverID":     driverID,
				"NetPayout":    netPayout,
				"PaymentRef":   paymentRef,
				"OccurredAt":   paidAt,
			},
		})
	}

	return s.findByTripID(ctx, db, tripID)
}

// ConfirmSettlement records driver confirmation for payout.
func (s *DriverSettlementService) ConfirmSettlement(ctx context.Context, settlementID string) (*DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required")
	}
	db := getter.DB()

	var tripID string
	err := db.QueryRowContext(ctx, `SELECT trip_id FROM driver_settlements WHERE id = ?`, settlementID).Scan(&tripID)
	if err != nil {
		return nil, ErrSettlementNotFound
	}

	_, err = db.ExecContext(ctx, `
		UPDATE driver_settlements
		SET confirmed_at = datetime('now'), updated_at = datetime('now')
		WHERE id = ?
	`, settlementID)
	if err != nil {
		return nil, err
	}

	return s.findByTripID(ctx, db, tripID)
}

// DisputeSettlement flags a settlement as disputed.
func (s *DriverSettlementService) DisputeSettlement(ctx context.Context, settlementID, reason string, expectedNet float64) (*DriverSettlementRecord, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database access required")
	}
	db := getter.DB()

	var tripID string
	err := db.QueryRowContext(ctx, `SELECT trip_id FROM driver_settlements WHERE id = ?`, settlementID).Scan(&tripID)
	if err != nil {
		return nil, ErrSettlementNotFound
	}

	_, err = db.ExecContext(ctx, `
		UPDATE driver_settlements
		SET status = 'disputed', disputed_at = datetime('now'), dispute_reason = ?, updated_at = datetime('now')
		WHERE id = ?
	`, reason, settlementID)
	if err != nil {
		return nil, err
	}

	if s.opsAlerts != nil {
		tenantID := string(shared.TenantIDFromContext(ctx))
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
		_, _ = s.opsAlerts.CreateAlert(ctx, OpsAlert{
			TenantID:    tenantID,
			AlertType:   OpsAlertSettlementDispute,
			Severity:    OpsAlertSeverityHigh,
			Title:       "Settlement disputed by driver",
			Description: fmt.Sprintf("Trip %s settlement %s disputed: %s", tripID, settlementID, reason),
			EntityType:  strPtr("trip"),
			EntityID:    &tripID,
		})
	}

	return s.findByTripID(ctx, db, tripID)
}

func (s *DriverSettlementService) getConfig(ctx context.Context, db *sql.DB, key string) string {
	var val string
	_ = db.QueryRowContext(ctx, `SELECT value FROM company_config WHERE key = ?`, key).Scan(&val)
	return val
}

func (s *DriverSettlementService) getConfigFloat(ctx context.Context, db *sql.DB, key string, def float64) float64 {
	v := s.getConfig(ctx, db, key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// CreateSettlementForTrip calculates initial net payout for a trip (legacy / explicit bridge).
func (s *DriverSettlementService) CreateSettlementForTrip(ctx context.Context, tripID domain.TripID, fare float64, advances float64, deductions float64) (DriverSettlementRecord, error) {
	driverID := domain.DriverID("drv-default")
	if s.store != nil {
		trip, err := s.store.GetTripByID(ctx, tripID)
		if err == nil && trip.DriverID != nil {
			driverID = *trip.DriverID
		}
	}

	netPayout := fare - advances - deductions
	bonus := 0.0
	if s.scorecard != nil {
		bonus = s.scorecard.BonusForPayout(ctx, string(driverID), netPayout)
		netPayout += bonus
	}
	if netPayout < 0 {
		netPayout = 0
	}

	settlement := DriverSettlementRecord{
		ID:               "stl-" + uuid.New().String(),
		TripID:           tripID,
		DriverID:         driverID,
		GrossFare:        fare,
		AdvancesKharcha:  advances,
		Deductions:       deductions,
		PerformanceBonus: bonus,
		NetPayout:        netPayout,
		RateModel:        "fixed",
		Status:           "pending",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if getter, ok := s.store.(repository.DBGetter); ok && getter.DB() != nil {
		if err := s.persistSettlement(ctx, getter.DB(), settlement); err != nil {
			return DriverSettlementRecord{}, fmt.Errorf("create settlement: %w", err)
		}
		if stored, err := s.findByTripID(ctx, getter.DB(), string(tripID)); err == nil && stored != nil {
			settlement = *stored
		}
	}

	return settlement, nil
}

// ProcessFinancialSettlement triggers payout when trip is delivered (legacy bridge).
func (s *DriverSettlementService) ProcessFinancialSettlement(ctx context.Context, tripID domain.TripID, paymentRef string) (DriverSettlementRecord, error) {
	trip, err := s.store.GetTripByID(ctx, tripID)
	if err != nil {
		return DriverSettlementRecord{}, domain.ErrTripNotFound
	}

	if trip.DriverID == nil {
		return DriverSettlementRecord{}, fmt.Errorf("cannot process settlement for trip without assigned driver")
	}

	// Calculate net payout (Fare - Kharcha - Advances)
	fare := s.defaultFare
	if trip.BookingID != nil {
		if bk, err := s.store.GetBookingByID(ctx, *trip.BookingID); err == nil && bk.Price > 0 {
			fare = bk.Price
		}
	}
	if fare <= 0 {
		fare = defaultSettlementFare
	}

	advances := s.defaultAdvances
	if getter, ok := s.store.(repository.DBGetter); ok && getter.DB() != nil {
		var sum float64
		if err := getter.DB().QueryRowContext(ctx,
			`SELECT COALESCE(SUM(amount), 0) FROM driver_expenses
			 WHERE trip_id = ? AND status = 'approved'`, string(tripID)).Scan(&sum); err == nil {
			advances = sum
		}
	}
	if advances < 0 {
		advances = 0
	}

	deductions := s.defaultDeductions
	if deductions < 0 {
		deductions = defaultSettlementDeductions
	}
	netPayout := fare - advances - deductions
	bonus := 0.0
	if s.scorecard != nil {
		bonus = s.scorecard.BonusForPayout(ctx, string(*trip.DriverID), netPayout)
		netPayout += bonus
	}
	if netPayout < 0 {
		netPayout = 0
	}

	now := time.Now()
	settlement := DriverSettlementRecord{
		ID:               "stl-" + uuid.New().String(),
		TripID:           tripID,
		DriverID:         *trip.DriverID,
		GrossFare:        fare,
		AdvancesKharcha:  advances,
		Deductions:       deductions,
		PerformanceBonus: bonus,
		NetPayout:        netPayout,
		RateModel:        "fixed",
		Status:           "paid",
		PaymentRef:       &paymentRef,
		PaidAt:           &now,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if getter, ok := s.store.(repository.DBGetter); ok && getter.DB() != nil {
		if err := s.upsertPaidSettlement(ctx, getter.DB(), settlement); err != nil {
			return DriverSettlementRecord{}, fmt.Errorf("process settlement: %w", err)
		}
		if stored, err := s.findByTripID(ctx, getter.DB(), string(tripID)); err == nil && stored != nil {
			settlement = *stored
		}
	}

	return settlement, nil
}

func (s *DriverSettlementService) upsertPaidSettlement(ctx context.Context, db *sql.DB, rec DriverSettlementRecord) error {
	nowStr := fuelTimeStr(time.Now())
	paidAtStr := fuelTimeStr(rec.PaidAtTime())
	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_settlements
		    (id, trip_id, driver_id, gross_fare, advances_kharcha, deductions,
		     performance_bonus, net_payout, status, payment_ref, paid_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'paid', ?, ?, ?, ?)
		 ON CONFLICT(trip_id) DO UPDATE SET
		   gross_fare = excluded.gross_fare,
		   advances_kharcha = excluded.advances_kharcha,
		   deductions = excluded.deductions,
		   performance_bonus = excluded.performance_bonus,
		   net_payout = excluded.net_payout,
		   status = 'paid',
		   payment_ref = excluded.payment_ref,
		   paid_at = excluded.paid_at,
		   updated_at = excluded.updated_at`,
		rec.ID, string(rec.TripID), string(rec.DriverID), rec.GrossFare,
		rec.AdvancesKharcha, rec.Deductions, rec.PerformanceBonus, rec.NetPayout,
		orNullPtr(rec.PaymentRef), paidAtStr, nowStr, nowStr)
	return err
}

func (s *DriverSettlementService) persistSettlement(ctx context.Context, db *sql.DB, rec DriverSettlementRecord) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_settlements
		    (id, trip_id, driver_id, gross_fare, advances_kharcha, deductions,
		     performance_bonus, net_payout, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)
		 ON CONFLICT(trip_id) DO NOTHING`,
		rec.ID, string(rec.TripID), string(rec.DriverID), rec.GrossFare,
		rec.AdvancesKharcha, rec.Deductions, rec.PerformanceBonus, rec.NetPayout,
		fuelTimeStr(rec.CreatedAt), fuelTimeStr(rec.UpdatedAt))
	return err
}

// PaidAtTime returns the paid timestamp (or zero) for storage.
func (r DriverSettlementRecord) PaidAtTime() time.Time {
	if r.PaidAt == nil {
		return time.Time{}
	}
	return *r.PaidAt
}

func orNullPtr(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}
