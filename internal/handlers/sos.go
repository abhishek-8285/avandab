package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/httpx"
	"transport-app/internal/shared"
)

// SOSRequest represents the payload sent from the driver mobile app or panic button.
type SOSRequest struct {
	CommandID    string    `json:"command_id,omitempty"`
	TripID       string    `json:"trip_id,omitempty"`
	VehicleID    string    `json:"vehicle_id,omitempty"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	Accuracy     float64   `json:"accuracy,omitempty"`
	BatteryLevel float64   `json:"battery_level,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	TriggeredAt  time.Time `json:"triggered_at,omitempty"`
	OccurredAt   time.Time `json:"occurred_at,omitempty"`
}

// SOSResponse represents the acknowledgment payload returned to the mobile client.
type SOSResponse struct {
	Status     string    `json:"status"`
	SOSID      string    `json:"sos_id"`
	DriverID   string    `json:"driver_id"`
	VehicleID  string    `json:"vehicle_id"`
	ReceivedAt time.Time `json:"received_at"`
	Message    string    `json:"message"`
}

// SOSHandlers handles emergency driver SOS alerts and forwards them to the Outbox & Alerting pipeline.
type SOSHandlers struct {
	app *App
	db  *sql.DB
}

// NewSOSHandlers constructs a new SOSHandlers instance.
func NewSOSHandlers(app *App, db *sql.DB) *SOSHandlers {
	return &SOSHandlers{
		app: app,
		db:  db,
	}
}

// Mount registers the SOS endpoints on the router.
func (h *SOSHandlers) Mount(r chi.Router) {
	r.Post("/api/sos", h.TriggerSOS)
	r.Post("/api/v1/sos", h.TriggerSOS)
}

// driverIDForUser maps the authenticated user session to their driver record.
func (h *SOSHandlers) driverIDForUser(r *http.Request) (string, bool) {
	session, ok := h.app.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		return "", false
	}

	var driverID string
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id FROM drivers
		WHERE id = ? OR email = (SELECT email FROM users WHERE id = ?)
		LIMIT 1`, session.UserID, session.UserID).Scan(&driverID)
	if err != nil {
		return session.UserID, true // Fallback to UserID as driver identifier
	}
	return driverID, true
}

// TriggerSOS handles emergency panic trigger from mobile client (Phase 8 §P3A).
func (h *SOSHandlers) TriggerSOS(w http.ResponseWriter, r *http.Request) {
	driverID, ok := h.driverIDForUser(r)
	if !ok {
		// If unauthenticated session, check if Driver-ID header provided
		if headerID := r.Header.Get("X-Driver-ID"); headerID != "" {
			driverID = headerID
		} else {
			httpx.JSON(w, http.StatusUnauthorized, map[string]string{
				"error":   "unauthorized",
				"message": "driver authentication required to trigger SOS",
			})
			return
		}
	}

	var req SOSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "invalid JSON body for SOS",
		})
		return
	}

	now := time.Now().UTC()
	if req.TriggeredAt.IsZero() {
		if !req.OccurredAt.IsZero() {
			req.TriggeredAt = req.OccurredAt
		} else {
			req.TriggeredAt = now
		}
	}

	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	// Resolve active trip and vehicle if omitted in mobile request
	if req.TripID == "" || req.VehicleID == "" {
		var activeTripID, activeVehicleID sql.NullString
		_ = h.db.QueryRowContext(r.Context(), `
			SELECT id, vehicle_id FROM trips
			WHERE driver_id = ? AND status IN ('assigned', 'started', 'reached_pickup', 'in_transit')
			ORDER BY created_at DESC LIMIT 1`,
			driverID).Scan(&activeTripID, &activeVehicleID)
		if req.TripID == "" && activeTripID.Valid {
			req.TripID = activeTripID.String
		}
		if req.VehicleID == "" && activeVehicleID.Valid {
			req.VehicleID = activeVehicleID.String
		}
	}

	if req.Reason == "" {
		req.Reason = "Emergency SOS Triggered from Driver Mobile App"
	}

	sosID := "sos_" + uuid.NewString()
	if req.CommandID != "" {
		sosID = "sos_" + req.CommandID
	}

	// 1. Persist to outbox_events table idempotently
	eventPayload := map[string]interface{}{
		"SOSID":        sosID,
		"TenantID":     tenantID,
		"DriverID":     driverID,
		"VehicleID":    req.VehicleID,
		"TripID":       req.TripID,
		"Latitude":     req.Latitude,
		"Longitude":    req.Longitude,
		"Accuracy":     req.Accuracy,
		"BatteryLevel": req.BatteryLevel,
		"Reason":       req.Reason,
		"TriggeredAt":  req.TriggeredAt,
		"ReceivedAt":   now,
		"vehicle_id":   req.VehicleID,
		"driver_id":    driverID,
		"trip_id":      req.TripID,
		"latitude":     req.Latitude,
		"longitude":    req.Longitude,
	}

	payloadBytes, _ := json.Marshal(eventPayload)
	res, err := h.db.ExecContext(r.Context(), `
		INSERT OR IGNORE INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at)
		VALUES (?, ?, 'sos', ?, ?, datetime('now'))
	`, "ob_"+sosID, sosID, events.SOSEvent, string(payloadBytes))

	rowsAffected := int64(0)
	if err == nil && res != nil {
		rowsAffected, _ = res.RowsAffected()
	}

	// Only publish to Event Bus and log audit if this is the first delivery of this command_id
	if rowsAffected > 0 {
		// 2. Publish to Event Bus for synchronous/asynchronous Alert Pipeline processing
		if h.app != nil && h.app.Services != nil && h.app.Services.Events != nil {
			h.app.Services.Events.Publish(r.Context(), events.Event{
				Type:    events.SOSEvent,
				Payload: eventPayload,
			})
		}

		// 3. Log Audit Trail
		auditDetail := fmt.Sprintf("Driver SOS triggered at lat:%.6f, lng:%.6f, vehicle:%s, trip:%s", req.Latitude, req.Longitude, req.VehicleID, req.TripID)
		_ = h.app.logAuditDirect(r.Context(), driverID, "driver_sos_triggered", "safety", sosID, auditDetail)
	}

	status := http.StatusCreated
	if rowsAffected == 0 {
		status = http.StatusOK // Idempotent duplicate acknowledgment
	}

	httpx.JSON(w, status, SOSResponse{
		Status:     "acknowledged",
		SOSID:      sosID,
		DriverID:   driverID,
		VehicleID:  req.VehicleID,
		ReceivedAt: now,
		Message:    "Emergency response team and dispatchers have been alerted.",
	})
}

func (a *App) logAuditDirect(ctx context.Context, userID, action, entityType, entityID, details string) error {
	if a.DB == nil {
		return nil
	}
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, user_id, action, entity_type, entity_id, new_values, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, "aud_"+uuid.NewString(), tenantID, userID, action, entityType, entityID, details)
	return err
}
