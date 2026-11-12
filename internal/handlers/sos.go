package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	alertsapp "transport-app/internal/alerts/application"
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

// eventBus returns the shared event bus when wired; nil-safe for partially
// built apps (the service skips publishing on a nil bus).
func (h *SOSHandlers) eventBus() events.EventBus {
	if h.app == nil || h.app.Services == nil {
		return nil
	}
	return h.app.Services.Events
}

// TriggerSOS handles emergency panic trigger from mobile client (Phase 8 §P3A).
func (h *SOSHandlers) TriggerSOS(w http.ResponseWriter, r *http.Request) {
	svc := alertsapp.NewSOSService(h.db, h.eventBus())

	driverID := ""
	authorized := false
	if session, ok := h.app.getUserFromContext(r); ok && session != nil && session.UserID != "" {
		driverID, authorized = svc.DriverForUser(r.Context(), session.UserID)
	}
	if !authorized {
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

	// Emergency path accepts header-auth drivers without a resolved tenant:
	// attribute via the driver's own org. Never another org, never dropped.
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		tenantID = svc.DriverTenant(r.Context(), driverID)
	}

	// Resolve active trip and vehicle if omitted in mobile request
	if req.TripID == "" || req.VehicleID == "" {
		tripID, vehicleID := svc.ActiveTrip(r.Context(), driverID)
		if req.TripID == "" {
			req.TripID = tripID
		}
		if req.VehicleID == "" {
			req.VehicleID = vehicleID
		}
	}

	if req.Reason == "" {
		req.Reason = "Emergency SOS Triggered from Driver Mobile App"
	}

	sosID := alertsapp.NewSOSID(req.CommandID)

	// Persist to outbox_events idempotently; only the first delivery of a
	// command id publishes the event + audit row and alerts responders.
	firstDelivery, _ := svc.Trigger(r.Context(), alertsapp.SOSTrigger{
		SOSID:        sosID,
		TenantID:     tenantID,
		DriverID:     driverID,
		VehicleID:    req.VehicleID,
		TripID:       req.TripID,
		Latitude:     req.Latitude,
		Longitude:    req.Longitude,
		Accuracy:     req.Accuracy,
		BatteryLevel: req.BatteryLevel,
		Reason:       req.Reason,
		TriggeredAt:  req.TriggeredAt,
		ReceivedAt:   now,
	})

	status := http.StatusCreated
	if !firstDelivery {
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
