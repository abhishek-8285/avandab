package domain

import (
	"time"
)

// ControlTowerStop represents one stop's projection in the dispatcher Control Tower.
type ControlTowerStop struct {
	ID              string     `json:"id"`
	TripID          string     `json:"trip_id"`
	StopSequence    int        `json:"stop_sequence"`
	StopType        string     `json:"stop_type"` // pickup, drop, intermediate
	LocationName    string     `json:"location_name"`
	Address         string     `json:"address"`
	Latitude        float64    `json:"latitude"`
	Longitude       float64    `json:"longitude"`
	GeofenceRadiusM float64    `json:"geofence_radius_m"`
	Status          string     `json:"status"` // pending, en_route, arrived, servicing, completed, skipped
	ActualArrival   *time.Time `json:"actual_arrival,omitempty"`
	ActualDeparture *time.Time `json:"actual_departure,omitempty"`
	RequiresPOD     bool       `json:"requires_pod"`
	RequiresOTP     bool       `json:"requires_otp"`
	PODSubmitted    bool       `json:"pod_submitted"`
	PODUrl          *string    `json:"pod_url,omitempty"`
	SignatureUrl    *string    `json:"signature_url,omitempty"`
	ConsigneeName   string     `json:"consignee_name,omitempty"`
	ConsigneePhone  string     `json:"consignee_phone,omitempty"`
}

// ControlTowerProgression summarizes stop completion and overall trip progress.
type ControlTowerProgression struct {
	TotalStops        int     `json:"total_stops"`
	CompletedStops    int     `json:"completed_stops"`
	ProgressPercent   float64 `json:"progress_percent"`
	AllStopsCompleted bool    `json:"all_stops_completed"`
}

// ControlTowerTelemetry captures real-time position and movement state.
type ControlTowerTelemetry struct {
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	Speed        *float64   `json:"speed,omitempty"`
	Heading      *float64   `json:"heading,omitempty"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	MarkerStatus string     `json:"marker_status"` // running, stopped, no_signal
	RemainingKM  *float64   `json:"remaining_km,omitempty"`
	RouteKM      *float64   `json:"route_km,omitempty"`
	EtaMin       *time.Time `json:"eta_min,omitempty"`
	EtaMax       *time.Time `json:"eta_max,omitempty"`
	EtaMethod    string     `json:"eta_method,omitempty"`
}

// ControlTowerSafety captures SOS, deviation, and safety alerts.
type ControlTowerSafety struct {
	HasActiveSOS       bool    `json:"has_active_sos"`
	ActiveAlertsCount  int     `json:"active_alerts_count"`
	LatestAlertType    string  `json:"latest_alert_type,omitempty"`
	IsDeviated         bool    `json:"is_deviated"`
	DeviationDistanceM float64 `json:"deviation_distance_m"`
}

// ControlTowerEWB captures E-Way Bill lifecycle state.
type ControlTowerEWB struct {
	EWBNumber  string     `json:"ewb_number,omitempty"`
	Status     string     `json:"status,omitempty"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`
}

// ControlTowerSyncState reports telemetry freshness and offline duration.
type ControlTowerSyncState struct {
	IsStale    bool       `json:"is_stale"`
	LastSyncAt *time.Time `json:"last_sync_at,omitempty"`
}

// DriverInfo contains driver identity.
type DriverInfo struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
}

// VehicleInfo contains vehicle identity.
type VehicleInfo struct {
	ID                 string `json:"id,omitempty"`
	VehicleNumber      string `json:"vehicle_number,omitempty"`
	RegistrationNumber string `json:"registration_number,omitempty"`
}

// ControlTowerTrip is the authoritative server-side projection of an active trip for Dispatcher / Control Tower.
type ControlTowerTrip struct {
	TripID      string                  `json:"trip_id"`
	TripNumber  string                  `json:"trip_number"`
	TenantID    string                  `json:"tenant_id"`
	BookingID   string                  `json:"booking_id,omitempty"`
	Status      string                  `json:"status"`
	StartTime   *time.Time              `json:"start_time,omitempty"`
	EndTime     *time.Time              `json:"end_time,omitempty"`
	Origin      string                  `json:"origin"`
	Destination string                  `json:"destination"`
	Driver      DriverInfo              `json:"driver"`
	Vehicle     VehicleInfo             `json:"vehicle"`
	Stops       []ControlTowerStop      `json:"stops"`
	CurrentStop *ControlTowerStop       `json:"current_stop,omitempty"`
	Progression ControlTowerProgression `json:"progression"`
	Telemetry   ControlTowerTelemetry   `json:"telemetry"`
	Safety      ControlTowerSafety      `json:"safety"`
	EWB         *ControlTowerEWB        `json:"ewb,omitempty"`
	SyncState   ControlTowerSyncState   `json:"sync_state"`
}
