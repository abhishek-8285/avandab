package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// BackhaulHandlers handles HTTP endpoints for the backhaul matching engine.
type BackhaulHandlers struct {
	*App
	BackhaulSvc *service.BackhaulService
}

// RegisterAPIRoutes registers backhaul endpoints with Chi.
func (h *BackhaulHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/backhaul", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "trips", "read")).Get("/matches", h.APIGetMatches)
		r.With(middleware.RequirePermission(h.AuthSrv, "trips", "update")).Post("/offers", h.APICreateOffer)
	})
}

func writeBackhaulJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeBackhaulError(w http.ResponseWriter, code int, msg string) {
	writeBackhaulJSON(w, code, map[string]string{"error": msg})
}

// APIGetMatches finds candidate backhaul matches for a trip nearing or at delivery.
func (h *BackhaulHandlers) APIGetMatches(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeBackhaulError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	tripID := strings.TrimSpace(r.URL.Query().Get("trip_id"))
	if tripID == "" {
		writeBackhaulError(w, http.StatusBadRequest, "trip_id is required")
		return
	}

	radiusKm := 50.0
	if rParam := strings.TrimSpace(r.URL.Query().Get("radius_km")); rParam != "" {
		if val, err := strconv.ParseFloat(rParam, 64); err == nil && val > 0 {
			radiusKm = val
		}
	}

	svc := h.BackhaulSvc
	if svc == nil {
		svc = service.NewBackhaulService(h.DB)
	}

	matches, err := svc.FindBackhaulMatches(r.Context(), tenantID, tripID, radiusKm)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeBackhaulError(w, http.StatusNotFound, err.Error())
			return
		}
		writeBackhaulError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeBackhaulJSON(w, http.StatusOK, matches)
}

// APICreateOffer dispatches an offer for a driver on a completing trip.
func (h *BackhaulHandlers) APICreateOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeBackhaulError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	var req service.BackhaulOfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBackhaulError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if strings.TrimSpace(req.TripID) == "" {
		writeBackhaulError(w, http.StatusBadRequest, "trip_id is required")
		return
	}
	if strings.TrimSpace(req.BookingID) == "" {
		writeBackhaulError(w, http.StatusBadRequest, "booking_id is required")
		return
	}

	svc := h.BackhaulSvc
	if svc == nil {
		svc = service.NewBackhaulService(h.DB)
	}

	resp, err := svc.CreateBackhaulOffer(r.Context(), tenantID, req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			writeBackhaulError(w, http.StatusNotFound, errMsg)
			return
		}
		if strings.Contains(errMsg, "already been accepted") || strings.Contains(errMsg, "conflict") {
			writeBackhaulError(w, http.StatusConflict, errMsg)
			return
		}
		if strings.Contains(errMsg, "blocked") || strings.Contains(errMsg, "maintenance") {
			writeBackhaulError(w, http.StatusUnprocessableEntity, errMsg)
			return
		}
		writeBackhaulError(w, http.StatusBadRequest, errMsg)
		return
	}

	writeBackhaulJSON(w, http.StatusCreated, resp)
}
