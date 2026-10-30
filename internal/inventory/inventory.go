package inventory

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrPartNotFound         = errors.New("part not found")
	ErrDuplicatePartNumber  = errors.New("part with this part number already exists")
	ErrDuplicateBillNumber  = errors.New("purchase bill with this bill number already exists")
	ErrInsufficientStock    = errors.New("insufficient stock on hand")
	ErrBillNotFound         = errors.New("purchase bill not found")
	ErrBillAlreadyProcessed = errors.New("purchase bill has already been processed or cancelled")
	ErrFacilityNotFound     = errors.New("facility not found")
)

type PartCategory string

const (
	CategoryEngine           PartCategory = "ENGINE"
	CategoryBrakes           PartCategory = "BRAKES"
	CategoryElectrical       PartCategory = "ELECTRICAL"
	CategorySuspension       PartCategory = "SUSPENSION"
	CategoryTransmission     PartCategory = "TRANSMISSION"
	CategoryFilters          PartCategory = "FILTERS"
	CategoryFluids           PartCategory = "FLUIDS"
	CategoryTyresAccessories PartCategory = "TYRES_ACCESSORIES"
	CategoryFasteners        PartCategory = "FASTENERS"
	CategoryBody             PartCategory = "BODY"
	CategoryGeneral          PartCategory = "GENERAL"
)

func IsValidCategory(c PartCategory) bool {
	switch c {
	case CategoryEngine, CategoryBrakes, CategoryElectrical, CategorySuspension,
		CategoryTransmission, CategoryFilters, CategoryFluids, CategoryTyresAccessories,
		CategoryFasteners, CategoryBody, CategoryGeneral:
		return true
	default:
		return false
	}
}

type PartUOM string

const (
	UOMPieces PartUOM = "PIECES"
	UOMLitres PartUOM = "LITRES"
	UOMMetres PartUOM = "METRES"
	UOMSets   PartUOM = "SETS"
	UOMKg     PartUOM = "KG"
	UOMBoxes  PartUOM = "BOXES"
)

func IsValidUOM(u PartUOM) bool {
	switch u {
	case UOMPieces, UOMLitres, UOMMetres, UOMSets, UOMKg, UOMBoxes:
		return true
	default:
		return false
	}
}

type BillStatus string

const (
	BillStatusDraft     BillStatus = "DRAFT"
	BillStatusReceived  BillStatus = "RECEIVED"
	BillStatusCancelled BillStatus = "CANCELLED"
)

type TransactionType string

const (
	TxPurchaseReceipt TransactionType = "PURCHASE_RECEIPT"
	TxWorkOrderIssue  TransactionType = "WORK_ORDER_ISSUE"
	TxWorkOrderReturn TransactionType = "WORK_ORDER_RETURN"
	TxSTOOut          TransactionType = "STO_OUT"
	TxSTOIn           TransactionType = "STO_IN"
	TxAdjustmentIn    TransactionType = "ADJUSTMENT_IN"
	TxAdjustmentOut   TransactionType = "ADJUSTMENT_OUT"
	TxScrap           TransactionType = "SCRAP"
)

