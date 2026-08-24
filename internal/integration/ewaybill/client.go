package ewaybill

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Config holds connection settings for the NIC E-way bill API.
type Config struct {
	Endpoint string
	APIKey   string
	Enabled  bool
	UseMock  bool
}

// GenerateRequest carries the inputs needed to create an E-way bill.
type GenerateRequest struct {
	DocumentNumber string  `json:"document_number"`
	FromGSTIN      string  `json:"from_gstin"`
	ToGSTIN        string  `json:"to_gstin"`
	TransporterID  string  `json:"transporter_id"`
	VehicleNumber  string  `json:"vehicle_number"`
	Distance       int     `json:"distance"`
	TaxAmount      float64 `json:"tax_amount"`
	TotalAmount    float64 `json:"total_amount"`
}

// EWayBill represents a generated or retrieved E-way bill.
type EWayBill struct {
	EwbNumber   string    `json:"ewb_number"`
	Status      string    `json:"status"`
	GeneratedAt time.Time `json:"generated_at"`
	ValidUpto   time.Time `json:"valid_upto"`
	QRCode      string    `json:"qr_code"`
	DocumentRef string    `json:"document_ref"`
}

// Cancellation represents the result of cancelling an E-way bill.
type Cancellation struct {
	EwbNumber   string    `json:"ewb_number"`
	CancelledAt time.Time `json:"cancelled_at"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
}

// ExtendRequest carries inputs needed to extend an E-way bill validity.
type ExtendRequest struct {
	EwbNumber         string `json:"ewb_number"`
	FromPlace         string `json:"from_place"`
	FromStateCode     string `json:"from_state_code"`
	RemainingDistance int    `json:"remaining_distance"`
	TransitToDate     string `json:"transit_to_date"`
	Reason            string `json:"reason"`
}

// Client defines operations supported by the NIC E-way bill API.
type Client interface {
	Generate(ctx context.Context, req GenerateRequest) (EWayBill, error)
	Get(ctx context.Context, ewbNumber string) (EWayBill, error)
	Cancel(ctx context.Context, ewbNumber, reason string) (Cancellation, error)

	// Lifecycle methods (Spec 07 §2.3-§2.7)
	GeneratePartA(ctx context.Context, req GenerateRequest) (EWayBill, error)
	AttachPartB(ctx context.Context, ewbNumber string, vehicleNumber string, transporterID string) (EWayBill, error)
	Extend(ctx context.Context, ewbNumber string, req ExtendRequest) (EWayBill, error)
	GetByNumber(ctx context.Context, ewbNumber string) (EWayBill, error)
	GetByTrip(ctx context.Context, tripID string) (EWayBill, error)
}

type stubClient struct {
	cfg Config
}

func (c *stubClient) Generate(ctx context.Context, req GenerateRequest) (EWayBill, error) {
	slog.Default().Info("[ewaybill] Generate called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "document", req.DocumentNumber)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	now := time.Now()
	return EWayBill{
		EwbNumber:   "EWB" + uuid.New().String()[:12],
		Status:      "ACTIVE",
		GeneratedAt: now,
		ValidUpto:   now.Add(24 * time.Hour),
		QRCode:      "data:image/png;base64,stubqrcode",
		DocumentRef: req.DocumentNumber,
	}, nil
}

func (c *stubClient) Get(ctx context.Context, ewbNumber string) (EWayBill, error) {
	slog.Default().Info("[ewaybill] Get called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "ewb_number", ewbNumber)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	return EWayBill{
		EwbNumber:   ewbNumber,
		Status:      "ACTIVE",
		GeneratedAt: time.Now().Add(-2 * time.Hour),
		ValidUpto:   time.Now().Add(22 * time.Hour),
		QRCode:      "data:image/png;base64,stubqrcode",
		DocumentRef: "DOC/2026/0001",
	}, nil
}

func (c *stubClient) Cancel(ctx context.Context, ewbNumber, reason string) (Cancellation, error) {
	slog.Default().Info("[ewaybill] Cancel called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "ewb_number", ewbNumber, "reason", reason)
	if !c.cfg.Enabled {
		return Cancellation{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return Cancellation{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	return Cancellation{
		EwbNumber:   ewbNumber,
		CancelledAt: time.Now(),
		Reason:      reason,
		Status:      "CANCELLED",
	}, nil
}

func (c *stubClient) GeneratePartA(ctx context.Context, req GenerateRequest) (EWayBill, error) {
	slog.Default().Info("[ewaybill] GeneratePartA called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "document", req.DocumentNumber)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	now := time.Now()
	uid := uuid.New().String()
	if len(uid) > 8 {
		uid = uid[:8]
	}
	return EWayBill{
		EwbNumber:   "EWB-MOCK-" + uid,
		Status:      "PART_A_GENERATED",
		GeneratedAt: now,
		ValidUpto:   now.Add(24 * time.Hour),
		QRCode:      "data:image/png;base64,mockqrcode",
		DocumentRef: req.DocumentNumber,
	}, nil
}

func (c *stubClient) AttachPartB(ctx context.Context, ewbNumber, vehicleNumber, transporterID string) (EWayBill, error) {
	slog.Default().Info("[ewaybill] AttachPartB called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "ewb_number", ewbNumber, "vehicle", vehicleNumber)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	now := time.Now()
	return EWayBill{
		EwbNumber:   ewbNumber,
		Status:      "ACTIVE",
		GeneratedAt: now.Add(-1 * time.Hour),
		ValidUpto:   now.Add(23 * time.Hour),
		QRCode:      "data:image/png;base64,mockqrcode",
		DocumentRef: "DOC/2026/0001",
	}, nil
}

func (c *stubClient) Extend(ctx context.Context, ewbNumber string, req ExtendRequest) (EWayBill, error) {
	slog.Default().Info("[ewaybill] Extend called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "ewb_number", ewbNumber, "reason", req.Reason)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	now := time.Now()
	return EWayBill{
		EwbNumber:   ewbNumber,
		Status:      "EXTENDED",
		GeneratedAt: now.Add(-24 * time.Hour),
		ValidUpto:   now.Add(24 * time.Hour),
		QRCode:      "data:image/png;base64,mockqrcode",
		DocumentRef: "DOC/2026/0001",
	}, nil
}

func (c *stubClient) GetByNumber(ctx context.Context, ewbNumber string) (EWayBill, error) {
	return c.Get(ctx, ewbNumber)
}

func (c *stubClient) GetByTrip(ctx context.Context, tripID string) (EWayBill, error) {
	slog.Default().Info("[ewaybill] GetByTrip called", "endpoint", c.cfg.Endpoint, "enabled", c.cfg.Enabled, "trip_id", tripID)
	if !c.cfg.Enabled {
		return EWayBill{}, fmt.Errorf("ewaybill integration disabled")
	}
	if !c.cfg.UseMock {
		return EWayBill{}, fmt.Errorf("ewaybill: NIC credentials not configured; set INTEGRATION_EWAYBILL_API_KEY or INTEGRATION_EWAYBILL_USE_MOCK=true for demo mode")
	}
	now := time.Now()
	return EWayBill{
		EwbNumber:   "EWB-TRIP-" + tripID[:min(8, len(tripID))],
		Status:      "ACTIVE",
		GeneratedAt: now.Add(-2 * time.Hour),
		ValidUpto:   now.Add(22 * time.Hour),
		QRCode:      "data:image/png;base64,mockqrcode",
		DocumentRef: "TRIP-" + tripID,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
