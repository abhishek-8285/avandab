package sto

import (
	"errors"
	"strings"
	"time"
)

// STOStatus represents the lifecycle state of a Stock Transfer Order.
type STOStatus string

const (
	StatusDRAFT     STOStatus = "DRAFT"
	StatusRELEASED  STOStatus = "RELEASED"
	StatusPOSTED    STOStatus = "POSTED"
	StatusASSIGNED  STOStatus = "ASSIGNED"
	StatusINTRANSIT STOStatus = "IN_TRANSIT"
	StatusRECEIVED  STOStatus = "RECEIVED"
	StatusCANCELLED STOStatus = "CANCELLED"
)

// LoadBoardStatus represents the status of a load board listing.
type LoadBoardStatus string

const (
	LBStatusOPEN      LoadBoardStatus = "OPEN"
	LBStatusBIDDING   LoadBoardStatus = "BIDDING"
	LBStatusAWARDED   LoadBoardStatus = "AWARDED"
	LBStatusEXPIRED   LoadBoardStatus = "EXPIRED"
	LBStatusCANCELLED LoadBoardStatus = "CANCELLED"
)

// Visibility represents load listing exposure.
type Visibility string

const (
	VisibilityPRIVATE   Visibility = "PRIVATE"
	VisibilityFEDERATED Visibility = "FEDERATED"
	VisibilityPUBLIC    Visibility = "PUBLIC"
)

// BidStatus represents the state of a carrier bid.
type BidStatus string

const (
	BidStatusSUBMITTED BidStatus = "SUBMITTED"
	BidStatusACCEPTED  BidStatus = "ACCEPTED"
	BidStatusREJECTED  BidStatus = "REJECTED"
	BidStatusWITHDRAWN BidStatus = "WITHDRAWN"
)

// StockTransferOrder represents an internal movement of goods between facilities.
type StockTransferOrder struct {
	ID                    string    `json:"id"`
	TenantID              string    `json:"tenant_id"`
	STONumber             string    `json:"sto_number"`
	OriginFacilityID      string    `json:"origin_facility_id"`
	OriginFacilityName    string    `json:"origin_facility_name,omitempty"`
	DestinationFacilityID string    `json:"destination_facility_id"`
	DestFacilityName      string    `json:"destination_facility_name,omitempty"`
	MaterialCode          string    `json:"material_code"`
	MaterialDescription   string    `json:"material_description"`
	Quantity              float64   `json:"quantity"`
	UOM                   string    `json:"uom"`
	RequiredDeliveryDate  string    `json:"required_delivery_date"`
	Status                STOStatus `json:"status"`
	Notes                 string    `json:"notes,omitempty"`
	CreatedBy             string    `json:"created_by"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// LoadBoardListing represents a syndicated load available for bidding.
type LoadBoardListing struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenant_id"`
	STOID               *string         `json:"sto_id,omitempty"`
	STONumber           string          `json:"sto_number,omitempty"`
	BookingID           *string         `json:"booking_id,omitempty"`
	OriginCity          string          `json:"origin_city"`
	DestinationCity     string          `json:"destination_city"`
	VehicleTypeRequired string          `json:"vehicle_type_required"`
	TargetRate          float64         `json:"target_rate"`
	MaxRate             float64         `json:"max_rate"`
	Visibility          Visibility      `json:"visibility"`
	Status              LoadBoardStatus `json:"status"`
	ExpiresAt           time.Time       `json:"expires_at"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	BidsCount           int             `json:"bids_count,omitempty"`
}

// LoadBoardBid represents an offer by a carrier to execute a listed load.
type LoadBoardBid struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	ListingID   string     `json:"listing_id"`
	CarrierID   string     `json:"carrier_id"`
	CarrierName string     `json:"carrier_name"`
	BidAmount   float64    `json:"bid_amount"`
	VehicleID   *string    `json:"vehicle_id,omitempty"`
	DriverID    *string    `json:"driver_id,omitempty"`
	Status      BidStatus  `json:"status"`
	Remarks     string     `json:"remarks,omitempty"`
	SubmittedAt time.Time  `json:"submitted_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
}

// CreateSTOInput carries user payload for creating a new STO draft.
type CreateSTOInput struct {
	OriginFacilityID      string  `json:"origin_facility_id"`
	DestinationFacilityID string  `json:"destination_facility_id"`
	MaterialCode          string  `json:"material_code"`
	MaterialDescription   string  `json:"material_description"`
	Quantity              float64 `json:"quantity"`
	UOM                   string  `json:"uom"`
	RequiredDeliveryDate  string  `json:"required_delivery_date"`
	Notes                 string  `json:"notes"`
	CreatedBy             string  `json:"created_by"`
}

func (in *CreateSTOInput) Validate() error {
	if strings.TrimSpace(in.OriginFacilityID) == "" {
		return errors.New("origin_facility_id is required")
	}
	if strings.TrimSpace(in.DestinationFacilityID) == "" {
		return errors.New("destination_facility_id is required")
	}
	if in.OriginFacilityID == in.DestinationFacilityID {
		return errors.New("origin and destination facility cannot be identical")
	}
	if strings.TrimSpace(in.MaterialCode) == "" {
		return errors.New("material_code is required")
	}
	if in.Quantity <= 0 {
		return errors.New("quantity must be strictly positive")
	}
	if strings.TrimSpace(in.UOM) == "" {
		return errors.New("uom is required")
	}
	if strings.TrimSpace(in.RequiredDeliveryDate) == "" {
		return errors.New("required_delivery_date is required")
	}
	return nil
}

// PostToLoadBoardInput specifies parameters for posting an STO to the load board.
type PostToLoadBoardInput struct {
	VehicleTypeRequired string     `json:"vehicle_type_required"`
	TargetRate          float64    `json:"target_rate"`
	MaxRate             float64    `json:"max_rate"`
	Visibility          Visibility `json:"visibility"`
	ExpiresHours        int        `json:"expires_hours"`
}

// SubmitBidInput represents carrier submission on a listing.
type SubmitBidInput struct {
	CarrierID   string  `json:"carrier_id"`
	CarrierName string  `json:"carrier_name"`
	BidAmount   float64 `json:"bid_amount"`
	VehicleID   *string `json:"vehicle_id,omitempty"`
	DriverID    *string `json:"driver_id,omitempty"`
	Remarks     string  `json:"remarks,omitempty"`
}

func (in *SubmitBidInput) Validate() error {
	if strings.TrimSpace(in.CarrierID) == "" {
		return errors.New("carrier_id is required")
	}
	if strings.TrimSpace(in.CarrierName) == "" {
		return errors.New("carrier_name is required")
	}
	if in.BidAmount <= 0 {
		return errors.New("bid_amount must be greater than zero")
	}
	return nil
}

// AwardBidInput represents the input to award a bid.
type AwardBidInput struct {
	BidID string `json:"bid_id"`
}

// AwardResult contains details of the awarded transaction.
type AwardResult struct {
	ListingID  string       `json:"listing_id"`
	WinningBid LoadBoardBid `json:"winning_bid"`
	STOID      *string      `json:"sto_id,omitempty"`
	BookingID  *string      `json:"booking_id,omitempty"`
	TripID     *string      `json:"trip_id,omitempty"`
	AwardedAt  time.Time    `json:"awarded_at"`
}
