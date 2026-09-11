package sto

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

// Service encapsulates business logic for STO management and Load Board bidding.
type Service struct {
	repo Repository
	db   *sql.DB
}

// NewService creates a new STO service.
func NewService(repo Repository, db *sql.DB) *Service {
	return &Service{repo: repo, db: db}
}

// CreateSTO creates a new STO draft.
func (s *Service) CreateSTO(ctx context.Context, tenantID string, in CreateSTOInput) (*StockTransferOrder, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	// Verify origin facility
	var origName string
	err := s.db.QueryRowContext(ctx, `
		SELECT name FROM facilities WHERE tenant_id = $1 AND id = $2`,
		tenantID, in.OriginFacilityID).Scan(&origName)
	if err != nil {
		return nil, fmt.Errorf("origin facility not found: %s", in.OriginFacilityID)
	}

	// Verify destination facility
	var dstName string
	err = s.db.QueryRowContext(ctx, `
		SELECT name FROM facilities WHERE tenant_id = $1 AND id = $2`,
		tenantID, in.DestinationFacilityID).Scan(&dstName)
	if err != nil {
		return nil, fmt.Errorf("destination facility not found: %s", in.DestinationFacilityID)
	}

	stoNumber := fmt.Sprintf("STO-%s-%04d", time.Now().Format("20060102"), rand.Intn(10000))
	record := &StockTransferOrder{
		ID:                    uuid.NewString(),
		TenantID:              tenantID,
		STONumber:             stoNumber,
		OriginFacilityID:      in.OriginFacilityID,
		OriginFacilityName:    origName,
		DestinationFacilityID: in.DestinationFacilityID,
		DestFacilityName:      dstName,
		MaterialCode:          in.MaterialCode,
		MaterialDescription:   in.MaterialDescription,
		Quantity:              in.Quantity,
		UOM:                   in.UOM,
		RequiredDeliveryDate:  in.RequiredDeliveryDate,
		Status:                StatusDRAFT,
		Notes:                 in.Notes,
		CreatedBy:             in.CreatedBy,
	}

	if err := s.repo.CreateSTO(ctx, record); err != nil {
		return nil, fmt.Errorf("failed creating STO: %w", err)
	}

	return record, nil
}

// GetSTO returns an STO by ID.
func (s *Service) GetSTO(ctx context.Context, tenantID, id string) (*StockTransferOrder, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if id == "" {
		return nil, errors.New("sto_id is required")
	}
	return s.repo.GetSTOByID(ctx, tenantID, id)
}

// ListSTOs lists STOs with filters and pagination.
func (s *Service) ListSTOs(ctx context.Context, tenantID, status string, limit, offset int) ([]StockTransferOrder, int, error) {
	if tenantID == "" {
		return nil, 0, errors.New("tenant_id is required")
	}
	return s.repo.ListSTOs(ctx, tenantID, status, limit, offset)
}

// ReleaseSTO moves a DRAFT STO to RELEASED.
func (s *Service) ReleaseSTO(ctx context.Context, tenantID, stoID string) (*StockTransferOrder, error) {
	record, err := s.repo.GetSTOByID(ctx, tenantID, stoID)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusDRAFT {
		return nil, fmt.Errorf("only DRAFT STOs can be released (current status: %s)", record.Status)
	}

	if err := s.repo.UpdateSTOStatus(ctx, tenantID, stoID, StatusRELEASED); err != nil {
		return nil, err
	}
	record.Status = StatusRELEASED
	record.UpdatedAt = time.Now()
	return record, nil
}

