package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/controltower/application"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// Handler serves Control Tower REST projection endpoints.
type Handler struct {
	service *application.Service
	authSrv auth.AuthorizationService
}

// NewHandler constructs a Control Tower REST handler.
func NewHandler(service *application.Service, authSrv auth.AuthorizationService) *Handler {
	return &Handler{
		service: service,
		authSrv: authSrv,
	}
}

// Register mounts the Control Tower API routes under /api/v1/control-tower.
func (h *Handler) Register(r chi.Router) {
	r.Route("/api/v1/control-tower", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.authSrv, "trips", "read")).Get("/trips", h.GetTrips)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "read")).Get("/trips/{id}", h.GetTrip)
	})
}

// GetTrips handles GET /api/v1/control-tower/trips.
func (h *Handler) GetTrips(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	statusFilter := r.URL.Query().Get("status")

	trips, err := h.service.GetTrips(r.Context(), tenantID, statusFilter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(trips)
}

// GetTrip handles GET /api/v1/control-tower/trips/{id}.
func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		tripID = r.URL.Query().Get("id")
	}
	if tripID == "" {
		http.Error(w, "trip id is required", http.StatusBadRequest)
		return
	}

	trip, err := h.service.GetTrip(r.Context(), tenantID, tripID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "trip not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(trip)
}
