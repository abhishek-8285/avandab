package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"transport-app/internal/apperr"
	"transport-app/internal/httpx"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// FounderHandlers exposes the founder visibility layer (Spec 16 §6, §7):
// signals, audit trail, and an aggregate dashboard. It embeds *App so that
// page-rendering helpers (renderPage, getUserFromContext) are promoted.
type FounderHandlers struct {
	*App
	Signals *service.FounderSignalsService
	Audit   *service.FounderAuditService
	KPIs    *service.KPIService
	authSrv auth.AuthorizationService
}

// NewFounderHandlers constructs a FounderHandlers instance.
func NewFounderHandlers(app *App, signals *service.FounderSignalsService, audit *service.FounderAuditService, authSrv auth.AuthorizationService) *FounderHandlers {
	return &FounderHandlers{App: app, Signals: signals, Audit: audit, authSrv: authSrv}
}

// RegisterRoutes mounts the founder endpoints under /api/v1/founder.
func (h *FounderHandlers) RegisterRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/signals", h.ListSignals)
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/signals/{id}", h.GetSignal)
	r.With(middleware.RequirePermission(h.authSrv, "founder", "update")).
		Post("/api/v1/founder/signals/{id}/acknowledge", h.AcknowledgeSignal)
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/audit", h.ListAudit)
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/dashboard", h.Dashboard)
	// Spec 22 §10-S12 — pilot KPI readout (?days=14 default).
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/kpis", h.PilotKPIs)
}

// PilotKPIs handles GET /api/v1/founder/kpis — the Spec 22 §12 pilot
// scorecard computed from existing tables.
func (h *FounderHandlers) PilotKPIs(w http.ResponseWriter, r *http.Request) {
	if h.KPIs == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeNotImplemented))
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 14
	}
	kpis, err := h.KPIs.PilotKPIs(r.Context(), string(shared.TenantIDFromContext(r.Context())), days)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, kpis)
}

func (h *FounderHandlers) tenantID(r *http.Request) string {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}
	return tenantID
}

// ListSignals handles GET /api/v1/founder/signals
func (h *FounderHandlers) ListSignals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := service.SignalFilters{
		SignalType:         q.Get("signal_type"),
		UnacknowledgedOnly: q.Get("unacknowledged") == "true" || q.Get("unacknowledged") == "1",
	}
	signals, total, err := h.Signals.ListSignals(r.Context(), h.tenantID(r), filters)
	if err != nil {
		http.Error(w, `{"error":"failed to list signals"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"signals": signals, "total": total})
}

// GetSignal handles GET /api/v1/founder/signals/{id}
func (h *FounderHandlers) GetSignal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"signal id required"}`, http.StatusBadRequest)
		return
	}
	signals, _, err := h.Signals.ListSignals(r.Context(), h.tenantID(r), service.SignalFilters{})
	if err != nil {
		http.Error(w, `{"error":"failed to list signals"}`, http.StatusInternalServerError)
		return
	}
	for _, s := range signals {
		if s.ID == id {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s)
			return
		}
	}
	http.Error(w, `{"error":"signal not found"}`, http.StatusNotFound)
}

// AcknowledgeSignal handles POST /api/v1/founder/signals/{id}/acknowledge
func (h *FounderHandlers) AcknowledgeSignal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		http.Error(w, `{"error":"signal id required"}`, http.StatusBadRequest)
		return
	}
	userID := "system"
	role := "admin"
	if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
		userID = s.UserID
		if s.Role != "" {
			role = s.Role
		}
	}
	if err := h.Signals.AcknowledgeSignal(r.Context(), id, userID, role); err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, "'")+`"}`, http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"signal_id": id, "acknowledged": true})
}

// ListAudit handles GET /api/v1/founder/audit
func (h *FounderHandlers) ListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := service.AuditFilters{
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
	}
	entries, total, err := h.Audit.ListAudit(r.Context(), h.tenantID(r), filters)
	if err != nil {
		http.Error(w, `{"error":"failed to list audit"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"audit": entries, "total": total})
}

// Dashboard handles GET /api/v1/founder/dashboard returning aggregate founder metrics.
func (h *FounderHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := h.tenantID(r)
	if h.Signals != nil && h.Audit != nil {
		_ = h.Audit.RecordAudit(ctx, service.AuditEntry{
			TenantID:     tenantID,
			ActorID:      "system",
			ActorRole:    "admin",
			Action:       service.AuditActionFounderDashboard,
			ResourceType: "founder_dashboard",
			ResourceID:   tenantID,
			Details:      "{}",
		})
	}

	var unackCount int
	if h.Signals != nil {
		unackCount, _ = h.Signals.CountUnacknowledged(ctx, tenantID)
	}
	recentSignals, _, _ := h.Signals.ListSignals(ctx, tenantID, service.SignalFilters{UnacknowledgedOnly: true, Limit: 5})

	var activeExp int
	if h.App.Services.Experiments != nil {
		activeExp, _ = h.App.Services.Experiments.CountByStatus(ctx, tenantID, service.ExperimentStatusRunning)
	}
	var openAlerts int
	if h.App.Services.OpsAlerts != nil {
		openAlerts, _ = h.App.Services.OpsAlerts.CountByStatus(ctx, tenantID, service.OpsAlertStatusOpen)
	}
	var latestPNL interface{}
	if h.App.Services.PNL != nil {
		latestPNL, _ = h.App.Services.PNL.GetLatest(ctx, tenantID)
	}

	data := map[string]interface{}{
		"unacknowledged_signals": unackCount,
		"recent_signals":         recentSignals,
		"active_experiments":     activeExp,
		"open_ops_alerts":        openAlerts,
		"latest_pnl":             latestPNL,
		"active_nav":             "founder",
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
