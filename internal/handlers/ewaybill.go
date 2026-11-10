package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/ewaybill"
	"transport-app/internal/middleware"
)

// EWayBillHandlers handles E-Way Bill lifecycle HTTP requests.
type EWayBillHandlers struct {
	*App
	svc     *ewaybill.EWayBillService
	authSrv auth.AuthorizationService
}

// NewEWayBillHandlers creates a new EWayBillHandlers.
func NewEWayBillHandlers(app *App, svc *ewaybill.EWayBillService, authSrv auth.AuthorizationService) *EWayBillHandlers {
	return &EWayBillHandlers{
		App:     app,
		svc:     svc,
		authSrv: authSrv,
	}
}

// Mount mounts ewaybill routes on the router.
func (h *EWayBillHandlers) Mount(r chi.Router) {
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/trips/{id}/ewaybill", h.GenerateForTrip)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/trips/{id}/ewaybill/generate", h.GenerateForTrip)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/trips/{id}/ewaybill", h.GetForTrip)

	// Standalone EWB management endpoints (Spec 07 §4.2)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill", h.List)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill/table", h.TableFragment)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Get("/ewaybill/new", h.NewForm)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/ewaybill/new", h.Create)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill/{ewb}", h.Detail)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/part-b", h.AttachPartB)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/extend", h.Extend)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/cancel", h.Cancel)
}

