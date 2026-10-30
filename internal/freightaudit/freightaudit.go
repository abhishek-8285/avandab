package freightaudit

import (
	"errors"
	"strings"
	"time"
)

// BillingBasis defines the tariff calculation method
type BillingBasis string

const (
	BillingBasisPerKM    BillingBasis = "PER_KM"
	BillingBasisPerKG    BillingBasis = "PER_KG"
	BillingBasisPerTon   BillingBasis = "PER_TON"
	BillingBasisPerTonKM BillingBasis = "PER_TON_KM"
	BillingBasisFlat     BillingBasis = "FLAT"
)

func (b BillingBasis) IsValid() bool {
	switch b {
	case BillingBasisPerKM, BillingBasisPerKG, BillingBasisPerTon, BillingBasisPerTonKM, BillingBasisFlat:
		return true
	default:
		return false
	}
}

// AuditStatus represents the audit lifecycle state
type AuditStatus string

const (
	AuditStatusPending            AuditStatus = "PENDING"
	AuditStatusVerified           AuditStatus = "VERIFIED"
	AuditStatusDiscrepancyFlagged AuditStatus = "DISCREPANCY_FLAGGED"
	AuditStatusDeductionApproved  AuditStatus = "DEDUCTION_APPROVED"
	AuditStatusDisputed           AuditStatus = "DISPUTED"
	AuditStatusSettled            AuditStatus = "SETTLED"
)

func (s AuditStatus) IsValid() bool {
	switch s {
	case AuditStatusPending, AuditStatusVerified, AuditStatusDiscrepancyFlagged,
		AuditStatusDeductionApproved, AuditStatusDisputed, AuditStatusSettled:
		return true
	default:
		return false
	}
}

// DeductionCategory categorises individual deduction items
type DeductionCategory string

const (
	DeductionCategoryDistanceInflation DeductionCategory = "DISTANCE_INFLATION"
	DeductionCategoryWeightMismatch    DeductionCategory = "WEIGHT_MISMATCH"
	DeductionCategoryRateOvercharge    DeductionCategory = "RATE_OVERCHARGE"
	DeductionCategoryUnverifiedToll    DeductionCategory = "UNVERIFIED_TOLL"
	DeductionCategoryExcessDetention   DeductionCategory = "EXCESS_DETENTION"
	DeductionCategoryDamagePenalty     DeductionCategory = "DAMAGE_PENALTY"
	DeductionCategoryOther             DeductionCategory = "OTHER"
)

// Domain Errors
var (
	ErrNotFound             = errors.New("freight audit resource not found")
	ErrInvalidInput         = errors.New("invalid input data")
	ErrInvalidStatus        = errors.New("action not allowed for current audit status")
	ErrContractNumberExists = errors.New("contract number already exists")
	ErrAuditNumberExists    = errors.New("audit number already exists")
)

// RateCard represents a freight contract tariff
type RateCard struct {
	ID                   string       `json:"id"`
	TenantID             string       `json:"tenant_id"`
	ContractNumber       string       `json:"contract_number"`
	Name                 string       `json:"name"`
	VendorID             *string      `json:"vendor_id,omitempty"`
	CustomerID           *string      `json:"customer_id,omitempty"`
	RouteID              *string      `json:"route_id,omitempty"`
	VehicleType          string       `json:"vehicle_type"`
	BillingBasis         BillingBasis `json:"billing_basis"`
	Rate                 float64      `json:"rate"`
	MinCharge            float64      `json:"min_charge"`
	FreeDetentionHours   float64      `json:"free_detention_hours"`
	DetentionRatePerHour float64      `json:"detention_rate_per_hour"`
	EffectiveFrom        time.Time    `json:"effective_from"`
	EffectiveTo          *time.Time   `json:"effective_to,omitempty"`
	IsActive             bool         `json:"is_active"`
	CreatedAt            time.Time    `json:"created_at"`
	UpdatedAt            time.Time    `json:"updated_at"`
}

