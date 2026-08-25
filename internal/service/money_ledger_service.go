package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// Money ledger (migration 00097): an append-only, double-entry-lite audit
// trail of every money movement. APPEND-ONLY BY DESIGN: this service
// intentionally exposes NO Update or Delete methods — rows are immutable
// once written; corrections are new 'adjustment' entries, never edits.
// Amounts are integer minor units (paise); the sign lives in Direction,
// never in AmountMinor.
type MoneyLedgerService struct {
	store Store
	log   *slog.Logger
}

// Valid transaction types and directions. Kept in lockstep with the CHECK
// constraints on money_ledger (db/migrations/00097_money_ledger.sql).
var (
	validLedgerTxnTypes = map[string]bool{
		"invoice_generated":   true,
		"payment_recorded":    true,
		"settlement_recorded": true,
		"kharcha_approved":    true,
		"adjustment":          true,
	}
	validLedgerDirections = map[string]bool{
		"debit":  true,
		"credit": true,
	}
)

const defaultLedgerCurrency = "INR"

// LedgerEntry is one immutable money-movement record.
type LedgerEntry struct {
	TxnType     string // whitelist: invoice_generated|payment_recorded|settlement_recorded|kharcha_approved|adjustment
	RefTable    string // source table, e.g. "payments", "invoices", "driver_settlements"
	RefID       string // row id in RefTable
	Direction   string // debit | credit
	AmountMinor int64  // paise, >= 0
	Currency    string // defaults to INR when empty
	Memo        string
	CreatedBy   string // acting user id, empty for system writes
}

// NewMoneyLedgerService builds the ledger service over the shared Store.
func NewMoneyLedgerService(store Store, log *slog.Logger) *MoneyLedgerService {
	if log == nil {
		log = slog.Default()
	}
	return &MoneyLedgerService{store: store, log: log}
}

// AppendEntry writes one immutable ledger row for the tenant in ctx.
// It fails closed without a tenant and validates txn_type/direction/amount
// against whitelists before touching the database.
func (s *MoneyLedgerService) AppendEntry(ctx context.Context, e LedgerEntry) error {
	tenant := shared.TenantIDFromContext(ctx)
	if tenant == "" {
		return fmt.Errorf("money ledger: tenant not set in context")
	}
	if !validLedgerTxnTypes[e.TxnType] {
		return fmt.Errorf("money ledger: invalid txn_type %q", e.TxnType)
	}
	if !validLedgerDirections[e.Direction] {
		return fmt.Errorf("money ledger: invalid direction %q", e.Direction)
	}
	if e.AmountMinor < 0 {
		return fmt.Errorf("money ledger: negative amount_minor %d not allowed; use direction", e.AmountMinor)
	}
	if e.RefTable == "" || e.RefID == "" {
		return fmt.Errorf("money ledger: ref_table and ref_id are required")
	}

	currency := e.Currency
	if currency == "" {
		currency = defaultLedgerCurrency
	}

	db, err := s.db()
	if err != nil {
		return err
	}

	id := generateID()
	_, err = db.ExecContext(ctx,
		`INSERT INTO money_ledger
			(id, tenant_id, txn_type, ref_table, ref_id, direction, amount_minor, currency, memo, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, string(tenant), e.TxnType, e.RefTable, e.RefID,
		e.Direction, e.AmountMinor, currency, e.Memo, e.CreatedBy)
	if err != nil {
		return fmt.Errorf("money ledger: insert: %w", err)
	}

	s.log.Info("ledger entry appended",
		"id", id, "tenant_id", tenant, "txn_type", e.TxnType,
		"ref_table", e.RefTable, "ref_id", e.RefID, "direction", e.Direction,
		"amount_minor", e.AmountMinor, "currency", currency)
	return nil
}

// db resolves the raw *sql.DB via the optional DBGetter capability so the
// ledger needs no Store-interface change (other agents own that interface).
func (s *MoneyLedgerService) db() (*sql.DB, error) {
	g, ok := s.store.(repository.DBGetter)
	if !ok || g == nil || g.DB() == nil {
		return nil, errors.New("money ledger: store exposes no *sql.DB")
	}
	return g.DB(), nil
}

// ToMinor converts a rupee float to paise with decimal round-half-up.
// It goes through the shortest round-trip decimal representation instead of
// v*100 so binary float error cannot flip a half boundary:
// 2.345*100 is 234.49999...97 in float64, but ToMinor(2.345) == 235.
// Negative inputs mirror to half-away-from-zero (callers reject negatives).
func ToMinor(v float64) int64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	sign := int64(1)
	if v < 0 {
		sign = -1
	}
	s := strconv.FormatFloat(math.Abs(v), 'f', -1, 64)
	intPart, fracPart, _ := strings.Cut(s, ".")

	major, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0
	}
	minor := major * 100
	if len(fracPart) > 0 {
		minor += int64(fracPart[0]-'0') * 10
	}
	if len(fracPart) > 1 {
		minor += int64(fracPart[1] - '0')
	}
	if len(fracPart) > 2 && fracPart[2] >= '5' {
		minor++
	}
	return sign * minor
}
