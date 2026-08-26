package handlers

import (
	"database/sql"
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
	createUC *vehicleapp.CreateVehicleUseCase
	updateUC *vehicleapp.UpdateVehicleUseCase
	getUC    *vehicleapp.GetVehicleUseCase
	listUC   *vehicleapp.ListVehiclesUseCase
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
}

func (h *VehicleHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	res, err := h.listUC.Execute(r.Context(), vehicleapp.ListVehiclesQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     pp.Page,
		Limit:    pp.Limit,
		Search:   pp.Query,
		Status:   pp.Status,
		DateFrom: pp.DateFrom,
		DateTo:   pp.DateTo,
	})
	if err != nil {
		http.Error(w, "Failed to list vehicles", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, res.Total, "/vehicles")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "vehicle_list_table.html", map[string]interface{}{
			"Vehicles":     res.Vehicles,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
			"KPIs":         h.vehicleKPIs(r.Context()),
		})
		return
	}

	h.renderPage(w, r, "vehicle_list.html", PageData{
		Title: "Vehicles",
		User:  session,
		Extra: map[string]interface{}{"Vehicles": res.Vehicles, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.vehicleKPIs(r.Context())},
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

	insExp, err := time.Parse("2006-01-02", r.PostFormValue("insurance_expiry"))
	if err != nil {
		insExp = time.Now().AddDate(1, 0, 0)
	}

	fitExp, err := time.Parse("2006-01-02", r.PostFormValue("fitness_expiry"))
	if err != nil {
		fitExp = time.Now().AddDate(1, 0, 0)
	}

	perExp, err := time.Parse("2006-01-02", r.PostFormValue("permit_expiry"))
	if err != nil {
		perExp = time.Now().AddDate(1, 0, 0)
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
		FROM vehicles WHERE id = ?`, id).Scan(&maintDue, &maintOvBy, &maintOvAt, &maintOvReason)

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
		`SELECT rc_expiry, puc_expiry FROM vehicles WHERE id = ?`, id).Scan(&rcExpiry, &pucExpiry)
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
		FROM vehicle_latest_position WHERE vehicle_id = ? AND tenant_id = ?`, id, posTenantID).
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
		WHERE vehicle_id = ? ORDER BY created_at DESC LIMIT 5`, id); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var t recentTrip
			if rows.Scan(&t.ID, &t.TripNumber, &t.Status, &t.CreatedAt) == nil {
				trips = append(trips, t)
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
		FROM vehicles WHERE id = ?`, id).Scan(&maintDue, &maintOvBy, &maintOvAt, &maintOvReason)

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

	insExp, err := time.Parse("2006-01-02", r.PostFormValue("insurance_expiry"))
	if err != nil {
		insExp = time.Now().AddDate(1, 0, 0)
	}

	fitExp, err := time.Parse("2006-01-02", r.PostFormValue("fitness_expiry"))
	if err != nil {
		fitExp = time.Now().AddDate(1, 0, 0)
	}

	perExp, err := time.Parse("2006-01-02", r.PostFormValue("permit_expiry"))
	if err != nil {
		perExp = time.Now().AddDate(1, 0, 0)
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
