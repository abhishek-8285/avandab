package handlers

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
	vehicleapp "transport-app/internal/vehicle/application"
	vehicleagg "transport-app/internal/vehicle/domain/aggregate"
)

// VehicleHandlers handles vehicle management.
type VehicleHandlers struct {
	*App
	createUC      *vehicleapp.CreateVehicleUseCase
	updateUC      *vehicleapp.UpdateVehicleUseCase
	getUC         *vehicleapp.GetVehicleUseCase
	listUC        *vehicleapp.ListVehiclesUseCase
	createPointUC *vehicleapp.CreateMeasuringPointUseCase
	recordUC      *vehicleapp.RecordMeasurementUseCase
	detailQ       *vehicleapp.VehicleDetailQueries
	commands      *vehicleapp.VehicleCommandService
}

func (h *VehicleHandlers) init() {
	if h.createUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()

		h.createUC = vehicleapp.NewCreateVehicleUseCase(uowImpl, idGenImpl, clockImpl)
		h.updateUC = vehicleapp.NewUpdateVehicleUseCase(uowImpl, clockImpl)
		h.getUC = vehicleapp.NewGetVehicleUseCase(uowImpl)
		h.listUC = vehicleapp.NewListVehiclesUseCase(uowImpl)
		h.createPointUC = vehicleapp.NewCreateMeasuringPointUseCase(uowImpl, idGenImpl)
		h.recordUC = vehicleapp.NewRecordMeasurementUseCase(uowImpl, idGenImpl, clockImpl)
		h.detailQ = vehicleapp.NewVehicleDetailQueries(h.DB)
		h.commands = vehicleapp.NewVehicleCommandService(h.DB)
	}
}

func (h *VehicleHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/status", h.UpdateStatus)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/points", h.CreatePoint)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/points/{pointID}/measurements", h.RecordMeasurement)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/command", h.SendCommand)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/assign-driver", h.AssignDriverToVehicle)
	r.With(middleware.ResourcePermission(h.AuthSrv, "vehicles", "update")).Post("/{id}/unassign-driver", h.UnassignDriverFromVehicle)
}

