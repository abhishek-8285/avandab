package application

import (
	"context"

	"transport-app/internal/shared"
	"transport-app/internal/sto"
)

// STOUseCase orchestrates STO and Load Board operations.
type STOUseCase struct {
	svc *sto.Service
}

// NewSTOUseCase creates a new STOUseCase.
func NewSTOUseCase(svc *sto.Service) *STOUseCase {
	return &STOUseCase{svc: svc}
}

func (uc *STOUseCase) CreateSTO(ctx context.Context, tenantID shared.TenantID, in sto.CreateSTOInput) (*sto.StockTransferOrder, error) {
	return uc.svc.CreateSTO(ctx, string(tenantID), in)
}

func (uc *STOUseCase) ReleaseSTO(ctx context.Context, tenantID shared.TenantID, stoID string) (*sto.StockTransferOrder, error) {
	return uc.svc.ReleaseSTO(ctx, string(tenantID), stoID)
}

func (uc *STOUseCase) PostToLoadBoard(ctx context.Context, tenantID shared.TenantID, stoID string, in sto.PostToLoadBoardInput) (*sto.LoadBoardListing, error) {
	return uc.svc.PostToLoadBoard(ctx, string(tenantID), stoID, in)
}

func (uc *STOUseCase) ListListings(ctx context.Context, tenantID shared.TenantID, status, originCity, destCity string, limit, offset int) ([]sto.LoadBoardListing, int, error) {
	return uc.svc.ListListings(ctx, string(tenantID), status, originCity, destCity, limit, offset)
}

func (uc *STOUseCase) SubmitBid(ctx context.Context, tenantID shared.TenantID, listingID string, in sto.SubmitBidInput) (*sto.LoadBoardBid, error) {
	return uc.svc.SubmitBid(ctx, string(tenantID), listingID, in)
}

func (uc *STOUseCase) AwardBid(ctx context.Context, tenantID shared.TenantID, listingID, bidID string) (*sto.AwardResult, error) {
	return uc.svc.AwardBid(ctx, string(tenantID), listingID, bidID)
}
