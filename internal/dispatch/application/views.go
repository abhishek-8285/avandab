package application

import "time"

// StopView is one stop rendered in run detail.
type StopView struct {
	ID         string     `json:"id"`
	BookingID  string     `json:"booking_id,omitempty"`
	Type       string     `json:"type"`
	Address    string     `json:"address"`
	Lat        float64    `json:"lat"`
	Lng        float64    `json:"lng"`
	Demand     float64    `json:"demand"`
	Status     string     `json:"status"`
	Seq        int        `json:"seq"`
	PlannedETA *time.Time `json:"planned_eta,omitempty"`
}

// RouteView is one vehicle route in run detail.
type RouteView struct {
	ID        string     `json:"id"`
	VehicleID string     `json:"vehicle_id"`
	Seq       int        `json:"seq"`
	Stops     []StopView `json:"stops"`
}

// RunDetail is the full run payload (run + routes + stops + KPI) — spec §4
// GET /api/v1/dispatch/runs/{id}.
type RunDetail struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Source string      `json:"source"`
	KPI    PlanKPI     `json:"kpi"`
	Stops  []StopView  `json:"stops"`
	Routes []RouteView `json:"routes"`
}

// RunSummary is a list row for the runs board.
type RunSummary struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Source    string  `json:"source"`
	CreatedAt string  `json:"created_at"`
	KPI       PlanKPI `json:"kpi"`
}
