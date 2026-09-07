package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// OpsAlertHandlers handles operational alerting API routes (Spec 16 §4).
type OpsAlertHandlers struct {
	App     *App
	Alerts  *service.OpsAlertService
	authSrv auth.AuthorizationService
}

// NewOpsAlertHandlers constructs an OpsAlertHandlers instance.
func NewOpsAlertHandlers(app *App, alerts *service.OpsAlertService, authSrv auth.AuthorizationService) *OpsAlertHandlers {
	return &OpsAlertHandlers{
		App:     app,
		Alerts:  alerts,
		authSrv: authSrv,
	}
}

// RegisterRoutes mounts the ops alerts endpoints onto the router.
func (h *OpsAlertHandlers) RegisterRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "read")).
		Get("/api/v1/ops-alerts", h.List)
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "read")).
		Get("/api/v1/ops-alerts/{id}", h.Get)
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "update")).
		Post("/api/v1/ops-alerts/{id}/acknowledge", h.Acknowledge)
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "update")).
		Post("/api/v1/ops-alerts/{id}/resolve", h.Resolve)
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "update")).
		Post("/api/v1/ops-alerts/{id}/dismiss", h.Dismiss)
	r.With(middleware.RequirePermission(h.authSrv, "ops_alerts", "update")).
		Post("/api/v1/ops-alerts/generate", h.GenerateManual)
}

// List returns a paginated list of operational alerts with optional filters.
// GET /api/v1/ops-alerts?status=open&type=vehicle_breakdown&severity=critical&page=1&limit=50
func (h *OpsAlertHandlers) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := string(shared.MustTenantID(ctx))

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	filters := service.OpsAlertFilters{
		Status:   q.Get("status"),
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		Page:     page,
		Limit:    limit,
	}

	alerts, total, err := h.Alerts.ListAlerts(ctx, tenantID, filters)
	if err != nil {
		http.Error(w, `{"error":"failed to list operational alerts"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": alerts,
		"total":  total,
		"page":   filters.Page,
		"limit":  filters.Limit,
	})
}

// Get returns a single operational alert by ID.
// GET /api/v1/ops-alerts/{id}
func (h *OpsAlertHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"alert id required"}`, http.StatusBadRequest)
		return
	}

	alert, err := h.Alerts.GetAlert(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrAlertNotFound) {
			http.Error(w, `{"error":"alert not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to get alert"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(alert)
}

// Acknowledge marks an open alert as acknowledged.
// POST /api/v1/ops-alerts/{id}/acknowledge
func (h *OpsAlertHandlers) Acknowledge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"alert id required"}`, http.StatusBadRequest)
		return
	}

	userID := "system"
	if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
		userID = s.UserID
	}

	err := h.Alerts.AcknowledgeAlert(r.Context(), id, userID)
	if err != nil {
		if errors.Is(err, service.ErrAlertNotFoundOrAlreadyAcknowledged) {
			http.Error(w, `{"error":"alert not found or not in open state"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"failed to acknowledge alert"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "acknowledged",
		"alert_id": id,
	})
}

// Resolve marks an open or acknowledged alert as resolved.
// POST /api/v1/ops-alerts/{id}/resolve
// Body: {"note": "Repaired in workshop"}
func (h *OpsAlertHandlers) Resolve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"alert id required"}`, http.StatusBadRequest)
		return
	}

	userID := "system"
	if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
		userID = s.UserID
	}

	var req struct {
		Note string `json:"note"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	err := h.Alerts.ResolveAlert(r.Context(), id, userID, req.Note)
	if err != nil {
		if errors.Is(err, service.ErrAlertNotFoundOrAlreadyResolved) {
			http.Error(w, `{"error":"alert not found or already in terminal state"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"failed to resolve alert"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "resolved",
		"alert_id": id,
	})
}

// Dismiss marks an open or acknowledged alert as dismissed.
// POST /api/v1/ops-alerts/{id}/dismiss
// Body: {"reason": "False alarm"}
func (h *OpsAlertHandlers) Dismiss(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"alert id required"}`, http.StatusBadRequest)
		return
	}

	userID := "system"
	if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
		userID = s.UserID
	}

	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	err := h.Alerts.DismissAlert(r.Context(), id, userID, req.Reason)
	if err != nil {
		if errors.Is(err, service.ErrAlertNotFoundOrAlreadyDismissed) {
			http.Error(w, `{"error":"alert not found or already in terminal state"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"failed to dismiss alert"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "dismissed",
		"alert_id": id,
	})
}

// GenerateManual allows an authorized operator or admin to create an alert manually.
// POST /api/v1/ops-alerts/generate
func (h *OpsAlertHandlers) GenerateManual(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := string(shared.MustTenantID(ctx))

	var req struct {
		AlertType   string  `json:"alert_type"`
		Severity    string  `json:"severity"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		EntityType  *string `json:"entity_type"`
		EntityID    *string `json:"entity_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.AlertType == "" || req.Title == "" {
		http.Error(w, `{"error":"alert_type and title are required"}`, http.StatusBadRequest)
		return
	}

	id, err := h.Alerts.CreateAlert(ctx, service.OpsAlert{
		TenantID:    tenantID,
		AlertType:   req.AlertType,
		Severity:    req.Severity,
		Title:       req.Title,
		Description: req.Description,
		EntityType:  req.EntityType,
		EntityID:    req.EntityID,
	})
	if err != nil {
		http.Error(w, `{"error":"failed to create alert"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"alert_id": id,
		"status":   "open",
	})
}
