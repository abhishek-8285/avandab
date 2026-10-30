package rfq

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// RFQ Management

func (s *Service) CreateRFQ(ctx context.Context, tenantID, userID string, in CreateRFQInput) (*RFQ, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	rfqNum, err := s.repo.GetNextRFQNumber(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rfq := &RFQ{
		ID:                  uuid.NewString(),
		TenantID:            tenantID,
		RFQNumber:           rfqNum,
		Title:               strings.TrimSpace(in.Title),
		OriginCity:          strings.TrimSpace(in.OriginCity),
		DestinationCity:     strings.TrimSpace(in.DestinationCity),
		VehicleTypeRequired: strings.TrimSpace(in.VehicleTypeRequired),
		CargoType:           strings.TrimSpace(in.CargoType),
		CargoWeightTons:     in.CargoWeightTons,
		TargetRate:          in.TargetRate,
		MaxAcceptableRate:   in.MaxAcceptableRate,
		PickupDate:          in.PickupDate,
		DeliveryDeadline:    in.DeliveryDeadline,
		BidDeadline:         in.BidDeadline,
		Status:              StatusOpen,
		BookingID:           in.BookingID,
		STOID:               in.STOID,
		CreatedBy:           userID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.repo.CreateRFQ(ctx, rfq); err != nil {
		return nil, err
	}
	return rfq, nil
}

func (s *Service) GetRFQ(ctx context.Context, tenantID, id string) (*RFQ, error) {
	return s.repo.GetRFQByID(ctx, tenantID, id)
}

func (s *Service) ListRFQs(ctx context.Context, tenantID string, filter ListRFQsFilter) ([]RFQ, int, error) {
	return s.repo.ListRFQs(ctx, tenantID, filter)
}

func (s *Service) CloseRFQ(ctx context.Context, tenantID, id string) error {
	rfq, err := s.repo.GetRFQByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if rfq.Status == StatusAwarded {
		return fmt.Errorf("%w: cannot close an awarded rfq", ErrInvalidInput)
	}
	return s.repo.UpdateRFQStatus(ctx, tenantID, id, StatusClosed)
}

func (s *Service) CancelRFQ(ctx context.Context, tenantID, id string) error {
	rfq, err := s.repo.GetRFQByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if rfq.Status == StatusAwarded {
		return fmt.Errorf("%w: cannot cancel an awarded rfq", ErrInvalidInput)
	}
	return s.repo.UpdateRFQStatus(ctx, tenantID, id, StatusCancelled)
}

// Carrier Bidding

func (s *Service) SubmitBid(ctx context.Context, tenantID, rfqID string, in SubmitRFQBidInput) (*RFQBid, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	rfq, err := s.repo.GetRFQByID(ctx, tenantID, rfqID)
	if err != nil {
		return nil, err
	}

	if rfq.Status != StatusOpen && rfq.Status != StatusUnderNegotiation {
		return nil, fmt.Errorf("%w: rfq is closed for bidding (current status: %s)", ErrRFQClosed, rfq.Status)
	}
	if time.Now().UTC().After(rfq.BidDeadline) {
		return nil, fmt.Errorf("%w: bid deadline has passed", ErrRFQClosed)
	}

	bid := &RFQBid{
		ID:                uuid.NewString(),
		TenantID:          tenantID,
		RFQID:             rfqID,
		CarrierID:         strings.TrimSpace(in.CarrierID),
		CarrierName:       strings.TrimSpace(in.CarrierName),
		BidAmount:         in.BidAmount,
		TransitDays:       in.TransitDays,
		VehiclesAvailable: in.VehiclesAvailable,
		Remarks:           strings.TrimSpace(in.Remarks),
		Status:            BidStatusSubmitted,
		SubmittedAt:       time.Now().UTC(),
	}

	if err := s.repo.CreateRFQBid(ctx, bid); err != nil {
		return nil, err
	}
	return bid, nil
}

func (s *Service) ListBids(ctx context.Context, tenantID, rfqID string) ([]RFQBid, error) {
	return s.repo.ListBidsByRFQ(ctx, tenantID, rfqID)
}

func (s *Service) GetBid(ctx context.Context, tenantID, bidID string) (*RFQBid, error) {
	return s.repo.GetRFQBidByID(ctx, tenantID, bidID)
}

func (s *Service) WithdrawBid(ctx context.Context, tenantID, bidID, carrierID string) error {
	bid, err := s.repo.GetRFQBidByID(ctx, tenantID, bidID)
	if err != nil {
		return err
	}
	if bid.CarrierID != carrierID {
		return ErrUnauthorized
	}
	if bid.Status == BidStatusAccepted || bid.Status == BidStatusRejected {
		return ErrBidDecided
	}
	return s.repo.UpdateRFQBidStatus(ctx, tenantID, bidID, BidStatusWithdrawn, carrierID)
}

// Multi-Round Counter-Offer Negotiations

func (s *Service) SubmitCounterOffer(
	ctx context.Context,
	tenantID, bidID string,
	senderID, senderName string,
	in SubmitCounterOfferInput,
) (*CounterOffer, error) {
	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}

	// 1. Verify target bid
	if in.EntityType == EntityRFQ {
		rfq, err := s.repo.GetRFQByID(ctx, tenantID, *in.RFQID)
		if err != nil {
			return nil, err
		}
		if rfq.Status != StatusOpen && rfq.Status != StatusUnderNegotiation {
			return nil, fmt.Errorf("%w: rfq is closed for negotiation (status: %s)", ErrRFQClosed, rfq.Status)
		}

		bid, err := s.repo.GetRFQBidByID(ctx, tenantID, bidID)
		if err != nil {
			return nil, err
		}
		if bid.Status == BidStatusAccepted || bid.Status == BidStatusRejected || bid.Status == BidStatusWithdrawn {
			return nil, fmt.Errorf("%w: bid status is %s", ErrBidDecided, bid.Status)
		}
	}

	// 2. Fetch existing counter rounds to determine next round number
	existingCounters, err := s.repo.ListCounterOffersByBid(ctx, tenantID, bidID)
	if err != nil {
		return nil, err
	}
	roundNumber := len(existingCounters) + 1

	// 3. Supersede any pending counter-offers on this bid
	_ = s.repo.SupersedePendingCounters(ctx, tenantID, bidID)

	// 4. Calculate expiration
	var expiresAt *time.Time
	if in.ExpiresInHours != nil && *in.ExpiresInHours > 0 {
		exp := time.Now().UTC().Add(time.Duration(*in.ExpiresInHours) * time.Hour)
		expiresAt = &exp
	}

	now := time.Now().UTC()
	co := &CounterOffer{
		ID:            uuid.NewString(),
		TenantID:      tenantID,
		EntityType:    in.EntityType,
		RFQID:         in.RFQID,
		ListingID:     in.ListingID,
		BidID:         bidID,
		RoundNumber:   roundNumber,
		SenderRole:    in.SenderRole,
		SenderID:      senderID,
		SenderName:    senderName,
		OfferedAmount: in.OfferedAmount,
		Remarks:       strings.TrimSpace(in.Remarks),
		Status:        CounterStatusPending,
		ExpiresAt:     expiresAt,
		CreatedAt:     now,
	}

	if err := s.repo.CreateCounterOffer(ctx, co); err != nil {
		return nil, err
	}

	// 5. Update Bid status & RFQ status
	if in.EntityType == EntityRFQ {
		_ = s.repo.UpdateRFQBidStatus(ctx, tenantID, bidID, BidStatusCounterOffered, senderID)
		_ = s.repo.UpdateRFQStatus(ctx, tenantID, *in.RFQID, StatusUnderNegotiation)
	}

	return co, nil
}

func (s *Service) AcceptCounterOffer(ctx context.Context, tenantID, counterID, userID string) error {
	co, err := s.repo.GetCounterOfferByID(ctx, tenantID, counterID)
	if err != nil {
		return err
	}

	if co.Status != CounterStatusPending {
		return fmt.Errorf("%w: counter offer status is %s", ErrCounterNotPending, co.Status)
	}
	if co.ExpiresAt != nil && time.Now().UTC().After(*co.ExpiresAt) {
		_ = s.repo.UpdateCounterOfferStatus(ctx, tenantID, counterID, CounterStatusExpired, "system")
		return ErrCounterExpired
	}

	// Mark counter accepted
	if err := s.repo.UpdateCounterOfferStatus(ctx, tenantID, counterID, CounterStatusAccepted, userID); err != nil {
		return err
	}

	// Award transaction
	if co.EntityType == EntityRFQ && co.RFQID != nil {
		bid, err := s.repo.GetRFQBidByID(ctx, tenantID, co.BidID)
		if err != nil {
			return err
		}
		return s.repo.AwardRFQBidTransaction(ctx, tenantID, *co.RFQID, co.BidID, co.OfferedAmount, bid.CarrierID, bid.CarrierName, userID)
	}

	if co.EntityType == EntityLoadBoard && co.ListingID != nil {
		return s.repo.AwardLoadBoardBidTransaction(ctx, tenantID, *co.ListingID, co.BidID, co.OfferedAmount, userID)
	}

	return nil
}

func (s *Service) RejectCounterOffer(ctx context.Context, tenantID, counterID, userID string) error {
	co, err := s.repo.GetCounterOfferByID(ctx, tenantID, counterID)
	if err != nil {
		return err
	}

	if co.Status != CounterStatusPending {
		return fmt.Errorf("%w: counter offer status is %s", ErrCounterNotPending, co.Status)
	}

	return s.repo.UpdateCounterOfferStatus(ctx, tenantID, counterID, CounterStatusRejected, userID)
}

func (s *Service) ListCounterOffers(ctx context.Context, tenantID, bidID string) ([]CounterOffer, error) {
	return s.repo.ListCounterOffersByBid(ctx, tenantID, bidID)
}

func (s *Service) AwardRFQ(ctx context.Context, tenantID, rfqID, bidID, userID string) error {
	rfq, err := s.repo.GetRFQByID(ctx, tenantID, rfqID)
	if err != nil {
		return err
	}
	if rfq.Status != StatusOpen && rfq.Status != StatusUnderNegotiation {
		return fmt.Errorf("%w: rfq is closed (status: %s)", ErrRFQClosed, rfq.Status)
	}

	bid, err := s.repo.GetRFQBidByID(ctx, tenantID, bidID)
	if err != nil {
		return err
	}
	if bid.Status == BidStatusWithdrawn {
		return fmt.Errorf("%w: bid was withdrawn", ErrBidDecided)
	}

	return s.repo.AwardRFQBidTransaction(ctx, tenantID, rfqID, bidID, bid.BidAmount, bid.CarrierID, bid.CarrierName, userID)
}
