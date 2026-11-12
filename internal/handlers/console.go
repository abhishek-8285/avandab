package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"

	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/alerts/repository"
	"transport-app/internal/apperr"
	"transport-app/internal/auth"
	"transport-app/internal/cache"
	"transport-app/internal/eta"
	"transport-app/internal/httpx"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/service"
	"transport-app/internal/shared"

	"strings"
	ctapp "transport-app/internal/controltower/application"
	"transport-app/internal/controltower/domain"
	"transport-app/internal/middleware"
)

// ConsoleHandlers serves the owner Command Center (Spec 22 §4.1):
// ranked alert inbox (S1), money strip (S2), fleet strip + context
// panel (S3) and inline panel actions (S4).
type ConsoleHandlers struct {
	app        *App
	repo       repository.AlertRepository
	pnl        *service.PNLService
	db         *sql.DB
	eta        *eta.EtaService
	fleetCache cache.Cache
	ewbClient  intEWB.Client
	ewbExtend  bool // EWB_EXTEND_ENABLED: call the real provider adapter
}

func NewConsoleHandlers(app *App, repo repository.AlertRepository, pnl *service.PNLService, db *sql.DB, etaSvc *eta.EtaService, fleetCache cache.Cache) *ConsoleHandlers {
	if fleetCache == nil {
		fleetCache = cache.Noop{}
	}
	return &ConsoleHandlers{app: app, repo: repo, pnl: pnl, db: db, eta: etaSvc, fleetCache: fleetCache}
}

// WithEwayBillAdapter attaches the integration client used by the inline
// extend action; only called when EWB_EXTEND_ENABLED is true.
func (h *ConsoleHandlers) WithEwayBillAdapter(client intEWB.Client, extendEnabled bool) *ConsoleHandlers {
	h.ewbClient = client
	h.ewbExtend = extendEnabled
	return h
}

// Page handles GET /console.
func (h *ConsoleHandlers) Page(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	alerts, err := h.repo.ListInbox(r.Context(), tenantID, "open", 50)
	if err != nil {
		h.app.renderError(w, http.StatusInternalServerError, "Console unavailable", "Could not load the alert inbox.", nil)
		return
	}
	user, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	// Spec 22 §10-S12 — console-open usage event (best-effort analytics).
	if h.app != nil && h.app.Founder != nil && h.app.Founder.KPIs != nil && user != nil {
		h.app.Founder.KPIs.RecordConsoleUsage(r.Context(), tenantID, user.UserID, "console_open")
	}
	inbox := make([]map[string]any, 0, len(alerts))
	for _, a := range alerts {
		item := map[string]any{
			"ID":           a.ID,
			"Title":        a.Title,
			"Severity":     a.Severity,
			"SeverityRank": a.SeverityRank,
			"MoneyAtRisk":  a.MoneyAtRisk,
			"CreatedAt":    a.CreatedAt.Format("02 Jan 15:04"),
			"AckStatus":    a.AckStatus,
			"Occurrences":  a.Occurrences,
		}
		if a.EntityType != nil && *a.EntityType == "vehicle" && a.EntityID != nil {
			item["VehicleID"] = *a.EntityID
		}
		inbox = append(inbox, item)
	}

	// Money strip is best-effort on the page render: without PNL service or
	// on error the partial renders placeholders instead of failing the page.
	var strip *service.MoneyStrip
	if h.pnl != nil {
		strip, _ = h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	}

	h.app.renderPage(w, r, "console.html", PageData{
		Title: "Command Center",
		User:  user,
		Extra: map[string]interface{}{
			"InboxAlerts": inbox,
			"MoneyStrip":  strip,
		},
	})
}

// MoneyStrip handles GET /api/dashboard/money-strip (Spec 22 §2.2).
func (h *ConsoleHandlers) MoneyStrip(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if h.pnl == nil || h.repo == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	strip, err := h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	open, critical, err := h.repo.InboxCounts(r.Context(), tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"date":        strip.Date,
		"revenue":     strip.Revenue,
		"spent":       strip.Spent,
		"receivables": strip.Receivables,
		"open_alerts": open,
		"critical":    critical,
	})
}

// ── S3: Fleet strip + vehicle context panel (Spec 22 §2.3, §4.1) ────────

// Fleet handles GET /api/fleet — every vehicle with its latest position
// (single composite query; no N+1).
func (h *ConsoleHandlers) Fleet(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	items, err := ctapp.NewConsoleReadModel(h.db, h.eta).Fleet(r.Context(), tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"vehicles": items})
}

const contextCacheTTL = 60 * time.Second

