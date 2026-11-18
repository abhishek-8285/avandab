package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/shared"
	vehicleapp "transport-app/internal/vehicle/application"
	vehicleagg "transport-app/internal/vehicle/domain/aggregate"
)

func (h *VehicleHandlers) Update(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	vehicleID := chi.URLParam(r, "id")
	capacity, _ := strconv.ParseInt(r.PostFormValue("capacity"), 10, 64)
	status := vehicleagg.VehicleStatus(r.PostFormValue("status"))
	if status == "" {
		status = vehicleagg.VehicleAvailable
	}

	insExp, err := parseRequiredDate(r, "insurance_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	fitExp, err := parseRequiredDate(r, "fitness_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	perExp, err := parseRequiredDate(r, "permit_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	profile, err := vehicleProfileFromForm(r)
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	var currentMileage *float64
	if milStr := r.PostFormValue("current_mileage"); milStr != "" {
		if mil, err := strconv.ParseFloat(milStr, 64); err == nil {
			currentMileage = &mil
		}
	}
	var standardKmpl *float64
	if s := r.PostFormValue("standard_kmpl"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			standardKmpl = &f
		}
	}
	rcExpiry, err := parseOptionalDate(r, "rc_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}
	pucExpiry, err := parseOptionalDate(r, "puc_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}
	var odometer float64
	if s := r.PostFormValue("odometer"); s != "" {
		odometer, _ = strconv.ParseFloat(s, 64)
	}
	if odometer == 0 && currentMileage != nil {
		odometer = *currentMileage
	} else if currentMileage == nil && odometer > 0 {
		currentMileage = &odometer
	}
	// The edit form has no blocked checkbox; derive the flag from the current
	// row so editing other fields cannot silently lift a compliance block.
	existing, err := h.getUC.Execute(r.Context(), vehicleapp.GetVehicleQuery{
		ID:       vehicleagg.VehicleID(vehicleID),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusNotFound, "Vehicle Update Failed")
		return
	}
	blocked := existing.Blocked || r.PostFormValue("blocked") == "1" || r.PostFormValue("blocked") == "on" || status == vehicleagg.VehicleBlocked
	blockedReason := strings.TrimSpace(r.PostFormValue("blocked_reason"))
	if blockedReason == "" {
		blockedReason = existing.BlockedReason
	}

	regNum := strings.ToUpper(strings.TrimSpace(r.PostFormValue("registration_number")))
	vehNum := strings.TrimSpace(r.PostFormValue("vehicle_number"))
	if regNum == "" && vehNum != "" {
		regNum = strings.ToUpper(vehNum)
	}
	if vehNum == "" {
		vehNum = regNum
	}
	if regNum == "" {
		h.failPage(w, r, errors.New("registration number is required"), http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	err = h.updateUC.Execute(r.Context(), vehicleapp.UpdateVehicleCommand{
		ID:                 vehicleagg.VehicleID(vehicleID),
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: regNum,
		VehicleNumber:      vehNum,
		VehicleType:        vehicleagg.VehicleType(r.PostFormValue("vehicle_type")),
		Capacity:           capacity,
		FuelType:           vehicleagg.FuelType(r.PostFormValue("fuel_type")),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		Status:             status,
		CurrentMileage:     currentMileage,
		StandardKmpl:       standardKmpl,
		Blocked:            blocked,
		BlockedReason:      blockedReason,
		RCExpiry:           rcExpiry,
		PUCExpiry:          pucExpiry,
		Odometer:           odometer,
		Profile:            profile,
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}
	http.Redirect(w, r, "/vehicles/"+vehicleID, http.StatusSeeOther)
}
