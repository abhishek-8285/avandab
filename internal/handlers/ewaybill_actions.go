package handlers

// EWB lifecycle action handlers, relocated from ewaybill.go (no raw SQL).

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/ewaybill"
	"transport-app/internal/shared"
)

// GenerateForTrip generates a Part-A EWB for a trip.
func (h *EWayBillHandlers) GenerateForTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		http.Error(w, "trip id required", http.StatusBadRequest)
		return
	}

	if ok, ownErr := h.tripOwned(r.Context(), tripID, h.requestTenant(r)); ownErr != nil {
		http.Error(w, "could not verify trip", http.StatusInternalServerError)
		return
	} else if !ok {
		http.Error(w, "trip not found", http.StatusNotFound)
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

	record, err := h.svc.GetByTripForTenant(r.Context(), tripID, string(shared.TenantIDFromContext(r.Context())))
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

	if !h.requireEWB(w, r, ewbNumber) {
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

	if !h.requireEWB(w, r, ewbNumber) {
		return
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

	if !h.requireEWB(w, r, ewbNumber) {
		return
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