type Part struct {
	ID               string       `json:"id"`
	TenantID         string       `json:"tenant_id"`
	PartNumber       string       `json:"part_number"`
	Name             string       `json:"name"`
	Category         PartCategory `json:"category"`
	Description      string       `json:"description"`
	UOM              PartUOM      `json:"uom"`
	MinStockLevel    float64      `json:"min_stock_level"`
	ReorderQuantity  float64      `json:"reorder_quantity"`
	CostPrice        float64      `json:"cost_price"`
	SellingPrice     float64      `json:"selling_price"`
	OEMBrand         string       `json:"oem_brand"`
	CompatibleModels string       `json:"compatible_models"`
	IsActive         bool         `json:"is_active"`
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`

	// Derived across stores
	TotalStockOnHand float64 `json:"total_stock_on_hand"`
	IsLowStock       bool    `json:"is_low_stock"`
}

type PartStock struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	PartID           string    `json:"part_id"`
	FacilityID       string    `json:"facility_id"`
	QuantityOnHand   float64   `json:"quantity_on_hand"`
	QuantityReserved float64   `json:"quantity_reserved"`
	BinLocation      string    `json:"bin_location"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Joined metadata
	PartNumber   string  `json:"part_number,omitempty"`
	PartName     string  `json:"part_name,omitempty"`
	UOM          PartUOM `json:"uom,omitempty"`
	MinStock     float64 `json:"min_stock,omitempty"`
	FacilityName string  `json:"facility_name,omitempty"`
	FacilityCode string  `json:"facility_code,omitempty"`
	IsLowStock   bool    `json:"is_low_stock"`
}

type PartPurchaseItem struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	BillID    string    `json:"bill_id"`
	PartID    string    `json:"part_id"`
	Quantity  float64   `json:"quantity"`
	UnitCost  float64   `json:"unit_cost"`
	TaxRate   float64   `json:"tax_rate"`
	TotalCost float64   `json:"total_cost"`
	CreatedAt time.Time `json:"created_at"`

	// Joined
	PartNumber string `json:"part_number,omitempty"`
	PartName   string `json:"part_name,omitempty"`
}

