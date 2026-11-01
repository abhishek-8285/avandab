package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"encoding/json"
	"errors"
	"strings"
	"transport-app/internal/maintenance"
	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/tyre"
)

// MaintenanceHandlers powers the preventive maintenance UI, schedule management, and DTC intake (Spec 04 §6, §12).
type MaintenanceHandlers struct {
	*App
	repo      *maintsql.MaintenanceRepository
	worker    *maintenance.Worker
	scheduler *maintenance.PlanScheduler
}

// NewMaintenanceHandlers creates a new MaintenanceHandlers instance.
func NewMaintenanceHandlers(app *App, db *sql.DB) *MaintenanceHandlers {
	repo := maintsql.NewMaintenanceRepository(db)
	return &MaintenanceHandlers{
		App:       app,
		repo:      repo,
		scheduler: maintenance.NewPlanScheduler(db, repo),
	}
}

// SetWorker attaches the background PM worker so handler actions can trigger immediate schedule/resolution evaluations.
func (h *MaintenanceHandlers) SetWorker(w *maintenance.Worker) {
	h.worker = w
}

// Routes registers maintenance web endpoints.
func (h *MaintenanceHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "read")).Get("/", h.Index)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "read")).Get("/schedules", h.ListSchedules)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Get("/schedules/new", h.NewSchedule)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Post("/schedules", h.CreateSchedule)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "update")).Post("/schedules/{id}/edit", h.EditSchedule)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Get("/records/new", h.NewRecord)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Post("/records", h.CreateRecord)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "read")).Get("/dtc", h.ListDTC)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "update")).Post("/dtc/{id}/resolve", h.ResolveDTC)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "update")).Post("/vehicles/{id}/override", h.OverrideBlock)
	// Job cards (work orders) — HTML pages reusing the tenant-scoped repo.
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Get("/work-orders/new", h.NewWorkOrder)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "create")).Post("/work-orders", h.CreateWorkOrder)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "read")).Get("/work-orders/{id}", h.ViewWorkOrder)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "update")).Post("/work-orders/{id}/assign", h.AssignWorkOrder)
	r.With(middleware.ResourcePermission(h.AuthSrv, "maintenance", "update")).Post("/work-orders/{id}/transition", h.TransitionWorkOrder)
}

