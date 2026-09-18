package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
	appdb "transport-app/internal/database"
	"transport-app/internal/repository"

	"transport-app/internal/settlement/domain"
)

type SQLSettlementRepository struct {
	db *sql.DB
}

func NewSQLSettlementRepository(db *sql.DB) *SQLSettlementRepository {
	return &SQLSettlementRepository{db: db}
}

// querier routes to the ambient transaction when the caller runs inside
// WithTransaction, else the pool. Reads must observe uncommitted rows of the
// enclosing money movement; writes must join it or SQLite deadlocks.
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *SQLSettlementRepository) q(ctx context.Context) querier {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return r.db
}

// WithTransaction runs fn with all repository calls in one transaction,
// committing on nil error and rolling back otherwise. Re-entrant: when the
// context already carries a transaction it runs fn directly.
func (r *SQLSettlementRepository) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	if repository.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(repository.WithTxInContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLSettlementRepository) CreateSettlement(ctx context.Context, tenantID string, s *domain.Settlement) error {
	if repository.TxFromContext(ctx) != nil {
		return r.createSettlementTx(ctx, tenantID, s)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.createSettlementTx(repository.WithTxInContext(ctx, tx), tenantID, s); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLSettlementRepository) createSettlementTx(ctx context.Context, tenantID string, s *domain.Settlement) error {
	q := r.q(ctx)
	// Concurrency guard: Exactly ONE settlement per trip
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM driver_settlements
		WHERE tenant_id = $1 AND trip_id = $2`,
		tenantID, s.TripID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("settlement already exists for trip %s", s.TripID)
	}

	deductions := s.CommissionAmount + s.AdvanceDeductions + s.TDSAmount - s.TollAdjustment

	_, err = q.ExecContext(ctx, `
		INSERT INTO driver_settlements (
			id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status,
			commission_rate, commission_amount, toll_adjustment, advance_deductions,
			tds_rate, tds_amount, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		s.ID, tenantID, s.TripID, s.DriverID, s.GrossFare, deductions, s.NetPayout, s.Status,
		s.CommissionRate, s.CommissionAmount, s.TollAdjustment, s.AdvanceDeductions,
		s.TDSRate, s.TDSAmount, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed inserting driver settlement: %w", err)
	}

	return nil
}

func (r *SQLSettlementRepository) GetSettlementByTripID(ctx context.Context, tenantID, tripID string) (*domain.Settlement, error) {
	var s domain.Settlement
	var deductions float64
	var commRate, commAmt, tollAdj, advDed, tdsRate, tdsAmt sql.NullFloat64

	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, tenant_id, trip_id, driver_id, gross_fare, deductions, net_payout, status,
		       commission_rate, commission_amount, toll_adjustment, advance_deductions,
		       tds_rate, tds_amount, created_at, updated_at
		FROM driver_settlements
		WHERE tenant_id = $1 AND trip_id = $2`,
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
	_, err := r.q(ctx).ExecContext(ctx, `
		UPDATE driver_settlements
		SET status = 'approved', updated_at = $1
		WHERE tenant_id = $2 AND id = $3`,
		time.Now(), tenantID, settlementID)
	return err
}

func (r *SQLSettlementRepository) AppendLedgerEntry(ctx context.Context, tenantID string, entry *domain.LedgerEntry) error {
	if repository.TxFromContext(ctx) != nil {
		return r.appendLedgerEntryTx(ctx, tenantID, entry)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.appendLedgerEntryTx(repository.WithTxInContext(ctx, tx), tenantID, entry); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLSettlementRepository) appendLedgerEntryTx(ctx context.Context, tenantID string, entry *domain.LedgerEntry) error {
	q := r.q(ctx)
	// Fetch current latest balance for driver
	var currentBalance float64
	err := q.QueryRowContext(ctx, `
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = $1 AND driver_id = $2
		ORDER BY created_at DESC, `+appdb.RowidOrder(r.db, false)+` LIMIT 1`,
		tenantID, entry.DriverID).Scan(&currentBalance)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	entry.BalanceAfter = math.Round((currentBalance+entry.Amount)*100) / 100
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	_, err = q.ExecContext(ctx, `
		INSERT INTO driver_ledger_entries (
			id, tenant_id, driver_id, trip_id, entry_type, amount, currency,
			reference_type, reference_id, balance_after, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		entry.ID, tenantID, entry.DriverID, entry.TripID, string(entry.EntryType),
		entry.Amount, entry.Currency, entry.ReferenceType, entry.ReferenceID,
		entry.BalanceAfter, entry.Description, entry.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed writing ledger entry: %w", err)
	}

	return nil
}

func (r *SQLSettlementRepository) ListLedgerEntryTypes(ctx context.Context, tenantID, referenceType, referenceID string) ([]domain.EntryType, error) {
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT DISTINCT entry_type FROM driver_ledger_entries
		WHERE tenant_id = $1 AND reference_type = $2 AND reference_id = $3`,
		tenantID, referenceType, referenceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.EntryType
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, domain.EntryType(t))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SQLSettlementRepository) HasCompensatingLedgerEntry(ctx context.Context, tenantID, referenceType, referenceID string) (bool, error) {
	var count int
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM driver_ledger_entries
		WHERE tenant_id = $1 AND reference_type = $2 AND reference_id = $3`,
		tenantID, referenceType, referenceID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *SQLSettlementRepository) GetDriverBalance(ctx context.Context, tenantID, driverID string) (float64, error) {
	var bal float64
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT balance_after FROM driver_ledger_entries
		WHERE tenant_id = $1 AND driver_id = $2
		ORDER BY created_at DESC, `+appdb.RowidOrder(r.db, false)+` LIMIT 1`,
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
	_ = r.q(ctx).QueryRowContext(ctx, `
		SELECT SUM(net_payout) FROM driver_settlements
		WHERE tenant_id = $1 AND driver_id = $2 AND status = 'calculated'`,
		tenantID, driverID).Scan(&pending)

	// 3. Paid balance (historical paid payouts)
	var paid sql.NullFloat64
	_ = r.q(ctx).QueryRowContext(ctx, `
		SELECT SUM(amount) FROM payout_instructions
		WHERE tenant_id = $1 AND driver_id = $2 AND status = 'paid'`,
		tenantID, driverID).Scan(&paid)

	// 4. Held balance (payouts initiated or processing)
	var held sql.NullFloat64
	_ = r.q(ctx).QueryRowContext(ctx, `
		SELECT SUM(amount) FROM payout_instructions
		WHERE tenant_id = $1 AND driver_id = $2 AND status IN ('initiated', 'processing')`,
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
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT id, tenant_id, driver_id, trip_id, entry_type, amount, currency,
		       reference_type, reference_id, balance_after, description, created_at
		FROM driver_ledger_entries
		WHERE tenant_id = $1 AND driver_id = $2
		ORDER BY created_at DESC, `+appdb.RowidOrder(r.db, false)+` LIMIT $3`,
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

func (r *SQLSettlementRepository) LockDriverPayout(ctx context.Context, tenantID, driverID string) error {
	// Serialize concurrent same-driver payouts before the balance read.
	// Postgres takes a row lock; SQLite has no FOR UPDATE, so a no-op touch
	// of the driver row upgrades the DEFERRED transaction to a write lock —
	// the peer's touch then busy-waits until this transaction commits and
	// reads the winner's debit. first_name is self-assigned so the touch is
	// portable across the sqlite/postgres drivers-table variants.
	if appdb.IsPostgres(r.db) {
		var id string
		err := r.q(ctx).QueryRowContext(ctx, `
			SELECT id FROM drivers
			WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
			tenantID, driverID).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // no row to lock; verified-check below rejects
			}
			return err
		}
		return nil
	}
	_, err := r.q(ctx).ExecContext(ctx, `
		UPDATE drivers SET first_name = first_name
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, driverID)
	return err
}

func (r *SQLSettlementRepository) CreatePayoutInstruction(ctx context.Context, tenantID string, p *domain.PayoutInstruction) error {
	_, err := r.q(ctx).ExecContext(ctx, `
		INSERT INTO payout_instructions (
			id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
			provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		p.ID, tenantID, p.DriverID, p.PayoutAccountID, p.Amount, p.Currency, p.IdempotencyKey,
		p.ProviderPayoutID, string(p.Status), p.FailureReason, p.UTR, p.InitiatedAt, p.CompletedAt, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *SQLSettlementRepository) GetPayoutByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*domain.PayoutInstruction, error) {
	var p domain.PayoutInstruction
	var provID, failReason, utr sql.NullString
	var compAt sql.NullTime

	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE tenant_id = $1 AND idempotency_key = $2`,
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

	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE tenant_id = $1 AND id = $2`,
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

	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key,
		       provider_payout_id, status, failure_reason, utr, initiated_at, completed_at, created_at, updated_at
		FROM payout_instructions
		WHERE id = $1`,
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

	_, err := r.q(ctx).ExecContext(ctx, `
		UPDATE payout_instructions
		SET status = $1, provider_payout_id = COALESCE($2, provider_payout_id),
		    utr = COALESCE($3, utr), failure_reason = COALESCE($4, failure_reason),
		    completed_at = COALESCE($5, completed_at), updated_at = $6
		WHERE tenant_id = $7 AND id = $8`,
		string(status), providerPayoutID, utr, failureReason, completedAt, now, tenantID, payoutID)
	return err
}

func (r *SQLSettlementRepository) RecordProviderEvent(ctx context.Context, tenantID, provider, eventID, eventType, payload string) error {
	_, err := r.q(ctx).ExecContext(ctx, `
		INSERT INTO provider_events (id, tenant_id, provider, provider_event_id, event_type, payload, processed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		"pe_"+eventID, tenantID, provider, eventID, eventType, payload, time.Now())
	return err
}

func (r *SQLSettlementRepository) IsProviderEventProcessed(ctx context.Context, provider, eventID string) (bool, error) {
	var count int
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM provider_events
		WHERE provider = $1 AND provider_event_id = $2`,
		provider, eventID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *SQLSettlementRepository) IsDriverPayoutAccountVerified(ctx context.Context, tenantID, driverID string) (bool, string, error) {
	var id, status string
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, verification_status FROM driver_payout_accounts
		WHERE tenant_id = $1 AND driver_id = $2 AND is_primary = 1
		LIMIT 1`,
		tenantID, driverID).Scan(&id, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Fallback: check legacy drivers.bank_details if present
			var bank sql.NullString
			_ = r.q(ctx).QueryRowContext(ctx, `SELECT bank_details FROM drivers WHERE tenant_id = $1 AND id = $2`, tenantID, driverID).Scan(&bank)
			if bank.Valid && len(bank.String) > 5 {
				return true, "legacy_account", nil
			}
			return false, "", nil
		}
		return false, "", err
	}
	return status == "verified", id, nil
}

// HasLegacySettlementLines is the gap-2 dual-write cross-check: it SELECTs from
// settlement_lines, a table only the legacy trip-close flow
// (internal/service DriverSettlementService.GenerateSettlement) writes. The
// wallet/payout rail shares driver_settlements/driver_ledger_entries with that
// flow, so a true result means the trip is already accounted for. Callers must
// fail open on error (e.g. table absent on older DBs): warn and continue, never
// block settlement creation. settlement_lines carries no tenant column, so the
// check is trip-scoped. No migration edits; read-only query.
func (r *SQLSettlementRepository) HasLegacySettlementLines(ctx context.Context, tripID string) (bool, error) {
	if tripID == "" {
		return false, nil
	}
	var count int
	if err := r.q(ctx).QueryRowContext(ctx,
		`SELECT COUNT(*) FROM settlement_lines WHERE trip_id = $1`, tripID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
