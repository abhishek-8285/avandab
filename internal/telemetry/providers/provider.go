package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// RawFrame is the provider-neutral input unit. Every front door (MQTT, HTTP,
// webhook, poll) must produce RawFrame; the canonical pipeline consumes only this.
type RawFrame struct {
	IMEI        string    `json:"imei"`
	DeviceTime  time.Time `json:"device_time"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	Speed       float64   `json:"speed,omitempty"`
	Heading     float64   `json:"heading,omitempty"`
	Ignition    *bool     `json:"ignition,omitempty"`
	EngineHours *float64  `json:"engine_hours,omitempty"`
	Accuracy    *float64  `json:"accuracy,omitempty"`
	FuelLevel   *float64  `json:"fuel_level,omitempty"`
	Odometer    *float64  `json:"odometer,omitempty"`
	// Provider-parity signals (migration 00117, Spec 17 §5). Each has a named
	// consumer: Valid gates vehicle_latest_position/snapshot overwrites so a
	// drifted fix can never corrupt the live map, Motion drives parked-dedup,
	// BatteryLevel/ExternalVoltage/GSMSignal feed device-health/tamper
	// alerting, Satellites qualifies the GNSS fix.
	Satellites      *int            `json:"satellites,omitempty"`
	BatteryLevel    *float64        `json:"battery_level,omitempty"`
	ExternalVoltage *float64        `json:"external_voltage,omitempty"`
	GSMSignal       *int            `json:"gsm_signal,omitempty"`
	Motion          *bool           `json:"motion,omitempty"`
	Valid           *bool           `json:"valid,omitempty"`
	FixTime         *time.Time      `json:"fix_time,omitempty"`
	DriverID        string          `json:"driver_id,omitempty"`
	TripID          string          `json:"trip_id,omitempty"`
	SOS             bool            `json:"sos,omitempty"`
	Provider        string          `json:"provider"`
	ProviderMsgID   string          `json:"provider_msg_id,omitempty"`
	RawPayload      json.RawMessage `json:"raw_payload,omitempty"`
}

// TelematicsProvider is the unified contract. "own" devices and every
// third-party source implement this. The pipeline never knows the source.
type TelematicsProvider interface {
	Name() string
	VerifySignature(rawBody []byte, header http.Header) error
	HandleWebhook(ctx context.Context, rawBody []byte) ([]RawFrame, error)
	Poll(ctx context.Context, since time.Time) ([]RawFrame, error)
}
