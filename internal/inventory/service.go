package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreatePart(ctx context.Context, tenantID string, in CreatePartInput) (*Part, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	// Check if part number already exists
	existing, err := s.repo.GetPartByNumber(ctx, tenantID, in.PartNumber)
	if err == nil && existing != nil {
		return nil, ErrDuplicatePartNumber
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	minStock := 5.0
	if in.MinStockLevel != nil {
		minStock = *in.MinStockLevel
	}
	reorderQty := 10.0
	if in.ReorderQuantity != nil {
		reorderQty = *in.ReorderQuantity
	}

	now := time.Now().UTC()
	p := &Part{
		ID:               "prt-" + uuid.NewString(),
		TenantID:         tenantID,
		PartNumber:       in.PartNumber,
		Name:             in.Name,
		Category:         in.Category,
		Description:      in.Description,
		UOM:              in.UOM,
		MinStockLevel:    minStock,
		ReorderQuantity:  reorderQty,
		CostPrice:        in.CostPrice,
		SellingPrice:     in.SellingPrice,
		OEMBrand:         in.OEMBrand,
		CompatibleModels: in.CompatibleModels,
		IsActive:         true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.repo.CreatePart(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) GetPart(ctx context.Context, tenantID, id string) (*Part, error) {
	p, err := s.repo.GetPartByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPartNotFound
		}
		return nil, err
	}
	return p, nil
}

func (s *Service) UpdatePart(ctx context.Context, tenantID, id string, in UpdatePartInput) (*Part, error) {
	p, err := s.GetPart(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if in.Name != nil && *in.Name != "" {
		p.Name = *in.Name
	}
	if in.Category != nil && IsValidCategory(*in.Category) {
		p.Category = *in.Category
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.UOM != nil && IsValidUOM(*in.UOM) {
		p.UOM = *in.UOM
	}
	if in.MinStockLevel != nil && *in.MinStockLevel >= 0 {
		p.MinStockLevel = *in.MinStockLevel
	}
	if in.ReorderQuantity != nil && *in.ReorderQuantity >= 0 {
		p.ReorderQuantity = *in.ReorderQuantity
	}
	if in.CostPrice != nil && *in.CostPrice >= 0 {
		p.CostPrice = *in.CostPrice
	}
	if in.SellingPrice != nil && *in.SellingPrice >= 0 {
		p.SellingPrice = *in.SellingPrice
	}
	if in.OEMBrand != nil {
		p.OEMBrand = *in.OEMBrand
	}
	if in.CompatibleModels != nil {
		p.CompatibleModels = *in.CompatibleModels
	}
	if in.IsActive != nil {
		p.IsActive = *in.IsActive
	}

	if err := s.repo.UpdatePart(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) ListParts(ctx context.Context, filter PartFilter) ([]Part, int, error) {
	return s.repo.ListParts(ctx, filter)
}

func (s *Service) ListStocks(ctx context.Context, filter StockFilter) ([]PartStock, int, error) {
	return s.repo.ListStocks(ctx, filter)
}

func (s *Service) CreatePurchaseBill(ctx context.Context, tenantID string, in CreatePurchaseBillInput) (*PartPurchaseBill, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	billDate := time.Now().UTC()
	if in.BillDate != nil {
		billDate = *in.BillDate
	}

	billID := "pb-" + uuid.NewString()
	var totalAmount float64
	var taxAmount float64

	items := make([]PartPurchaseItem, len(in.Items))
	for i, item := range in.Items {
		part, err := s.GetPart(ctx, tenantID, item.PartID)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}

		lineCost := math.Round(item.Quantity*item.UnitCost*100) / 100
		lineTax := math.Round(lineCost*(item.TaxRate/100)*100) / 100

		items[i] = PartPurchaseItem{
			ID:         "pbi-" + uuid.NewString(),
			TenantID:   tenantID,
			BillID:     billID,
			PartID:     part.ID,
			Quantity:   item.Quantity,
			UnitCost:   item.UnitCost,
			TaxRate:    item.TaxRate,
			TotalCost:  lineCost + lineTax,
			CreatedAt:  billDate,
			PartNumber: part.PartNumber,
			PartName:   part.Name,
		}

		totalAmount += (lineCost + lineTax)
		taxAmount += lineTax
	}

	now := time.Now().UTC()
	bill := &PartPurchaseBill{
		ID:          billID,
		TenantID:    tenantID,
		BillNumber:  in.BillNumber,
		VendorName:  in.VendorName,
		VendorGSTIN: in.VendorGSTIN,
		FacilityID:  in.FacilityID,
		BillDate:    billDate,
		TotalAmount: math.Round(totalAmount*100) / 100,
		TaxAmount:   math.Round(taxAmount*100) / 100,
		Status:      BillStatusDraft,
		Notes:       in.Notes,
		CreatedAt:   now,
		UpdatedAt:   now,
		Items:       items,
	}

	if err := s.repo.CreatePurchaseBill(ctx, bill, items); err != nil {
		return nil, err
	}
	return bill, nil
}

func (s *Service) GetPurchaseBill(ctx context.Context, tenantID, id string) (*PartPurchaseBill, error) {
	b, err := s.repo.GetPurchaseBillByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBillNotFound
		}
		return nil, err
	}
	return b, nil
}

func (s *Service) ListPurchaseBills(ctx context.Context, filter PurchaseBillFilter) ([]PartPurchaseBill, int, error) {
	return s.repo.ListPurchaseBills(ctx, filter)
}

func (s *Service) ReceivePurchaseBill(ctx context.Context, tenantID, id, receivedBy string) (*PartPurchaseBill, error) {
	bill, err := s.GetPurchaseBill(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if bill.Status != BillStatusDraft {
		return nil, ErrBillAlreadyProcessed
	}

	now := time.Now().UTC()
	var acts []*PartTransaction
	for _, item := range bill.Items {
		act := &PartTransaction{
			ID:              "ptx-" + uuid.NewString(),
			TenantID:        tenantID,
			PartID:          item.PartID,
			FacilityID:      bill.FacilityID,
			TransactionType: TxPurchaseReceipt,
			Quantity:        item.Quantity,
			UnitCost:        item.UnitCost,
			TotalCost:       item.TotalCost,
			ReferenceType:   "PURCHASE_BILL",
			ReferenceID:     bill.BillNumber,
			Notes:           fmt.Sprintf("Received from vendor %s", bill.VendorName),
			PerformedBy:     receivedBy,
			RecordedAt:      now,
		}
		acts = append(acts, act)
	}

	if err := s.repo.ReceivePurchaseBillTx(ctx, tenantID, id, receivedBy, acts); err != nil {
		return nil, err
	}

	return s.GetPurchaseBill(ctx, tenantID, id)
}

func (s *Service) IssuePart(ctx context.Context, tenantID string, in IssuePartInput) (*PartTransaction, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	part, err := s.GetPart(ctx, tenantID, in.PartID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var vehID, woID *string
	if in.VehicleID != "" {
		vehID = &in.VehicleID
	}
	if in.WorkOrderID != "" {
		woID = &in.WorkOrderID
	}

	act := &PartTransaction{
		ID:              "ptx-" + uuid.NewString(),
		TenantID:        tenantID,
		PartID:          part.ID,
		FacilityID:      in.FacilityID,
		TransactionType: TxWorkOrderIssue,
		Quantity:        in.Quantity,
		UnitCost:        part.CostPrice,
		TotalCost:       math.Round(in.Quantity*part.CostPrice*100) / 100,
		ReferenceType:   "WORK_ORDER",
		ReferenceID:     in.WorkOrderID,
		VehicleID:       vehID,
		WorkOrderID:     woID,
		Notes:           in.Notes,
		PerformedBy:     in.PerformedBy,
		RecordedAt:      now,
		PartNumber:      part.PartNumber,
		PartName:        part.Name,
	}

	if err := s.repo.IssuePartTx(ctx, tenantID, act); err != nil {
		return nil, err
	}
	return act, nil
}

func (s *Service) ReturnPart(ctx context.Context, tenantID string, in ReturnPartInput) (*PartTransaction, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	part, err := s.GetPart(ctx, tenantID, in.PartID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var vehID, woID *string
	if in.VehicleID != "" {
		vehID = &in.VehicleID
	}
	if in.WorkOrderID != "" {
		woID = &in.WorkOrderID
	}

	act := &PartTransaction{
		ID:              "ptx-" + uuid.NewString(),
		TenantID:        tenantID,
		PartID:          part.ID,
		FacilityID:      in.FacilityID,
		TransactionType: TxWorkOrderReturn,
		Quantity:        in.Quantity,
		UnitCost:        part.CostPrice,
		TotalCost:       math.Round(in.Quantity*part.CostPrice*100) / 100,
		ReferenceType:   "WORK_ORDER_RETURN",
		ReferenceID:     in.WorkOrderID,
		VehicleID:       vehID,
		WorkOrderID:     woID,
		Notes:           in.Notes,
		PerformedBy:     in.PerformedBy,
		RecordedAt:      now,
		PartNumber:      part.PartNumber,
		PartName:        part.Name,
	}

	if err := s.repo.ReturnPartTx(ctx, tenantID, act); err != nil {
		return nil, err
	}
	return act, nil
}

func (s *Service) AdjustStock(ctx context.Context, tenantID string, in AdjustStockInput) (*PartTransaction, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	part, err := s.GetPart(ctx, tenantID, in.PartID)
	if err != nil {
		return nil, err
	}

	currentStock, err := s.repo.GetPartStock(ctx, tenantID, in.PartID, in.FacilityID)
	var currentQty float64
	if err == nil && currentStock != nil {
		currentQty = currentStock.QuantityOnHand
	}

	diff := in.NewQuantityOnHand - currentQty
	if diff == 0 {
		return nil, errors.New("new quantity matches current quantity; no adjustment needed")
	}

	now := time.Now().UTC()
	txType := TxAdjustmentIn
	adjQty := diff
	if diff < 0 {
		txType = TxAdjustmentOut
		adjQty = -diff
	}

	act := &PartTransaction{
		ID:              "ptx-" + uuid.NewString(),
		TenantID:        tenantID,
		PartID:          part.ID,
		FacilityID:      in.FacilityID,
		TransactionType: txType,
		Quantity:        adjQty,
		UnitCost:        part.CostPrice,
		TotalCost:       math.Round(adjQty*part.CostPrice*100) / 100,
		BalanceAfter:    in.NewQuantityOnHand,
		ReferenceType:   "MANUAL_ADJUSTMENT",
		Notes:           fmt.Sprintf("Reason: %s. %s", in.Reason, in.Notes),
		PerformedBy:     in.PerformedBy,
		RecordedAt:      now,
		PartNumber:      part.PartNumber,
		PartName:        part.Name,
	}

	if err := s.repo.AdjustStockTx(ctx, tenantID, act, in.NewQuantityOnHand); err != nil {
		return nil, err
	}
	return act, nil
}

func (s *Service) TransferStock(ctx context.Context, tenantID string, in TransferStockInput) (*PartTransaction, *PartTransaction, error) {
	if err := in.Validate(); err != nil {
		return nil, nil, err
	}

	part, err := s.GetPart(ctx, tenantID, in.PartID)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	outAct := &PartTransaction{
		ID:              "ptx-" + uuid.NewString(),
		TenantID:        tenantID,
		PartID:          part.ID,
		FacilityID:      in.FromFacilityID,
		TransactionType: TxSTOOut,
		Quantity:        in.Quantity,
		UnitCost:        part.CostPrice,
		TotalCost:       math.Round(in.Quantity*part.CostPrice*100) / 100,
		ReferenceType:   "STO",
		ReferenceID:     in.STOID,
		Notes:           fmt.Sprintf("Transferred to facility %s. %s", in.ToFacilityID, in.Notes),
		PerformedBy:     in.PerformedBy,
		RecordedAt:      now,
		PartNumber:      part.PartNumber,
		PartName:        part.Name,
	}

	inAct := &PartTransaction{
		ID:              "ptx-" + uuid.NewString(),
		TenantID:        tenantID,
		PartID:          part.ID,
		FacilityID:      in.ToFacilityID,
		TransactionType: TxSTOIn,
		Quantity:        in.Quantity,
		UnitCost:        part.CostPrice,
		TotalCost:       math.Round(in.Quantity*part.CostPrice*100) / 100,
		ReferenceType:   "STO",
		ReferenceID:     in.STOID,
		Notes:           fmt.Sprintf("Received from facility %s. %s", in.FromFacilityID, in.Notes),
		PerformedBy:     in.PerformedBy,
		RecordedAt:      now,
		PartNumber:      part.PartNumber,
		PartName:        part.Name,
	}

	if err := s.repo.TransferStockTx(ctx, tenantID, outAct, inAct); err != nil {
		return nil, nil, err
	}
	return outAct, inAct, nil
}

func (s *Service) ListTransactions(ctx context.Context, filter TransactionFilter) ([]PartTransaction, int, error) {
	return s.repo.ListTransactions(ctx, filter)
}

func (s *Service) GetSummary(ctx context.Context, tenantID string) (*InventorySummary, error) {
	return s.repo.GetSummary(ctx, tenantID)
}
