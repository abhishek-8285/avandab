package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/shared"
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

	// Step 3b: Mobile driver topic — payload driver_id must match the topic,
	// then resolve the driver's own tenant and auto-provision the synthetic
	// device (Decision D3). MQTT carries no session, so the tenant always
	// comes from the database, never from the payload; an unknown driver
	// keeps the plain context and the frame is quarantined as unknown.
	//
	// canonicalDriver is the drivers.id the position row may actually FK to
	// (see step 4): the app publishes a driver code or auth user id, neither
	// of which is that key.
	var canonicalDriver string
	if isDriverGPSTopic(topic) {
		if p.DriverID != "" && p.DriverID != imei {
			h.logger.Warn("MQTT driver_id mismatch", "topic_driver", imei, "payload_driver", p.DriverID)
			return
		}
		ctx, canonicalDriver = h.driverDeviceContext(ctx, imei)
	} else if p.DriverID != "" {
		canonicalDriver = h.driverRowID(ctx, p.DriverID)
	}

	// Step 4: Build RawFrame.
	valid := p.Valid
	if p.IsStale != nil && *p.IsStale {
		stale := false
		valid = &stale
	}
	frame := providers.RawFrame{
		IMEI:            imei,
		Latitude:        p.Latitude,
		Longitude:       p.Longitude,
		Speed:           p.Speed,
		Heading:         p.Heading,
		Ignition:        p.Ignition,
		EngineHours:     p.EngineHours,
		Accuracy:        p.Accuracy,
		FuelLevel:       p.FuelLevel,
		Odometer:        p.Odometer,
		Satellites:      p.Satellites,
		BatteryLevel:    p.BatteryLevel,
		ExternalVoltage: p.ExternalVoltage,
		GSMSignal:       p.GSMSignal,
		Motion:          p.Motion,
		Valid:           valid,
		// telemetry_positions.driver_id FKs to drivers.id. Writing the raw
		// payload value (driver code / auth user id) fails the insert under
		// PRAGMA foreign_keys=ON — production's connection — and silently
		// drops the frame, so only a value that IS a drivers.id survives.
		DriverID:   canonicalDriver,
		TripID:     p.TripID,
		SOS:        p.SOS,
		Provider:   "own",
		RawPayload: payload,
	}

	// Raw dedup is UNIQUE(imei, provider_msg_id) WHERE provider_msg_id IS NOT
	// NULL. The mobile app sends no sequence, so a constant "mqtt:0" would
	// collide with itself and reject every frame after the first — leave it
	// empty (NULL, outside the index) and let the moving/parked deadband in
	// Step 7 decide what is worth storing.
	if p.Seq != 0 {
		frame.ProviderMsgID = fmt.Sprintf("mqtt:%d", p.Seq)
	}

	// Mobile publishes `timestamp`; hardware publishes `device_time`.
	deviceTime := p.DeviceTime
	if deviceTime == "" {
		deviceTime = p.Timestamp
	}
	if deviceTime != "" {
		if t, err := parseDeviceTime(deviceTime); err == nil {
			frame.DeviceTime = t
		}
	}

	// Step 5: Ingest through canonical async pipeline.
	if err := h.ingestor.IngestAsync(ctx, frame); err != nil {
		h.logger.Error("MQTT pipeline error", "imei", imei, "error", err)
		return
	}

	// Step 6: SOS detection log
	if p.SOS {
		h.logger.Warn("MQTT SOS received", "imei", imei,
			"lat", frame.Latitude, "lng", frame.Longitude)
	}
}

