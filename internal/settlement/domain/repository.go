package domain

import (
	"context"
	"time"
)

type SettlementRepository interface {
	// WithTransaction runs fn with all repository calls in one transaction.
	// Re-entrant: nested calls run in the ambient transaction.
	WithTransaction(ctx context.Context, fn func(context.Context) error) error

	// Settlements
	CreateSettlement(ctx context.Context, tenantID string, s *Settlement) error
	GetSettlementByTripID(ctx context.Context, tenantID, tripID string) (*Settlement, error)
	ApproveSettlement(ctx context.Context, tenantID, settlementID string) error

	// Ledger
	AppendLedgerEntry(ctx context.Context, tenantID string, entry *LedgerEntry) error
	// ListLedgerEntryTypes returns the entry types already stored for one
	// money movement (reference_type + reference_id). Crash-partial recovery
	// backfills only the missing types — never blind re-appends.
	ListLedgerEntryTypes(ctx context.Context, tenantID, referenceType, referenceID string) ([]EntryType, error)
	HasCompensatingLedgerEntry(ctx context.Context, tenantID, referenceType, referenceID string) (bool, error)
	GetDriverBalance(ctx context.Context, tenantID, driverID string) (float64, error)
	GetDriverWallet(ctx context.Context, tenantID, driverID string) (*DriverWallet, error)
	GetRecentLedgerEntries(ctx context.Context, tenantID, driverID string, limit int) ([]LedgerEntry, error)

	// Payouts
	// LockDriverPayout serializes concurrent payouts for one driver. Call it
	// first inside the payout transaction, before the balance read: Postgres
	// takes a row lock (SELECT ... FOR UPDATE), SQLite performs a no-op row
	// touch that upgrades the transaction to a write lock (no FOR UPDATE).
	LockDriverPayout(ctx context.Context, tenantID, driverID string) error
	CreatePayoutInstruction(ctx context.Context, tenantID string, p *PayoutInstruction) error
	GetPayoutByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*PayoutInstruction, error)
	GetPayoutByID(ctx context.Context, tenantID, payoutID string) (*PayoutInstruction, error)
	GetPayoutByIDGlobal(ctx context.Context, payoutID string) (*PayoutInstruction, error)
	UpdatePayoutStatus(ctx context.Context, tenantID, payoutID string, status PayoutStatus, providerPayoutID, utr, failureReason *string) error
	// ListPayoutsByDriver returns the latest payout instructions for one driver.
	ListPayoutsByDriver(ctx context.Context, tenantID, driverID string, limit int) ([]PayoutInstruction, error)
	// ListPayoutsAwaitingCompletion returns non-terminal payouts older than a cutoff
	// (stuck-payout sweep: provider webhook may never arrive).
	ListPayoutsAwaitingCompletion(ctx context.Context, olderThan time.Time, limit int) ([]PayoutInstruction, error)
	// GetPrimaryPayoutAccount returns the driver primary account, nil when absent.
	GetPrimaryPayoutAccount(ctx context.Context, tenantID, driverID string) (*PayoutAccount, error)

	// Webhook / Provider Events
	RecordProviderEvent(ctx context.Context, tenantID, provider, eventID, eventType, payload string) error
	IsProviderEventProcessed(ctx context.Context, provider, eventID string) (bool, error)

	// Bank Account Verification lookup
	IsDriverPayoutAccountVerified(ctx context.Context, tenantID, driverID string) (bool, string, error)
	// GetPayoutAccount returns one driver payout account (number encrypted).
	GetPayoutAccount(ctx context.Context, tenantID, accountID string) (*PayoutAccount, error)
	// GetPayoutAccountByVerificationReference finds the account by the provider
	// fund_account id stored at penny-drop initiation (webhook reconciliation).
	GetPayoutAccountByVerificationReference(ctx context.Context, reference string) (*PayoutAccount, error)
	// UpdatePayoutAccountVerification stores the penny-drop outcome.
	UpdatePayoutAccountVerification(ctx context.Context, tenantID, accountID, status, reference, reason string) error

	// Dual-write guard (gap 2): reports whether the legacy trip-close accounting
	// flow (internal/service DriverSettlementService.GenerateSettlement) already
	// produced a per-trip breakdown in settlement_lines — a table the
	// wallet/payout rail never writes. Both flows share driver_settlements and
	// driver_ledger_entries, so callers must treat a true result as "legacy owns
	// this trip": log a warning and reuse the single row, never append duplicate
	// ledger entries. Fail open on error (missing table on older DBs).
	HasLegacySettlementLines(ctx context.Context, tripID string) (bool, error)
}
