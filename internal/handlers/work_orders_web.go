package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/shared"
)

// Work-order (job card) web UI — HTML pages under /maintenance reusing the
// same tenant-scoped repo calls as the JSON API (work_orders.go).

// NewWorkOrder renders the job-card creation form.
func (h *MaintenanceHandlers) NewWorkOrder(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	h.renderPage(w, r, "maintenance_work_order_form.html", PageData{
		Title: "New Job Card",
		User:  user,
		Extra: map[string]interface{}{
			"VehicleID": r.URL.Query().Get("vehicle_id"),
		},
	})
}

// CreateWorkOrder opens a job card in 'open' status, then shows its detail page.
func (h *MaintenanceHandlers) CreateWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := r.FormValue("vehicle_id")
	title := r.FormValue("title")
	if vehicleID == "" || title == "" {
		http.SetCookie(w, flashCookie("flash_error", "vehicle and title are required"))
		http.Redirect(w, r, "/maintenance/work-orders/new", http.StatusSeeOther)
		return
	}

	wo := domain.WorkOrder{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		VehicleID:   vehicleID,
		Title:       title,
		Description: r.FormValue("description"),
		Assignee:    r.FormValue("assignee"),
		Vendor:      r.FormValue("vendor"),
		Status:      domain.WorkOrderOpen,
	}
	if s := r.FormValue("schedule_id"); s != "" {
		wo.ScheduleID = &s
	}
	if s := r.FormValue("trip_id"); s != "" {
		wo.TripID = &s
	}
	if f, err := strconv.ParseFloat(r.FormValue("cost_estimate"), 64); err == nil {
		wo.CostEstimate = &f
	}
	if f, err := strconv.ParseFloat(r.FormValue("cost_actual"), 64); err == nil {
		wo.CostActual = &f
	}
	if d := r.FormValue("due_at"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			tUTC := t.UTC()
			wo.DueAt = &tUTC
		}
	}

	if err := h.repo.CreateWorkOrder(r.Context(), wo); err != nil {
		http.SetCookie(w, flashCookie("flash_error", "Could not open job card: "+err.Error()))
		http.Redirect(w, r, "/maintenance/work-orders/new?vehicle_id="+vehicleID, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/maintenance/work-orders/"+wo.ID, http.StatusSeeOther)
}

// ViewWorkOrder renders one job card of the org with assign/transition actions.
func (h *MaintenanceHandlers) ViewWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	wo, err := h.repo.FindWorkOrder(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wo == nil {
		http.Error(w, "work order not found", http.StatusNotFound)
		return
	}
	user, _ := h.getUserFromContext(r)
	vehicleLabel := ""
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(NULLIF(registration_number,''), vehicle_number, id) FROM vehicles WHERE id = ?`,
		wo.VehicleID).Scan(&vehicleLabel)
	h.renderPage(w, r, "maintenance_work_order.html", PageData{
		Title: "Job Card",
		User:  user,
		Extra: map[string]interface{}{"WorkOrder": wo, "VehicleLabel": vehicleLabel},
	})
}

// AssignWorkOrder sets mechanic/vendor from the detail page form.
func (h *MaintenanceHandlers) AssignWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	if err := h.repo.AssignWorkOrder(r.Context(), tenantID, id, r.FormValue("assignee"), r.FormValue("vendor")); err != nil {
		http.SetCookie(w, flashCookie("flash_error", err.Error()))
	}
	http.Redirect(w, r, "/maintenance/work-orders/"+id, http.StatusSeeOther)
}

// TransitionWorkOrder moves a card along its lifecycle from the detail page.
func (h *MaintenanceHandlers) TransitionWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	to := r.FormValue("status")
	switch to {
	case domain.WorkOrderOpen, domain.WorkOrderAssigned, domain.WorkOrderInProgress,
		domain.WorkOrderOnHold, domain.WorkOrderDone, domain.WorkOrderCancelled:
		if to == domain.WorkOrderDone {
			// Closing the books: done + service record in one repo call,
			// then due-flag re-evaluation. A record failure surfaces as a
			// banner; the card stays done.
			var actor string
			if user, _ := h.getUserFromContext(r); user != nil {
				actor = user.UserID
			}
			if wo, err := h.repo.CompleteWorkOrder(r.Context(), tenantID, id, actor); err != nil {
				http.SetCookie(w, flashCookie("flash_error", err.Error()))
			} else if h.worker != nil {
				h.worker.EvaluateResolution(r.Context(), wo.VehicleID)
			}
		} else if err := h.repo.TransitionWorkOrder(r.Context(), tenantID, id, to); err != nil {
			http.SetCookie(w, flashCookie("flash_error", err.Error()))
		}
	default:
		http.SetCookie(w, flashCookie("flash_error", "unknown work order status"))
	}
	http.Redirect(w, r, "/maintenance/work-orders/"+id, http.StatusSeeOther)
}