// VehicleContext handles GET /api/fleet/{vehicleId}/context. Every section
// comes from its real tables via single tenant-scoped queries (no N+1),
// cached for 60s per (tenant, vehicle). Unknown vehicle for the tenant → 404.
func (h *ConsoleHandlers) VehicleContext(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "vehicleId")
	cacheKey := "fleetctx:" + tenantID + ":" + vehicleID

	if blob, ok, _ := h.fleetCache.Get(r.Context(), cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(blob)
		return
	}

	resp, err := ctapp.NewConsoleReadModel(h.db, h.eta).VehicleContext(r.Context(), tenantID, vehicleID)
	if errors.Is(err, ctapp.ErrConsoleNotFound) {
		httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	blob, err := json.Marshal(resp)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	_ = h.fleetCache.Set(r.Context(), cacheKey, blob, contextCacheTTL)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(blob)
}

// ── S4: Inline panel actions (Spec 22 §2.3, §10 Step 4) ─────────────────

type extendBody struct {
	ValidUptoHours int `json:"valid_upto_hours"`
}

// ExtendEwayBill handles POST /api/ewaybill/{id}/extend
// {"valid_upto_hours":4} → {"ok":true,"new_expiry":"..."}.
// Default behavior is the repo-wide mock contract: shift expiry locally.
// With EWB_EXTEND_ENABLED=true the provider adapter is consulted first and
// its returned validity wins. Every call writes an eway_bill_events row and
// an audit_logs entry (Spec 22 Step 4 exit gate).
func (h *ConsoleHandlers) ExtendEwayBill(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	var body extendBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.ValidUptoHours <= 0 || body.ValidUptoHours > 24 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "valid_upto_hours must be between 1 and 24"})
		return
	}

	ctx := r.Context()
	tenantID := string(shared.TenantIDFromContext(ctx))
	ewbID := chi.URLParam(r, "id")

	uc := ctapp.NewExtendEwayBillUseCase(h.db, h.ewbClient, h.ewbExtend)
	newExpiry, err := uc.Extend(ctx, ctapp.ExtendEwayBillCommand{
		EWBID:    ewbID,
		TenantID: tenantID,
		ActorID:  contextUserID(r),
		Hours:    body.ValidUptoHours,
	})
	if errors.Is(err, ctapp.ErrConsoleNotFound) {
		httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"new_expiry": newExpiry.UTC().Format(time.RFC3339),
	})
}

// writeAuditLog records one audit trail row for a console action; failures
// are logged but never fail the action itself.
func writeAuditLog(r *http.Request, db *sql.DB, action, table, recordID string, newValues any) {
	_ = ctapp.WriteAuditLog(r.Context(), db, contextUserID(r), string(shared.TenantOrPlatform(r.Context())), action, table, recordID, newValues)
}

// WriteAuditLog exposes writeAuditLog for vertical-slice presentation packages.
func WriteAuditLog(r *http.Request, db *sql.DB, action, table, recordID string, newValues any) {
	writeAuditLog(r, db, action, table, recordID, newValues)
}

// ---------------------------------------------------------------------------
// Merged from control_tower.go
// ---------------------------------------------------------------------------

// ControlTowerHandlers serves web cockpit UI for the Dispatcher Control Tower.
type ControlTowerHandlers struct {
	*App
	service *ctapp.Service
}

// NewControlTowerHandlers constructs a new ControlTowerHandlers.
func NewControlTowerHandlers(app *App, ctSvc *ctapp.Service) *ControlTowerHandlers {
	return &ControlTowerHandlers{
		App:     app,
		service: ctSvc,
	}
}

// Routes registers web UI routes for the control tower under /control-tower.
func (h *ControlTowerHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "trips", "read")).Get("/", h.WebControlTowerPage)
}

// WebControlTowerPage renders the operations cockpit / control tower dashboard.
func (h *ControlTowerHandlers) WebControlTowerPage(w http.ResponseWriter, r *http.Request) {
	tenantID := shared.TenantIDFromContext(r.Context())
	user, _ := h.getUserFromContext(r)
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	var trips []domain.ControlTowerTrip
	if h.service != nil {
		res, err := h.service.GetTrips(r.Context(), tenantID, statusFilter)
		if err != nil {
			h.renderError(w, http.StatusInternalServerError, "Failed to load control tower data", err.Error(), user)
			return
		}
		trips = res
	}

	var activeCount, delayedCount, sosCount, staleCount int
	for _, t := range trips {
		st := strings.ToUpper(t.Status)
		if st == "STARTED" || st == "IN_TRANSIT" || st == "ASSIGNED" || st == "SCHEDULED" {
			activeCount++
		}
		if t.Safety.HasActiveSOS {
			sosCount++
		}
		if t.SyncState.IsStale {
			staleCount++
		}
		if t.Safety.IsDeviated || t.Safety.ActiveAlertsCount > 0 {
			delayedCount++
		}
	}

	data := PageData{
		Title: "Control Tower",
		User:  user,
		Extra: map[string]interface{}{
			"Trips":        trips,
			"ActiveCount":  activeCount,
			"DelayedCount": delayedCount,
			"SOSCount":     sosCount,
			"StaleCount":   staleCount,
			"StatusFilter": statusFilter,
		},
	}
	h.renderPage(w, r, "control_tower.html", data)
}
