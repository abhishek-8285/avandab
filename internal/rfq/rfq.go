package rfq

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound          = errors.New("rfq, bid, or counter-offer not found")
	ErrInvalidInput      = errors.New("invalid rfq input")
	ErrUnauthorized      = errors.New("unauthorized rfq action")
	ErrRFQClosed         = errors.New("rfq is not open for bidding or negotiation")
	ErrBidDecided        = errors.New("bid has already been accepted, rejected, or withdrawn")
	ErrCounterExpired    = errors.New("counter-offer has expired")
	ErrCounterNotPending = errors.New("counter-offer is not in pending state")
)

type RFQStatus string

const (
	StatusOpen             RFQStatus = "OPEN"
	StatusUnderNegotiation RFQStatus = "UNDER_NEGOTIATION"
	StatusAwarded          RFQStatus = "AWARDED"
	StatusClosed           RFQStatus = "CLOSED"
	StatusExpired          RFQStatus = "EXPIRED"
	StatusCancelled        RFQStatus = "CANCELLED"
)

type RFQBidStatus string

const (
	BidStatusSubmitted      RFQBidStatus = "SUBMITTED"
	BidStatusCounterOffered RFQBidStatus = "COUNTER_OFFERED"
	BidStatusAccepted       RFQBidStatus = "ACCEPTED"
	BidStatusRejected       RFQBidStatus = "REJECTED"
	BidStatusWithdrawn      RFQBidStatus = "WITHDRAWN"
)

type CounterOfferStatus string

const (
	CounterStatusPending    CounterOfferStatus = "PENDING"
	CounterStatusAccepted   CounterOfferStatus = "ACCEPTED"
	CounterStatusRejected   CounterOfferStatus = "REJECTED"
	CounterStatusSuperseded CounterOfferStatus = "SUPERSEDED"
	CounterStatusExpired    CounterOfferStatus = "EXPIRED"
)

type SenderRole string

const (
	RoleShipper SenderRole = "SHIPPER"
	RoleCarrier SenderRole = "CARRIER"
)

type EntityType string

const (
	EntityRFQ       EntityType = "RFQ"
	EntityLoadBoard EntityType = "LOAD_BOARD"
)

