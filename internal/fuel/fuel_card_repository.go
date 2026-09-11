package fuel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// FuelCardRepository defines the persistence contract for fuel card management.
type FuelCardRepository interface {
	RegisterCard(ctx context.Context, card FuelCard) (*FuelCard, error)
	GetCardByTokenHash(ctx context.Context, tenantID, tokenHash string) (*FuelCard, error)
	GetCardByID(ctx context.Context, tenantID, cardID string) (*FuelCard, error)
	ListCards(ctx context.Context, tenantID string) ([]FuelCard, error)
	GetVehicleTankCapacity(ctx context.Context, tenantID, vehicleID string) (float64, error)
	FindMatchingExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount float64, txnTime time.Time) (string, error)
	CreateVerifiedExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount, litres float64, txnTime time.Time, notes string) (string, error)
	MarkExpenseVerified(ctx context.Context, tenantID, expenseID, reason string) error
	PostGeneralLedgerAndSyncLog(ctx context.Context, tenantID string, txn FuelCardTransaction, provider string) (string, error)
	InsertTransaction(ctx context.Context, txn FuelCardTransaction) (*FuelCardTransaction, error)
	GetTransactionByID(ctx context.Context, tenantID, txnID string) (*FuelCardTransaction, error)
	GetTransactionByExternalID(ctx context.Context, tenantID, externalTxnID string) (*FuelCardTransaction, error)
	ReconcileTransaction(ctx context.Context, tenantID, txnID, expenseID, notes string) (*FuelCardTransaction, error)
	RecordPilferageAlert(ctx context.Context, tenantID, vehicleID, title, description string) error
}

// SQLFuelCardRepository implements FuelCardRepository using database/sql.
type SQLFuelCardRepository struct {
	db *sql.DB
}

// NewSQLFuelCardRepository creates a new SQLFuelCardRepository.
func NewSQLFuelCardRepository(db *sql.DB) *SQLFuelCardRepository {
	return &SQLFuelCardRepository{db: db}
}

