package handlers

import (
	"net/http"
	"time"
)

// tenantDriverGate keeps mobile self-service lookups inside the calling
// tenant; the application service's legacy unscoped rule stays intact.
func tenantDriverGate(h *DriverHandlers, w http.ResponseWriter, r *http.Request, userID string) bool {
	if err := h.self.RequireTenantDriver(r.Context(), userID); err != nil {
		writeJSONError(w, http.StatusNotFound, "driver not found")
		return false
	}
	return true
}

// parseLicenseExpiry validates the driver's YYYY-MM-DD licence expiry. On a
// malformed value it writes a 400 and returns an error so the caller stops.
func parseLicenseExpiry(w http.ResponseWriter, r *http.Request) (time.Time, error) {
	expiry, err := time.Parse("2006-01-02", r.PostFormValue("license_expiry"))
	if err != nil {
		http.Error(w, "license_expiry must be YYYY-MM-DD", http.StatusBadRequest)
		return time.Time{}, err
	}
	return expiry, nil
}
