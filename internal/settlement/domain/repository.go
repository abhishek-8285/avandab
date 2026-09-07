package domain

import (
	"context"
)

type SettlementRepository interface {
	// Settlements
	CreateSettlement(ctx context.Context, tenantID string, s *Settlement) error
	GetSettlementByTripID(ctx context.Context, tenantID, tripID string) (*Settlement, error)
	ApproveSettlement(ctx context.Context, tenantID, settlementID string) error

	// Ledger
	AppendLedgerEntry(ctx context.Context, tenantID string, entry *LedgerEntry) error
	HasCompensatingLedgerEntry(ctx context.Context, tenantID, referenceType, referenceID string) (bool, error)
	GetDriverBalance(ctx context.Context, tenantID, driverID string) (float64, error)
	GetDriverWallet(ctx context.Context, tenantID, driverID string) (*DriverWallet, error)
	GetRecentLedgerEntries(ctx context.Context, tenantID, driverID string, limit int) ([]LedgerEntry, error)

	// Payouts
	CreatePayoutInstruction(ctx context.Context, tenantID string, p *PayoutInstruction) error
	GetPayoutByIdempotencyKey(ctx context.Context, tenantID, idempotencyKey string) (*PayoutInstruction, error)
	GetPayoutByID(ctx context.Context, tenantID, payoutID string) (*PayoutInstruction, error)
	GetPayoutByIDGlobal(ctx context.Context, payoutID string) (*PayoutInstruction, error)
	UpdatePayoutStatus(ctx context.Context, tenantID, payoutID string, status PayoutStatus, providerPayoutID, utr, failureReason *string) error

	// Webhook / Provider Events
	RecordProviderEvent(ctx context.Context, tenantID, provider, eventID, eventType, payload string) error
	IsProviderEventProcessed(ctx context.Context, provider, eventID string) (bool, error)

	// Bank Account Verification lookup
	IsDriverPayoutAccountVerified(ctx context.Context, tenantID, driverID string) (bool, string, error)

	// Dual-write guard (gap 2): reports whether the legacy trip-close accounting
	// flow (internal/service DriverSettlementService.GenerateSettlement) already
	// produced a per-trip breakdown in settlement_lines — a table the
	// wallet/payout rail never writes. Both flows share driver_settlements and
	// driver_ledger_entries, so callers must treat a true result as "legacy owns
	// this trip": log a warning and reuse the single row, never append duplicate
	// ledger entries. Fail open on error (missing table on older DBs).
	HasLegacySettlementLines(ctx context.Context, tripID string) (bool, error)
}