// Index renders the preventive maintenance dashboard (due vehicles, active schedules, DTCs, recent records).
func (h *MaintenanceHandlers) Index(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	dueVehicles, err := h.repo.ListDueVehicles(r.Context(), tenantID)
	if err != nil {
		dueVehicles = []map[string]interface{}{}
	}

	schedules, err := h.repo.ListActiveSchedulesForTenant(r.Context(), "", tenantID)
	if err != nil {
		schedules = []domain.Schedule{}
	}

	dtcs, err := h.repo.ListDtcEventsForTenant(r.Context(), "", tenantID, 20)
	if err != nil {
		dtcs = []domain.DtcEvent{}
	}

	records, err := h.repo.ListRecordsForTenant(r.Context(), "", tenantID, 20)
	if err != nil {
		records = []domain.Record{}
	}

	workOrders, err := h.repo.ListWorkOrders(r.Context(), tenantID, "", 50)
	if err != nil {
		workOrders = []domain.WorkOrder{}
	}

	// Registration numbers for the job-card panel (never raw UUIDs).
	vehicleLabels := map[string]string{}
	if rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, COALESCE(NULLIF(registration_number,''), vehicle_number, id) FROM vehicles WHERE tenant_id = $1`, tenantID); err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var vid, label string
			if rows.Scan(&vid, &label) == nil {
				vehicleLabels[vid] = label
			}
		}
	}

	data := PageData{
		Title: "Preventive Maintenance",
		User:  user,
		Extra: map[string]interface{}{
			"DueVehicles":   dueVehicles,
			"Schedules":     schedules,
			"DTCs":          dtcs,
			"Records":       records,
			"WorkOrders":    workOrders,
			"VehicleLabels": vehicleLabels,
		},
	}

	h.renderPage(w, r, "maintenance_index.html", data)
}

// ListSchedules lists schedules for a vehicle or fleetwide (own org only).
func (h *MaintenanceHandlers) ListSchedules(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.URL.Query().Get("vehicle_id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	schedules, err := h.repo.ListSchedulesForTenant(r.Context(), vehicleID, tenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "maintenance_index.html", PageData{
		Title: "Maintenance Schedules",
		User:  user,
		Extra: map[string]interface{}{
			"Schedules":     schedules,
			"VehicleLabels": map[string]string{},
		},
	})
}

// NewSchedule renders the schedule creation form.
func (h *MaintenanceHandlers) NewSchedule(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	vehicleID := r.URL.Query().Get("vehicle_id")

	h.renderPage(w, r, "maintenance_schedule_form.html", PageData{
		Title: "New Maintenance Schedule",
		User:  user,
		Extra: map[string]interface{}{
			"VehicleID":    vehicleID,
			"ServiceTypes": domain.ServiceTypes,
		},
	})
}

// CreateSchedule saves a new schedule and triggers PM evaluation.
func (h *MaintenanceHandlers) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.FormValue("vehicle_id")
	serviceType := r.FormValue("service_type")
	if vehicleID == "" || serviceType == "" {
		http.Error(w, "vehicle_id and service_type are required", http.StatusBadRequest)
		return
	}

	s := domain.Schedule{
		ID:          uuid.NewString(),
		VehicleID:   vehicleID,
		ServiceType: serviceType,
		Active:      r.FormValue("active") != "0" && r.FormValue("active") != "false",
	}

	if kmStr := r.FormValue("interval_km"); kmStr != "" {
		if km, err := strconv.ParseFloat(kmStr, 64); err == nil && km > 0 {
			s.IntervalKM = &km
		}
	}
	if daysStr := r.FormValue("interval_days"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil && days > 0 {
			s.IntervalDays = &days
		}
	}
	if dueKMStr := r.FormValue("due_km"); dueKMStr != "" {
		if km, err := strconv.ParseFloat(dueKMStr, 64); err == nil && km > 0 {
			s.DueKM = &km
		}
	}
	if dueAtStr := r.FormValue("due_at"); dueAtStr != "" {
		if t, err := time.Parse("2006-01-02", dueAtStr); err == nil {
			tUTC := t.UTC()
			s.DueAt = &tUTC
		}
	}

	if err := h.repo.SaveSchedule(r.Context(), s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if h.worker != nil {
		h.worker.EvaluateSchedules(r.Context())
	}

	http.Redirect(w, r, "/maintenance", http.StatusSeeOther)
}

// EditSchedule updates an existing schedule.
func (h *MaintenanceHandlers) EditSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	vehicleID := r.FormValue("vehicle_id")
	serviceType := r.FormValue("service_type")

	s := domain.Schedule{
		ID:          id,
		VehicleID:   vehicleID,
		ServiceType: serviceType,
		Active:      r.FormValue("active") == "1" || r.FormValue("active") == "true",
	}

	if kmStr := r.FormValue("interval_km"); kmStr != "" {
		if km, err := strconv.ParseFloat(kmStr, 64); err == nil {
			s.IntervalKM = &km
		}
	}
	if daysStr := r.FormValue("interval_days"); daysStr != "" {
		if days, err := strconv.Atoi(daysStr); err == nil {
			s.IntervalDays = &days
		}
	}
	if dueKMStr := r.FormValue("due_km"); dueKMStr != "" {
		if km, err := strconv.ParseFloat(dueKMStr, 64); err == nil {
			s.DueKM = &km
		}
	}
	if dueAtStr := r.FormValue("due_at"); dueAtStr != "" {
		if t, err := time.Parse("2006-01-02", dueAtStr); err == nil {
			tUTC := t.UTC()
			s.DueAt = &tUTC
		}
	}

	if err := h.repo.SaveSchedule(r.Context(), s); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if h.worker != nil {
		h.worker.EvaluateSchedules(r.Context())
	}

	http.Redirect(w, r, "/maintenance", http.StatusSeeOther)
}

// NewRecord renders the maintenance record creation form.
func (h *MaintenanceHandlers) NewRecord(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	vehicleID := r.URL.Query().Get("vehicle_id")

	h.renderPage(w, r, "maintenance_record_form.html", PageData{
		Title: "Record Maintenance Work",
		User:  user,
		Extra: map[string]interface{}{
			"VehicleID":    vehicleID,
			"ServiceTypes": domain.ServiceTypes,
		},
	})
}

// CreateRecord logs completed maintenance work, updates schedule history, and runs resolution checks (Spec 04 §6).
func (h *MaintenanceHandlers) CreateRecord(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.FormValue("vehicle_id")
	serviceType := r.FormValue("service_type")
	if vehicleID == "" || serviceType == "" {
		http.Error(w, "vehicle_id and service_type are required", http.StatusBadRequest)
		return
	}

	perfAt := time.Now().UTC()
	if perfStr := r.FormValue("performed_at"); perfStr != "" {
		if t, err := time.Parse("2006-01-02", perfStr); err == nil {
			perfAt = t.UTC()
		}
	}

	rec := domain.Record{
		ID:          uuid.NewString(),
		VehicleID:   vehicleID,
		ServiceType: serviceType,
		PerformedAt: perfAt,
	}

	if schedID := r.FormValue("schedule_id"); schedID != "" {
		rec.ScheduleID = &schedID
	}
	if odoStr := r.FormValue("odometer_km"); odoStr != "" {
		if odo, err := strconv.ParseFloat(odoStr, 64); err == nil {
			rec.OdometerKM = &odo
		}
	}
	if costStr := r.FormValue("cost"); costStr != "" {
		if c, err := strconv.ParseFloat(costStr, 64); err == nil {
			rec.Cost = &c
		}
	}
	if vendor := r.FormValue("vendor"); vendor != "" {
		rec.Vendor = &vendor
	}
	if notes := r.FormValue("notes"); notes != "" {
		rec.Notes = &notes
	}

	user, _ := h.getUserFromContext(r)
	if user != nil {
		rec.RecordedBy = &user.UserID
	}

	if err := h.repo.InsertRecord(r.Context(), rec); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Spec 04 §6: Evaluate resolution flow when work is recorded
	if h.worker != nil {
		h.worker.EvaluateResolution(r.Context(), vehicleID)
	}

	http.Redirect(w, r, "/maintenance", http.StatusSeeOther)
}

// ListDTC lists DTC events (own org only).
func (h *MaintenanceHandlers) ListDTC(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	vehicleID := r.URL.Query().Get("vehicle_id")
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	events, err := h.repo.ListDtcEventsForTenant(r.Context(), vehicleID, tenantID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "maintenance_index.html", PageData{
		Title: "DTC Events",
		User:  user,
		Extra: map[string]interface{}{
			"DTCs":          events,
			"DueVehicles":   []map[string]interface{}{},
			"Schedules":     []domain.Schedule{},
			"WorkOrders":    []domain.WorkOrder{},
			"VehicleLabels": map[string]string{},
		},
	})
}

// ResolveDTC marks a DTC as resolved and evaluates resolution status.
func (h *MaintenanceHandlers) ResolveDTC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	vehicleID := r.FormValue("vehicle_id")

	if err := h.repo.ResolveDtcEvent(r.Context(), id); err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<span class="text-status-alert text-xs">` + template.HTMLEscapeString(err.Error()) + `</span>`))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if vehicleID != "" && h.worker != nil {
		h.worker.EvaluateResolution(r.Context(), vehicleID)
	}

	if isDatastarRequest(r) {
		w.Header().Set("HX-Trigger", `{"showToast": {"tone":"success","msg":"DTC resolved"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Chip tokens, not raw palette: `bg-emerald-500/10` and friends are
		// frozen values that cannot respond to `.dark`. Safelisted in input.css.
		_, _ = w.Write([]byte(`<span class="inline-flex px-2 py-0.5 text-[11px] rounded-full bg-emerald-soft border border-emerald-line text-emerald-strong font-bold">Resolved</span>`))
		return
	}

	http.Redirect(w, r, "/maintenance", http.StatusSeeOther)
}

// OverrideBlock allows users with maintenance:update to lift a dispatch blocker with reason logging (Spec 04 §6, §12).
func (h *MaintenanceHandlers) OverrideBlock(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "id")
	reason := r.FormValue("reason")
	if reason == "" {
		http.Error(w, "override reason is required", http.StatusBadRequest)
		return
	}

	user, _ := h.getUserFromContext(r)
	actorID := "admin"
	if user != nil {
		actorID = user.UserID
	}

	if err := h.repo.OverrideMaintenance(r.Context(), vehicleID, actorID, reason); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Audit log
	_, _ = h.DB.ExecContext(r.Context(), `
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, tenant_id, created_at)
		VALUES ($1, $2, 'maintenance_override', 'vehicles', $3, $4, $5, CURRENT_TIMESTAMP)`,
		uuid.NewString(), actorID, vehicleID, fmt.Sprintf(`{"reason":%q,"override_by":%q}`, reason, actorID), string(shared.TenantOrPlatform(r.Context())),
	)

	// safeRedirect provably returns only same-origin paths (covered by
	// redirect_test.go), so an attacker-supplied Referer cannot bounce the
	// user off-site. gosec's taint analysis cannot see through the helper.
	target := safeRedirect(r, r.Header.Get("Referer"), "/vehicles/"+vehicleID)
	http.Redirect(w, r, target, http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
}

// ---------------------------------------------------------------------------
// Merged from tyres.go
// ---------------------------------------------------------------------------

type TyreHandlers struct {
	*App
}

func (h *TyreHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/tyres", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/", h.APIListTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/", h.APICreateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/summary", h.APIGetFleetSummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/rotate", h.APIRotateTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}", h.APIGetTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Put("/{id}", h.APIUpdateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/mount", h.APIMountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/dismount", h.APIDismountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/inspect", h.APIRecordInspection)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/retread", h.APIRetreadTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/scrap", h.APIScrapTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/activities", h.APIListActivities)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/inspections", h.APIListInspections)
	})

	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/api/v1/vehicles/{id}/tyres", h.APIGetVehicleTyres)
}

// Routes mounts the Tyre Management web UI handlers at /tyres.
func (h *TyreHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/", h.WebTyresDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/", h.WebCreateTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/mount", h.WebMountTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/dismount", h.WebDismountTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/inspect", h.WebRecordInspection)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/retread", h.WebRetreadTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/scrap", h.WebScrapTyre)
}

// WebTyresDashboard renders the tyre lifecycle and CPKM management cockpit.
func (h *TyreHandlers) WebTyresDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	summary, err := h.service().GetFleetSummary(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load tyre metrics", err.Error(), user)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	tyres, _, err := h.service().ListTyres(r.Context(), tyre.TyreFilter{
		TenantID: tenantID,
		Status:   statusFilter,
		Search:   r.URL.Query().Get("search"),
		Limit:    50,
	})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load tyres", err.Error(), user)
		return
	}

	// Fetch vehicles for mounting dropdown
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var allVehicles []map[string]string
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, vNum, regNum string
			if err := rows.Scan(&id, &vNum, &regNum); err == nil {
				allVehicles = append(allVehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Tyre Management",
		User:  user,
		Extra: map[string]interface{}{
			"Summary":     summary,
			"Tyres":       tyres,
			"AllVehicles": allVehicles,
			"Status":      statusFilter,
		},
	}

	h.renderPage(w, r, "tyres_dashboard.html", data)
}

// WebCreateTyre handles form submission to add a new tyre to stock.
func (h *TyreHandlers) WebCreateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	serial := strings.TrimSpace(r.FormValue("serial_number"))
	if serial == "" {
		h.renderError(w, http.StatusBadRequest, "Serial number required", "serial_number cannot be blank", user)
		return
	}

	brand := strings.TrimSpace(r.FormValue("brand"))
	if brand == "" {
		brand = "MRF"
	}
	model := strings.TrimSpace(r.FormValue("model"))
	size := strings.TrimSpace(r.FormValue("size"))
	if size == "" {
		size = "295/80 R22.5"
	}

	tType := tyre.TyreType(r.FormValue("tyre_type"))
	if !tyre.IsValidTyreType(tType) {
		tType = tyre.TyreTypeRadial
	}

	initTread, _ := strconv.ParseFloat(r.FormValue("initial_tread_depth_mm"), 64)
	if initTread <= 0 {
		initTread = 15.0
	}
	minTread, _ := strconv.ParseFloat(r.FormValue("min_tread_depth_mm"), 64)
	if minTread <= 0 {
		minTread = 3.0
	}
	cost, _ := strconv.ParseFloat(r.FormValue("purchase_cost"), 64)

	in := tyre.CreateTyreInput{
		SerialNumber:        serial,
		Brand:               brand,
		Model:               model,
		Size:                size,
		TyreType:            tType,
		InitialTreadDepthMM: &initTread,
		MinTreadDepthMM:     &minTread,
		PurchaseCost:        cost,
		VendorName:          strings.TrimSpace(r.FormValue("vendor_name")),
		InvoiceNumber:       strings.TrimSpace(r.FormValue("invoice_number")),
	}

	if _, err := h.service().CreateTyre(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebMountTyre handles mounting a tyre onto an axle position.
func (h *TyreHandlers) WebMountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vehID := strings.TrimSpace(r.FormValue("vehicle_id"))
	axle := strings.TrimSpace(r.FormValue("axle_position"))
	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)

	performer := "Fleet Tech"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.MountTyreInput{
		VehicleID:       vehID,
		AxlePosition:    axle,
		MountOdometerKM: odo,
		PerformedBy:     performer,
	}

	if _, err := h.service().MountTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to mount tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebDismountTyre handles dismounting a tyre to stock.
func (h *TyreHandlers) WebDismountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)
	reason := strings.TrimSpace(r.FormValue("reason"))

	performer := "Fleet Tech"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.DismountTyreInput{
		DismountOdometerKM: odo,
		Reason:             reason,
		PerformedBy:        performer,
	}

	if _, err := h.service().DismountTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to dismount tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebRecordInspection records tread depth and pressure check.
func (h *TyreHandlers) WebRecordInspection(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	tread, _ := strconv.ParseFloat(r.FormValue("tread_depth_mm"), 64)
	pressure, _ := strconv.ParseFloat(r.FormValue("pressure_psi"), 64)
	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)
	cond := tyre.InspectionCondition(r.FormValue("condition"))
	if cond == "" {
		cond = tyre.ConditionGood
	}

	inspector := "Inspector"
	if user != nil && user.UserID != "" {
		inspector = user.UserID
	}

	in := tyre.RecordInspectionInput{
		OdometerKM:    odo,
		TreadDepthMM:  tread,
		PressurePSI:   &pressure,
		Condition:     cond,
		ActionTaken:   strings.TrimSpace(r.FormValue("action_taken")),
		InspectorName: inspector,
		Notes:         strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().RecordInspection(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to record tyre inspection", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebRetreadTyre records sending or receiving tyre from retread.
func (h *TyreHandlers) WebRetreadTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	action := strings.TrimSpace(r.FormValue("action"))
	if action == "" {
		action = "SEND"
	}
	cost, _ := strconv.ParseFloat(r.FormValue("cost"), 64)
	newTread, _ := strconv.ParseFloat(r.FormValue("new_tread_depth_mm"), 64)

	performer := "Fleet Manager"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.RetreadTyreInput{
		Action:          action,
		VendorName:      strings.TrimSpace(r.FormValue("vendor_name")),
		Cost:            cost,
		NewTreadDepthMM: &newTread,
		PerformedBy:     performer,
		Notes:           strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().RetreadTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to process retread", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebScrapTyre scraps a tyre and records salvage recovery.
func (h *TyreHandlers) WebScrapTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	recovery, _ := strconv.ParseFloat(r.FormValue("recovery_amount"), 64)
	reason := strings.TrimSpace(r.FormValue("reason"))

	performer := "Fleet Manager"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.ScrapTyreInput{
		Reason:         reason,
		RecoveryAmount: recovery,
		PerformedBy:    performer,
		Notes:          strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().ScrapTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to scrap tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

func writeTyreJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTyreError(w http.ResponseWriter, code int, msg string) {
	writeTyreJSON(w, code, map[string]string{"error": msg})
}

func (h *TyreHandlers) service() *tyre.Service {
	return tyre.NewService(tyre.NewSQLRepository(h.DB))
}

func (h *TyreHandlers) APIListTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := tyre.TyreFilter{
		TenantID:  tenantID,
		VehicleID: r.URL.Query().Get("vehicle_id"),
		Status:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		Brand:     r.URL.Query().Get("brand"),
		Search:    r.URL.Query().Get("q"),
		Limit:     limit,
		Offset:    offset,
	}

	tyres, total, err := h.service().ListTyres(r.Context(), filter)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyres":  tyres,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *TyreHandlers) APICreateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.CreateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	created, err := h.service().CreateTyre(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, tyre.ErrDuplicateSerial) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"tyre": created,
	})
}

func (h *TyreHandlers) APIGetTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	t, err := h.service().GetTyre(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": t,
	})
}

func (h *TyreHandlers) APIUpdateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.UpdateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().UpdateTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIMountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.MountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	mounted, err := h.service().MountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrAxleOccupied) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": mounted,
	})
}

func (h *TyreHandlers) APIDismountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.DismountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	dismounted, err := h.service().DismountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": dismounted,
	})
}

func (h *TyreHandlers) APIRotateTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.RotateTyresInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.service().RotateTyres(r.Context(), tenantID, in); err != nil {
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"message": "tyres rotated successfully",
	})
}

func (h *TyreHandlers) APIRecordInspection(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RecordInspectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	insp, err := h.service().RecordInspection(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"inspection": insp,
	})
}

func (h *TyreHandlers) APIRetreadTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RetreadTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().RetreadTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrMaxRetreadsReached) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIScrapTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.ScrapTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	scrapped, err := h.service().ScrapTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": scrapped,
	})
}

func (h *TyreHandlers) APIGetVehicleTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehID := chi.URLParam(r, "id")

	tyres, err := h.service().GetVehicleTyres(r.Context(), tenantID, vehID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"vehicle_id": vehID,
		"tyres":      tyres,
	})
}

func (h *TyreHandlers) APIGetFleetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetFleetSummary(r.Context(), tenantID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"summary": summary,
	})
}

func (h *TyreHandlers) APIListActivities(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	activities, err := h.service().ListActivities(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"activities": activities,
	})
}

func (h *TyreHandlers) APIListInspections(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	inspections, err := h.service().ListInspections(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"inspections": inspections,
	})
}