// RegisterCard stores a new fuel card record.
func (r *SQLFuelCardRepository) RegisterCard(ctx context.Context, card FuelCard) (*FuelCard, error) {
	if card.ID == "" {
		card.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	card.CreatedAt = now
	card.UpdatedAt = now

	query := `
		INSERT INTO fuel_cards (
			id, tenant_id, card_number_masked, card_token_hash, provider,
			assigned_vehicle_id, assigned_driver_id, daily_spend_limit, status,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

	_, err := r.db.ExecContext(ctx, query,
		card.ID, card.TenantID, card.CardNumberMasked, card.CardTokenHash, string(card.Provider),
		card.AssignedVehicleID, card.AssignedDriverID, card.DailySpendLimit, string(card.Status),
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register fuel card: %w", err)
	}

	return &card, nil
}

// GetCardByTokenHash finds an active or registered card by its token hash.
func (r *SQLFuelCardRepository) GetCardByTokenHash(ctx context.Context, tenantID, tokenHash string) (*FuelCard, error) {
	var c FuelCard
	var prov, stat string
	var vehID, drvID sql.NullString

	query := `
		SELECT id, tenant_id, card_number_masked, card_token_hash, provider,
		       assigned_vehicle_id, assigned_driver_id, daily_spend_limit, status,
		       created_at, updated_at
		FROM fuel_cards
		WHERE tenant_id = $1 AND card_token_hash = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, tokenHash).Scan(
		&c.ID, &c.TenantID, &c.CardNumberMasked, &c.CardTokenHash, &prov,
		&vehID, &drvID, &c.DailySpendLimit, &stat,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.Provider = FuelCardProvider(prov)
	c.Status = FuelCardStatus(stat)
	if vehID.Valid {
		c.AssignedVehicleID = &vehID.String
	}
	if drvID.Valid {
		c.AssignedDriverID = &drvID.String
	}

	return &c, nil
}

// GetCardByID finds a card by its ID.
func (r *SQLFuelCardRepository) GetCardByID(ctx context.Context, tenantID, cardID string) (*FuelCard, error) {
	var c FuelCard
	var prov, stat string
	var vehID, drvID sql.NullString

	query := `
		SELECT id, tenant_id, card_number_masked, card_token_hash, provider,
		       assigned_vehicle_id, assigned_driver_id, daily_spend_limit, status,
		       created_at, updated_at
		FROM fuel_cards
		WHERE tenant_id = $1 AND id = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, cardID).Scan(
		&c.ID, &c.TenantID, &c.CardNumberMasked, &c.CardTokenHash, &prov,
		&vehID, &drvID, &c.DailySpendLimit, &stat,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	c.Provider = FuelCardProvider(prov)
	c.Status = FuelCardStatus(stat)
	if vehID.Valid {
		c.AssignedVehicleID = &vehID.String
	}
	if drvID.Valid {
		c.AssignedDriverID = &drvID.String
	}

	return &c, nil
}

// ListCards returns all cards for a tenant with today's spend metric.
func (r *SQLFuelCardRepository) ListCards(ctx context.Context, tenantID string) ([]FuelCard, error) {
	query := `
		SELECT c.id, c.tenant_id, c.card_number_masked, c.card_token_hash, c.provider,
		       c.assigned_vehicle_id, c.assigned_driver_id, c.daily_spend_limit, c.status,
		       c.created_at, c.updated_at,
		       COALESCE(SUM(CASE WHEN date(t.txn_time) = date('now') THEN t.total_amount ELSE 0 END), 0) as spend_today,
		       COUNT(t.id) as total_txns
		FROM fuel_cards c
		LEFT JOIN fuel_card_transactions t ON t.fuel_card_id = c.id
		WHERE c.tenant_id = $1
		GROUP BY c.id
		ORDER BY c.created_at DESC`

	rows, err := r.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cards []FuelCard
	for rows.Next() {
		var c FuelCard
		var prov, stat string
		var vehID, drvID sql.NullString
		var spendToday float64
		var totalTxns int

		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.CardNumberMasked, &c.CardTokenHash, &prov,
			&vehID, &drvID, &c.DailySpendLimit, &stat,
			&c.CreatedAt, &c.UpdatedAt,
			&spendToday, &totalTxns,
		); err != nil {
			continue
		}

		c.Provider = FuelCardProvider(prov)
		c.Status = FuelCardStatus(stat)
		if vehID.Valid {
			c.AssignedVehicleID = &vehID.String
		}
		if drvID.Valid {
			c.AssignedDriverID = &drvID.String
		}
		c.SpendToday = spendToday
		c.TotalTransactions = totalTxns

		cards = append(cards, c)
	}

	return cards, nil
}

// GetVehicleTankCapacity returns the fuel tank capacity of a vehicle.
func (r *SQLFuelCardRepository) GetVehicleTankCapacity(ctx context.Context, tenantID, vehicleID string) (float64, error) {
	var cap sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `
		SELECT tank_capacity_litres FROM vehicles
		WHERE id = $1 AND tenant_id = $2`, vehicleID, tenantID).Scan(&cap)
	if err != nil {
		return 0, err
	}
	if cap.Valid {
		return cap.Float64, nil
	}
	return 0, nil
}

// FindMatchingExpense searches driver_expenses for a matching date (+-24h) and amount (+-10 INR).
func (r *SQLFuelCardRepository) FindMatchingExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount float64, txnTime time.Time) (string, error) {
	start := txnTime.Add(-24 * time.Hour)
	end := txnTime.Add(24 * time.Hour)
	minAmt := amount - 10.0
	if minAmt < 0 {
		minAmt = 0
	}
	maxAmt := amount + 10.0

	var expenseID string
	var err error

	if driverID != nil && *driverID != "" {
		err = r.db.QueryRowContext(ctx, `
			SELECT id FROM driver_expenses
			WHERE tenant_id = $1
			  AND category = 'fuel'
			  AND driver_id = $2
			  AND amount >= $3 AND amount <= $4
			  AND created_at >= $5 AND created_at <= $6
			  AND status != 'rejected'
			ORDER BY ABS(amount - $7) ASC
			LIMIT 1`, tenantID, *driverID, minAmt, maxAmt, start, end, amount).Scan(&expenseID)
	} else {
		err = r.db.QueryRowContext(ctx, `
			SELECT id FROM driver_expenses
			WHERE tenant_id = $1
			  AND category = 'fuel'
			  AND amount >= $2 AND amount <= $3
			  AND created_at >= $4 AND created_at <= $5
			  AND status != 'rejected'
			ORDER BY ABS(amount - $6) ASC
			LIMIT 1`, tenantID, minAmt, maxAmt, start, end, amount).Scan(&expenseID)
	}

	if err != nil {
		return "", err
	}
	return expenseID, nil
}

// CreateVerifiedExpense automatically creates a verified driver_expenses entry.
func (r *SQLFuelCardRepository) CreateVerifiedExpense(ctx context.Context, tenantID string, vehicleID, driverID *string, amount, litres float64, txnTime time.Time, notes string) (string, error) {
	expenseID := uuid.NewString()
	var dID *string
	if driverID != nil && *driverID != "" {
		dID = driverID
	}

	query := `
		INSERT INTO driver_expenses (
			id, tenant_id, driver_id, expense_type, amount, description,
			status, category, verification_state, flag_reason, fuel_litres, created_at
		) VALUES ($1, $2, $3, 'fuel', $4, $5, 'approved', 'fuel', 'auto_verified', 'Created from fuel card transaction', $6, $7)`

	_, err := r.db.ExecContext(ctx, query,
		expenseID, tenantID, dID, amount, notes, litres, txnTime,
	)
	if err != nil {
		return "", fmt.Errorf("failed to auto-create verified expense: %w", err)
	}

	return expenseID, nil
}

// MarkExpenseVerified updates an existing expense to verified status.
func (r *SQLFuelCardRepository) MarkExpenseVerified(ctx context.Context, tenantID, expenseID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE driver_expenses
		SET verification_state = 'auto_verified', status = 'approved', flag_reason = $1
		WHERE id = $2 AND tenant_id = $3`, reason, expenseID, tenantID)
	return err
}

