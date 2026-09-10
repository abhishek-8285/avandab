package fuel

import (
	"context"
	"time"
)

// FuelIssue represents an official departmental or commercial fuel dispensing
// transaction from a fuel station (FS/OFS) to a fleet vehicle (TMS_SOP p.9).
type FuelIssue struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	IssueNumber     *string   `json:"issue_number,omitempty"`
	FuelStationID   string    `json:"fuel_station_id"`
	PumpPointID     *string   `json:"pump_point_id,omitempty"`
	VehicleID       string    `json:"vehicle_id"`
	DriverID        *string   `json:"driver_id,omitempty"`
	TripID          *string   `json:"trip_id,omitempty"`
	FuelType        string    `json:"fuel_type"`
	OpeningReading  float64   `json:"opening_reading"`
	ClosingReading  float64   `json:"closing_reading"`
	LitresIssued    float64   `json:"litres_issued"`
	VehicleOdometer *float64  `json:"vehicle_odometer,omitempty"`
	RatePerLitre    *float64  `json:"rate_per_litre,omitempty"`
	TotalCost       *float64  `json:"total_cost,omitempty"`
	Remarks         string    `json:"remarks"`
	IssuedAt        time.Time `json:"issued_at"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// RecordFuelIssueRequest captures the input for recording a fuel issue entry.
type RecordFuelIssueRequest struct {
	IssueNumber     *string    `json:"issue_number"`
	FuelStationID   string     `json:"fuel_station_id"`
	PumpPointID     *string    `json:"pump_point_id"`
	VehicleID       string     `json:"vehicle_id"`
	DriverID        *string    `json:"driver_id"`
	TripID          *string    `json:"trip_id"`
	FuelType        string     `json:"fuel_type"`
	OpeningReading  float64    `json:"opening_reading"`
	ClosingReading  float64    `json:"closing_reading"`
	VehicleOdometer *float64   `json:"vehicle_odometer"`
	RatePerLitre    *float64   `json:"rate_per_litre"`
	Remarks         string     `json:"remarks"`
	IssuedAt        *time.Time `json:"issued_at"`
}

// FuelIssueFilter supports filtered listing of fuel issue logs.
type FuelIssueFilter struct {
	VehicleID     string
	FuelStationID string
	Limit         int
	Offset        int
}

// FuelIssueRepository defines the persistence contract for fuel issues.
type FuelIssueRepository interface {
	RecordFuelIssue(ctx context.Context, tenantID, actorID string, req RecordFuelIssueRequest) (*FuelIssue, error)
	ListFuelIssues(ctx context.Context, tenantID string, filter FuelIssueFilter) ([]FuelIssue, error)
	GetFuelIssue(ctx context.Context, tenantID, id string) (*FuelIssue, error)
}