type RFQ struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	RFQNumber           string     `json:"rfq_number"`
	Title               string     `json:"title"`
	OriginCity          string     `json:"origin_city"`
	DestinationCity     string     `json:"destination_city"`
	VehicleTypeRequired string     `json:"vehicle_type_required"`
	CargoType           string     `json:"cargo_type"`
	CargoWeightTons     float64    `json:"cargo_weight_tons"`
	TargetRate          float64    `json:"target_rate"`
	MaxAcceptableRate   float64    `json:"max_acceptable_rate"`
	PickupDate          time.Time  `json:"pickup_date"`
	DeliveryDeadline    time.Time  `json:"delivery_deadline"`
	BidDeadline         time.Time  `json:"bid_deadline"`
	Status              RFQStatus  `json:"status"`
	BookingID           *string    `json:"booking_id,omitempty"`
	STOID               *string    `json:"sto_id,omitempty"`
	AwardedBidID        *string    `json:"awarded_bid_id,omitempty"`
	AwardedCarrierID    string     `json:"awarded_carrier_id"`
	AwardedCarrierName  string     `json:"awarded_carrier_name"`
	AwardedRate         float64    `json:"awarded_rate"`
	AwardedAt           *time.Time `json:"awarded_at,omitempty"`
	CreatedBy           string     `json:"created_by"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	BidsCount           int        `json:"bids_count,omitempty"`
}

type CreateRFQInput struct {
	Title               string    `json:"title"`
	OriginCity          string    `json:"origin_city"`
	DestinationCity     string    `json:"destination_city"`
	VehicleTypeRequired string    `json:"vehicle_type_required"`
	CargoType           string    `json:"cargo_type"`
	CargoWeightTons     float64   `json:"cargo_weight_tons"`
	TargetRate          float64   `json:"target_rate"`
	MaxAcceptableRate   float64   `json:"max_acceptable_rate"`
	PickupDate          time.Time `json:"pickup_date"`
	DeliveryDeadline    time.Time `json:"delivery_deadline"`
	BidDeadline         time.Time `json:"bid_deadline"`
	BookingID           *string   `json:"booking_id,omitempty"`
	STOID               *string   `json:"sto_id,omitempty"`
}

func (in *CreateRFQInput) Validate() error {
	if strings.TrimSpace(in.Title) == "" {
		return errors.New("title is required")
	}
	if strings.TrimSpace(in.OriginCity) == "" {
		return errors.New("origin_city is required")
	}
	if strings.TrimSpace(in.DestinationCity) == "" {
		return errors.New("destination_city is required")
	}
	if strings.TrimSpace(in.VehicleTypeRequired) == "" {
		return errors.New("vehicle_type_required is required")
	}
	if in.TargetRate < 0 {
		return errors.New("target_rate must be non-negative")
	}
	if in.MaxAcceptableRate < in.TargetRate {
		return errors.New("max_acceptable_rate cannot be less than target_rate")
	}
	if in.BidDeadline.IsZero() {
		return errors.New("bid_deadline is required")
	}
	if in.PickupDate.IsZero() {
		return errors.New("pickup_date is required")
	}
	if in.DeliveryDeadline.IsZero() {
		return errors.New("delivery_deadline is required")
	}
	if in.DeliveryDeadline.Before(in.PickupDate) {
		return errors.New("delivery_deadline cannot be before pickup_date")
	}
	return nil
}

type ListRFQsFilter struct {
	Status          *RFQStatus
	OriginCity      string
	DestinationCity string
	VehicleType     string
	Search          string
	Limit           int
	Offset          int
}

type RFQBid struct {
	ID                string       `json:"id"`
	TenantID          string       `json:"tenant_id"`
	RFQID             string       `json:"rfq_id"`
	CarrierID         string       `json:"carrier_id"`
	CarrierName       string       `json:"carrier_name"`
	BidAmount         float64      `json:"bid_amount"`
	TransitDays       int          `json:"transit_days"`
	VehiclesAvailable int          `json:"vehicles_available"`
	Remarks           string       `json:"remarks"`
	Status            RFQBidStatus `json:"status"`
	SubmittedAt       time.Time    `json:"submitted_at"`
	DecidedAt         *time.Time   `json:"decided_at,omitempty"`
	DecidedBy         string       `json:"decided_by,omitempty"`
}

type SubmitRFQBidInput struct {
	CarrierID         string  `json:"carrier_id"`
	CarrierName       string  `json:"carrier_name"`
	BidAmount         float64 `json:"bid_amount"`
	TransitDays       int     `json:"transit_days"`
	VehiclesAvailable int     `json:"vehicles_available"`
	Remarks           string  `json:"remarks"`
}

func (in *SubmitRFQBidInput) Validate() error {
	if strings.TrimSpace(in.CarrierID) == "" {
		return errors.New("carrier_id is required")
	}
	if strings.TrimSpace(in.CarrierName) == "" {
		return errors.New("carrier_name is required")
	}
	if in.BidAmount <= 0 {
		return errors.New("bid_amount must be greater than zero")
	}
	if in.TransitDays <= 0 {
		in.TransitDays = 1
	}
	if in.VehiclesAvailable <= 0 {
		in.VehiclesAvailable = 1
	}
	return nil
}

type CounterOffer struct {
	ID            string             `json:"id"`
	TenantID      string             `json:"tenant_id"`
	EntityType    EntityType         `json:"entity_type"` // RFQ or LOAD_BOARD
	RFQID         *string            `json:"rfq_id,omitempty"`
	ListingID     *string            `json:"listing_id,omitempty"`
	BidID         string             `json:"bid_id"`
	RoundNumber   int                `json:"round_number"`
	SenderRole    SenderRole         `json:"sender_role"` // SHIPPER or CARRIER
	SenderID      string             `json:"sender_id"`
	SenderName    string             `json:"sender_name"`
	OfferedAmount float64            `json:"offered_amount"`
	Remarks       string             `json:"remarks"`
	Status        CounterOfferStatus `json:"status"` // PENDING, ACCEPTED, REJECTED, SUPERSEDED, EXPIRED
	ExpiresAt     *time.Time         `json:"expires_at,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	DecidedAt     *time.Time         `json:"decided_at,omitempty"`
	DecidedBy     string             `json:"decided_by,omitempty"`
}

type SubmitCounterOfferInput struct {
	EntityType     EntityType `json:"entity_type"` // RFQ or LOAD_BOARD
	RFQID          *string    `json:"rfq_id,omitempty"`
	ListingID      *string    `json:"listing_id,omitempty"`
	SenderRole     SenderRole `json:"sender_role"` // SHIPPER or CARRIER
	OfferedAmount  float64    `json:"offered_amount"`
	Remarks        string     `json:"remarks"`
	ExpiresInHours *int       `json:"expires_in_hours,omitempty"`
}

func (in *SubmitCounterOfferInput) Validate() error {
	if in.EntityType != EntityRFQ && in.EntityType != EntityLoadBoard {
		return errors.New("entity_type must be RFQ or LOAD_BOARD")
	}
	if in.EntityType == EntityRFQ && (in.RFQID == nil || *in.RFQID == "") {
		return errors.New("rfq_id is required for RFQ counter-offers")
	}
	if in.EntityType == EntityLoadBoard && (in.ListingID == nil || *in.ListingID == "") {
		return errors.New("listing_id is required for LOAD_BOARD counter-offers")
	}
	if in.SenderRole != RoleShipper && in.SenderRole != RoleCarrier {
		return errors.New("sender_role must be SHIPPER or CARRIER")
	}
	if in.OfferedAmount <= 0 {
		return errors.New("offered_amount must be greater than zero")
	}
	return nil
}
