package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/maintenance/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// Work-order (job card) JSON API (00123 feature layer).
// Mounted inside the RequireAPIAuth group in main.go; tenant always comes
// from shared.TenantIDFromContext — never from the request body.

// RegisterAPIRoutes mounts /api/v1/work-orders behind maintenance RBAC.
func (h *MaintenanceHandlers) RegisterAPIRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/work-orders", h.APIListWorkOrders)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/work-orders/{id}", h.APIGetWorkOrder)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "create")).Post("/api/v1/work-orders", h.APICreateWorkOrder)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "update")).Post("/api/v1/work-orders/{id}/assign", h.APIAssignWorkOrder)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "update")).Post("/api/v1/work-orders/{id}/transition", h.APITransitionWorkOrder)
}

func writeWOJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeWOError(w http.ResponseWriter, code int, msg string) {
	writeWOJSON(w, code, map[string]string{"error": msg})
}

// APIListWorkOrders returns the org's job cards, optionally ?status= & ?limit=.
func (h *MaintenanceHandlers) APIListWorkOrders(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	orders, err := h.repo.ListWorkOrders(r.Context(), tenantID, r.URL.Query().Get("status"), limit)
	if err != nil {
		writeWOError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if orders == nil {
		orders = []domain.WorkOrder{}
	}
	writeWOJSON(w, http.StatusOK, map[string]interface{}{"work_orders": orders})
}

// APIGetWorkOrder returns one card of the org; foreign/absent ids are 404.
func (h *MaintenanceHandlers) APIGetWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	wo, err := h.repo.FindWorkOrder(r.Context(), tenantID, chi.URLParam(r, "id"))
	if err != nil {
		writeWOError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wo == nil {
		writeWOError(w, http.StatusNotFound, "work order not found")
		return
	}
	writeWOJSON(w, http.StatusOK, wo)
}

// workOrderCreateBody mirrors the writable fields; id/tenant/status-close are server-owned.
type workOrderCreateBody struct {
	VehicleID    string   `json:"vehicle_id"`
	ScheduleID   *string  `json:"schedule_id"`
	TripID       *string  `json:"trip_id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Assignee     string   `json:"assignee"`
	Vendor       string   `json:"vendor"`
	CostEstimate *float64 `json:"cost_estimate"`
	CostActual   *float64 `json:"cost_actual"`
	DueAt        *string  `json:"due_at"`
}

// APICreateWorkOrder opens a job card in 'open' status for the caller's org.
func (h *MaintenanceHandlers) APICreateWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	var body workOrderCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWOError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	wo := domain.WorkOrder{
		ID:           uuid.NewString(),
		TenantID:     tenantID,
		VehicleID:    strings.TrimSpace(body.VehicleID),
		ScheduleID:   body.ScheduleID,
		TripID:       body.TripID,
		Title:        strings.TrimSpace(body.Title),
		Description:  body.Description,
		Assignee:     body.Assignee,
		Vendor:       body.Vendor,
		CostEstimate: body.CostEstimate,
		CostActual:   body.CostActual,
		Status:       domain.WorkOrderOpen,
	}
	if body.DueAt != nil && *body.DueAt != "" {
		t, err := time.Parse(time.RFC3339, *body.DueAt)
		if err != nil {
			writeWOError(w, http.StatusBadRequest, "due_at must be RFC3339")
			return
		}
		tUTC := t.UTC()
		wo.DueAt = &tUTC
	}
	if err := h.repo.CreateWorkOrder(r.Context(), wo); err != nil {
		writeWOError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := h.repo.FindWorkOrder(r.Context(), tenantID, wo.ID)
	if err != nil || saved == nil {
		writeWOError(w, http.StatusInternalServerError, "work order created but not readable")
		return
	}
	writeWOJSON(w, http.StatusCreated, saved)
}

// APIAssignWorkOrder sets mechanic/vendor; open cards move to assigned.
func (h *MaintenanceHandlers) APIAssignWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	var body struct {
		Assignee string `json:"assignee"`
		Vendor   string `json:"vendor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWOError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.repo.AssignWorkOrder(r.Context(), tenantID, id, body.Assignee, body.Vendor); err != nil {
		writeWOError(w, h.woMutateStatus(r, tenantID, id), err.Error())
		return
	}
	wo, _ := h.repo.FindWorkOrder(r.Context(), tenantID, id)
	writeWOJSON(w, http.StatusOK, wo)
}

// APITransitionWorkOrder moves a card along its lifecycle; terminal cards are immutable.
func (h *MaintenanceHandlers) APITransitionWorkOrder(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeWOError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	switch body.Status {
	case domain.WorkOrderOpen, domain.WorkOrderAssigned, domain.WorkOrderInProgress,
		domain.WorkOrderOnHold, domain.WorkOrderDone, domain.WorkOrderCancelled:
	default:
		writeWOError(w, http.StatusBadRequest, "unknown work order status")
		return
	}
	if body.Status == domain.WorkOrderDone {
		var actor string
		if user, _ := h.getUserFromContext(r); user != nil {
			actor = user.UserID
		}
		wo, err := h.repo.CompleteWorkOrder(r.Context(), tenantID, id, actor)
		if err != nil {
			writeWOError(w, h.woMutateStatus(r, tenantID, id), err.Error())
			return
		}
		if h.worker != nil {
			h.worker.EvaluateResolution(r.Context(), wo.VehicleID)
		}
		writeWOJSON(w, http.StatusOK, wo)
		return
	}
	if err := h.repo.TransitionWorkOrder(r.Context(), tenantID, id, body.Status); err != nil {
		writeWOError(w, h.woMutateStatus(r, tenantID, id), err.Error())
		return
	}
	wo, _ := h.repo.FindWorkOrder(r.Context(), tenantID, id)
	writeWOJSON(w, http.StatusOK, wo)
}

// woMutateStatus maps a failed mutation to 404 (absent/foreign) or 409 (terminal).
func (h *MaintenanceHandlers) woMutateStatus(r *http.Request, tenantID, id string) int {
	wo, err := h.repo.FindWorkOrder(r.Context(), tenantID, id)
	if err != nil || wo == nil {
		return http.StatusNotFound
	}
	return http.StatusConflict
}
