package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	dispatchapp "transport-app/internal/dispatch/application"
	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

func (h *TripHandlers) Update(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tripID := domain.TripID(chi.URLParam(r, "id"))
	var driverID *domain.DriverID
	if d := r.PostFormValue("driver_id"); d != "" {
		did := domain.DriverID(d)
		driverID = &did
	}
	var vehicleID *domain.VehicleID
	if v := r.PostFormValue("vehicle_id"); v != "" {
		vid := domain.VehicleID(v)
		vehicleID = &vid
	}

	h.init()
	tenantID := shared.TenantIDFromContext(r.Context())
	current, err := h.getUC.Execute(r.Context(), tripapp.GetTripQuery{
		TripID:   tripagg.TripID(tripID),
		TenantID: tenantID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Driver/vehicle changes must go through the assignment use cases, which
	// enforce the trip state machine and the maintenance/compliance gate. The
	// edit form previously wrote them straight into UpdateTrip, bypassing both
	// and with only trips:update instead of trips:assign.
	driverChanged := driverID != nil && (current.DriverID == nil || *current.DriverID != driverID.String())
	vehicleChanged := vehicleID != nil && (current.VehicleID == nil || *current.VehicleID != vehicleID.String())
	if driverChanged || vehicleChanged {
		session, _ := h.getUserFromContext(r)
		if session == nil || h.AuthSrv == nil || !h.AuthSrv.Can(session.UserID, "trips", "assign") {
			http.Error(w, "forbidden: trips:assign is required to change driver or vehicle", http.StatusForbidden)
			return
		}
	}
	if driverChanged {
		if err := h.dispatchSvc.AssignDriver(r.Context(), dispatchapp.AssignDriverCommand{
			TripID: tripagg.TripID(tripID.String()), DriverID: driverID.String(), TenantID: tenantID,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if vehicleChanged {
		if err := h.dispatchSvc.AssignVehicle(r.Context(), dispatchapp.AssignVehicleCommand{
			TripID: tripagg.TripID(tripID.String()), VehicleID: vehicleID.String(), TenantID: tenantID,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	_, err = h.Services.Trips.UpdateTrip(r.Context(), tripID, service.CreateTripRequest{
		RouteID:       domain.RouteID(r.PostFormValue("route_id")),
		DriverID:      nil,
		VehicleID:     nil,
		DepartureTime: r.PostFormValue("departure_time"),
		ArrivalTime:   r.PostFormValue("arrival_time"),
		Remarks:       r.PostFormValue("remarks"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+tripID.String(), http.StatusSeeOther)
}