// driverDeviceContext pins a driver-topic frame to the driver's own tenant and
// auto-provisions the synthetic mobile device (Decision D3) so the pipeline
// accepts it instead of quarantining an unknown IMEI. It returns the drivers.id
// to store on the position row ("" when identity came from users — there is no
// drivers row to FK to).
//
// There is no auth session on an MQTT callback, so the tenant is read from the
// database — never from the payload. Identity can be a driver code
// (drivers.driver_id / drivers.id) or, until a driver row exists, the auth user
// id the mobile publishes as (`driverId ?? id`, authStore). Production has an
// empty drivers table (verified 2026-09-27), so without the users arm every
// phone frame would quarantine. A value resolving to nothing leaves ctx
// untouched and the frame falls through to the unknown-device path.
func (h *MQTTIngestHandler) driverDeviceContext(ctx context.Context, driverID string) (context.Context, string) {
	var rowID, tenantID string
	if err := h.ingestor.deviceStore.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT id, tenant_id FROM (
			SELECT id, tenant_id FROM drivers WHERE driver_id = $1 OR id = $1
			UNION ALL
			SELECT '' AS id, tenant_id FROM users WHERE id = $1
		) LIMIT 1`, driverID).Scan(&rowID, &tenantID); err != nil || tenantID == "" {
		return ctx, ""
	}
	ctx = shared.ContextWithTenantID(ctx, shared.TenantID(tenantID))
	h.ingestor.ensureSyntheticDevice(ctx, driverID)
	return ctx, rowID
}

// driverRowID maps a published driver identity onto the drivers.id that
// telemetry_positions.driver_id may reference, or "" when no such row exists.
func (h *MQTTIngestHandler) driverRowID(ctx context.Context, driverID string) string {
	var rowID string
	_ = h.ingestor.deviceStore.dbFromContext(ctx).QueryRowContext(ctx,
		`SELECT id FROM drivers WHERE id = $1 OR driver_id = $1 LIMIT 1`, driverID).Scan(&rowID)
	return rowID
}

// mqttPayload is the JSON structure published by own GPS devices on the
// canonical topic avandab/telemetry/devices/{imei}/gps. The mobile app uses
// the same shape on avandab/telemetry/drivers/{driverId}/gps (no imei/seq).
type mqttPayload struct {
	IMEI        string   `json:"imei,omitempty"`
	Seq         int64    `json:"seq"`
	DeviceTime  string   `json:"device_time"`
	Timestamp   string   `json:"timestamp,omitempty"` // mobile app (RFC3339)
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	Speed       float64  `json:"speed"`
	Heading     float64  `json:"heading"`
	Ignition    *bool    `json:"ignition,omitempty"`
	EngineHours *float64 `json:"engine_hours,omitempty"`
	Accuracy    *float64 `json:"accuracy,omitempty"`
	FuelLevel   *float64 `json:"fuel_level,omitempty"`
	Odometer    *float64 `json:"odometer,omitempty"`
	// Provider-parity signals (migration 00117): hardwired trackers report
	// voltage/satellites/GSM; app bridges may report phone battery.
	Satellites      *int     `json:"satellites,omitempty"`
	BatteryLevel    *float64 `json:"battery_level,omitempty"`
	ExternalVoltage *float64 `json:"external_voltage,omitempty"`
	GSMSignal       *int     `json:"gsm_signal,omitempty"`
	Motion          *bool    `json:"motion,omitempty"`
	Valid           *bool    `json:"valid,omitempty"`
	// IsStale marks a last-known (not live) fix from the mobile app; it
	// forces Valid=false below so stale fixes stay in history, never live.
	IsStale  *bool  `json:"is_stale,omitempty"`
	DriverID string `json:"driver_id,omitempty"`
	TripID   string `json:"trip_id,omitempty"`
	SOS      bool   `json:"sos"`
}

// extractIMEIFromTopic parses the two GPS topic shapes the broker bridges:
//
//	avandab/telemetry/devices/{imei}/gps     — hardware + synthetic device IMEI
//	avandab/telemetry/drivers/{driverId}/gps — mobile app
//
// On the driver topic the driver_id doubles as the device identity (Decision
// D3, same fallback as HandleTelemetrySync). It used to return "" here, so
// every phone-published fix was dropped as "invalid topic format".
func extractIMEIFromTopic(topic string) string {
	parts := strings.Split(topic, "/")
	// Expected: ["avandab", "telemetry", "devices"|"drivers", "{id}", "gps"]
	if len(parts) == 5 && parts[0] == "avandab" && parts[1] == "telemetry" &&
		(parts[2] == "devices" || parts[2] == "drivers") && parts[4] == "gps" {
		return parts[3]
	}
	return ""
}

// isDriverGPSTopic reports whether the message arrived on the mobile-app
// topic (avandab/telemetry/drivers/{driverId}/gps).
func isDriverGPSTopic(topic string) bool {
	return strings.HasPrefix(topic, "avandab/telemetry/drivers/") && strings.HasSuffix(topic, "/gps")
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
