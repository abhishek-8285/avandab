package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
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

	pd := newPaginationData(pp, res.Total, "/vehicles")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "vehicle_list_table.html", map[string]interface{}{
			"Vehicles":         res.Vehicles,
			"Pagination":       pd,
			"Query":            pp.Query,
			"StatusFilter":     pp.Status,
			"FleetClassFilter": fleetClassFilter,
			"OwnershipFilter":  ownershipFilter,
			"DateFrom":         pp.DateFrom,
			"DateTo":           pp.DateTo,
			"KPIs":             h.vehicleKPIs(r.Context()),
		})
		return
	}

	h.renderPage(w, r, "vehicle_list.html", PageData{
		Title: "Vehicles",
		User:  session,
		Extra: map[string]interface{}{"Vehicles": res.Vehicles, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "FleetClassFilter": fleetClassFilter, "OwnershipFilter": ownershipFilter, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.vehicleKPIs(r.Context())},
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

	// Identity errors re-render the form (200) with a message, as before.
	if r.PostFormValue("registration_number") == "" || r.PostFormValue("vehicle_number") == "" {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "vehicle_edit.html", PageData{Title: "New Vehicle", User: session, FlashError: "registration number and vehicle number are required"})
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

	_, err = h.createUC.Execute(r.Context(), vehicleapp.CreateVehicleCommand{
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: r.PostFormValue("registration_number"),
		VehicleNumber:      r.PostFormValue("vehicle_number"),
		VehicleType:        vehicleagg.VehicleType(r.PostFormValue("vehicle_type")),
		Capacity:           capacity,
		FuelType:           vehicleagg.FuelType(r.PostFormValue("fuel_type")),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		CurrentMileage:     currentMileage,
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
	vehicle, err := h.getUC.Execute(r.Context(), vehicleapp.GetVehicleQuery{
		ID:       vehicleagg.VehicleID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Vehicle not found", http.StatusNotFound)
		return
	}
	files, _ := h.Services.Files.GetFilesByEntity(r.Context(), "vehicle_insurance", id)

	var maintDue, maintOvBy, maintOvReason sql.NullString
	var maintOvAt sql.NullTime
	_ = h.DB.QueryRowContext(r.Context(), `
		SELECT maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason
		FROM vehicles WHERE id = $1`, id).Scan(&maintDue, &maintOvBy, &maintOvAt, &maintOvReason)

	// Compliance doc-expiry strip: RC/permit/fitness/insurance/PUCC with days left.
	type docStatus struct {
		Name   string
		Expiry time.Time
	}
	docs := []docStatus{
		{"Insurance", vehicle.InsuranceExpiry},
		{"Fitness", vehicle.FitnessExpiry},
		{"Permit", vehicle.PermitExpiry},
	}
	var rcExpiry, pucExpiry sql.NullTime
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT rc_expiry, puc_expiry FROM vehicles WHERE id = $1`, id).Scan(&rcExpiry, &pucExpiry)
	if rcExpiry.Valid {
		docs = append(docs, docStatus{"RC", rcExpiry.Time})
	}
	if pucExpiry.Valid {
		docs = append(docs, docStatus{"PUCC", pucExpiry.Time})
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

	// Last known telemetry for this vehicle.
	lastPos := map[string]interface{}{"Has": false}
	var lat, lng, speed float64
	var at sql.NullString
	posTenantID := string(shared.TenantIDFromContext(r.Context()))
	if posTenantID == "" {
		posTenantID = string(shared.DefaultTenant)
	}
	if err := h.DB.QueryRowContext(r.Context(), `
		SELECT latitude, longitude, speed, device_time
		FROM vehicle_latest_position WHERE vehicle_id = $1 AND tenant_id = $2`, id, posTenantID).
		Scan(&lat, &lng, &speed, &at); err == nil {
		lastPos = map[string]interface{}{"Has": true, "Lat": lat, "Lng": lng, "Speed": speed, "At": at.String}
	}

	// Recent trips for this vehicle.
	type recentTrip struct {
		ID, TripNumber, Status string
		CreatedAt              sql.NullTime
	}
	trips := []recentTrip{}
	if rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, trip_number, status, created_at FROM trips
		WHERE vehicle_id = $1 ORDER BY created_at DESC LIMIT 5`, id); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var t recentTrip
			if rows.Scan(&t.ID, &t.TripNumber, &t.Status, &t.CreatedAt) == nil {
				trips = append(trips, t)
			}
		}
	}

	// Open job cards for this vehicle (own org only; empty tenant matches nothing).
	type openWorkOrder struct {
		ID, Title, Status, Assignee string
		CreatedAt                   sql.NullTime
	}
	workOrders := []openWorkOrder{}
	if rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, title, status, assignee, created_at FROM work_orders
		WHERE vehicle_id = $1 AND tenant_id = $2 AND status NOT IN ('done','cancelled')
		ORDER BY created_at DESC LIMIT 10`, id, string(shared.TenantIDFromContext(r.Context()))); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var wo openWorkOrder
			if rows.Scan(&wo.ID, &wo.Title, &wo.Status, &wo.Assignee, &wo.CreatedAt) == nil {
				workOrders = append(workOrders, wo)
			}
		}
	}

	extra := map[string]interface{}{
		"Vehicle":                   vehicle,
		"Files":                     files,
		"MaintenanceDue":            maintDue.String,
		"MaintenanceOverrideBy":     maintOvBy.String,
		"MaintenanceOverrideReason": maintOvReason.String,
		"IsMaintenanceDue":          maintDue.Valid && maintDue.String != "",
		"IsMaintenanceOverridden":   maintOvBy.Valid && maintOvBy.String != "",
		"DocCards":                  docCards,
		"LastPosition":              lastPos,
		"RecentTrips":               trips,
		"OpenWorkOrders":            workOrders,
		"MeasuringPoints":           h.measuringPoints(r.Context(), id),
		"RecentMeasurements":        h.recentMeasurements(r.Context(), id),
	}

	session, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "vehicle_view.html", PageData{Title: "View Vehicle", User: session, Extra: extra})
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

	var maintDue, maintOvBy, maintOvReason sql.NullString
	var maintOvAt sql.NullTime
	_ = h.DB.QueryRowContext(r.Context(), `
		SELECT maintenance_due, maintenance_override_by, maintenance_override_at, maintenance_override_reason
		FROM vehicles WHERE id = $1`, id).Scan(&maintDue, &maintOvBy, &maintOvAt, &maintOvReason)

	extra := map[string]interface{}{
		"Vehicle":                   vehicle,
		"MaintenanceDue":            maintDue.String,
		"MaintenanceOverrideBy":     maintOvBy.String,
		"MaintenanceOverrideReason": maintOvReason.String,
		"IsMaintenanceDue":          maintDue.Valid && maintDue.String != "",
		"IsMaintenanceOverridden":   maintOvBy.Valid && maintOvBy.String != "",
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

	err = h.updateUC.Execute(r.Context(), vehicleapp.UpdateVehicleCommand{
		ID:                 vehicleagg.VehicleID(id),
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: r.PostFormValue("registration_number"),
		VehicleNumber:      r.PostFormValue("vehicle_number"),
		VehicleType:        vehicleagg.VehicleType(r.PostFormValue("vehicle_type")),
		Capacity:           capacity,
		FuelType:           vehicleagg.FuelType(r.PostFormValue("fuel_type")),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		Status:             status,
		CurrentMileage:     currentMileage,
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
		Profile:            vehicle.Profile,
	})
	if err != nil {
		h.failPage(w, r, err, http.StatusBadRequest, "Vehicle Update Failed")
		return
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "vehicle_view.html", nil)
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

// measuringPoints lists the SOP IK01 points registered on a vehicle.
func (h *VehicleHandlers) measuringPoints(ctx context.Context, vehicleID string) []map[string]interface{} {
	type point struct {
		ID, Kind, MeasPosition, Unit, Description string
		AnnualEstimate                            sql.NullFloat64
	}
	out := []map[string]interface{}{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT id, kind, meas_position, unit, description, annual_estimate
		FROM vehicle_measuring_points
		WHERE vehicle_id = $1 AND tenant_id = $2
		ORDER BY created_at ASC`, vehicleID, string(shared.TenantIDFromContext(ctx)))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p point
		if rows.Scan(&p.ID, &p.Kind, &p.MeasPosition, &p.Unit, &p.Description, &p.AnnualEstimate) == nil {
			m := map[string]interface{}{
				"ID": p.ID, "Kind": p.Kind, "MeasPosition": p.MeasPosition,
				"Unit": p.Unit, "Description": p.Description,
			}
			if p.AnnualEstimate.Valid {
				m["AnnualEstimate"] = p.AnnualEstimate.Float64
			}
			out = append(out, m)
		}
	}
	return out
}

