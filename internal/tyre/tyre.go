package tyre

import (
	"errors"
	"math"
	"strings"
	"time"
)

type TyreType string

const (
	TyreTypeTubeless TyreType = "TUBELESS"
	TyreTypeTubeType TyreType = "TUBE_TYPE"
	TyreTypeRadial   TyreType = "RADIAL"
	TyreTypeNylon    TyreType = "NYLON"
)

func IsValidTyreType(t TyreType) bool {
	switch t {
	case TyreTypeTubeless, TyreTypeTubeType, TyreTypeRadial, TyreTypeNylon:
		return true
	default:
		return false
	}
}

type TyreStatus string

const (
	TyreStatusInStock      TyreStatus = "IN_STOCK"
	TyreStatusMounted      TyreStatus = "MOUNTED"
	TyreStatusUnderRetread TyreStatus = "UNDER_RETREAD"
	TyreStatusScrapped     TyreStatus = "SCRAPPED"
)

func IsValidTyreStatus(s TyreStatus) bool {
	switch s {
	case TyreStatusInStock, TyreStatusMounted, TyreStatusUnderRetread, TyreStatusScrapped:
		return true
	default:
		return false
	}
}

type ActivityType string

const (
	ActivityMount          ActivityType = "MOUNT"
	ActivityDismount       ActivityType = "DISMOUNT"
	ActivityRotation       ActivityType = "ROTATION"
	ActivityInspection     ActivityType = "INSPECTION"
	ActivityRetreadSend    ActivityType = "RETREAD_SEND"
	ActivityRetreadReceive ActivityType = "RETREAD_RECEIVE"
	ActivityScrap          ActivityType = "SCRAP"
)

type InspectionCondition string

const (
	ConditionGood         InspectionCondition = "GOOD"
	ConditionUnevenWear   InspectionCondition = "UNEVEN_WEAR"
	ConditionCutsOrCracks InspectionCondition = "CUTS_OR_CRACKS"
	ConditionBulge        InspectionCondition = "BULGE"
	ConditionPuncture     InspectionCondition = "PUNCTURE"
	ConditionWornOut      InspectionCondition = "WORN_OUT"
)

type Tyre struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	SerialNumber        string     `json:"serial_number"`
	Brand               string     `json:"brand"`
	Model               string     `json:"model"`
	Size                string     `json:"size"`
	TyreType            TyreType   `json:"tyre_type"`
	Status              TyreStatus `json:"status"`
	VehicleID           *string    `json:"vehicle_id,omitempty"`
	AxlePosition        *string    `json:"axle_position,omitempty"`
	MountOdometerKM     *float64   `json:"mount_odometer_km,omitempty"`
	MountDate           *time.Time `json:"mount_date,omitempty"`
	InitialTreadDepthMM float64    `json:"initial_tread_depth_mm"`
	CurrentTreadDepthMM float64    `json:"current_tread_depth_mm"`
	MinTreadDepthMM     float64    `json:"min_tread_depth_mm"`
	PurchaseDate        *time.Time `json:"purchase_date,omitempty"`
	PurchaseCost        float64    `json:"purchase_cost"`
	VendorName          string     `json:"vendor_name"`
	InvoiceNumber       string     `json:"invoice_number"`
	RetreadCount        int        `json:"retread_count"`
	MaxRetreads         int        `json:"max_retreads"`
	TotalRetreadCost    float64    `json:"total_retread_cost"`
	TotalKMRun          float64    `json:"total_km_run"`
	ScrapDate           *time.Time `json:"scrap_date,omitempty"`
	ScrapReason         string     `json:"scrap_reason"`
	ScrapRecoveryAmount float64    `json:"scrap_recovery_amount"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`

	// Derived / live metrics
	Metrics *TyreMetrics `json:"metrics,omitempty"`
}

type TyreMetrics struct {
	CurrentRunKM         float64 `json:"current_run_km"`
	TotalRunKM           float64 `json:"total_run_km"`
	CPKM                 float64 `json:"cpkm"`
	TreadLossMM          float64 `json:"tread_loss_mm"`
	KMPerMM              float64 `json:"km_per_mm"`
	UsableTreadLeftMM    float64 `json:"usable_tread_left_mm"`
	ProjectedRemainingKM float64 `json:"projected_remaining_km"`
	HealthStatus         string  `json:"health_status"`
}

