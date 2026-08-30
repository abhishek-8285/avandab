package domain

import "time"

// CustomerBookingTrackingProjection represents the unified operational tracking view for customers.
type CustomerBookingTrackingProjection struct {
	BookingID     string               `json:"booking_id"`
	BookingNumber string               `json:"booking_number"`
	Status        string               `json:"status"` // CONFIRMED, DISPATCHING, ASSIGNED, IN_TRANSIT, COMPLETED, CANCELLED
	Pickup        PickupLocationView   `json:"pickup"`
	Delivery      DeliveryLocationView `json:"delivery"`
	Vehicle       *VehicleView         `json:"vehicle,omitempty"`
	Driver        *DriverMaskedView    `json:"driver,omitempty"`
	ETA           *string              `json:"eta,omitempty"`
	Tracking      *LiveTrackingPoint   `json:"tracking,omitempty"`
	Documents     []DocumentSummary    `json:"documents"`
	Payment       PaymentSummary       `json:"payment"`
	CreatedAt     time.Time            `json:"created_at"`
	UpdatedAt     time.Time            `json:"updated_at"`
}

type PickupLocationView struct {
	Address      string   `json:"address"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
}

type DeliveryLocationView struct {
	Address      string   `json:"address"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	ContactName  string   `json:"contact_name,omitempty"`
	ContactPhone string   `json:"contact_phone,omitempty"`
}

type VehicleView struct {
	VehicleID   string `json:"vehicle_id"`
	PlateNumber string `json:"plate_number"`
	Model       string `json:"model,omitempty"`
	VehicleType string `json:"vehicle_type"`
}

// DriverMaskedView exposes only operational driver details, strictly hiding Aadhaar, PAN, Bank, or Home Address.
type DriverMaskedView struct {
	DriverID    string   `json:"driver_id"`
	FirstName   string   `json:"first_name"`
	PhoneMasked string   `json:"phone_masked"`
	Score       *float64 `json:"score,omitempty"`
}

type LiveTrackingPoint struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	SpeedKmph *float64  `json:"speed_kmph,omitempty"`
	Heading   *float64  `json:"heading,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DocumentSummary struct {
	DocumentID   string    `json:"document_id"`
	DocumentType string    `json:"document_type"` // LR, EWAY_BILL, POD
	Status       string    `json:"status"`
	URL          string    `json:"url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type PaymentSummary struct {
	Status        string  `json:"status"` // pending, paid, partial, refunded
	Subtotal      float64 `json:"subtotal"`
	TaxAmount     float64 `json:"tax_amount"`
	TotalPrice    float64 `json:"total_price"`
	PaymentMethod string  `json:"payment_method,omitempty"`
}

// MaskPhoneNumber turns "9876543210" into "987****210".
func MaskPhoneNumber(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	runes := []rune(phone)
	start := runes[:3]
	end := runes[len(runes)-3:]
	return string(start) + "****" + string(end)
}
