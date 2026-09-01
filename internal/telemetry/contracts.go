package telemetry

import (
	"time"

	"transport-app/internal/telemetry/providers"
)

// RawFrame is the provider-neutral input unit (type alias from providers
// package to avoid a circular dependency between telemetry and providers).
type RawFrame = providers.RawFrame

// Event type constants — exact strings stored in outbox_events.event_type.
// The relay derives these via getEventTypeName; consumers must match them.
const (
	EventTypePosition = "PositionEvent"
	EventTypeAlert    = "AlertEvent"
	EventTypeSOS      = "SOSEvent"
)

// Alert kind enum — device/provider-reported alert types.
// Rule-evaluated alerts (speeding, deviation, fuel-drop) belong to the
// alerting spec (migration 00059), not the ingestion layer.
const (
	AlertKindSpeeding   = "speeding"
	AlertKindFuelTheft  = "fuel_theft"
	AlertKindDTC        = "dtc"
	AlertKindTamper     = "tamper"
	AlertKindPowerCut   = "power_cut"
	AlertKindSOS        = "sos"
	AlertKindGeofence   = "geofence_breach"
	AlertKindNightDrive = "night_driving"
	AlertKindOverSpeed  = "overspeed"
	AlertKindIdle       = "idling"
	AlertKindHarshBrake = "harsh_braking"
	AlertKindHarshAccel = "harsh_acceleration"
	AlertKindTow        = "tow"
	// Device-health kinds (migration 00117) — emitted by the ingestion
	// deviceHealthGuard on healthy→unhealthy transitions.
	AlertKindLowBattery = "low_battery"
	AlertKindPoorSignal = "poor_signal"
)

// Alert severity levels.
const (
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// PositionEvent — published once per accepted frame. Canonical; exact field
// names and JSON tags are contractual for all consuming specs.
type PositionEvent struct {
	EventID     string    `json:"event_id"`
	TenantID    string    `json:"tenant_id"`
	DeviceIMEI  string    `json:"device_imei"`
	VehicleID   string    `json:"vehicle_id"`
	DriverID    string    `json:"driver_id,omitempty"`
	TripID      string    `json:"trip_id,omitempty"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	Speed       float64   `json:"speed"`
	Heading     float64   `json:"heading,omitempty"`
	Ignition    *bool     `json:"ignition,omitempty"`
	EngineHours *float64  `json:"engine_hours,omitempty"`
	Accuracy    *float64  `json:"accuracy,omitempty"`
	FuelLevel   *float64  `json:"fuel_level,omitempty"`
	Odometer    *float64  `json:"odometer,omitempty"`
	DeviceTime  time.Time `json:"device_time"`
	ReceivedAt  time.Time `json:"received_at"`
}

// AlertEvent — ingestion layer only relays device/provider-reported alerts
// (e.g. LocoNav DTC, fuel-theft kinds; own-device tamper). Rule evaluation
// (speeding, deviation, fuel-drop) belongs to the alerting spec (00059).
type AlertEvent struct {
	EventID    string    `json:"event_id"`
	TenantID   string    `json:"tenant_id"`
	DeviceIMEI string    `json:"device_imei"`
	VehicleID  string    `json:"vehicle_id"`
	DriverID   string    `json:"driver_id,omitempty"`
	TripID     string    `json:"trip_id,omitempty"`
	AlertType  string    `json:"alert_type"`
	Severity   string    `json:"severity"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	Details    string    `json:"details"`
	OccurredAt time.Time `json:"occurred_at"`
}

// SOSEvent — panic button / crash detection on own devices.
type SOSEvent struct {
	EventID    string    `json:"event_id"`
	TenantID   string    `json:"tenant_id"`
	DeviceIMEI string    `json:"device_imei"`
	VehicleID  string    `json:"vehicle_id"`
	DriverID   string    `json:"driver_id,omitempty"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	OccurredAt time.Time `json:"occurred_at"`
	DeviceTime time.Time `json:"device_time"`
}

// IngestResult describes the outcome of processing a single RawFrame.
type IngestResult struct {
	Accepted    bool   // frame was accepted into the pipeline
	Deduped     bool   // frame was a replay (provider_msg_id already seen)
	Quarantined bool   // frame was quarantined (unknown/retired/quarantined device)
	Reason      string // quarantine reason (empty if not quarantined)
}

// Telemetry event types published to the in-memory bus for real-time consumers
// (SSE hub in Spec 04). These are distinct from outbox event_type strings.
const (
	BusEventTelemetrySnapshot = "telemetry.snapshot"
	BusEventPositionEvent     = "PositionEvent"
	BusEventTelemetryAlert    = "telemetry.alert"
)