// ComputeMetrics evaluates live CPKM, wear rate, and projected remaining life against vehicle odometer.
func (t *Tyre) ComputeMetrics(vehicleOdo *float64) TyreMetrics {
	var currentLegKM float64
	if t.Status == TyreStatusMounted && t.MountOdometerKM != nil && vehicleOdo != nil {
		if *vehicleOdo > *t.MountOdometerKM {
			currentLegKM = *vehicleOdo - *t.MountOdometerKM
		}
	}

	totalEffectiveKM := t.TotalKMRun + currentLegKM
	netCost := t.PurchaseCost + t.TotalRetreadCost - t.ScrapRecoveryAmount
	if netCost < 0 {
		netCost = 0
	}

	var cpkm float64
	if totalEffectiveKM > 0 {
		cpkm = math.Round((netCost/totalEffectiveKM)*1000) / 1000
	}

	treadLoss := t.InitialTreadDepthMM - t.CurrentTreadDepthMM
	if treadLoss < 0 {
		treadLoss = 0
	}

	var kmPerMM float64
	if treadLoss > 0 && totalEffectiveKM > 0 {
		kmPerMM = math.Round((totalEffectiveKM/treadLoss)*100) / 100
	}

	usableTread := t.CurrentTreadDepthMM - t.MinTreadDepthMM
	if usableTread < 0 {
		usableTread = 0
	}

	var projectedKM float64
	if kmPerMM > 0 {
		projectedKM = math.Round(usableTread * kmPerMM)
	}

	health := "GOOD"
	if t.Status == TyreStatusScrapped {
		health = "SCRAPPED"
	} else if t.CurrentTreadDepthMM <= t.MinTreadDepthMM {
		health = "CRITICAL_TREAD"
	} else if t.CurrentTreadDepthMM <= t.MinTreadDepthMM+1.5 {
		health = "LOW_TREAD"
	} else if t.RetreadCount >= t.MaxRetreads {
		health = "MAX_RETREADS_REACHED"
	}

	return TyreMetrics{
		CurrentRunKM:         math.Round(currentLegKM*100) / 100,
		TotalRunKM:           math.Round(totalEffectiveKM*100) / 100,
		CPKM:                 cpkm,
		TreadLossMM:          math.Round(treadLoss*10) / 10,
		KMPerMM:              kmPerMM,
		UsableTreadLeftMM:    math.Round(usableTread*10) / 10,
		ProjectedRemainingKM: projectedKM,
		HealthStatus:         health,
	}
}

type TyreActivity struct {
	ID           string       `json:"id"`
	TenantID     string       `json:"tenant_id"`
	TyreID       string       `json:"tyre_id"`
	ActivityType ActivityType `json:"activity_type"`
	VehicleID    *string      `json:"vehicle_id,omitempty"`
	FromPosition *string      `json:"from_position,omitempty"`
	ToPosition   *string      `json:"to_position,omitempty"`
	OdometerKM   float64      `json:"odometer_km"`
	TreadDepthMM *float64     `json:"tread_depth_mm,omitempty"`
	PressurePSI  *float64     `json:"pressure_psi,omitempty"`
	Cost         float64      `json:"cost"`
	Notes        string       `json:"notes"`
	PerformedBy  string       `json:"performed_by"`
	RecordedAt   time.Time    `json:"recorded_at"`
}

type TyreInspection struct {
	ID             string              `json:"id"`
	TenantID       string              `json:"tenant_id"`
	TyreID         string              `json:"tyre_id"`
	VehicleID      *string             `json:"vehicle_id,omitempty"`
	InspectionDate time.Time           `json:"inspection_date"`
	OdometerKM     float64             `json:"odometer_km"`
	TreadDepthMM   float64             `json:"tread_depth_mm"`
	PressurePSI    *float64            `json:"pressure_psi,omitempty"`
	Condition      InspectionCondition `json:"condition"`
	ActionTaken    string              `json:"action_taken"`
	InspectorName  string              `json:"inspector_name"`
	Notes          string              `json:"notes"`
	CreatedAt      time.Time           `json:"created_at"`
}

// Input DTOs

type CreateTyreInput struct {
	SerialNumber        string     `json:"serial_number"`
	Brand               string     `json:"brand"`
	Model               string     `json:"model"`
	Size                string     `json:"size"`
	TyreType            TyreType   `json:"tyre_type"`
	InitialTreadDepthMM *float64   `json:"initial_tread_depth_mm,omitempty"`
	MinTreadDepthMM     *float64   `json:"min_tread_depth_mm,omitempty"`
	PurchaseDate        *time.Time `json:"purchase_date,omitempty"`
	PurchaseCost        float64    `json:"purchase_cost"`
	VendorName          string     `json:"vendor_name"`
	InvoiceNumber       string     `json:"invoice_number"`
	MaxRetreads         *int       `json:"max_retreads,omitempty"`
}

