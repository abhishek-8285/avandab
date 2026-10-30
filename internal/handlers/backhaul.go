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

// Routes registers web UI routes for backhaul under /backhaul.
func (h *BackhaulHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "trips", "read")).Get("/", h.WebBackhaulPage)
	r.With(middleware.RequirePermission(h.AuthSrv, "trips", "update")).Post("/offers", h.WebCreateOffer)
}

// BackhaulTripOption represents a summary of a trip available for backhaul matching.
type BackhaulTripOption struct {
	ID         string
	TripNumber string
	Status     string
}

// WebBackhaulPage renders the interactive backhaul corridor matching workspace.
func (h *BackhaulHandlers) WebBackhaulPage(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	svc := h.BackhaulSvc
	if svc == nil {
		svc = service.NewBackhaulService(h.DB)
	}

	var trips []BackhaulTripOption
	if h.DB != nil {
		rows, err := h.DB.QueryContext(r.Context(), `
			SELECT id, trip_number, status
			FROM trips
			WHERE tenant_id = $1 AND status IN ('scheduled', 'assigned', 'started', 'completed')
			ORDER BY created_at DESC
			LIMIT 50`, tenantID)
		if err == nil {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var t BackhaulTripOption
				if scanErr := rows.Scan(&t.ID, &t.TripNumber, &t.Status); scanErr == nil {
					trips = append(trips, t)
				}
			}
		}
	}

	selectedTripID := strings.TrimSpace(r.URL.Query().Get("trip_id"))
	if selectedTripID == "" && len(trips) > 0 {
		selectedTripID = trips[0].ID
	}

	radiusKm := 50.0
	if rParam := strings.TrimSpace(r.URL.Query().Get("radius_km")); rParam != "" {
		if val, err := strconv.ParseFloat(rParam, 64); err == nil && val > 0 {
			radiusKm = val
		}
	}

	var matches []service.BackhaulMatchDTO
	if selectedTripID != "" {
		if res, err := svc.FindBackhaulMatches(r.Context(), tenantID, selectedTripID, radiusKm); err == nil {
			matches = res
		}
	}

	var bestMargin float64
	var minDeadhead float64 = -1
	for _, m := range matches {
		if m.FreightMargin > bestMargin {
			bestMargin = m.FreightMargin
		}
		if minDeadhead < 0 || m.DeadheadKm < minDeadhead {
			minDeadhead = m.DeadheadKm
		}
	}
	if minDeadhead < 0 {
		minDeadhead = 0
	}

	data := PageData{
		Title: "Backhaul Matching",
		User:  user,
		Extra: map[string]interface{}{
			"Trips":          trips,
			"SelectedTripID": selectedTripID,
			"RadiusKm":       radiusKm,
			"Matches":        matches,
			"BestMargin":     bestMargin,
			"MinDeadhead":    minDeadhead,
			"Offered":        r.URL.Query().Get("offered") == "1",
		},
	}
	h.renderPage(w, r, "backhaul_matches.html", data)
}

// WebCreateOffer handles web form dispatch submissions for backhaul loads.
func (h *BackhaulHandlers) WebCreateOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	svc := h.BackhaulSvc
	if svc == nil {
		svc = service.NewBackhaulService(h.DB)
	}

	tripID := strings.TrimSpace(r.PostFormValue("trip_id"))
	bookingID := strings.TrimSpace(r.PostFormValue("booking_id"))
	rateStr := strings.TrimSpace(r.PostFormValue("offered_rate"))

	if tripID == "" || bookingID == "" {
		http.Redirect(w, r, "/backhaul?error=missing_fields", http.StatusSeeOther)
		return
	}

	var offeredRate float64
	if rateStr != "" {
		if f, err := strconv.ParseFloat(rateStr, 64); err == nil {
			offeredRate = f
		}
	}

	req := service.BackhaulOfferRequest{
		TripID:      tripID,
		BookingID:   bookingID,
		OfferedRate: offeredRate,
	}

	_, err := svc.CreateBackhaulOffer(r.Context(), tenantID, req)
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Failed to create backhaul offer", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/backhaul?trip_id="+tripID+"&offered=1", http.StatusSeeOther)
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
