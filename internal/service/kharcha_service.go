package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/repository"
)

// KharchaExpense is the service-layer view of a driver expense with joined trip/driver data.
type KharchaExpense struct {
	ID             string
	TripID         string
	TripNumber     string
	DriverID       string
	DriverName     string
	Category       string // advance | fuel | toll | food | repair | other | rto | tyre | bhatta
	Amount         float64
	Description    string
	ReceiptURL     *string
	Status         string // pending | approved | rejected | settled
	RejectedReason *string
	ApprovedBy     *string
	ApprovedAt     *time.Time
	CreatedAt      time.Time
	// Fuel audit columns (Spec 03 §3.3): audit_status + litres on the
	// expense, variance from the latest fuel_claim_audits row.
	AuditStatus    string
	FuelLitres     *float64
	VarianceLitres *float64
	VariancePct    *float64
}

// ExpectedLitres reconstructs the expected value used for the variance
// tooltip: claimed − variance (Spec 03 §3.3). Zero when no audit row exists.
func (e KharchaExpense) ExpectedLitres() float64 {
	if e.FuelLitres != nil && e.VarianceLitres != nil {
		return *e.FuelLitres - *e.VarianceLitres
	}
	return 0
}

// KharchaStats holds dashboard summary counts/totals.
type KharchaStats struct {
	PendingCount   int
	ApprovedToday  int
	MonthTotal     float64
	UnsettledTotal float64
}

// KharchaService manages driver expense (kharcha) approvals and the ledger.
// It uses raw SQL because driver_expenses has no generated SQLC repository methods yet.
type KharchaService struct {
	baseService
}

// ListPendingExpenses returns all driver expenses awaiting approval.