// recentMeasurements lists the latest SOP IK11 documents across a vehicle's
// points (counter story for KMPL-style follow-ups).
func (h *VehicleHandlers) recentMeasurements(ctx context.Context, vehicleID string) []map[string]interface{} {
	out := []map[string]interface{}{}
	rows, err := h.DB.QueryContext(ctx, `
		SELECT m.id, m.point_id, m.counter_reading, m.difference_reading,
			m.measured_at, m.read_by, p.kind, p.meas_position, p.unit
		FROM vehicle_measurements m
		JOIN vehicle_measuring_points p ON p.id = m.point_id
		WHERE p.vehicle_id = $1 AND m.tenant_id = $2
		ORDER BY m.recorded_at DESC LIMIT 20`, vehicleID, string(shared.TenantIDFromContext(ctx)))
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var docID, pointID, kind, pos, unit string
		var counter float64
		var diff sql.NullFloat64
		var mAt sql.NullTime
		var readBy sql.NullString
		if rows.Scan(&docID, &pointID, &counter, &diff, &mAt, &readBy, &kind, &pos, &unit) == nil {
			m := map[string]interface{}{
				"ID": docID, "PointID": pointID, "Counter": counter,
				"Kind": kind, "MeasPosition": pos, "Unit": unit,
				"ReadBy": readBy.String,
			}
			if diff.Valid {
				m["Difference"] = diff.Float64
			}
			if mAt.Valid {
				m["MeasuredAt"] = mAt.Time
			}
			out = append(out, m)
		}
	}
	return out
}
