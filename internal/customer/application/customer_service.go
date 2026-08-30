package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/customer/domain"
)

type CustomerAppService struct {
	repo domain.CustomerRepository
}

func NewCustomerAppService(repo domain.CustomerRepository) *CustomerAppService {
	return &CustomerAppService{repo: repo}
}

type CreateQuoteRequest struct {
	Origin      string  `json:"origin"`
	Destination string  `json:"destination"`
	CargoType   string  `json:"cargo_type"`
	VehicleType string  `json:"vehicle_type"`
	WeightKg    float64 `json:"weight_kg"`
	DistanceKm  float64 `json:"distance_km"`
	TTLMinutes  int     `json:"ttl_minutes,omitempty"`
}

func (s *CustomerAppService) CreateQuote(ctx context.Context, tenantID, customerID string, req CreateQuoteRequest) (*domain.Quote, error) {
	if tenantID == "" || customerID == "" {
		return nil, errors.New("tenant_id and customer_id are required")
	}

	quoteID := "quo_" + uuid.NewString()
	q, err := domain.CalculateQuote(
		quoteID, tenantID, customerID,
		req.Origin, req.Destination, req.CargoType, req.VehicleType,
		req.WeightKg, req.DistanceKm, req.TTLMinutes,
	)
	if err != nil {
		return nil, err
	}

	if err := s.repo.SaveQuote(ctx, tenantID, q); err != nil {
		return nil, fmt.Errorf("failed saving quote: %w", err)
	}

	return q, nil
}

type CreateBookingRequest struct {
	IdempotencyKey       string     `json:"idempotency_key"`
	QuoteID              string     `json:"quote_id"`
	PickupAddress        string     `json:"pickup_address"`
	PickupLat            *float64   `json:"pickup_lat,omitempty"`
	PickupLng            *float64   `json:"pickup_lng,omitempty"`
	PickupContactName    string     `json:"pickup_contact_name,omitempty"`
	PickupContactPhone   string     `json:"pickup_contact_phone,omitempty"`
	DeliveryAddress      string     `json:"delivery_address"`
	DeliveryLat          *float64   `json:"delivery_lat,omitempty"`
	DeliveryLng          *float64   `json:"delivery_lng,omitempty"`
	DeliveryContactName  string     `json:"delivery_contact_name,omitempty"`
	DeliveryContactPhone string     `json:"delivery_contact_phone,omitempty"`
	ScheduledAt          *time.Time `json:"scheduled_at,omitempty"`
	CargoDescription     string     `json:"cargo_description,omitempty"`
	SpecialInstructions  string     `json:"special_instructions,omitempty"`
	PaymentMethod        string     `json:"payment_method,omitempty"`
}

type CreateBookingResponse struct {
	BookingID     string  `json:"booking_id"`
	BookingNumber string  `json:"booking_number"`
	Status        string  `json:"status"`
	TotalPrice    float64 `json:"total_price"`
	IsIdempotent  bool    `json:"is_idempotent,omitempty"`
}