// FreightAudit is the core freight audit record
type FreightAudit struct {
	ID            string       `json:"id"`
	TenantID      string       `json:"tenant_id"`
	AuditNumber   string       `json:"audit_number"`
	TripID        *string      `json:"trip_id,omitempty"`
	InvoiceID     *string      `json:"invoice_id,omitempty"`
	BookingID     *string      `json:"booking_id,omitempty"`
	RateCardID    *string      `json:"rate_card_id,omitempty"`
	VendorName    string       `json:"vendor_name"`
	VehicleNumber string       `json:"vehicle_number"`
	Status        AuditStatus  `json:"status"`
	BillingBasis  BillingBasis `json:"billing_basis"`

	// Billed amounts
	BilledDistanceKM       float64 `json:"billed_distance_km"`
	BilledWeightKG         float64 `json:"billed_weight_kg"`
	BilledRate             float64 `json:"billed_rate"`
	BilledBaseFreight      float64 `json:"billed_base_freight"`
	BilledTollCharges      float64 `json:"billed_toll_charges"`
	BilledDetentionCharges float64 `json:"billed_detention_charges"`
	BilledTotal            float64 `json:"billed_total"`

	// Audited ground truth
	GPSDistanceKM           float64 `json:"gps_distance_km"`
	ActualWeightKG          float64 `json:"actual_weight_kg"`
	AuditedRate             float64 `json:"audited_rate"`
	AuditedBaseFreight      float64 `json:"audited_base_freight"`
	AuditedTollCharges      float64 `json:"audited_toll_charges"`
	AuditedDetentionHours   float64 `json:"audited_detention_hours"`
	AuditedDetentionCharges float64 `json:"audited_detention_charges"`
	AuditedTotal            float64 `json:"audited_total"`

	// Variances
	DistanceVarianceKM float64 `json:"distance_variance_km"`
	WeightVarianceKG   float64 `json:"weight_variance_kg"`
	RateVariance       float64 `json:"rate_variance"`
	TollVariance       float64 `json:"toll_variance"`
	OverbillingAmount  float64 `json:"overbilling_amount"`
	VariancePct        float64 `json:"variance_pct"`

	// Deductions
	DeductionAmount  float64 `json:"deduction_amount"`
	PayableAmount    float64 `json:"payable_amount"`
	DeductionReason  string  `json:"deduction_reason"`
	DebitNoteNumber  string  `json:"debit_note_number"`
	DiscrepancyFlags string  `json:"discrepancy_flags"`
	DisputeReason    string  `json:"dispute_reason"`

	// Workflow
	ReviewedBy *string    `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	ApprovedBy *string    `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`

	// Sub-resources
	Deductions []FreightDeduction `json:"deductions,omitempty"`
}

// FreightDeduction is a single deduction line item
type FreightDeduction struct {
	ID              string            `json:"id"`
	TenantID        string            `json:"tenant_id"`
	AuditID         string            `json:"audit_id"`
	Category        DeductionCategory `json:"category"`
	Description     string            `json:"description"`
	ClaimedValue    float64           `json:"claimed_value"`
	VerifiedValue   float64           `json:"verified_value"`
	UnitRate        float64           `json:"unit_rate"`
	DeductionAmount float64           `json:"deduction_amount"`
	CreatedAt       time.Time         `json:"created_at"`
}

// AuditSummary contains rollup KPIs
type AuditSummary struct {
	TotalAudits            int     `json:"total_audits"`
	TotalBilledAmount      float64 `json:"total_billed_amount"`
	TotalAuditedAmount     float64 `json:"total_audited_amount"`
	TotalOverbillingAmount float64 `json:"total_overbilling_amount"`
	TotalDeductionsAmount  float64 `json:"total_deductions_amount"`
	DiscrepancyCount       int     `json:"discrepancy_count"`
	SettledCount           int     `json:"settled_count"`
}

// Request and filter DTOs

type CreateRateCardInput struct {
	ContractNumber       string       `json:"contract_number"`
	Name                 string       `json:"name"`
	VendorID             *string      `json:"vendor_id,omitempty"`
	CustomerID           *string      `json:"customer_id,omitempty"`
	RouteID              *string      `json:"route_id,omitempty"`
	VehicleType          string       `json:"vehicle_type"`
	BillingBasis         BillingBasis `json:"billing_basis"`
	Rate                 float64      `json:"rate"`
	MinCharge            float64      `json:"min_charge"`
	FreeDetentionHours   float64      `json:"free_detention_hours"`
	DetentionRatePerHour float64      `json:"detention_rate_per_hour"`
	EffectiveFrom        *time.Time   `json:"effective_from,omitempty"`
	EffectiveTo          *time.Time   `json:"effective_to,omitempty"`
}

func (in *CreateRateCardInput) Validate() error {
	if strings.TrimSpace(in.ContractNumber) == "" {
		return errors.New("contract_number is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("rate card name is required")
	}
	if !in.BillingBasis.IsValid() {
		return errors.New("invalid billing_basis")
	}
	if in.Rate < 0.0 {
		return errors.New("rate must be non-negative")
	}
	if in.MinCharge < 0.0 {
		return errors.New("min_charge must be non-negative")
	}
	return nil
}

type UpdateRateCardInput struct {
	Name                 *string    `json:"name,omitempty"`
	Rate                 *float64   `json:"rate,omitempty"`
	MinCharge            *float64   `json:"min_charge,omitempty"`
	FreeDetentionHours   *float64   `json:"free_detention_hours,omitempty"`
	DetentionRatePerHour *float64   `json:"detention_rate_per_hour,omitempty"`
	EffectiveTo          *time.Time `json:"effective_to,omitempty"`
	IsActive             *bool      `json:"is_active,omitempty"`
}

type CreateAuditInput struct {
	TripID        *string       `json:"trip_id,omitempty"`
	InvoiceID     *string       `json:"invoice_id,omitempty"`
	BookingID     *string       `json:"booking_id,omitempty"`
	RateCardID    *string       `json:"rate_card_id,omitempty"`
	VendorName    string        `json:"vendor_name"`
	VehicleNumber string        `json:"vehicle_number"`
	BillingBasis  *BillingBasis `json:"billing_basis,omitempty"`

	// Claimed/billed metrics from invoice
	BilledDistanceKM       float64 `json:"billed_distance_km"`
	BilledWeightKG         float64 `json:"billed_weight_kg"`
	BilledRate             float64 `json:"billed_rate"`
	BilledBaseFreight      float64 `json:"billed_base_freight"`
	BilledTollCharges      float64 `json:"billed_toll_charges"`
	BilledDetentionCharges float64 `json:"billed_detention_charges"`
	BilledTotal            float64 `json:"billed_total"`

	// Optional ground-truth overrides
	GPSDistanceKM         *float64 `json:"gps_distance_km,omitempty"`
	ActualWeightKG        *float64 `json:"actual_weight_kg,omitempty"`
	AuditedRate           *float64 `json:"audited_rate,omitempty"`
	AuditedTollCharges    *float64 `json:"audited_toll_charges,omitempty"`
	AuditedDetentionHours *float64 `json:"audited_detention_hours,omitempty"`
	Notes                 string   `json:"notes"`
}

func (in *CreateAuditInput) Validate() error {
	if in.BilledTotal < 0.0 {
		return errors.New("billed_total must be non-negative")
	}
	return nil
}

type ApproveDeductionInput struct {
	DeductionAmount *float64 `json:"deduction_amount,omitempty"`
	Notes           string   `json:"notes"`
}

type DisputeAuditInput struct {
	Reason string `json:"reason"`
}

type SettleAuditInput struct {
	Notes string `json:"notes"`
}

type RateCardFilter struct {
	RouteID     string
	VehicleType string
	ActiveOnly  bool
	Search      string
	Limit       int
	Offset      int
}

type AuditFilter struct {
	Status          string
	TripID          string
	InvoiceID       string
	DiscrepancyOnly bool
	Search          string
	Limit           int
	Offset          int
}
