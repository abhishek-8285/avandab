package application

// Driver SOS/emergency trigger service, extracted from
// internal/handlers/sos.go (handlers-end-state ADR, Phase 2; Phase 8 §P3A,
// Spec 05 §8). The handler keeps HTTP auth/header fallback, JSON parsing,
// validation, time defaults and the response JSON; this file keeps the
// driver-identity rule and emergency tenant attribution, the active-trip
// lookup, the idempotent outbox_events write, the event-bus publish and the
// audit_logs trail.
//
// Legacy semantics preserved verbatim:
// - driver identity is id-or-user-email with NO tenant filter (the emergency
//   path must not drop a driver), falling back to the session user id when
//   the lookup fails;
// - a header-authenticated driver without tenant context is attributed to
//   their own org (never another org);
// - the outbox insert is idempotent by aggregate id (ON CONFLICT DO NOTHING):
//   only the first delivery publishes the SOS event and writes the audit row,
//   duplicates are acknowledged without re-alerting;
// - insert failures also render as a duplicate acknowledgment (rowsAffected 0).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/shared"
)

// SOSService persists driver SOS triggers to the outbox (idempotently),
// publishes the SOS event and writes the audit trail. It is stateless and
// cheap to construct per request.
type SOSService struct {
	db  *sql.DB
	bus events.EventBus
}

// NewSOSService wires the service against the database and shared event bus.
// A nil bus skips publishing (partially built apps in tests).
func NewSOSService(db *sql.DB, bus events.EventBus) *SOSService {
	return &SOSService{db: db, bus: bus}
}

// SOSTrigger carries the resolved SOS fields for persistence and the event
// payload. Field names/order mirror the legacy outbox payload map.
type SOSTrigger struct {
	SOSID        string
	TenantID     string
	DriverID     string
	VehicleID    string
	TripID       string
	Latitude     float64
	Longitude    float64
	Accuracy     float64
	BatteryLevel float64
	Reason       string
	TriggeredAt  time.Time
	ReceivedAt   time.Time
}

// NewSOSID builds the legacy SOS id: sos_<command_id> when the client supplied
// an idempotency key, else sos_<uuid>.
func NewSOSID(commandID string) string {
	if commandID != "" {
		return "sos_" + commandID
	}
	return "sos_" + uuid.NewString()
}

// DriverForUser maps the authenticated user session to their driver record
// (legacy rule: drivers.id = user id OR drivers.email = user email, no tenant
// filter). On lookup failure it falls back to the user id as the driver
// identifier so an emergency is never dropped.
func (s *SOSService) DriverForUser(ctx context.Context, userID string) (string, bool) {
	if s.db == nil {
		return userID, true
	}
	var driverID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM drivers
		WHERE id = $1 OR email = (SELECT email FROM users WHERE id = $2)
		LIMIT 1`, userID, userID).Scan(&driverID)
	if err != nil {
		return userID, true // Fallback to UserID as driver identifier
	}
	return driverID, true
}

// DriverTenant returns the driver's own tenant for emergency attribution when
// the request carries no tenant context. Best-effort like the legacy code
// (errors leave the tenant empty).
func (s *SOSService) DriverTenant(ctx context.Context, driverID string) string {
	if s.db == nil {
		return ""
	}
	var tenantID string
	_ = s.db.QueryRowContext(ctx, `SELECT tenant_id FROM drivers WHERE id = $1`, driverID).Scan(&tenantID)
	return tenantID
}

// ActiveTrip returns the driver's newest active trip and vehicle (legacy
// statuses), best-effort: any lookup error leaves both empty.
func (s *SOSService) ActiveTrip(ctx context.Context, driverID string) (tripID, vehicleID string) {
	if s.db == nil {
		return "", ""
	}
	var activeTripID, activeVehicleID sql.NullString
	_ = s.db.QueryRowContext(ctx, `
		SELECT id, vehicle_id FROM trips
		WHERE driver_id = $1 AND status IN ('assigned', 'started', 'reached_pickup', 'in_transit')
		ORDER BY created_at DESC LIMIT 1`,
		driverID).Scan(&activeTripID, &activeVehicleID)
	return activeTripID.String, activeVehicleID.String
}

// Trigger persists the SOS event to outbox_events idempotently. Returns
// firstDelivery=true only when the row was inserted (the legacy rowsAffected
// gate): publish + audit run only then. The error is returned so callers can
// log it, but the legacy HTTP contract treats any failure as a duplicate
// acknowledgment (rowsAffected 0 => 200).
func (s *SOSService) Trigger(ctx context.Context, trig SOSTrigger) (bool, error) {
	eventPayload := map[string]interface{}{
		"SOSID":        trig.SOSID,
		"TenantID":     trig.TenantID,
		"DriverID":     trig.DriverID,
		"VehicleID":    trig.VehicleID,
		"TripID":       trig.TripID,
		"Latitude":     trig.Latitude,
		"Longitude":    trig.Longitude,
		"Accuracy":     trig.Accuracy,
		"BatteryLevel": trig.BatteryLevel,
		"Reason":       trig.Reason,
		"TriggeredAt":  trig.TriggeredAt,
		"ReceivedAt":   trig.ReceivedAt,
		"vehicle_id":   trig.VehicleID,
		"driver_id":    trig.DriverID,
		"trip_id":      trig.TripID,
		"latitude":     trig.Latitude,
		"longitude":    trig.Longitude,
	}
	if s.db == nil {
		return false, sql.ErrConnDone
	}
	payloadBytes, _ := json.Marshal(eventPayload)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at)
		VALUES ($1, $2, 'sos', $3, $4, CURRENT_TIMESTAMP)
		ON CONFLICT DO NOTHING
	`, "ob_"+trig.SOSID, trig.SOSID, events.SOSEvent, string(payloadBytes))

	rowsAffected := int64(0)
	if err == nil && res != nil {
		rowsAffected, _ = res.RowsAffected()
	}
	if rowsAffected == 0 {
		return false, err
	}

	// Only publish to the Event Bus and log audit on the first delivery of
	// this command id.
	if s.bus != nil {
		_ = s.bus.Publish(ctx, events.Event{
			Type:    events.SOSEvent,
			Payload: eventPayload,
		})
	}
	auditDetail := fmt.Sprintf("Driver SOS triggered at lat:%.6f, lng:%.6f, vehicle:%s, trip:%s", trig.Latitude, trig.Longitude, trig.VehicleID, trig.TripID)
	_ = s.writeAudit(ctx, trig.DriverID, trig.SOSID, auditDetail)
	return true, nil
}

// writeAudit appends the driver_sos_triggered audit row (legacy direct write:
// user_id is the driver id, entity/table "safety", record id the SOS id, and
// the tenant falls back to the platform tenant via shared.TenantOrPlatform).
func (s *SOSService) writeAudit(ctx context.Context, userID, sosID, details string) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, tenant_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
	`, "aud_"+uuid.NewString(), userID, "driver_sos_triggered", "safety", sosID, details, string(shared.TenantOrPlatform(ctx)))
	return err
}