func (h *VehicleHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	res, err := h.listUC.Execute(r.Context(), vehicleapp.ListVehiclesQuery{
		TenantID:   shared.TenantIDFromContext(r.Context()),
		Page:       pp.Page,
		Limit:      pp.Limit,
		Search:     pp.Query,
		Status:     pp.Status,
		FleetClass: r.URL.Query().Get("fleet_class"),
		Ownership:  r.URL.Query().Get("ownership"),
		DateFrom:   pp.DateFrom,
		DateTo:     pp.DateTo,
	})
	if err != nil {
		http.Error(w, "Failed to list vehicles", http.StatusInternalServerError)
		return
	}

	fleetClassFilter := r.URL.Query().Get("fleet_class")
	ownershipFilter := r.URL.Query().Get("ownership")
	// Empty-state copy needs to know whether the garage is empty or the
	// filters just match nothing. Derived from request params, not stored.
	isFiltered := pp.Query != "" || pp.Status != "" || fleetClassFilter != "" || ownershipFilter != "" || pp.DateFrom != "" || pp.DateTo != ""

	pd := newPaginationData(pp, res.Total, "/vehicles")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	// One filter state: every control preserves every other active filter.
	// ChipParams carries everything except status (the chip being clicked
	// supplies that); ClearParams carries everything except fleet_class and
	// ownership (the Clear link's own scope). Both are url-encoded once here
	// so templates never assemble raw query strings.
	chipValues := url.Values{}
	if pp.Query != "" {
		chipValues.Set("q", pp.Query)
	}
	if pp.DateFrom != "" {
		chipValues.Set("from", pp.DateFrom)
	}
	if pp.DateTo != "" {
		chipValues.Set("to", pp.DateTo)
	}
	if fleetClassFilter != "" {
		chipValues.Set("fleet_class", fleetClassFilter)
	}
	if ownershipFilter != "" {
		chipValues.Set("ownership", ownershipFilter)
	}
	chipValues.Set("limit", strconv.Itoa(pp.Limit))
	// template.URL: values are url-encoded above; without the trusted type
	// html/template would escape the & and = separators on render.
	chipParams := template.URL(chipValues.Encode())
	clearValues := url.Values{}
	if pp.Query != "" {
		clearValues.Set("q", pp.Query)
	}
	if pp.Status != "" {
		clearValues.Set("status", pp.Status)
	}
	if pp.DateFrom != "" {
		clearValues.Set("from", pp.DateFrom)
	}
	if pp.DateTo != "" {
		clearValues.Set("to", pp.DateTo)
	}
	clearValues.Set("limit", strconv.Itoa(pp.Limit))
	clearParams := template.URL(clearValues.Encode())
	filterHidden := []map[string]interface{}{
		{"Name": "fleet_class", "Value": fleetClassFilter},
		{"Name": "ownership", "Value": ownershipFilter},
		{"Name": "limit", "Value": strconv.Itoa(pp.Limit)},
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, r, "vehicle_list_table.html", map[string]interface{}{
			"Vehicles":         res.Vehicles,
			"Pagination":       pd,
			"Query":            pp.Query,
			"StatusFilter":     pp.Status,
			"FleetClassFilter": fleetClassFilter,
			"OwnershipFilter":  ownershipFilter,
			"IsFiltered":       isFiltered,
			"ChipParams":       chipParams,
			"ClearParams":      clearParams,
			"FilterHidden":     filterHidden,
			"DateFrom":         pp.DateFrom,
			"DateTo":           pp.DateTo,
			"KPIs":             h.vehicleKPIs(r.Context()),
		})
		return
	}

	h.renderPage(w, r, "vehicle_list.html", PageData{
		Title: "Vehicles",
		User:  session,
		Extra: map[string]interface{}{"Vehicles": res.Vehicles, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "FleetClassFilter": fleetClassFilter, "OwnershipFilter": ownershipFilter, "IsFiltered": isFiltered, "ChipParams": chipParams, "ClearParams": clearParams, "FilterHidden": filterHidden, "DateFilterError": pp.DateFilterError, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.vehicleKPIs(r.Context())},
	})
}

func (h *VehicleHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "vehicle_edit.html", PageData{Title: "New Vehicle", User: session})
}

func (h *VehicleHandlers) Create(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	capacity, _ := strconv.ParseInt(r.PostFormValue("capacity"), 10, 64)

	// Identity: registration number is canonical; vehicle number defaults to it if omitted.
	regNum := strings.ToUpper(strings.TrimSpace(r.PostFormValue("registration_number")))
	vehNum := strings.TrimSpace(r.PostFormValue("vehicle_number"))
	if regNum == "" && vehNum != "" {
		regNum = strings.ToUpper(vehNum)
	}
	if vehNum == "" {
		vehNum = regNum
	}
	if regNum == "" {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "vehicle_edit.html", PageData{Title: "New Vehicle", User: session, FlashError: "registration number is required"})
		return
	}

	// Compliance dates are required and strict: silently defaulting an
	// expiry (old now+1yr fallback) fabricates compliance data.
	insExp, err := parseRequiredDate(r, "insurance_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
		return
	}
	fitExp, err := parseRequiredDate(r, "fitness_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
		return
	}
	perExp, err := parseRequiredDate(r, "permit_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
		return
	}

	profile, err := vehicleProfileFromForm(r)
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
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
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
		return
	}
	pucExpiry, err := parseOptionalDate(r, "puc_expiry")
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Create Failed")
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

	_, err = h.createUC.Execute(r.Context(), vehicleapp.CreateVehicleCommand{
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: regNum,
		VehicleNumber:      vehNum,
		VehicleType:        vehicleagg.VehicleType(r.PostFormValue("vehicle_type")),
		Capacity:           capacity,
		FuelType:           vehicleagg.FuelType(r.PostFormValue("fuel_type")),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		CurrentMileage:     currentMileage,
		StandardKmpl:       standardKmpl,
		RCExpiry:           rcExpiry,
		PUCExpiry:          pucExpiry,
		Odometer:           odometer,
		Profile:            profile,
	})
	if err != nil {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "vehicle_edit.html", PageData{Title: "New Vehicle", User: session, FlashError: err.Error()})
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("Location", "/vehicles")
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/vehicles", http.StatusSeeOther)
}

