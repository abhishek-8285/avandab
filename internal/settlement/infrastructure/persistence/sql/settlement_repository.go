package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"transport-app/internal/settlement/domain"
)

type SQLSettlementRepository struct {
	db *sql.DB
}

func NewSQLSettlementRepository(db *sql.DB) *SQLSettlementRepository {
	return &SQLSettlementRepository{db: db}
}

func (r *SQLSettlementRepository) CreateSettlement(ctx context.Context, tenantID string, s *domain.Settlement) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Concurrency guard: Exactly ONE settlement per trip
	var count int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM driver_settlements
		WHERE tenant_id = ? AND trip_id = ?`,
		tenantID, s.TripID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("settlement already exists for trip %s", s.TripID)
	}

	deductions := s.CommissionAmount + s.AdvanceDeductions + s.TDSAmount - s.TollAdjustment

	_, err = tx.ExecContext(ctx, `
		INSERT INTO driver_settlements (
			id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status,
			commission_rate, commission_amount, toll_adjustment, advance_deductions,
			tds_rate, tds_amount, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, tenantID, s.TripID, s.DriverID, s.GrossFare, deductions, s.NetPayout, s.Status,
		s.CommissionRate, s.CommissionAmount, s.TollAdjustment, s.AdvanceDeductions,
		s.TDSRate, s.TDSAmount, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed inserting driver settlement: %w", err)
	}

	return tx.Commit()
}

func (r *SQLSettlementRepository) GetSettlementByTripID(ctx context.Context, tenantID, tripID string) (*domain.Settlement, error) {
	var s domain.Settlement
	var deductions float64
	var commRate, commAmt, tollAdj, advDed, tdsRate, tdsAmt sql.NullFloat64

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status,
		       commission_rate, commission_amount, toll_adjustment, advance_deductions,
		       tds_rate, tds_amount, created_at, updated_at
		FROM driver_settlements
		WHERE tenant_id = ? AND trip_id = ?`,
		tenantID, tripID).Scan(
		&s.ID, &s.TenantID, &s.TripID, &s.DriverID, &s.GrossFare, &deductions, &s.NetPayout, &s.Status,
		&commRate, &commAmt, &tollAdj, &advDed, &tdsRate, &tdsAmt, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if commRate.Valid {
		s.CommissionRate = commRate.Float64
	}
	if commAmt.Valid {
		s.CommissionAmount = commAmt.Float64
	}
	if tollAdj.Valid {
		s.TollAdjustment = tollAdj.Float64
	}
	if advDed.Valid {
		s.AdvanceDeductions = advDed.Float64
	}
	if tdsRate.Valid {
		s.TDSRate = tdsRate.Float64
	}
	if tdsAmt.Valid {
		s.TDSAmount = tdsAmt.Float64
	}

	return &s, nil
}

func (r *SQLSettlementRepository) ApproveSettlement(ctx context.Context, tenantID, settlementID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE driver_settlements
		SET status = 'approved', updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		time.Now(), tenantID, settlementID)
	return err
}

func (r *SQLSettlementRepository) AppendLedgerEntry(ctx context.Context, tenantID string, entry *domain.LedgerEntry) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Fetch current latest balance for driver
	var currentBalance float64
	err = tx.QueryRowContext(ctx, `
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = ? AND driver_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`,
		tenantID, entry.DriverID).Scan(&currentBalance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	entry.BalanceAfter = math.Round((currentBalance+entry.Amount)*100) / 100
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO driver_ledger_entries (
			id, tenant_id, driver_id, trip_id, entry_type, amount, currency,
			reference_type, reference_id, balance_after, description, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, tenantID, entry.DriverID, entry.TripID, string(entry.EntryType),
		entry.Amount, entry.Currency, entry.ReferenceType, entry.ReferenceID,
		entry.BalanceAfter, entry.Description, entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed writing ledger entry: %w", err)
	}

	return tx.Commit()
}

func (r *SQLSettlementRepository) HasCompensatingLedgerEntry(ctx context.Context, tenantID, referenceType, referenceID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM driver_ledger_entries
		WHERE tenant_id = ? AND reference_type = ? AND reference_id = ?`,
		tenantID, referenceType, referenceID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *SQLSettlementRepository) GetDriverBalance(ctx context.Context, tenantID, driverID string) (float64, error) {
	var bal float64
	err := r.db.QueryRowContext(ctx, `
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = ? AND driver_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`,
		tenantID, driverID).Scan(&bal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0.0, nil
		}
		return 0.0, err
	}
	return bal, nil
}