func (s *CustomerAppService) CreateBooking(ctx context.Context, tenantID, customerID string, req CreateBookingRequest) (*CreateBookingResponse, error) {
	if tenantID == "" || customerID == "" {
		return nil, errors.New("tenant_id and customer_id are required")
	}

	// 1. Idempotency Check: if idempotency key was supplied and exists, return existing booking
	if req.IdempotencyKey != "" {
		existingID, err := s.repo.GetBookingByIdempotencyKey(ctx, tenantID, req.IdempotencyKey)
		if err == nil && existingID != "" {
			proj, err := s.repo.GetCustomerTrackingProjection(ctx, tenantID, customerID, existingID)
			if err == nil && proj != nil {
				return &CreateBookingResponse{
					BookingID:     proj.BookingID,
					BookingNumber: proj.BookingNumber,
					Status:        proj.Status,
					TotalPrice:    proj.Payment.TotalPrice,
					IsIdempotent:  true,
				}, nil
			}
		}
	}

	// 2. Validate Quote (server-authoritative pricing)
	if req.QuoteID == "" {
		return nil, errors.New("quote_id is required for booking creation")
	}
	quote, err := s.repo.GetQuote(ctx, tenantID, req.QuoteID)
	if err != nil {
		return nil, fmt.Errorf("quote error: %w", err)
	}
	if quote.CustomerID != customerID {
		return nil, errors.New("unauthorized: quote belongs to another customer")
	}
	if quote.Status != "active" {
		return nil, fmt.Errorf("quote is no longer active (current status: %s)", quote.Status)
	}
	if time.Now().After(quote.ExpiresAt) {
		return nil, errors.New("quote has expired; please generate a fresh quote")
	}

	// 3. Mark Quote as converted
	if err := s.repo.MarkQuoteConverted(ctx, tenantID, quote.ID); err != nil {
		return nil, fmt.Errorf("failed converting quote: %w", err)
	}

	// 4. Construct Booking and Customer Details
	bookingID := "bk_" + uuid.NewString()
	bookingNumber := fmt.Sprintf("BKG-%d", time.Now().UnixNano()%1000000)

	pickupDate := time.Now()
	if req.ScheduledAt != nil {
		pickupDate = *req.ScheduledAt
	}

	bookingMap := map[string]interface{}{
		"id":             bookingID,
		"booking_number": bookingNumber,
		"customer_id":    customerID,
		"pickup_date":    pickupDate,
		"route_id":       quote.Origin + " -> " + quote.Destination,
		"vehicle_type":   quote.VehicleType,
		"passengers":     1,
		"cargo_weight":   quote.WeightKg,
		"price":          quote.TotalPrice,
		"notes":          req.SpecialInstructions,
		"status":         "confirmed", // Confirmed & ready for dispatch
	}

	pAddr := req.PickupAddress
	if pAddr == "" {
		pAddr = quote.Origin
	}
	dAddr := req.DeliveryAddress
	if dAddr == "" {
		dAddr = quote.Destination
	}

	detailsMap := map[string]interface{}{
		"booking_id":             bookingID,
		"quote_id":               quote.ID,
		"idempotency_key":        req.IdempotencyKey,
		"pickup_address":         pAddr,
		"pickup_lat":             req.PickupLat,
		"pickup_lng":             req.PickupLng,
		"pickup_contact_name":    req.PickupContactName,
		"pickup_contact_phone":   req.PickupContactPhone,
		"delivery_address":       dAddr,
		"delivery_lat":           req.DeliveryLat,
		"delivery_lng":           req.DeliveryLng,
		"delivery_contact_name":  req.DeliveryContactName,
		"delivery_contact_phone": req.DeliveryContactPhone,
		"scheduled_at":           req.ScheduledAt,
		"cargo_description":      req.CargoDescription,
		"special_instructions":   req.SpecialInstructions,
		"payment_status":         "pending",
		"payment_method":         req.PaymentMethod,
	}

	if err := s.repo.CreateBookingWithDetails(ctx, tenantID, bookingMap, detailsMap); err != nil {
		return nil, fmt.Errorf("failed persisting booking: %w", err)
	}

	return &CreateBookingResponse{
		BookingID:     bookingID,
		BookingNumber: bookingNumber,
		Status:        "confirmed",
		TotalPrice:    quote.TotalPrice,
		IsIdempotent:  false,
	}, nil
}

func (s *CustomerAppService) CancelBooking(ctx context.Context, tenantID, customerID, bookingID, reason string) error {
	if tenantID == "" || customerID == "" || bookingID == "" {
		return errors.New("tenant_id, customer_id, and booking_id are required")
	}
	return s.repo.CancelCustomerBooking(ctx, tenantID, customerID, bookingID, reason)
}

func (s *CustomerAppService) GetBookingTracking(ctx context.Context, tenantID, customerID, bookingID string) (*domain.CustomerBookingTrackingProjection, error) {
	if tenantID == "" || customerID == "" || bookingID == "" {
		return nil, errors.New("tenant_id, customer_id, and booking_id are required")
	}
	return s.repo.GetCustomerTrackingProjection(ctx, tenantID, customerID, bookingID)
}

func (s *CustomerAppService) ListBookings(ctx context.Context, tenantID, customerID string, limit, offset int) ([]domain.CustomerBookingTrackingProjection, error) {
	if tenantID == "" || customerID == "" {
		return nil, errors.New("tenant_id and customer_id are required")
	}
	return s.repo.ListCustomerBookings(ctx, tenantID, customerID, limit, offset)
}