type PartPurchaseBill struct {
	ID           string             `json:"id"`
	TenantID     string             `json:"tenant_id"`
	BillNumber   string             `json:"bill_number"`
	VendorName   string             `json:"vendor_name"`
	VendorGSTIN  string             `json:"vendor_gstin"`
	FacilityID   string             `json:"facility_id"`
	BillDate     time.Time          `json:"bill_date"`
	TotalAmount  float64            `json:"total_amount"`
	TaxAmount    float64            `json:"tax_amount"`
	Status       BillStatus         `json:"status"`
	ReceivedDate *time.Time         `json:"received_date,omitempty"`
	ReceivedBy   string             `json:"received_by,omitempty"`
	Notes        string             `json:"notes"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Items        []PartPurchaseItem `json:"items,omitempty"`

	// Joined
	FacilityName string `json:"facility_name,omitempty"`
}

type PartTransaction struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	PartID          string          `json:"part_id"`
	FacilityID      string          `json:"facility_id"`
	TransactionType TransactionType `json:"transaction_type"`
	Quantity        float64         `json:"quantity"`
	UnitCost        float64         `json:"unit_cost"`
	TotalCost       float64         `json:"total_cost"`
	BalanceAfter    float64         `json:"balance_after"`
	ReferenceType   string          `json:"reference_type"`
	ReferenceID     string          `json:"reference_id"`
	VehicleID       *string         `json:"vehicle_id,omitempty"`
	WorkOrderID     *string         `json:"work_order_id,omitempty"`
	Notes           string          `json:"notes"`
	PerformedBy     string          `json:"performed_by"`
	RecordedAt      time.Time       `json:"recorded_at"`

	// Joined metadata
	PartNumber   string `json:"part_number,omitempty"`
	PartName     string `json:"part_name,omitempty"`
	FacilityName string `json:"facility_name,omitempty"`
}

type InventorySummary struct {
	TotalParts          int     `json:"total_parts"`
	TotalValuation      float64 `json:"total_valuation"`
	LowStockCount       int     `json:"low_stock_count"`
	TotalStockedUnits   float64 `json:"total_stocked_units"`
	ActiveFacilityCount int     `json:"active_facility_count"`
}

// Input DTOs

type CreatePartInput struct {
	PartNumber       string       `json:"part_number"`
	Name             string       `json:"name"`
	Category         PartCategory `json:"category"`
	Description      string       `json:"description"`
	UOM              PartUOM      `json:"uom"`
	MinStockLevel    *float64     `json:"min_stock_level,omitempty"`
	ReorderQuantity  *float64     `json:"reorder_quantity,omitempty"`
	CostPrice        float64      `json:"cost_price"`
	SellingPrice     float64      `json:"selling_price"`
	OEMBrand         string       `json:"oem_brand"`
	CompatibleModels string       `json:"compatible_models"`
}

func (in *CreatePartInput) Validate() error {
	in.PartNumber = strings.TrimSpace(in.PartNumber)
	if in.PartNumber == "" {
		return errors.New("part_number is required")
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errors.New("name is required")
	}
	if in.Category == "" {
		in.Category = CategoryGeneral
	}
	if !IsValidCategory(in.Category) {
		return errors.New("invalid category")
	}
	if in.UOM == "" {
		in.UOM = UOMPieces
	}
	if !IsValidUOM(in.UOM) {
		return errors.New("invalid uom")
	}
	if in.CostPrice < 0 {
		return errors.New("cost_price cannot be negative")
	}
	if in.SellingPrice < 0 {
		return errors.New("selling_price cannot be negative")
	}
	return nil
}

type UpdatePartInput struct {
	Name             *string       `json:"name,omitempty"`
	Category         *PartCategory `json:"category,omitempty"`
	Description      *string       `json:"description,omitempty"`
	UOM              *PartUOM      `json:"uom,omitempty"`
	MinStockLevel    *float64      `json:"min_stock_level,omitempty"`
	ReorderQuantity  *float64      `json:"reorder_quantity,omitempty"`
	CostPrice        *float64      `json:"cost_price,omitempty"`
	SellingPrice     *float64      `json:"selling_price,omitempty"`
	OEMBrand         *string       `json:"oem_brand,omitempty"`
	CompatibleModels *string       `json:"compatible_models,omitempty"`
	IsActive         *bool         `json:"is_active,omitempty"`
}

type PurchaseBillItemInput struct {
	PartID   string  `json:"part_id"`
	Quantity float64 `json:"quantity"`
	UnitCost float64 `json:"unit_cost"`
	TaxRate  float64 `json:"tax_rate"`
}

type CreatePurchaseBillInput struct {
	BillNumber  string                  `json:"bill_number"`
	VendorName  string                  `json:"vendor_name"`
	VendorGSTIN string                  `json:"vendor_gstin"`
	FacilityID  string                  `json:"facility_id"`
	BillDate    *time.Time              `json:"bill_date,omitempty"`
	Notes       string                  `json:"notes"`
	Items       []PurchaseBillItemInput `json:"items"`
}

func (in *CreatePurchaseBillInput) Validate() error {
	in.BillNumber = strings.TrimSpace(in.BillNumber)
	if in.BillNumber == "" {
		return errors.New("bill_number is required")
	}
	in.VendorName = strings.TrimSpace(in.VendorName)
	if in.VendorName == "" {
		return errors.New("vendor_name is required")
	}
	in.FacilityID = strings.TrimSpace(in.FacilityID)
	if in.FacilityID == "" {
		return errors.New("facility_id is required")
	}
	if len(in.Items) == 0 {
		return errors.New("at least one item is required")
	}
	for i, item := range in.Items {
		if strings.TrimSpace(item.PartID) == "" {
			return errors.New("item part_id is required")
		}
		if item.Quantity <= 0 {
			return errors.New("item quantity must be greater than zero")
		}
		if item.UnitCost < 0 {
			return errors.New("item unit_cost cannot be negative")
		}
		in.Items[i].PartID = strings.TrimSpace(item.PartID)
	}
	return nil
}

type IssuePartInput struct {
	PartID      string  `json:"part_id"`
	FacilityID  string  `json:"facility_id"`
	Quantity    float64 `json:"quantity"`
	WorkOrderID string  `json:"work_order_id,omitempty"`
	VehicleID   string  `json:"vehicle_id,omitempty"`
	PerformedBy string  `json:"performed_by"`
	Notes       string  `json:"notes"`
}

func (in *IssuePartInput) Validate() error {
	in.PartID = strings.TrimSpace(in.PartID)
	if in.PartID == "" {
		return errors.New("part_id is required")
	}
	in.FacilityID = strings.TrimSpace(in.FacilityID)
	if in.FacilityID == "" {
		return errors.New("facility_id is required")
	}
	if in.Quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}
	return nil
}

type ReturnPartInput struct {
	PartID      string  `json:"part_id"`
	FacilityID  string  `json:"facility_id"`
	Quantity    float64 `json:"quantity"`
	WorkOrderID string  `json:"work_order_id,omitempty"`
	VehicleID   string  `json:"vehicle_id,omitempty"`
	PerformedBy string  `json:"performed_by"`
	Notes       string  `json:"notes"`
}

func (in *ReturnPartInput) Validate() error {
	in.PartID = strings.TrimSpace(in.PartID)
	if in.PartID == "" {
		return errors.New("part_id is required")
	}
	in.FacilityID = strings.TrimSpace(in.FacilityID)
	if in.FacilityID == "" {
		return errors.New("facility_id is required")
	}
	if in.Quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}
	return nil
}

type AdjustStockInput struct {
	PartID            string  `json:"part_id"`
	FacilityID        string  `json:"facility_id"`
	NewQuantityOnHand float64 `json:"new_quantity_on_hand"`
	Reason            string  `json:"reason"`
	PerformedBy       string  `json:"performed_by"`
	Notes             string  `json:"notes"`
}

func (in *AdjustStockInput) Validate() error {
	in.PartID = strings.TrimSpace(in.PartID)
	if in.PartID == "" {
		return errors.New("part_id is required")
	}
	in.FacilityID = strings.TrimSpace(in.FacilityID)
	if in.FacilityID == "" {
		return errors.New("facility_id is required")
	}
	if in.NewQuantityOnHand < 0 {
		return errors.New("new_quantity_on_hand cannot be negative")
	}
	return nil
}

type TransferStockInput struct {
	PartID         string  `json:"part_id"`
	FromFacilityID string  `json:"from_facility_id"`
	ToFacilityID   string  `json:"to_facility_id"`
	Quantity       float64 `json:"quantity"`
	STOID          string  `json:"sto_id,omitempty"`
	PerformedBy    string  `json:"performed_by"`
	Notes          string  `json:"notes"`
}

func (in *TransferStockInput) Validate() error {
	in.PartID = strings.TrimSpace(in.PartID)
	if in.PartID == "" {
		return errors.New("part_id is required")
	}
	in.FromFacilityID = strings.TrimSpace(in.FromFacilityID)
	if in.FromFacilityID == "" {
		return errors.New("from_facility_id is required")
	}
	in.ToFacilityID = strings.TrimSpace(in.ToFacilityID)
	if in.ToFacilityID == "" {
		return errors.New("to_facility_id is required")
	}
	if in.FromFacilityID == in.ToFacilityID {
		return errors.New("cannot transfer stock to the same facility")
	}
	if in.Quantity <= 0 {
		return errors.New("quantity must be greater than zero")
	}
	return nil
}

type PartFilter struct {
	TenantID     string
	Category     string
	Search       string
	LowStockOnly bool
	ActiveOnly   bool
	Limit        int
	Offset       int
}

type StockFilter struct {
	TenantID     string
	FacilityID   string
	PartID       string
	LowStockOnly bool
	Limit        int
	Offset       int
}

type TransactionFilter struct {
	TenantID        string
	PartID          string
	FacilityID      string
	TransactionType string
	WorkOrderID     string
	VehicleID       string
	Limit           int
	Offset          int
}

type PurchaseBillFilter struct {
	TenantID   string
	FacilityID string
	Status     string
	Search     string
	Limit      int
	Offset     int
}