// PostGeneralLedgerAndSyncLog pushes double-entry ledger rows and accounting sync log.
func (r *SQLFuelCardRepository) PostGeneralLedgerAndSyncLog(ctx context.Context, tenantID string, txn FuelCardTransaction, provider string) (string, error) {
	syncLogID := uuid.NewString()
	amountMinor := int64(math.Round(txn.TotalAmount * 100))

	payload, _ := json.Marshal(map[string]interface{}{
		"transaction_id": txn.ID,
		"external_id":    txn.ExternalTxnID,
		"station_name":   txn.FuelStationName,
		"volume_litres":  txn.VolumeLitres,
		"rate":           txn.RatePerLitre,
		"total_amount":   txn.TotalAmount,
		"provider":       provider,
		"txn_time":       txn.TxnTime.Format(time.RFC3339),
	})

	// 1. Insert into accounting_sync_log
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO accounting_sync_log (
			id, idempotency_key, direction, entity_type, entity_id,
			adapter, payload_json, external_id, status, attempts, created_at, updated_at
		) VALUES ($1, $2, 'out', 'fuel_card_transaction', $3, $4, $5, $6, 'acked', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		syncLogID, "fuel-card-txn-"+txn.ExternalTxnID, txn.ID, provider, string(payload), txn.ExternalTxnID,
	)
	if err != nil {
		return "", fmt.Errorf("failed to insert accounting sync log: %w", err)
	}

	// 2. Double-entry lite into money_ledger
	// Debit: Fuel Expense
	debitID := uuid.NewString()
	memoDebit := fmt.Sprintf("Fuel expense at %s (Card %s)", txn.FuelStationName, txn.FuelCardID)
	_, _ = r.db.ExecContext(ctx, `
		INSERT INTO money_ledger (
			id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor, currency, memo, created_by, created_at
		) VALUES ($1, $2, 'kharcha_approved', 'fuel_card_transactions', $3, 'debit', $4, 'INR', $5, 'system', CURRENT_TIMESTAMP)`,
		debitID, tenantID, txn.ID, amountMinor, memoDebit,
	)

	// Credit: Fuel Card Clearing
	creditID := uuid.NewString()
	memoCredit := fmt.Sprintf("Fuel card clearing %s", provider)
	_, _ = r.db.ExecContext(ctx, `
		INSERT INTO money_ledger (
			id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor, currency, memo, created_by, created_at
		) VALUES ($1, $2, 'kharcha_approved', 'fuel_card_transactions', $3, 'credit', $4, 'INR', $5, 'system', CURRENT_TIMESTAMP)`,
		creditID, tenantID, txn.ID, amountMinor, memoCredit,
	)

	return syncLogID, nil
}