// GenerateForTrip generates a Part-A EWB for a trip.
func (h *EWayBillHandlers) GenerateForTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		http.Error(w, "trip id required", http.StatusBadRequest)
		return
	}

	record, err := h.svc.GeneratePartA(r.Context(), ewaybill.GeneratePartARequest{
		TripID:  tripID,
		GenMode: "MANUAL",
		Force:   r.URL.Query().Get("force") == "true" || r.FormValue("force") == "true",
	})
	if err != nil {
		if err == ewaybill.ErrGoodsValueTooLow {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}

// GetForTrip returns the latest EWB record for a trip.
func (h *EWayBillHandlers) GetForTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		http.Error(w, "trip id required", http.StatusBadRequest)
		return
	}

	record, err := h.svc.GetByTrip(r.Context(), tripID)
	if err != nil && err != ewaybill.ErrEWBNotFound {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		if err == ewaybill.ErrEWBNotFound {
			http.Error(w, "no e-way bill found for trip", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	user, _ := h.getUserFromContext(r)
	data := map[string]interface{}{
		"User":     user,
		"Trip":     map[string]string{"ID": tripID},
		"TripID":   tripID,
		"EWayBill": record,
	}
	h.renderFragment(w, r, "ewaybill_card.html", data)
}

// AttachPartB attaches a vehicle to an existing EWB.
func (h *EWayBillHandlers) AttachPartB(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewb")
	vehicleNumber := r.FormValue("vehicle_number")
	transporterID := r.FormValue("transporter_id")

	if vehicleNumber == "" {
		// Try parsing JSON body if form is empty
		var req struct {
			VehicleNumber string `json:"vehicle_number"`
			TransporterID string `json:"transporter_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			vehicleNumber = req.VehicleNumber
			transporterID = req.TransporterID
		}
	}

	if ewbNumber == "" || vehicleNumber == "" {
		http.Error(w, "ewb number and vehicle number are required", http.StatusBadRequest)
		return
	}

	record, err := h.svc.AttachPartB(r.Context(), ewbNumber, vehicleNumber, transporterID)
	if err != nil {
		if err == ewaybill.ErrEWBNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	if record.TripID != "" {
		http.Redirect(w, r, "/trips/"+record.TripID, http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Extend extends validity of an EWB if geofence evidence exists.
func (h *EWayBillHandlers) Extend(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewb")
	reason := r.FormValue("reason")
	if reason == "" {
		reason = "manual_admin_extension"
	}

	record, err := h.svc.Extend(r.Context(), ewbNumber, ewaybill.ExtendRequest{
		EwbNumber: ewbNumber,
		Reason:    reason,
	})
	if err != nil {
		switch err {
		case ewaybill.ErrEWBNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case ewaybill.ErrNotActive:
			http.Error(w, err.Error(), http.StatusBadRequest)
		case ewaybill.ErrExtensionLimitExceeded:
			http.Error(w, "extension_limit_exceeded: "+err.Error(), http.StatusUnprocessableEntity)
		case ewaybill.ErrNoGeofenceEvidence:
			http.Error(w, "no_geofence_evidence: "+err.Error(), http.StatusUnprocessableEntity)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	if record.TripID != "" {
		http.Redirect(w, r, "/trips/"+record.TripID, http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Cancel cancels an EWB.
func (h *EWayBillHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewb")
	reason := r.FormValue("reason")
	if reason == "" {
		reason = "order_cancelled"
	}

	record, err := h.svc.Cancel(r.Context(), ewbNumber, reason)
	if err != nil {
		if err == ewaybill.ErrEWBNotFound {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(record)
		return
	}

	if record.TripID != "" {
		http.Redirect(w, r, "/trips/"+record.TripID, http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.Contains(contentType, "application/json")
}

// EWB page view models live in internal/ewaybill (handlers-end-state ADR,
// Phase 2); the aliases keep template and render-test references unchanged.
type (
	EWBStats       = ewaybill.EWBStats
	EWBListItem    = ewaybill.EWBListItem
	TripOption     = ewaybill.TripOption
	EWBEventRecord = ewaybill.EWBEventRecord
)

func (h *EWayBillHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	// 1. Fetch stats (global legacy scope: the EWB read model documents why).
	reads := ewaybill.NewEWayBillReadQueries(h.DB)
	stats, sErr := reads.Stats(r.Context())
	if sErr != nil && sErr != sql.ErrNoRows {
		// Zeroed stats on a failed query is a silent failure - surface it in logs.
		slog.WarnContext(r.Context(), "EWB stats query failed", slog.Any("error", sErr))
	}

	// 2. Fetch list items
	items := reads.Items(r.Context())

	// 3. Fetch candidate trips for new modal
	trips := reads.CandidateTrips(r.Context())

	h.renderPage(w, r, "ewaybill_index.html", PageData{
		Title: "E-Way Bills",
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"Stats":     stats,
			"EWayBills": items,
			"Trips":     trips,
		},
	})
}

func (h *EWayBillHandlers) TableFragment(w http.ResponseWriter, r *http.Request) {
	items := ewaybill.NewEWayBillReadQueries(h.DB).Items(r.Context())
	h.renderFragment(w, r, "ewaybill_row.html", map[string]interface{}{
		"EWayBills": items,
	})
}

func (h *EWayBillHandlers) NewForm(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	trips := ewaybill.NewEWayBillReadQueries(h.DB).CandidateTrips(r.Context())

	h.renderPage(w, r, "ewaybill_index.html", PageData{
		Title: "Generate E-Way Bill",
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"Trips":     trips,
		},
	})
}

func (h *EWayBillHandlers) Create(w http.ResponseWriter, r *http.Request) {
	tripID := strings.TrimSpace(r.FormValue("trip_id"))
	if tripID == "" {
		http.Error(w, "trip_id required", http.StatusBadRequest)
		return
	}

	var goodsVal float64
	if gv := r.FormValue("goods_value"); gv != "" {
		goodsVal, _ = strconv.ParseFloat(gv, 64)
	}
	force := r.FormValue("force") == "true"

	rec, err := h.svc.GeneratePartA(r.Context(), ewaybill.GeneratePartARequest{
		TripID:     tripID,
		GoodsValue: goodsVal,
		GenMode:    "MANUAL",
		Force:      force,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate EWB: %v", err), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/ewaybill/"+rec.EwbNumber, http.StatusSeeOther)
}

func (h *EWayBillHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewb")
	if ewbNumber == "" {
		http.Error(w, "ewb number required", http.StatusBadRequest)
		return
	}

	session, _ := h.getUserFromContext(r)
	record, err := h.svc.GetByNumber(r.Context(), ewbNumber)
	if err != nil {
		if err == ewaybill.ErrEWBNotFound {
			http.Error(w, "E-Way Bill not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch event history in chronological order
	events := ewaybill.NewEWayBillReadQueries(h.DB).Events(r.Context(), ewbNumber)

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ewaybill": record,
			"events":   events,
		})
		return
	}

	h.renderPage(w, r, "ewaybill_detail.html", PageData{
		Title: fmt.Sprintf("EWB %s", record.EwbNumber),
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"EWayBill":  record,
			"Events":    events,
		},
	})
}