// PostToLoadBoard syndicates a RELEASED STO to the load board.
func (s *Service) PostToLoadBoard(ctx context.Context, tenantID, stoID string, in PostToLoadBoardInput) (*LoadBoardListing, error) {
	record, err := s.repo.GetSTOByID(ctx, tenantID, stoID)
	if err != nil {
		return nil, err
	}
	if record.Status != StatusRELEASED {
		return nil, fmt.Errorf("only RELEASED STOs can be posted to the load board (current status: %s)", record.Status)
	}

	// Resolve origin & destination cities
	var originCity, destCity string
	_ = s.db.QueryRowContext(ctx, `SELECT city FROM facilities WHERE id = $1`, record.OriginFacilityID).Scan(&originCity)
	_ = s.db.QueryRowContext(ctx, `SELECT city FROM facilities WHERE id = $1`, record.DestinationFacilityID).Scan(&destCity)
	if originCity == "" {
		originCity = "Origin Hub"
	}
	if destCity == "" {
		destCity = "Dest Hub"
	}

	vehType := in.VehicleTypeRequired
	if vehType == "" {
		vehType = "truck"
	}

	vis := in.Visibility
	if vis == "" {
		vis = VisibilityPRIVATE
	}

	expHours := in.ExpiresHours
	if expHours <= 0 {
		expHours = 48
	}

	targetRate := in.TargetRate
	maxRate := in.MaxRate
	if maxRate < targetRate {
		maxRate = targetRate
	}

	listing := &LoadBoardListing{
		ID:                  uuid.NewString(),
		TenantID:            tenantID,
		STOID:               &stoID,
		STONumber:           record.STONumber,
		OriginCity:          originCity,
		DestinationCity:     destCity,
		VehicleTypeRequired: vehType,
		TargetRate:          targetRate,
		MaxRate:             maxRate,
		Visibility:          vis,
		Status:              LBStatusOPEN,
		ExpiresAt:           time.Now().Add(time.Duration(expHours) * time.Hour),
	}

	if err := s.repo.CreateListing(ctx, listing); err != nil {
		return nil, fmt.Errorf("failed creating load board listing: %w", err)
	}

	// Transition STO status to POSTED
	_ = s.repo.UpdateSTOStatus(ctx, tenantID, stoID, StatusPOSTED)

	return listing, nil
}

// GetListing returns a listing by ID.
func (s *Service) GetListing(ctx context.Context, tenantID, id string) (*LoadBoardListing, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if id == "" {
		return nil, errors.New("listing_id is required")
	}
	return s.repo.GetListingByID(ctx, tenantID, id)
}

// ListListings lists load board listings.
func (s *Service) ListListings(ctx context.Context, tenantID, status, originCity, destCity string, limit, offset int) ([]LoadBoardListing, int, error) {
	if tenantID == "" {
		return nil, 0, errors.New("tenant_id is required")
	}
	return s.repo.ListListings(ctx, tenantID, status, originCity, destCity, limit, offset)
}

// SubmitBid submits a carrier quote on an open listing.
func (s *Service) SubmitBid(ctx context.Context, tenantID, listingID string, in SubmitBidInput) (*LoadBoardBid, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if listingID == "" {
		return nil, errors.New("listing_id is required")
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}

	listing, err := s.repo.GetListingByID(ctx, tenantID, listingID)
	if err != nil {
		return nil, err
	}

	if listing.Status != LBStatusOPEN && listing.Status != LBStatusBIDDING {
		return nil, fmt.Errorf("listing is closed for bidding (current status: %s)", listing.Status)
	}
	if time.Now().After(listing.ExpiresAt) {
		return nil, errors.New("listing has expired")
	}

	bid := &LoadBoardBid{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		ListingID:   listingID,
		CarrierID:   in.CarrierID,
		CarrierName: in.CarrierName,
		BidAmount:   in.BidAmount,
		VehicleID:   in.VehicleID,
		DriverID:    in.DriverID,
		Remarks:     in.Remarks,
	}

	if err := s.repo.CreateBid(ctx, bid); err != nil {
		return nil, fmt.Errorf("failed submitting bid: %w", err)
	}

	// Move listing to BIDDING if it was OPEN
	if listing.Status == LBStatusOPEN {
		_, _ = s.db.ExecContext(ctx, `
			UPDATE load_board_listings SET status = 'BIDDING', updated_at = $1
			WHERE tenant_id = $2 AND id = $3 AND status = 'OPEN'`,
			time.Now(), tenantID, listingID)
	}

	return bid, nil
}

// ListBids returns all bids for a listing.
func (s *Service) ListBids(ctx context.Context, tenantID, listingID string) ([]LoadBoardBid, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListBidsForListing(ctx, tenantID, listingID)
}

// AwardBid awards a listing to the winning carrier bid.
func (s *Service) AwardBid(ctx context.Context, tenantID, listingID, bidID string) (*AwardResult, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if listingID == "" {
		return nil, errors.New("listing_id is required")
	}
	if bidID == "" {
		return nil, errors.New("bid_id is required")
	}

	return s.repo.AwardBidTransaction(ctx, tenantID, listingID, bidID)
}