func (h *VehicleHandlers) View(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenantID := shared.TenantIDFromContext(r.Context())
	vehicle, err := h.getUC.Execute(r.Context(), vehicleapp.GetVehicleQuery{
		ID:       vehicleagg.VehicleID(id),
		TenantID: tenantID,
	})
	if err != nil {
		http.Error(w, "Vehicle not found", http.StatusNotFound)
		return
	}
	files, _ := h.Services.Files.GetFilesByEntity(r.Context(), "vehicle_insurance", id)

	detail := h.detailQ.Load(r.Context(), tenantID, id)

	extra := map[string]interface{}{
		"Vehicle":                   vehicle,
		"Files":                     files,
		"MaintenanceDue":            detail.Maintenance.MaintenanceDue,
		"MaintenanceOverrideBy":     detail.Maintenance.MaintenanceOverrideBy,
		"MaintenanceOverrideReason": detail.Maintenance.MaintenanceOverrideReason,
		"IsMaintenanceDue":          detail.Maintenance.IsMaintenanceDue,
		"IsMaintenanceOverridden":   detail.Maintenance.IsMaintenanceOverridden,
		"DocCards":                  vehicleDocCards(vehicle),
		"LastPosition":              detail.LastPosition,
		"RecentTrips":               detail.RecentTrips,
		"OpenWorkOrders":            detail.OpenWorkOrders,
		"MeasuringPoints":           detail.MeasuringPoints,
		"RecentMeasurements":        detail.RecentMeasurements,
		"RecentCommands":            detail.RecentCommands,
		"AssignedDriver":            detail.AssignedDriver,
		"AssignDrivers":             detail.AssignDrivers,
	}

	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "vehicle_view.html", PageData{Title: "View Vehicle", User: session, Extra: extra})
}

// vehicleDocCards builds the compliance doc-expiry strip (RC/permit/fitness/
// insurance/PUCC with days left) rendered by vehicle_view.html.
func vehicleDocCards(vehicle vehicleapp.VehicleResponseDTO) []map[string]interface{} {
	type docStatus struct {
		Name   string
		Expiry time.Time
	}
	docs := []docStatus{
		{"Insurance", vehicle.InsuranceExpiry},
		{"Fitness", vehicle.FitnessExpiry},
		{"Permit", vehicle.PermitExpiry},
	}
	if vehicle.RCExpiry != nil && !vehicle.RCExpiry.IsZero() {
		docs = append(docs, docStatus{"RC", *vehicle.RCExpiry})
	}
	if vehicle.PUCExpiry != nil && !vehicle.PUCExpiry.IsZero() {
		docs = append(docs, docStatus{"PUCC", *vehicle.PUCExpiry})
	}
	docCards := make([]map[string]interface{}, 0, len(docs))
	for _, d := range docs {
		card := map[string]interface{}{"Name": d.Name, "HasDate": !d.Expiry.IsZero()}
		if !d.Expiry.IsZero() {
			card["Expiry"] = d.Expiry.Format("02-01-2006")
			card["Days"] = int(time.Until(d.Expiry).Hours() / 24)
		}
		docCards = append(docCards, card)
	}
	return docCards
}

