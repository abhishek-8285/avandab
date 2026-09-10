package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"transport-app/internal/maintenance"
	"transport-app/internal/maintenance/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// RegisterPlanAPIRoutes mounts the /api/v1/maintenance/plans REST endpoints (Spec 04 §14, B3).
func (h *MaintenanceHandlers) RegisterPlanAPIRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/maintenance/plans", h.APIListMaintenancePlans)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/maintenance/plans/{id}", h.APIGetMaintenancePlan)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "create")).Post("/api/v1/maintenance/plans", h.APICreateMaintenancePlan)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "update")).Post("/api/v1/maintenance/plans/{id}/evaluate", h.APIEvaluateMaintenancePlan)
}

func writeMaintenanceJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeMaintenanceError(w http.ResponseWriter, code int, msg string) {
	writeMaintenanceJSON(w, code, map[string]string{"error": msg})
}

// CreateMaintenancePlanRequest wire payload.
type CreateMaintenancePlanRequest struct {
	PlanNumber         string   `json:"plan_number"`
	VehicleID          string   `json:"vehicle_id"`
	MeasuringPointID   *string  `json:"measuring_point_id,omitempty"`
	ServiceType        string   `json:"service_type"`
	Description        string   `json:"description"`
	CycleIntervalKM    *float64 `json:"cycle_interval_km,omitempty"`
	CycleIntervalDays  *int     `json:"cycle_interval_days,omitempty"`
	CallHorizonPercent float64  `json:"call_horizon_percent"`
	LastScheduledKM    *float64 `json:"last_scheduled_km,omitempty"`
	NextDueKM          *float64 `json:"next_due_km,omitempty"`
}

// APICreateMaintenancePlan creates a single cycle maintenance plan.
func (h *MaintenanceHandlers) APICreateMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeMaintenanceError(w, http.StatusUnauthorized, "unauthorized: tenant context missing")
		return
	}

	var req CreateMaintenancePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMaintenanceError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PlanNumber == "" || req.VehicleID == "" || req.ServiceType == "" {
		writeMaintenanceError(w, http.StatusBadRequest, "plan_number, vehicle_id, and service_type are required")
		return
	}

	if req.CallHorizonPercent <= 0 {
		req.CallHorizonPercent = 100.0
	}

	now := time.Now().UTC()
	planID := uuid.NewString()

	plan := domain.MaintenancePlan{
		ID:                 planID,
		TenantID:           tenantID,
		PlanNumber:         req.PlanNumber,
		VehicleID:          req.VehicleID,
		MeasuringPointID:   req.MeasuringPointID,
		ServiceType:        req.ServiceType,
		Description:        req.Description,
		CycleIntervalKM:    req.CycleIntervalKM,
		CycleIntervalDays:  req.CycleIntervalDays,
		CallHorizonPercent: req.CallHorizonPercent,
		LastScheduledKM:    req.LastScheduledKM,
		NextDueKM:          req.NextDueKM,
		Status:             "active",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Compute initial projection
	currOdo, _ := h.repo.GetLatestOdometer(r.Context(), req.VehicleID)
	mpID := ""
	if req.MeasuringPointID != nil {
		mpID = *req.MeasuringPointID
	}
	annualEstimate, _ := h.repo.GetMeasuringPointAnnualEstimate(r.Context(), tenantID, req.VehicleID, mpID)

	nextKM, nextDate, _ := maintenance.CalculateProjection(plan, currOdo, annualEstimate, now)
	plan.NextDueKM = nextKM
	plan.NextDueDate = nextDate

	if err := h.repo.CreateMaintenancePlan(r.Context(), plan); err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMaintenanceJSON(w, http.StatusCreated, plan)
}

// APIListMaintenancePlans returns active and configured maintenance plans for tenant.
func (h *MaintenanceHandlers) APIListMaintenancePlans(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := r.URL.Query().Get("vehicle_id")

	plans, err := h.repo.ListMaintenancePlans(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"maintenance_plans": plans,
		"count":             len(plans),
	})
}

// APIGetMaintenancePlan returns a single maintenance plan with live projections.
func (h *MaintenanceHandlers) APIGetMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	plan, err := h.repo.FindMaintenancePlan(r.Context(), tenantID, id)
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		writeMaintenanceError(w, http.StatusNotFound, "maintenance plan not found")
		return
	}

	currOdo, _ := h.repo.GetLatestOdometer(r.Context(), plan.VehicleID)
	mpID := ""
	if plan.MeasuringPointID != nil {
		mpID = *plan.MeasuringPointID
	}
	annualEstimate, _ := h.repo.GetMeasuringPointAnnualEstimate(r.Context(), tenantID, plan.VehicleID, mpID)

	projKM, projDate, needsCall := maintenance.CalculateProjection(*plan, currOdo, annualEstimate, time.Now().UTC())

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"maintenance_plan":   plan,
		"current_odometer":   currOdo,
		"annual_estimate":    annualEstimate,
		"projected_due_km":   projKM,
		"projected_due_date": projDate,
		"call_horizon_due":   needsCall,
	})
}

// APIEvaluateMaintenancePlan triggers immediate schedule evaluation and work order creation.
func (h *MaintenanceHandlers) APIEvaluateMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	if h.scheduler == nil {
		h.scheduler = maintenance.NewPlanScheduler(h.DB, h.repo)
	}

	wo, err := h.scheduler.EvaluatePlan(r.Context(), tenantID, id, time.Now().UTC())
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if wo != nil {
		writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
			"status":         "call_triggered",
			"call_triggered": true,
			"work_order":     wo,
		})
		return
	}

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "up_to_date",
		"call_triggered": false,
	})
}