func (in *CreateTyreInput) Validate() error {
	in.SerialNumber = strings.TrimSpace(in.SerialNumber)
	if in.SerialNumber == "" {
		return errors.New("serial_number is required")
	}
	in.Brand = strings.TrimSpace(in.Brand)
	if in.Brand == "" {
		return errors.New("brand is required")
	}
	in.Size = strings.TrimSpace(in.Size)
	if in.Size == "" {
		return errors.New("size is required")
	}
	if in.TyreType == "" {
		in.TyreType = TyreTypeTubeless
	}
	if !IsValidTyreType(in.TyreType) {
		return errors.New("invalid tyre_type (must be TUBELESS, TUBE_TYPE, RADIAL, or NYLON)")
	}
	if in.PurchaseCost < 0 {
		return errors.New("purchase_cost cannot be negative")
	}
	return nil
}

type UpdateTyreInput struct {
	Brand         *string    `json:"brand,omitempty"`
	Model         *string    `json:"model,omitempty"`
	Size          *string    `json:"size,omitempty"`
	VendorName    *string    `json:"vendor_name,omitempty"`
	InvoiceNumber *string    `json:"invoice_number,omitempty"`
	PurchaseCost  *float64   `json:"purchase_cost,omitempty"`
	PurchaseDate  *time.Time `json:"purchase_date,omitempty"`
}

type MountTyreInput struct {
	VehicleID       string     `json:"vehicle_id"`
	AxlePosition    string     `json:"axle_position"`
	MountOdometerKM float64    `json:"mount_odometer_km"`
	MountDate       *time.Time `json:"mount_date,omitempty"`
	PerformedBy     string     `json:"performed_by"`
	Notes           string     `json:"notes"`
}

func (in *MountTyreInput) Validate() error {
	in.VehicleID = strings.TrimSpace(in.VehicleID)
	if in.VehicleID == "" {
		return errors.New("vehicle_id is required")
	}
	in.AxlePosition = strings.ToUpper(strings.TrimSpace(in.AxlePosition))
	if in.AxlePosition == "" {
		return errors.New("axle_position is required")
	}
	if in.MountOdometerKM < 0 {
		return errors.New("mount_odometer_km cannot be negative")
	}
	return nil
}

type DismountTyreInput struct {
	DismountOdometerKM float64    `json:"dismount_odometer_km"`
	DismountDate       *time.Time `json:"dismount_date,omitempty"`
	Reason             string     `json:"reason"`
	PerformedBy        string     `json:"performed_by"`
	Notes              string     `json:"notes"`
}

type TyreRotationItem struct {
	TyreID       string `json:"tyre_id"`
	FromPosition string `json:"from_position"`
	ToPosition   string `json:"to_position"`
}

type RotateTyresInput struct {
	VehicleID   string             `json:"vehicle_id"`
	Rotations   []TyreRotationItem `json:"rotations"`
	OdometerKM  float64            `json:"odometer_km"`
	PerformedBy string             `json:"performed_by"`
	Notes       string             `json:"notes"`
}

type RecordInspectionInput struct {
	OdometerKM     float64             `json:"odometer_km"`
	TreadDepthMM   float64             `json:"tread_depth_mm"`
	PressurePSI    *float64            `json:"pressure_psi,omitempty"`
	Condition      InspectionCondition `json:"condition"`
	ActionTaken    string              `json:"action_taken"`
	InspectorName  string              `json:"inspector_name"`
	Notes          string              `json:"notes"`
	InspectionDate *time.Time          `json:"inspection_date,omitempty"`
}

func (in *RecordInspectionInput) Validate() error {
	if in.TreadDepthMM < 0 {
		return errors.New("tread_depth_mm cannot be negative")
	}
	if in.Condition == "" {
		in.Condition = ConditionGood
	}
	return nil
}

type RetreadTyreInput struct {
	Action          string   `json:"action"` // "SEND" or "RECEIVE"
	VendorName      string   `json:"vendor_name"`
	Cost            float64  `json:"cost"`
	NewTreadDepthMM *float64 `json:"new_tread_depth_mm,omitempty"`
	PerformedBy     string   `json:"performed_by"`
	Notes           string   `json:"notes"`
}

type ScrapTyreInput struct {
	Reason         string  `json:"reason"`
	RecoveryAmount float64 `json:"recovery_amount"`
	PerformedBy    string  `json:"performed_by"`
	Notes          string  `json:"notes"`
}

type TyreFilter struct {
	TenantID  string
	VehicleID string
	Status    string
	Brand     string
	Search    string
	Limit     int
	Offset    int
}

type FleetTyreSummary struct {
	TotalTyres          int     `json:"total_tyres"`
	MountedCount        int     `json:"mounted_count"`
	InStockCount        int     `json:"in_stock_count"`
	UnderRetreadCount   int     `json:"under_retread_count"`
	ScrappedCount       int     `json:"scrapped_count"`
	LowTreadCount       int     `json:"low_tread_count"`
	CriticalTreadCount  int     `json:"critical_tread_count"`
	FleetAverageCPKM    float64 `json:"fleet_average_cpkm"`
	TotalInvestmentCost float64 `json:"total_investment_cost"`
}
