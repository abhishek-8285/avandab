package domain

import "time"

type EntryType string

const (
	EntryTripEarning      EntryType = "TRIP_EARNING"
	EntryCommission       EntryType = "COMMISSION"
	EntryTollAdjustment   EntryType = "TOLL_ADJUSTMENT"
	EntryPenalty          EntryType = "PENALTY"
	EntryPayout           EntryType = "PAYOUT"
	EntryPayoutReversal   EntryType = "PAYOUT_REVERSAL"
	EntryAdvanceDeduction EntryType = "ADVANCE_DEDUCTION"
	EntryBonus            EntryType = "BONUS"
)

// LedgerEntry represents an immutable line item in the driver's money ledger.
type LedgerEntry struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	DriverID      string    `json:"driver_id"`
	TripID        *string   `json:"trip_id,omitempty"`
	EntryType     EntryType `json:"entry_type"`
	Amount        float64   `json:"amount"` // + for credits, - for debits
	Currency      string    `json:"currency"`
	ReferenceType string    `json:"reference_type"` // settlement, payout, advance, adjustment
	ReferenceID   string    `json:"reference_id"`
	BalanceAfter  float64   `json:"balance_after"`
	Description   string    `json:"description,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// DriverWallet aggregates structured balances from the authoritative ledger.
type DriverWallet struct {
	DriverID         string        `json:"driver_id"`
	AvailableBalance float64       `json:"available_balance"` // Funds immediately payable
	PendingBalance   float64       `json:"pending_balance"`   // Settlements calculated but not yet payable
	PaidBalance      float64       `json:"paid_balance"`      // Historical disbursed funds
	HeldBalance      float64       `json:"held_balance"`      // Funds currently in processing payout
	Currency         string        `json:"currency"`
	RecentEntries    []LedgerEntry `json:"recent_entries"`
	UpdatedAt        time.Time     `json:"updated_at"`
}