func (s *KharchaService) ListPendingExpenses(ctx context.Context) ([]KharchaExpense, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return nil, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	rows, err := db.QueryContext(ctx, `
		SELECT de.id,
		       COALESCE(de.trip_id, '') AS trip_id,
		       COALESCE(t.trip_number, '') AS trip_number,
		       COALESCE(de.driver_id, '') AS driver_id,
		       COALESCE(d.first_name||' '||d.last_name, '') AS driver_name,
		       COALESCE(de.category, de.expense_type, 'other') AS category,
		       de.amount,
		       COALESCE(de.description, '') AS description,
		       de.receipt_url,
		       COALESCE(de.status, 'pending') AS status,
		       de.rejected_reason,
		       de.approved_by,
		       de.approved_at,
		       de.created_at,
		       COALESCE(de.audit_status, 'pending') AS audit_status,
		       de.fuel_litres,
		       fca.variance_litres,
		       fca.variance_pct
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		LEFT JOIN fuel_claim_audits fca ON fca.expense_id = de.id
		WHERE COALESCE(de.status, 'pending') = 'pending'
		  AND de.tenant_id = ?
		ORDER BY de.created_at ASC`, tenantIDFor(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanKharchaRows(rows)
}

// ListLedger returns expenses filtered by tripID (empty = all), newest first.
func (s *KharchaService) ListLedger(ctx context.Context, tripID string) ([]KharchaExpense, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return nil, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	query := `
		SELECT de.id,
		       COALESCE(de.trip_id, '') AS trip_id,
		       COALESCE(t.trip_number, '') AS trip_number,
		       COALESCE(de.driver_id, '') AS driver_id,
		       COALESCE(d.first_name||' '||d.last_name, '') AS driver_name,
		       COALESCE(de.category, de.expense_type, 'other') AS category,
		       de.amount,
		       COALESCE(de.description, '') AS description,
		       de.receipt_url,
		       COALESCE(de.status, 'pending') AS status,
		       de.rejected_reason,
		       de.approved_by,
		       de.approved_at,
		       de.created_at,
		       COALESCE(de.audit_status, 'pending') AS audit_status,
		       de.fuel_litres,
		       fca.variance_litres,
		       fca.variance_pct
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		LEFT JOIN fuel_claim_audits fca ON fca.expense_id = de.id`
	args := []interface{}{tenantIDFor(ctx)}
	query += " WHERE de.tenant_id = ?"
	if tripID != "" {
		query += " AND de.trip_id = ?"
		args = append(args, tripID)
	}
	query += " ORDER BY de.created_at DESC LIMIT 200"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanKharchaRows(rows)
}

// GetExpenseByID retrieves a single expense with joined data.
func (s *KharchaService) GetExpenseByID(ctx context.Context, id string) (KharchaExpense, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return KharchaExpense{}, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	row := db.QueryRowContext(ctx, `
		SELECT de.id,
		       COALESCE(de.trip_id, '') AS trip_id,
		       COALESCE(t.trip_number, '') AS trip_number,
		       COALESCE(de.driver_id, '') AS driver_id,
		       COALESCE(d.first_name||' '||d.last_name, '') AS driver_name,
		       COALESCE(de.category, de.expense_type, 'other') AS category,
		       de.amount,
		       COALESCE(de.description, '') AS description,
		       de.receipt_url,
		       COALESCE(de.status, 'pending') AS status,
		       de.rejected_reason,
		       de.approved_by,
		       de.approved_at,
		       de.created_at,
		       COALESCE(de.audit_status, 'pending') AS audit_status,
		       de.fuel_litres,
		       fca.variance_litres,
		       fca.variance_pct
		FROM driver_expenses de
		LEFT JOIN trips t ON t.id = de.trip_id
		LEFT JOIN drivers d ON d.id = de.driver_id
		LEFT JOIN fuel_claim_audits fca ON fca.expense_id = de.id
		WHERE de.id = ? AND de.tenant_id = ?`, id, tenantIDFor(ctx))

	var e KharchaExpense
	var receiptURL, rejectedReason, approvedBy *string
	var approvedAt *time.Time
	if err := row.Scan(
		&e.ID, &e.TripID, &e.TripNumber, &e.DriverID, &e.DriverName,
		&e.Category, &e.Amount, &e.Description, &receiptURL,
		&e.Status, &rejectedReason, &approvedBy, &approvedAt, &e.CreatedAt,
		&e.AuditStatus, &e.FuelLitres, &e.VarianceLitres, &e.VariancePct,
	); err != nil {
		return KharchaExpense{}, fmt.Errorf("expense not found: %w", err)
	}
	e.ReceiptURL = receiptURL
	e.RejectedReason = rejectedReason
	e.ApprovedBy = approvedBy
	e.ApprovedAt = approvedAt
	return e, nil
}

// ApproveExpense approves an expense in a transaction and deducts from the driver's settlement.
func (s *KharchaService) ApproveExpense(ctx context.Context, expenseID, approvedByUserID string) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now()

	// Fuel audit enforce gate (Spec 03 §3.2 step 4). MUST run BEFORE the
	// status UPDATE: in enforce mode a claim flagged needs_review must never
	// flip to approved, so the gate is a pre-flight check, not a post-check.
	if s.fuelAuditEnforce(ctx, db) {
		var as string
		if err := db.QueryRowContext(ctx,
			`SELECT COALESCE(audit_status, 'pending') FROM driver_expenses WHERE id = ? AND tenant_id = ?`,
			expenseID, tenantIDFor(ctx)).Scan(&as); err != nil {
			return fmt.Errorf("expense not found")
		}
		if as == "needs_review" {
			return fmt.Errorf("claim flagged by fuel audit (needs review); review at /fuel/audit")
		}
	}

	// 1. Mark approved (only if currently pending)
	res, err := tx.ExecContext(ctx,
		`UPDATE driver_expenses
		 SET status = 'approved', approved_by = ?, approved_at = ?
		 WHERE id = ? AND tenant_id = ? AND COALESCE(status, 'pending') = 'pending'`,
		approvedByUserID, now, expenseID, tenantIDFor(ctx))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("expense already processed or not found")
	}

	// 2. Fetch trip_id, driver_id, amount to update settlement
	var tripID, driverID string
	var amount float64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(trip_id,''), COALESCE(driver_id,''), amount FROM driver_expenses WHERE id = ? AND tenant_id = ?`,
		expenseID, tenantIDFor(ctx)).Scan(&tripID, &driverID, &amount); err != nil {
		return err
	}

	// 3. Deduct from settlement net_payout and add settlement line if a settlement row exists for this trip
	if tripID != "" && driverID != "" {
		var settlementID string
		var category string
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(category, 'kharcha') FROM driver_expenses WHERE id = ? AND tenant_id = ?`, expenseID, tenantIDFor(ctx)).Scan(&category)

		err := tx.QueryRowContext(ctx, `SELECT id FROM driver_settlements WHERE trip_id = ? AND driver_id = ?`, tripID, driverID).Scan(&settlementID)
		if err == nil && settlementID != "" {
			_, _ = tx.ExecContext(ctx,
				`UPDATE driver_settlements
				 SET advances_kharcha = advances_kharcha + ?,
				     net_payout = MAX(0.0, net_payout - ?)
				 WHERE id = ?`,
				amount, amount, settlementID)

			lineID := "stl-ln-" + uuid.New().String()
			label := fmt.Sprintf("Approved expense (%s) #%s", category, expenseID)
			_, _ = tx.ExecContext(ctx,
				`INSERT INTO settlement_lines (id, settlement_id, trip_id, line_type, label, amount, ref_id, created_at)
				 VALUES (?, ?, ?, 'deduction', ?, ?, ?, datetime('now'))`,
				lineID, settlementID, tripID, label, -amount, expenseID)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.logAudit(ctx, nil, "approve_kharcha", "driver_expenses", expenseID, nil, nil)
	s.log.Info("kharcha approved", "expense_id", expenseID, "amount", amount, "by", approvedByUserID)
	return nil
}

