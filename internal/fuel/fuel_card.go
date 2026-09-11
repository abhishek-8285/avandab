package fuel

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// FuelCardProvider specifies the recognized OMC or bank fleet card network.
type FuelCardProvider string

const (
	ProviderIOCL      FuelCardProvider = "IOCL"
	ProviderBPCL      FuelCardProvider = "BPCL"
	ProviderHPCL      FuelCardProvider = "HPCL"
	ProviderShell     FuelCardProvider = "SHELL"
	ProviderFleetBank FuelCardProvider = "FLEET_BANK"
)

// FuelCardStatus represents the operational card status.
type FuelCardStatus string

const (
	CardStatusActive    FuelCardStatus = "ACTIVE"
	CardStatusSuspended FuelCardStatus = "SUSPENDED"
	CardStatusCancelled FuelCardStatus = "CANCELLED"
)

// ReconciliationStatus indicates the reconciliation state of a fuel transaction.
type ReconciliationStatus string

const (
	ReconStatusUnreconciled    ReconciliationStatus = "UNRECONCILED"
	ReconStatusMatchedExpense  ReconciliationStatus = "MATCHED_EXPENSE"
	ReconStatusSystemGenerated ReconciliationStatus = "SYSTEM_GENERATED"
	ReconStatusFlaggedAnomaly  ReconciliationStatus = "FLAGGED_ANOMALY"
)

// FuelCard represents a commercial fleet card registered with an OMC.
type FuelCard struct {
	ID                string           `json:"id"`
	TenantID          string           `json:"tenant_id"`
	CardNumberMasked  string           `json:"card_number_masked"`
	CardTokenHash     string           `json:"card_token_hash"`
	Provider          FuelCardProvider `json:"provider"`
	AssignedVehicleID *string          `json:"assigned_vehicle_id,omitempty"`
	AssignedDriverID  *string          `json:"assigned_driver_id,omitempty"`
	DailySpendLimit   float64          `json:"daily_spend_limit"`
	Status            FuelCardStatus   `json:"status"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`

	// Metrics
	SpendToday        float64 `json:"spend_today,omitempty"`
	TotalTransactions int     `json:"total_transactions,omitempty"`
}

// RegisterFuelCardRequest captures payload for onboarding a fuel card.
type RegisterFuelCardRequest struct {
	CardNumber        string  `json:"card_number"`
	Provider          string  `json:"provider"`
	AssignedVehicleID *string `json:"assigned_vehicle_id"`
	AssignedDriverID  *string `json:"assigned_driver_id"`
	DailySpendLimit   float64 `json:"daily_spend_limit"`
}

// FuelCardTransaction represents an OMC settlement record.
type FuelCardTransaction struct {
	ID                   string               `json:"id"`
	TenantID             string               `json:"tenant_id"`
	FuelCardID           string               `json:"fuel_card_id"`
	ExternalTxnID        string               `json:"external_txn_id"`
	TxnTime              time.Time            `json:"txn_time"`
	FuelStationName      string               `json:"fuel_station_name"`
	FuelStationCity      *string              `json:"fuel_station_city,omitempty"`
	FuelType             string               `json:"fuel_type"`
	VolumeLitres         float64              `json:"volume_litres"`
	RatePerLitre         float64              `json:"rate_per_litre"`
	TotalAmount          float64              `json:"total_amount"`
	OdometerReported     *float64             `json:"odometer_reported,omitempty"`
	ReconciliationStatus ReconciliationStatus `json:"reconciliation_status"`
	MatchedExpenseID     *string              `json:"matched_expense_id,omitempty"`
	SyncLogID            *string              `json:"sync_log_id,omitempty"`
	Notes                *string              `json:"notes,omitempty"`
	CreatedAt            time.Time            `json:"created_at"`
}

// IngestTransactionItem represents a single transaction line in statement feeds.
type IngestTransactionItem struct {
	CardNumber       string    `json:"card_number,omitempty"`
	CardTokenHash    string    `json:"card_token_hash,omitempty"`
	ExternalTxnID    string    `json:"external_txn_id"`
	TxnTime          time.Time `json:"txn_time"`
	FuelStationName  string    `json:"fuel_station_name"`
	FuelStationCity  *string   `json:"fuel_station_city"`
	FuelType         string    `json:"fuel_type"`
	VolumeLitres     float64   `json:"volume_litres"`
	RatePerLitre     float64   `json:"rate_per_litre"`
	TotalAmount      float64   `json:"total_amount"`
	OdometerReported *float64  `json:"odometer_reported"`
	Notes            *string   `json:"notes"`
}

// SyncFuelTransactionsRequest captures statement batch sync payload.
type SyncFuelTransactionsRequest struct {
	Transactions []IngestTransactionItem `json:"transactions"`
}

// ReconcileTransactionRequest captures manual override payload.
type ReconcileTransactionRequest struct {
	ExpenseID string `json:"expense_id"`
	Notes     string `json:"notes"`
}

// MaskCardNumber formats card number to standard PCI-compliant masked pattern.
// Example: "1234567890123456" -> "**** **** **** 3456".
func MaskCardNumber(raw string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)

	if len(clean) < 4 {
		return "****"
	}
	last4 := clean[len(clean)-4:]
	return "**** **** **** " + last4
}

// HashCardToken generates a cryptographic SHA-256 token hash of the card number.
func HashCardToken(raw string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	h := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(h[:])
}
