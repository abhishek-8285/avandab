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
	experimentapp "transport-app/internal/experiments"
	founderapp "transport-app/internal/founder"
	"transport-app/internal/middleware"
	pnlapp "transport-app/internal/pnl"
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
	// Spec 23 §10-R0 — storage measurement for the scale triggers.
	r.With(middleware.RequirePermission(h.authSrv, "founder", "read")).
		Get("/api/v1/founder/storage", h.Storage)
}

// Storage handles GET /api/v1/founder/storage (Spec 23 R0).
func (h *FounderHandlers) Storage(w http.ResponseWriter, r *http.Request) {
	if h.KPIs == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeNotImplemented))
		return
	}
	stats, err := h.KPIs.StorageStats(r.Context())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, stats)
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

// tenantID resolves the acting org. Fail closed: founder routes sit behind
// auth middleware which always sets tenant (panics surface as 500 via Recoverer).
func (h *FounderHandlers) tenantID(r *http.Request) string {
	return string(shared.MustTenantID(r.Context()))
}

// ListSignals handles GET /api/v1/founder/signals
func (h *FounderHandlers) ListSignals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := founderapp.SignalFilters{
		SignalType:         q.Get("signal_type"),
		UnacknowledgedOnly: q.Get("unacknowledged") == "true" || q.Get("unacknowledged") == "1",
	}
	signals, total, err := founderapp.NewSignalQueries(h.DB).ListSignals(r.Context(), h.tenantID(r), filters)
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
	signal, err := founderapp.NewSignalQueries(h.DB).FindByID(r.Context(), h.tenantID(r), id)
	if err != nil {
		http.Error(w, `{"error":"failed to list signals"}`, http.StatusInternalServerError)
		return
	}
	if signal == nil {
		http.Error(w, `{"error":"signal not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(signal)
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
	if err := founderapp.NewSignalQueries(h.DB).Acknowledge(r.Context(), h.tenantID(r), id, userID); err != nil {
		http.Error(w, `{"error":"`+strings.ReplaceAll(err.Error(), `"`, "'")+`"}`, http.StatusConflict)
		return
	}
	if h.Audit != nil {
		_ = h.Audit.RecordAudit(r.Context(), service.AuditEntry{
			TenantID:     h.tenantID(r),
			ActorID:      userID,
			ActorRole:    role,
			Action:       service.AuditActionSignalAcknowledge,
			ResourceType: "founder_signal",
			ResourceID:   id,
			Details:      "{}",
		})
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
	if h.Audit != nil {
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

	unackCount, _ := founderapp.NewSignalQueries(h.DB).CountUnacknowledged(ctx, tenantID)
	recentSignals, _, _ := founderapp.NewSignalQueries(h.DB).ListSignals(ctx, tenantID, founderapp.SignalFilters{UnacknowledgedOnly: true, Limit: 5})

	var activeExp int
	activeExp, _ = experimentapp.NewQueries(h.DB).CountByStatus(ctx, tenantID, experimentapp.ExperimentStatusRunning)
	var openAlerts int
	if h.App.Services.OpsAlerts != nil {
		openAlerts, _ = h.App.Services.OpsAlerts.CountByStatus(ctx, tenantID, service.OpsAlertStatusOpen)
	}
	var latestPNL interface{}
	latestPNL, _ = pnlapp.NewSnapshotQueries(h.DB).Latest(ctx, tenantID)

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