// RejectExpense rejects an expense with a mandatory reason.
func (s *KharchaService) RejectExpense(ctx context.Context, expenseID, rejectedByUserID, reason string) error {
	if reason == "" {
		return fmt.Errorf("rejection reason is required")
	}
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	res, err := db.ExecContext(ctx,
		`UPDATE driver_expenses
		 SET status = 'rejected', approved_by = ?, rejected_reason = ?
		 WHERE id = ? AND tenant_id = ? AND COALESCE(status, 'pending') = 'pending'`,
		rejectedByUserID, reason, expenseID, tenantIDFor(ctx))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("expense already processed or not found")
	}

	s.logAudit(ctx, nil, "reject_kharcha", "driver_expenses", expenseID, nil, &reason)
	s.log.Info("kharcha rejected", "expense_id", expenseID, "reason", reason, "by", rejectedByUserID)
	return nil
}

// fuelAuditEnforce reports whether company_config fuel.audit_enforce is
// 'true' (enforce mode gates approval of needs_review claims; annotate mode
// leaves them approvable, Spec 03 §3.2 step 4).
func (s *KharchaService) fuelAuditEnforce(ctx context.Context, db interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) bool {
	var v string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM company_config WHERE tenant_id = ? AND key = 'fuel.audit_enforce'`,
		tenantIDFor(ctx)).Scan(&v)
	return err == nil && v == "true"
}

// CreateExpense logs a new driver kharcha claim. fuelLitres is persisted
// only for fuel claims (Spec 03 §3.2 step 1; NULL elsewhere).
// IdempotencyKey prevents duplicate offline sync (Spec 21.1 Seam 2); empty = no dedup.
// CreateExpenseOpts carries everything needed to record a driver expense.
// Zero-value optional fields are stored as NULL. Latitude/Longitude capture
// the driver's GPS at claim time (Spec 13 mobile flow).
type CreateExpenseOpts struct {
	TripID         string
	DriverID       string
	Category       string
	Amount         float64
	Description    string
	ReceiptURL     string
	FuelLitres     float64
	IdempotencyKey string
	Latitude       *float64
	Longitude      *float64
}

func (s *KharchaService) CreateExpense(ctx context.Context, tripID, driverID, category string, amount float64, description, receiptURL string, fuelLitres float64, idempotencyKey ...string) (string, error) {
	opts := CreateExpenseOpts{
		TripID: tripID, DriverID: driverID, Category: category,
		Amount: amount, Description: description, ReceiptURL: receiptURL,
		FuelLitres: fuelLitres,
	}
	if len(idempotencyKey) > 0 {
		opts.IdempotencyKey = idempotencyKey[0]
	}
	return s.CreateExpenseWithOpts(ctx, opts)
}

// CreateExpenseWithOpts records a driver expense claim with full options
// (geo capture, idempotency). See CreateExpense for the legacy shorthand.
func (s *KharchaService) CreateExpenseWithOpts(ctx context.Context, o CreateExpenseOpts) (string, error) {
	if o.Amount <= 0 {
		return "", fmt.Errorf("amount must be greater than zero")
	}
	validCategories := map[string]bool{
		"advance": true, "fuel": true, "toll": true, "food": true, "repair": true, "other": true,
		"rto": true, "tyre": true, "bhatta": true,
	}
	if !validCategories[o.Category] {
		return "", fmt.Errorf("invalid category: %s", o.Category)
	}

	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return "", fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	expID := generateID()
	var recURL interface{} = nil
	if o.ReceiptURL != "" {
		recURL = o.ReceiptURL
	}
	var desc interface{} = nil
	if o.Description != "" {
		desc = o.Description
	}
	var tID interface{} = nil
	if o.TripID != "" {
		tID = o.TripID
	}
	var dID interface{} = nil
	if o.DriverID != "" {
		dID = o.DriverID
	}
	var litres interface{} = nil
	if o.Category == "fuel" && o.FuelLitres > 0 {
		litres = o.FuelLitres
	}
	var idemKey interface{} = nil
	if o.IdempotencyKey != "" {
		idemKey = o.IdempotencyKey
		// Idempotency check: if key exists, return existing ID (offline retry safe).
		// Tenant-scoped; the global unique index stays (documented limitation).
		var existingID string
		if err := db.QueryRowContext(ctx, `SELECT id FROM driver_expenses WHERE idempotency_key = ? AND tenant_id = ?`, idemKey, tenantIDFor(ctx)).Scan(&existingID); err == nil && existingID != "" {
			return existingID, nil
		}
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_expenses
		 (id, trip_id, driver_id, expense_type, category, amount, description, receipt_url, fuel_litres, status, created_at, idempotency_key, latitude, longitude, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?)`,
		expID, tID, dID, o.Category, o.Category, o.Amount, desc, recURL, litres, time.Now(), idemKey, o.Latitude, o.Longitude, tenantIDFor(ctx))
	if err != nil {
		// Handle race: unique index violation → return existing
		if idemKey != nil {
			var existingID string
			if err2 := db.QueryRowContext(ctx, `SELECT id FROM driver_expenses WHERE idempotency_key = ? AND tenant_id = ?`, idemKey, tenantIDFor(ctx)).Scan(&existingID); err2 == nil && existingID != "" {
				return existingID, nil
			}
		}
		return "", err
	}

	s.logAudit(ctx, nil, "create_kharcha", "driver_expenses", expID, nil, nil)
	s.log.Info("kharcha created", "expense_id", expID, "driver_id", o.DriverID, "amount", o.Amount)

	// Async verification hook (Spec 22 §5.3): never blocks the driver's
	// sync path; the verifier subscribes and writes verification_state.
	if s.events != nil {
		s.events.Publish(ctx, events.Event{
			Type:    events.ExpenseCreated,
			Payload: map[string]any{"expense_id": expID},
		})
	}
	return expID, nil
}

