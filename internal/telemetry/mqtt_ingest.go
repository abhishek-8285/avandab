package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/telemetry/providers"
)

// MQTTIngestHandler processes MQTT messages from own GPS devices.
type MQTTIngestHandler struct {
	ingestor *Ingestor
	logger   *slog.Logger
}

// NewMQTTIngestHandler constructs an MQTTIngestHandler.
func NewMQTTIngestHandler(ingestor *Ingestor, logger *slog.Logger) *MQTTIngestHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &MQTTIngestHandler{ingestor: ingestor, logger: logger}
}

// HandleMessage implements mqttservice.TelemetryHandler — the callback invoked
// by mqttservice for each canonical GPS message. The topic format is
// avandab/telemetry/devices/{imei}/gps.
func (h *MQTTIngestHandler) HandleMessage(ctx context.Context, topic string, payload []byte) {
	// Step 1: Extract IMEI from topic.
	imei := extractIMEIFromTopic(topic)
	if imei == "" {
		h.logger.Warn("MQTT invalid topic format", "topic", topic)
		return
	}

	// Step 2: Parse payload.
	var p mqttPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		h.logger.Warn("MQTT invalid JSON", "imei", imei, "error", err)
		return
	}

	// Step 3: Spoof guard — payload IMEI (if present) must match topic IMEI.
	if p.IMEI != "" && p.IMEI != imei {
		h.logger.Warn("MQTT IMEI mismatch", "topic_imei", imei, "payload_imei", p.IMEI)
		_ = h.ingestor.auditSpoof(ctx, imei, p.IMEI)
		return
	}

	// Step 4: Build RawFrame.
	frame := providers.RawFrame{
		IMEI:          imei,
		Latitude:      p.Latitude,
		Longitude:     p.Longitude,
		Speed:         p.Speed,
		Heading:       p.Heading,
		Ignition:      p.Ignition,
		EngineHours:   p.EngineHours,
		Accuracy:      p.Accuracy,
		FuelLevel:     p.FuelLevel,
		Odometer:      p.Odometer,
		DriverID:      p.DriverID,
		TripID:        p.TripID,
		SOS:           p.SOS,
		Provider:      "own",
		ProviderMsgID: fmt.Sprintf("mqtt:%d", p.Seq),
		RawPayload:    payload,
	}

	if p.DeviceTime != "" {
		if t, err := parseDeviceTime(p.DeviceTime); err == nil {
			frame.DeviceTime = t
		}
	}

	// Step 5: Ingest through canonical pipeline.
	result, err := h.ingestor.IngestRawFrame(ctx, frame)
	if err != nil {
		h.logger.Error("MQTT pipeline error", "imei", imei, "error", err)
		return
	}

	if result.Quarantined {
		h.logger.Warn("MQTT device quarantined", "imei", imei, "reason", result.Reason)
		return
	}
	if result.Deduped {
		h.logger.Debug("MQTT frame deduped", "imei", imei, "provider_msg_id", frame.ProviderMsgID)
	}

	// Step 6: SOS detection — emission happens inside the ingest pipeline
	// (same outbox transaction as the position).
	if p.SOS {
		h.logger.Warn("MQTT SOS received", "imei", imei,
			"lat", frame.Latitude, "lng", frame.Longitude)
	}
}

// mqttPayload is the JSON structure published by own GPS devices on the
// canonical topic avandab/telemetry/devices/{imei}/gps.
type mqttPayload struct {
	IMEI        string   `json:"imei,omitempty"`
	Seq         int64    `json:"seq"`
	DeviceTime  string   `json:"device_time"`
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	Speed       float64  `json:"speed"`
	Heading     float64  `json:"heading"`
	Ignition    *bool    `json:"ignition,omitempty"`
	EngineHours *float64 `json:"engine_hours,omitempty"`
	Accuracy    *float64 `json:"accuracy,omitempty"`
	FuelLevel   *float64 `json:"fuel_level,omitempty"`
	Odometer    *float64 `json:"odometer,omitempty"`
	DriverID    string   `json:"driver_id,omitempty"`
	TripID      string   `json:"trip_id,omitempty"`
	SOS         bool     `json:"sos"`
}

// extractIMEIFromTopic parses "avandab/telemetry/devices/{imei}/gps" -> "{imei}".
func extractIMEIFromTopic(topic string) string {
	parts := strings.Split(topic, "/")
	// Expected: ["avandab", "telemetry", "devices", "{imei}", "gps"]
	if len(parts) == 5 && parts[0] == "avandab" && parts[1] == "telemetry" &&
		parts[2] == "devices" && parts[4] == "gps" {
		return parts[3]
	}
	return ""
}

// parseDeviceTime parses a device-time string (RFC3339 preferred).
func parseDeviceTime(s string) (time.Time, error) {
	// Prefer RFC3339; fall back to a bare date.
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z07:00", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unparseable device_time %q", s)
}
