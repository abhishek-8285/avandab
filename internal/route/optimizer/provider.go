package optimizer

import (
	"context"
	"time"
)

// Optimizer is the VRP contract. Every routing backend implements this.
// The API/handler layer never branches on provider — it calls optimizer.Solve.
type Optimizer interface {
	// Name returns canonical id: "mock" | "osrm-public" | "osrm-selfhost" | "osrm"
	Name() string
	// Solve computes routes. Must be deterministic for same input.
	Solve(ctx context.Context, input OptimizationInput) (OptimizationOutput, error)
}

// Shipment is a delivery/request to route.
type Shipment struct {
	ID              string   `json:"id"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	Demand          float64  `json:"demand,omitempty"`            // capacity units
	TimeWindowStart *string  `json:"time_window_start,omitempty"` // RFC3339
	TimeWindowEnd   *string  `json:"time_window_end,omitempty"`
	Skills          []string `json:"skills,omitempty"`
}

// Vehicle is a routing vehicle.
type Vehicle struct {
	ID       string   `json:"id"`
	StartLat float64  `json:"start_lat"`
	StartLng float64  `json:"start_lng"`
	EndLat   *float64 `json:"end_lat,omitempty"`
	EndLng   *float64 `json:"end_lng,omitempty"`
	Capacity float64  `json:"capacity,omitempty"`
	Skills   []string `json:"skills,omitempty"`
}

// Constraints holds VRP constraints.
type Constraints struct {
	MaxShipmentsPerVehicle *int     `json:"max_shipments_per_vehicle,omitempty"`
	MaxRouteDurationMin    *int     `json:"max_route_duration_min,omitempty"`
	TerrainAvoid           []string `json:"terrain_avoid,omitempty"`
}

// OptimizationInput is the job input stored as input_json.
type OptimizationInput struct {
	Shipments   []Shipment  `json:"shipments"`
	Vehicles    []Vehicle   `json:"vehicles"`
	Constraints Constraints `json:"constraints,omitempty"`
}

// RouteLeg is one leg of an optimized route.
type RouteLeg struct {
	ShipmentID  string  `json:"shipment_id"`
	DistanceKM  float64 `json:"distance_km"`
	DurationMin float64 `json:"duration_min"`
	Sequence    int     `json:"sequence"`
}

// OptimizedRoute is one vehicle's planned sequence.
type OptimizedRoute struct {
	VehicleID string     `json:"vehicle_id"`
	Legs      []RouteLeg `json:"legs"`
	Geometry  string     `json:"geometry,omitempty"`
	TotalKM   float64    `json:"total_km"`
	TotalMin  float64    `json:"total_min"`
}

// OptimizationOutput is stored as result_json.
type OptimizationOutput struct {
	Routes       []OptimizedRoute `json:"routes"`
	TotalCost    float64          `json:"total_cost"`
	TotalKM      float64          `json:"total_km"`
	SolveTimeMS  int64            `json:"solve_time_ms"`
	ProviderName string           `json:"provider_name"`
	CreatedAt    time.Time        `json:"created_at"`
}

// Validate checks input invariants before solving.
func (in OptimizationInput) Validate() error {
	if len(in.Shipments) == 0 {
		return ErrNoShipments
	}
	if len(in.Shipments) > 50 {
		return ErrTooManyShipments
	}
	if len(in.Vehicles) == 0 {
		return ErrNoVehicles
	}
	return nil
}