func (h *VehicleHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	vehicle, err := h.getUC.Execute(r.Context(), vehicleapp.GetVehicleQuery{
		ID:       vehicleagg.VehicleID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Vehicle not found", http.StatusNotFound)
		return
	}

	maintenance := h.detailQ.Maintenance(r.Context(), shared.TenantIDFromContext(r.Context()), id)

	extra := map[string]interface{}{
		"Vehicle":                   vehicle,
		"MaintenanceDue":            maintenance.MaintenanceDue,
		"MaintenanceOverrideBy":     maintenance.MaintenanceOverrideBy,
		"MaintenanceOverrideReason": maintenance.MaintenanceOverrideReason,
		"IsMaintenanceDue":          maintenance.IsMaintenanceDue,
		"IsMaintenanceOverridden":   maintenance.IsMaintenanceOverridden,
	}

	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "vehicle_edit.html", PageData{Title: "Edit Vehicle", User: session, Extra: extra})
}

func (h *VehicleHandlers) Update(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	id := chi.URLParam(r, "id")
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
	blocked := r.PostFormValue("blocked") == "1" || r.PostFormValue("blocked") == "on" || status == vehicleagg.VehicleBlocked

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
		ID:                 vehicleagg.VehicleID(id),
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
		BlockedReason:      r.PostFormValue("blocked_reason"),
		RCExpiry:           rcExpiry,
		PUCExpiry:          pucExpiry,
		Odometer:           odometer,
		Profile:            profile,
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}
	http.Redirect(w, r, "/vehicles/"+id, http.StatusSeeOther)
}

func (h *VehicleHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.VehicleID(chi.URLParam(r, "id"))
	if err := h.Services.Vehicles.DeleteVehicle(r.Context(), id); err != nil {
		h.failPage(w, r, err, http.StatusInternalServerError, "Vehicle Action Failed")
		return
	}
	if isDatastarRequest(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/vehicles", http.StatusSeeOther)
}

func (h *VehicleHandlers) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	status := r.PostFormValue("status")

	vehicle, err := h.getUC.Execute(r.Context(), vehicleapp.GetVehicleQuery{
		ID:       vehicleagg.VehicleID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	err = h.updateUC.Execute(r.Context(), vehicleapp.UpdateVehicleCommand{
		ID:                 vehicleagg.VehicleID(id),
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: vehicle.RegistrationNumber,
		VehicleNumber:      vehicle.VehicleNumber,
		VehicleType:        vehicleagg.VehicleType(vehicle.VehicleType),
		Capacity:           vehicle.Capacity,
		FuelType:           vehicleagg.FuelType(vehicle.FuelType),
		InsuranceExpiry:    vehicle.InsuranceExpiry,
		FitnessExpiry:      vehicle.FitnessExpiry,
		PermitExpiry:       vehicle.PermitExpiry,
		Status:             vehicleagg.VehicleStatus(status),
		CurrentMileage:     vehicle.CurrentMileage,
		Blocked:            vehicle.Blocked || vehicleagg.VehicleStatus(status) == vehicleagg.VehicleBlocked,
		BlockedReason:      vehicle.BlockedReason,
		RCExpiry:           vehicle.RCExpiry,
		PUCExpiry:          vehicle.PUCExpiry,
		Odometer:           vehicle.Odometer,
		Profile:            vehicle.Profile,
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, r, "vehicle_view.html", nil)
		return
	}
	http.Redirect(w, r, "/vehicles/"+id, http.StatusSeeOther)
}

// parseRequiredDate parses a mandatory YYYY-MM-DD compliance date. Empty or
// malformed input is a 400: the old silent now+1yr fallback fabricated
// compliance expiries.
func parseRequiredDate(r *http.Request, name string) (time.Time, error) {
	s := r.PostFormValue(name)
	if s == "" {
		return time.Time{}, fmt.Errorf("%s is required (YYYY-MM-DD)", name)
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be YYYY-MM-DD, got %q", name, s)
	}
	return t, nil
}

func parseOptionalDate(r *http.Request, name string) (*time.Time, error) {
	s := r.PostFormValue(name)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("%s must be YYYY-MM-DD, got %q", name, s)
	}
	return &t, nil
}

func parseOptionalFloat(r *http.Request, name string) *float64 {
	if s := r.PostFormValue(name); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return &f
		}
	}
	return nil
}