func (r *SQLSettlementRepository) GetDriverWallet(ctx context.Context, tenantID, driverID string) (*domain.DriverWallet, error) {
	// 1. Available balance from immutable ledger
	available, err := r.GetDriverBalance(ctx, tenantID, driverID)
	if err != nil {
		return nil, err
	}

	// 2. Pending settlements (calculated but not yet approved or entered in ledger)
	var pending sql.NullFloat64
	_ = r.db.QueryRowContext(ctx, `
		SELECT SUM(net_payout) FROM driver_settlements
		WHERE tenant_id = ? AND driver_id = ? AND status = 'calculated'`,
		tenantID, driverID).Scan(&pending)

	// 3. Paid balance (historical paid payouts)
	var paid sql.NullFloat64
	_ = r.db.QueryRowContext(ctx, `
		SELECT SUM(amount) FROM payout_instructions
		WHERE tenant_id = ? AND driver_id = ? AND status = 'paid'`,
		tenantID, driverID).Scan(&paid)

	// 4. Held balance (payouts initiated or processing)
	var held sql.NullFloat64
	_ = r.db.QueryRowContext(ctx, `
		SELECT SUM(amount) FROM payout_instructions
		WHERE tenant_id = ? AND driver_id = ? AND status IN ('initiated', 'processing')`,
		tenantID, driverID).Scan(&held)

	// 5. Recent entries
	entries, _ := r.GetRecentLedgerEntries(ctx, tenantID, driverID, 20)

	return &domain.DriverWallet{
		DriverID:         driverID,
		AvailableBalance: available,
		PendingBalance:   pending.Float64,
		PaidBalance:      paid.Float64,
		HeldBalance:      held.Float64,
		Currency:         "INR",
		RecentEntries:    entries,
		UpdatedAt:        time.Now(),
	}, nil
}

func (r *SQLSettlementRepository) GetRecentLedgerEntries(ctx context.Context, tenantID, driverID string, limit int) ([]domain.LedgerEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, driver_id, trip_id, entry_type, amount, currency,
		       reference_type, reference_id, balance_after, description, created_at
		FROM driver_ledger_entries
		WHERE tenant_id = ? AND driver_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT ?`,
		tenantID, driverID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var tripID, desc sql.NullString
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.DriverID, &tripID, &e.EntryType, &e.Amount, &e.Currency,
			&e.ReferenceType, &e.ReferenceID, &e.BalanceAfter, &desc, &e.CreatedAt); err != nil {
			return nil, err
		}
		if tripID.Valid {
			e.TripID = &tripID.String
		}
		e.Description = desc.String
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *SQLSettlementRepository) CreatePayoutInstruction(ctx context.Context, tenantID string, p *domain.PayoutInstruction) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO payout_instructions (
			id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
			provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, tenantID, p.DriverID, p.PayoutAccountID, p.Amount, p.Currency, p.IdempotencyKey,
		p.ProviderPayoutID, string(p.Status), p.FailureReason, p.UTR, p.InitiatedAt, p.CompletedAt, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *SQLSettlementRepository) GetPayoutByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*domain.PayoutInstruction, error) {
	var p domain.PayoutInstruction
	var provID, failReason, utr sql.NullString
	var compAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE tenant_id = ? AND idempotency_key = ?`,
		tenantID, idempotencyKey).Scan(
		&p.ID, &p.TenantID, &p.DriverID, &p.PayoutAccountID, &p.Amount, &p.Currency, &p.IdempotencyKey,
		&provID, &p.Status, &failReason, &utr, &p.InitiatedAt, &compAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if provID.Valid {
		p.ProviderPayoutID = &provID.String
	}
	if failReason.Valid {
		p.FailureReason = &failReason.String
	}
	if utr.Valid {
		p.UTR = &utr.String
	}
	if compAt.Valid {
		p.CompletedAt = &compAt.Time
	}
	return &p, nil
}

