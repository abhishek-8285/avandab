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

	"transport-app/internal/maintenance"
	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// MaintenanceHandlers powers the preventive maintenance UI, schedule management, and DTC intake (Spec 04 §6, §12).
type MaintenanceHandlers struct {
	*App
	repo   *maintsql.MaintenanceRepository
	worker *maintenance.Worker
}

// NewMaintenanceHandlers creates a new MaintenanceHandlers instance.
func NewMaintenanceHandlers(app *App, db *sql.DB) *MaintenanceHandlers {
	return &MaintenanceHandlers{
		App:  app,
		repo: maintsql.NewMaintenanceRepository(db),
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
}

// Index renders the preventive maintenance dashboard (due vehicles, active schedules, DTCs, recent records).
func (h *MaintenanceHandlers) Index(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	dueVehicles, err := h.repo.ListDueVehicles(r.Context(), tenantID)
	if err != nil {
		dueVehicles = []map[string]interface{}{}
	}

	schedules, err := h.repo.ListActiveSchedules(r.Context(), "")
	if err != nil {
		schedules = []domain.Schedule{}
	}

	dtcs, err := h.repo.ListDtcEvents(r.Context(), "", 20)
	if err != nil {
		dtcs = []domain.DtcEvent{}
	}

	records, err := h.repo.ListRecords(r.Context(), "", 20)
	if err != nil {
		records = []domain.Record{}
	}

	data := PageData{
		Title: "Preventive Maintenance",
		User:  user,
		Extra: map[string]interface{}{
			"DueVehicles": dueVehicles,
			"Schedules":   schedules,
			"DTCs":        dtcs,
			"Records":     records,
		},
	}

	h.renderPage(w, r, "maintenance_index.html", data)
}

// ListSchedules lists schedules for a vehicle or fleetwide.
func (h *MaintenanceHandlers) ListSchedules(w http.ResponseWriter, r *http.Request) {
	vehicleID := r.URL.Query().Get("vehicle_id")
	schedules, err := h.repo.ListSchedules(r.Context(), vehicleID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	user, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "maintenance_index.html", PageData{
		Title: "Maintenance Schedules",
		User:  user,
		Extra: map[string]interface{}{
			"Schedules": schedules,
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

// ListDTC lists DTC events.
func (h *MaintenanceHandlers) ListDTC(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	vehicleID := r.URL.Query().Get("vehicle_id")
	events, err := h.repo.ListDtcEvents(r.Context(), vehicleID, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.renderPage(w, r, "maintenance_index.html", PageData{
		Title: "DTC Events",
		User:  user,
		Extra: map[string]interface{}{
			"DTCs": events,
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
		_, _ = w.Write([]byte(`<span class="inline-flex px-2 py-0.5 text-[11px] rounded-full bg-emerald-500/10 border border-emerald-500/15 text-emerald-700 font-bold">Resolved</span>`))
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
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, created_at)
		VALUES (?, ?, 'maintenance_override', 'vehicles', ?, ?, CURRENT_TIMESTAMP)`,
		uuid.NewString(), actorID, vehicleID, fmt.Sprintf(`{"reason":%q,"override_by":%q}`, reason, actorID),
	)

	redirect := r.Header.Get("Referer")
	if redirect == "" {
		redirect = "/vehicles/" + vehicleID
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