func parseOptionalInt(r *http.Request, name string) *int64 {
	if s := r.PostFormValue(name); s != "" {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return &i
		}
	}
	return nil
}

// vehicleProfileFromForm builds the SOP fleet-object profile from the grouped
// SAP-tab fieldsets (General / Organization / Vehicle Details / Technology).
// Classification errors fail the request; numerics stay lenient (ignored when
// malformed, mirroring current_mileage).
func vehicleProfileFromForm(r *http.Request) (vehicleagg.VehicleProfile, error) {
	acqDate, err := parseOptionalDate(r, "acquisition_date")
	if err != nil {
		return vehicleagg.VehicleProfile{}, err
	}
	validFrom, err := parseOptionalDate(r, "valid_from")
	if err != nil {
		return vehicleagg.VehicleProfile{}, err
	}
	validTo, err := parseOptionalDate(r, "valid_to")
	if err != nil {
		return vehicleagg.VehicleProfile{}, err
	}

	p := vehicleagg.VehicleProfile{
		FleetClass:          vehicleagg.FleetClass(r.PostFormValue("fleet_class")),
		Ownership:           vehicleagg.Ownership(r.PostFormValue("ownership")),
		FleetNumber:         r.PostFormValue("fleet_number"),
		Description:         r.PostFormValue("description"),
		Manufacturer:        r.PostFormValue("manufacturer"),
		ManufCountry:        r.PostFormValue("manuf_country"),
		Model:               r.PostFormValue("model"),
		ConstrYearMonth:     r.PostFormValue("constr_year_month"),
		AcquisitionValue:    parseOptionalFloat(r, "acquisition_value"),
		AcquisitionCurrency: r.PostFormValue("acquisition_currency"),
		AcquisitionDate:     acqDate,
		PurchaseVendor:      r.PostFormValue("purchase_vendor"),
		ValidFrom:           validFrom,
		ValidTo:             validTo,
		FacilityID:          r.PostFormValue("facility_id"),
		MaintPlant:          r.PostFormValue("maint_plant"),
		PlanningPlant:       r.PostFormValue("planning_plant"),
		CompanyCode:         r.PostFormValue("company_code"),
		BusinessArea:        r.PostFormValue("business_area"),
		CostCenter:          r.PostFormValue("cost_center"),
		AssetNo:             r.PostFormValue("asset_no"),
		FleetObjectNo:       r.PostFormValue("fleet_object_no"),
		ChassisNo:           r.PostFormValue("chassis_no"),
		VehicleCategory:     r.PostFormValue("vehicle_category"),
		EngineNumber:        r.PostFormValue("engine_number"),
		EnginePower:         r.PostFormValue("engine_power"),
		EngineCapacity:      r.PostFormValue("engine_capacity"),
		CylinderCount:       parseOptionalInt(r, "cylinder_count"),
		MaxSpeed:            parseOptionalFloat(r, "max_speed"),
		Weight:              parseOptionalFloat(r, "weight"),
		WeightUnit:          r.PostFormValue("weight_unit"),
		LoadVolume:          parseOptionalFloat(r, "load_volume"),
		VolumeUnit:          r.PostFormValue("volume_unit"),
		SecondaryFuel:       r.PostFormValue("secondary_fuel"),
		UsageIndicator:      r.PostFormValue("usage_indicator"),
	}
	if err := vehicleagg.ValidateProfile(p); err != nil {
		return vehicleagg.VehicleProfile{}, err
	}
	return p, nil
}

