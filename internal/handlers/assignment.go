package handlers

// Driver-vehicle preferred assignment HTTP actions (handlers-end-state ADR,
// Phase 2). The read/write logic lives in internal/dispatch/application
// (AssignmentService); this file keeps HTTP parsing, validation, the
// auth-derived actor identity, page error rendering and redirects.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	dispatchapp "transport-app/internal/dispatch/application"
	"transport-app/internal/shared"
)

func (h *DriverHandlers) AssignVehicleToDriver(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "id")
	vehicleID := r.PostFormValue("vehicle_id")
	tenantID := shared.TenantIDFromContext(r.Context())
	if vehicleID == "" {
		http.Error(w, "vehicle_id is required", http.StatusBadRequest)
		return
	}
	assignedBy := ""
	if s, _ := h.getUserFromContext(r); s != nil {
		assignedBy = s.UserID
	}
	if err := dispatchapp.NewAssignmentService(h.DB).AssignVehicleToDriver(r.Context(), dispatchapp.AssignVehicleToDriverCommand{
		TenantID:   tenantID,
		DriverID:   driverID,
		VehicleID:  vehicleID,
		AssignedBy: assignedBy,
	}); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Assignment Failed")
		return
	}
	http.Redirect(w, r, safeRedirect(r, "/drivers/"+driverID, "/drivers"), http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}

func (h *DriverHandlers) UnassignVehicleFromDriver(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())
	_ = dispatchapp.NewAssignmentService(h.DB).UnassignDriver(r.Context(), tenantID, driverID)
	http.Redirect(w, r, safeRedirect(r, "/drivers/"+driverID, "/drivers"), http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}

func (h *VehicleHandlers) AssignDriverToVehicle(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "id")
	driverID := r.PostFormValue("driver_id")
	tenantID := shared.TenantIDFromContext(r.Context())
	if driverID == "" {
		http.Error(w, "driver_id is required", http.StatusBadRequest)
		return
	}
	assignedBy := ""
	if s, _ := h.getUserFromContext(r); s != nil {
		assignedBy = s.UserID
	}
	if err := dispatchapp.NewAssignmentService(h.DB).AssignVehicleToDriver(r.Context(), dispatchapp.AssignVehicleToDriverCommand{
		TenantID:   tenantID,
		DriverID:   driverID,
		VehicleID:  vehicleID,
		AssignedBy: assignedBy,
	}); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Assignment Failed")
		return
	}
	http.Redirect(w, r, safeRedirect(r, "/vehicles/"+vehicleID, "/vehicles"), http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}

func (h *VehicleHandlers) UnassignDriverFromVehicle(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())
	_ = dispatchapp.NewAssignmentService(h.DB).UnassignVehicle(r.Context(), tenantID, vehicleID)
	http.Redirect(w, r, safeRedirect(r, "/vehicles/"+vehicleID, "/vehicles"), http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}