func (r *SQLSettlementRepository) GetPayoutByID(ctx context.Context, tenantID, payoutID string) (*domain.PayoutInstruction, error) {
	var p domain.PayoutInstruction
	var provID, failReason, utr sql.NullString
	var compAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE tenant_id = ? AND id = ?`,
		tenantID, payoutID).Scan(
		&p.ID, &p.TenantID, &p.DriverID, &p.PayoutAccountID, &p.Amount, &p.Currency, &p.IdempotencyKey,
		&provID, &p.Status, &failReason, &utr, &p.InitiatedAt, &compAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("payout instruction not found")
		}
		return nil, err
	}
	if provID.Valid {
		p.ProviderPayoutID = &provID.String
	}
	if failReason.Valid {
		p.FailureReason = &failReason.String
	}
	if utr.Valid {
		p.UTR = &utr.String
	}
	if compAt.Valid {
		p.CompletedAt = &compAt.Time
	}
	return &p, nil
}

func (r *SQLSettlementRepository) GetPayoutByIDGlobal(ctx context.Context, payoutID string) (*domain.PayoutInstruction, error) {
	var p domain.PayoutInstruction
	var provID, failReason, utr sql.NullString
	var compAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE id = ?`,
		payoutID).Scan(
		&p.ID, &p.TenantID, &p.DriverID, &p.PayoutAccountID, &p.Amount, &p.Currency, &p.IdempotencyKey,
		&provID, &p.Status, &failReason, &utr, &p.InitiatedAt, &compAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("payout instruction not found")
		}
		return nil, err
	}
	if provID.Valid {
		p.ProviderPayoutID = &provID.String
	}
	if failReason.Valid {
		p.FailureReason = &failReason.String
	}
	if utr.Valid {
		p.UTR = &utr.String
	}
	if compAt.Valid {
		p.CompletedAt = &compAt.Time
	}
	return &p, nil
}

func (r *SQLSettlementRepository) UpdatePayoutStatus(ctx context.Context, tenantID, payoutID string, status domain.PayoutStatus, providerPayoutID, utr, failureReason *string) error {
	now := time.Now()
	var completedAt *time.Time
	if status == domain.PayoutPaid || status == domain.PayoutFailed || status == domain.PayoutReversed {
		completedAt = &now
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE payout_instructions
		SET status = ?, provider_payout_id = COALESCE(?, provider_payout_id),
		    utr = COALESCE(?, utr), failure_reason = COALESCE(?, failure_reason),
		    completed_at = COALESCE(?, completed_at), updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		string(status), providerPayoutID, utr, failureReason, completedAt, now, tenantID, payoutID)
	return err
}

func (r *SQLSettlementRepository) RecordProviderEvent(ctx context.Context, tenantID, provider, eventID, eventType, payload string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO provider_events (id, tenant_id, provider, provider_event_id, event_type, payload, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"pe_"+eventID, tenantID, provider, eventID, eventType, payload, time.Now())
	return err
}

func (r *SQLSettlementRepository) IsProviderEventProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM provider_events
		WHERE provider = ? AND provider_event_id = ?`,
		provider, eventID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *SQLSettlementRepository) IsDriverPayoutAccountVerified(ctx context.Context, tenantID, driverID string) (bool, string, error) {
	var id, status string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, status FROM driver_payout_accounts
		WHERE tenant_id = ? AND driver_id = ? AND is_primary = 1
		LIMIT 1`,
		tenantID, driverID).Scan(&id, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Fallback: check legacy drivers.bank_details if present
			var bank sql.NullString
			_ = r.db.QueryRowContext(ctx, `SELECT bank_details FROM drivers WHERE tenant_id = ? AND id = ?`, tenantID, driverID).Scan(&bank)
			if bank.Valid && len(bank.String) > 5 {
				return true, "legacy_account", nil
			}
			return false, "", nil
		}
		return false, "", err
	}
	return status == "verified", id, nil
}