// CreatePoint creates an SOP IK01 measuring point on a vehicle.
func (h *VehicleHandlers) CreatePoint(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Action Failed")
		return
	}
	vehicleID := chi.URLParam(r, "id")

	var annualEstimate float64
	if s := r.PostFormValue("annual_estimate"); s != "" {
		annualEstimate, _ = strconv.ParseFloat(s, 64)
	}

	_, err := h.createPointUC.Execute(r.Context(), vehicleapp.CreateMeasuringPointCommand{
		TenantID:       shared.TenantIDFromContext(r.Context()),
		VehicleID:      vehicleID,
		Kind:           r.PostFormValue("kind"),
		MeasPosition:   r.PostFormValue("meas_position"),
		Unit:           r.PostFormValue("unit"),
		AnnualEstimate: annualEstimate,
		Description:    r.PostFormValue("description"),
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Action Failed")
		return
	}
	http.Redirect(w, r, "/vehicles/"+vehicleID, http.StatusSeeOther)
}

// RecordMeasurement records an SOP IK11 measuring document against a point.
func (h *VehicleHandlers) RecordMeasurement(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Action Failed")
		return
	}
	pointID := chi.URLParam(r, "pointID")

	counter, err := strconv.ParseFloat(r.PostFormValue("counter_reading"), 64)
	if err != nil {
		h.failPage(w, r, fmt.Errorf("counter_reading must be a number"), http.StatusBadRequest, "Vehicle Action Failed")
		return
	}

	var measuredAt time.Time
	if s := r.PostFormValue("measured_at"); s != "" {
		measuredAt, err = time.Parse("2006-01-02", s)
		if err != nil {
			h.failPage(w, r, fmt.Errorf("measured_at must be YYYY-MM-DD"), http.StatusBadRequest, "Vehicle Action Failed")
			return
		}
	}

	_, err = h.recordUC.Execute(r.Context(), vehicleapp.RecordMeasurementCommand{
		TenantID:   shared.TenantIDFromContext(r.Context()),
		PointID:    pointID,
		Counter:    counter,
		MeasuredAt: measuredAt,
		ReadBy:     r.PostFormValue("read_by"),
		Remarks:    r.PostFormValue("remarks"),
		RecordedBy: r.PostFormValue("recorded_by"),
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Action Failed")
		return
	}

	vehicleID := r.PostFormValue("vehicle_id")
	if vehicleID == "" {
		vehicleID = r.URL.Query().Get("vehicle_id")
	}
	http.Redirect(w, r, "/vehicles/"+vehicleID, http.StatusSeeOther)
}

func (h *VehicleHandlers) SendCommand(w http.ResponseWriter, r *http.Request) {
	h.init()
	vehID := chi.URLParam(r, "id")
	cmdType := strings.ToUpper(strings.TrimSpace(r.PostFormValue("command_type")))
	switch cmdType {
	case "E_STOP", "HOLD_POSITION", "RESUME_MISSION", "SET_SPEED_LIMIT", "REROUTE", "RETURN_TO_BASE":
	default:
		h.failPage(w, r, errors.New("invalid command type"), http.StatusBadRequest, "AV Command Failed")
		return
	}

	session, _ := h.getUserFromContext(r)
	issuedBy := "operator"
	if session != nil {
		if session.Name != "" {
			issuedBy = session.Name
		} else if session.UserID != "" {
			issuedBy = session.UserID
		}
	}

	if err := h.commands.Send(r.Context(), vehicleapp.SendVehicleCommand{
		TenantID:    shared.TenantIDFromContext(r.Context()),
		VehicleID:   vehID,
		CommandType: cmdType,
		IssuedBy:    issuedBy,
	}); err != nil {
		h.failPage(w, r, err, http.StatusInternalServerError, "Command Failed")
		return
	}
	http.Redirect(w, r, "/vehicles/"+vehID, http.StatusSeeOther)
}
