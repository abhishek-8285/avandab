package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"transport-app/internal/auth"
	driverapp "transport-app/internal/driver/application"
	"transport-app/internal/shared"
)

// requireSessionTenant resolves the acting user and tenant for the driver
// self-service API. Auth middleware sets both; a missing one is a bad/expired
// session, so answer 401 instead of panicking into a 500 through Recoverer.
func (h *DriverHandlers) requireSessionTenant(w http.ResponseWriter, r *http.Request) (*auth.SessionData, shared.TenantID, bool) {
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, "", false
	}
	tenantID, err := shared.TenantRequired(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "tenant required")
		return nil, "", false
	}
	return session, tenantID, true
}

// GetMe returns the driver profile for the authenticated user.
// Spec 13 §2.2: GET /api/v1/drivers/me
func (h *DriverHandlers) GetMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	session, tenantID, ok := h.requireSessionTenant(w, r)
	if !ok {
		return
	}
	h.init()

	me, err := h.self.GetMe(r.Context(), session.UserID, tenantID)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "driver not found"})
		return
	}

	fullName := strings.TrimSpace(me.Profile.FirstName + " " + me.Profile.LastName)
	type Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	var curLoc *Location
	if me.CurrentLocation != nil {
		curLoc = &Location{Latitude: me.CurrentLocation.Latitude, Longitude: me.CurrentLocation.Longitude}
	}

	resp := map[string]interface{}{
		"driver_id":        me.DriverCode,
		"user_id":          session.UserID,
		"name":             fullName,
		"phone":            me.Profile.Phone,
		"status":           me.Profile.Status,
		"vehicle_plate":    me.VehiclePlate,
		"current_location": curLoc,
		"license_number":   me.Profile.LicenseNumber,
		"license_expiry":   me.Profile.LicenseExpiry,
		"bank_details":     me.Profile.BankDetails,
		"aadhaar":          me.Profile.Aadhaar,
		"pan":              me.Profile.Pan,
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// UpdateMe updates the driver profile, driving license, and bank details for the authenticated user.
func (h *DriverHandlers) UpdateMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	session, tenantID, ok := h.requireSessionTenant(w, r)
	if !ok {
		return
	}

	var req struct {
		LicenseNumber string `json:"license_number"`
		LicenseExpiry string `json:"license_expiry"`
		BankDetails   string `json:"bank_details"`
		Aadhaar       string `json:"aadhaar"`
		Pan           string `json:"pan"`
		Status        string `json:"status"`
		VehicleNumber string `json:"vehicle_number"`
		VehiclePlate  string `json:"vehicle_plate"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	h.init()

	result, err := h.self.UpdateMe(r.Context(), driverapp.UpdateMeCommand{
		UserID:        session.UserID,
		TenantID:      tenantID,
		LicenseNumber: req.LicenseNumber,
		LicenseExpiry: req.LicenseExpiry,
		BankDetails:   req.BankDetails,
		Aadhaar:       req.Aadhaar,
		Pan:           req.Pan,
		Status:        req.Status,
		VehicleNumber: req.VehicleNumber,
		VehiclePlate:  req.VehiclePlate,
	})
	if err != nil {
		if errors.Is(err, driverapp.ErrDriverNotFound) {
			writeJSONError(w, http.StatusNotFound, "driver not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"driver_id":     result.DriverID,
		"status":        req.Status,
		"vehicle_plate": result.VehiclePlate,
	})
}