// InsertTransaction inserts a fuel card transaction.
func (r *SQLFuelCardRepository) InsertTransaction(ctx context.Context, txn FuelCardTransaction) (*FuelCardTransaction, error) {
	if txn.ID == "" {
		txn.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	txn.CreatedAt = now

	query := `
		INSERT INTO fuel_card_transactions (
			id, tenant_id, fuel_card_id, external_txn_id, txn_time,
			fuel_station_name, fuel_station_city, fuel_type,
			volume_litres, rate_per_litre, total_amount, odometer_reported,
			reconciliation_status, matched_expense_id, sync_log_id, notes, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`

	_, err := r.db.ExecContext(ctx, query,
		txn.ID, txn.TenantID, txn.FuelCardID, txn.ExternalTxnID, txn.TxnTime,
		txn.FuelStationName, txn.FuelStationCity, txn.FuelType,
		txn.VolumeLitres, txn.RatePerLitre, txn.TotalAmount, txn.OdometerReported,
		string(txn.ReconciliationStatus), txn.MatchedExpenseID, txn.SyncLogID, txn.Notes, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert fuel card transaction: %w", err)
	}

	return &txn, nil
}

// GetTransactionByID retrieves a transaction by ID.
func (r *SQLFuelCardRepository) GetTransactionByID(ctx context.Context, tenantID, txnID string) (*FuelCardTransaction, error) {
	var t FuelCardTransaction
	var status string
	var city, matchExp, syncLog, notes sql.NullString
	var odo sql.NullFloat64

	query := `
		SELECT id, tenant_id, fuel_card_id, external_txn_id, txn_time,
		       fuel_station_name, fuel_station_city, fuel_type,
		       volume_litres, rate_per_litre, total_amount, odometer_reported,
		       reconciliation_status, matched_expense_id, sync_log_id, notes, created_at
		FROM fuel_card_transactions
		WHERE tenant_id = $1 AND id = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, txnID).Scan(
		&t.ID, &t.TenantID, &t.FuelCardID, &t.ExternalTxnID, &t.TxnTime,
		&t.FuelStationName, &city, &t.FuelType,
		&t.VolumeLitres, &t.RatePerLitre, &t.TotalAmount, &odo,
		&status, &matchExp, &syncLog, &notes, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	t.ReconciliationStatus = ReconciliationStatus(status)
	if city.Valid {
		t.FuelStationCity = &city.String
	}
	if matchExp.Valid {
		t.MatchedExpenseID = &matchExp.String
	}
	if syncLog.Valid {
		t.SyncLogID = &syncLog.String
	}
	if notes.Valid {
		t.Notes = &notes.String
	}
	if odo.Valid {
		t.OdometerReported = &odo.Float64
	}

	return &t, nil
}

// GetTransactionByExternalID retrieves a transaction by external OMC transaction ID.
func (r *SQLFuelCardRepository) GetTransactionByExternalID(ctx context.Context, tenantID, externalTxnID string) (*FuelCardTransaction, error) {
	var t FuelCardTransaction
	var status string
	var city, matchExp, syncLog, notes sql.NullString
	var odo sql.NullFloat64

	query := `
		SELECT id, tenant_id, fuel_card_id, external_txn_id, txn_time,
		       fuel_station_name, fuel_station_city, fuel_type,
		       volume_litres, rate_per_litre, total_amount, odometer_reported,
		       reconciliation_status, matched_expense_id, sync_log_id, notes, created_at
		FROM fuel_card_transactions
		WHERE tenant_id = $1 AND external_txn_id = $2`

	err := r.db.QueryRowContext(ctx, query, tenantID, externalTxnID).Scan(
		&t.ID, &t.TenantID, &t.FuelCardID, &t.ExternalTxnID, &t.TxnTime,
		&t.FuelStationName, &city, &t.FuelType,
		&t.VolumeLitres, &t.RatePerLitre, &t.TotalAmount, &odo,
		&status, &matchExp, &syncLog, &notes, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	t.ReconciliationStatus = ReconciliationStatus(status)
	if city.Valid {
		t.FuelStationCity = &city.String
	}
	if matchExp.Valid {
		t.MatchedExpenseID = &matchExp.String
	}
	if syncLog.Valid {
		t.SyncLogID = &syncLog.String
	}
	if notes.Valid {
		t.Notes = &notes.String
	}
	if odo.Valid {
		t.OdometerReported = &odo.Float64
	}

	return &t, nil
}

// ReconcileTransaction manually matches a transaction against an expense claim.
func (r *SQLFuelCardRepository) ReconcileTransaction(ctx context.Context, tenantID, txnID, expenseID, notes string) (*FuelCardTransaction, error) {
	_, err := r.db.ExecContext(ctx, `
		UPDATE fuel_card_transactions
		SET matched_expense_id = $1, reconciliation_status = 'MATCHED_EXPENSE', notes = COALESCE(notes || ' | ' || $2, $2)
		WHERE id = $3 AND tenant_id = $4`,
		expenseID, notes, txnID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile transaction: %w", err)
	}

	// Update driver expense to verified
	_ = r.MarkExpenseVerified(ctx, tenantID, expenseID, "Manually matched to fuel card transaction")

	return r.GetTransactionByID(ctx, tenantID, txnID)
}

// RecordPilferageAlert creates a high-severity alert in ops_alerts for volume anomalies.
func (r *SQLFuelCardRepository) RecordPilferageAlert(ctx context.Context, tenantID, vehicleID, title, description string) error {
	alertID := uuid.NewString()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ops_alerts (
			id, tenant_id, alert_type, severity, title, description, entity_type, entity_id, status, created_at
		) VALUES ($1, $2, 'fuel_theft_confirmed', 'high', $3, $4, 'vehicle', $5, 'open', CURRENT_TIMESTAMP)`,
		alertID, tenantID, title, description, vehicleID,
	)
	return err
}