// GetKharchaStats returns dashboard summary statistics.
func (s *KharchaService) GetKharchaStats(ctx context.Context) (KharchaStats, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok {
		return KharchaStats{}, fmt.Errorf("store does not support raw DB access")
	}
	db := getter.DB()

	var stats KharchaStats

	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses WHERE COALESCE(status,'pending') = 'pending' AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&stats.PendingCount)

	today := time.Now().Format("2006-01-02")
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM driver_expenses WHERE status = 'approved' AND DATE(approved_at) = ? AND tenant_id = ?`,
		today, tenantIDFor(ctx)).
		Scan(&stats.ApprovedToday)

	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM driver_expenses
		 WHERE status = 'approved' AND strftime('%Y-%m', approved_at) = strftime('%Y-%m','now') AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&stats.MonthTotal)

	_ = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM driver_expenses
		 WHERE COALESCE(status,'pending') IN ('pending','approved') AND tenant_id = ?`,
		tenantIDFor(ctx)).
		Scan(&stats.UnsettledTotal)

	return stats, nil
}

// --- internal helpers ---

type kharchaScanner interface {
	Next() bool
	Scan(...interface{}) error
	Close() error
}

func scanKharchaRows(rows kharchaScanner) ([]KharchaExpense, error) {
	var expenses []KharchaExpense
	for rows.Next() {
		var e KharchaExpense
		var receiptURL, rejectedReason, approvedBy *string
		var approvedAt *time.Time
		if err := rows.Scan(
			&e.ID, &e.TripID, &e.TripNumber, &e.DriverID, &e.DriverName,
			&e.Category, &e.Amount, &e.Description, &receiptURL,
			&e.Status, &rejectedReason, &approvedBy, &approvedAt, &e.CreatedAt,
			&e.AuditStatus, &e.FuelLitres, &e.VarianceLitres, &e.VariancePct,
		); err != nil {
			return nil, err
		}
		e.ReceiptURL = receiptURL
		e.RejectedReason = rejectedReason
		e.ApprovedBy = approvedBy
		e.ApprovedAt = approvedAt
		expenses = append(expenses, e)
	}
	return expenses, rows.Close()
}
