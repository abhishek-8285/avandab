package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// PNLHandlers serves the daily P&L snapshot API.
// Spec 16 §2 (PNL Engine), §3.1 (API routes).
type PNLHandlers struct {
	App     *App
	PNLSvc  *service.PNLService
	authSrv auth.AuthorizationService
}

// NewPNLHandlers constructs the handler group.
func NewPNLHandlers(app *App, svc *service.PNLService, authSrv auth.AuthorizationService) *PNLHandlers {
	return &PNLHandlers{App: app, PNLSvc: svc, authSrv: authSrv}
}

// RegisterRoutes mounts PNL routes under the provided router.
// All routes require RequireAPIAuth (applied by the outer group in main.go)
// plus a pnl:read or pnl:write permission check.
func (h *PNLHandlers) RegisterRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.authSrv, "pnl", "read")).
		Get("/api/v1/pnl", h.GetPNLRange)

	r.With(middleware.RequirePermission(h.authSrv, "pnl", "write")).
		Post("/api/v1/pnl/generate", h.GenerateSnapshot)
}

// GetPNLRange returns daily snapshots for a ?from=&to= date range.
// Both params are required in YYYY-MM-DD format.
// GET /api/v1/pnl?from=2025-01-01&to=2025-01-31
func (h *PNLHandlers) GetPNLRange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := string(shared.MustTenantID(ctx))

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, `{"error":"from and to query params are required (YYYY-MM-DD)"}`, http.StatusBadRequest)
		return
	}

	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		http.Error(w, `{"error":"invalid from date format, use YYYY-MM-DD"}`, http.StatusBadRequest)
		return
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		http.Error(w, `{"error":"invalid to date format, use YYYY-MM-DD"}`, http.StatusBadRequest)
		return
	}
	if to.Before(from) {
		http.Error(w, `{"error":"to must be >= from"}`, http.StatusBadRequest)
		return
	}

	snapshots, err := h.PNLSvc.GetPNLRange(ctx, tenantID, from, to)
	if err != nil {
		http.Error(w, `{"error":"failed to retrieve P&L data"}`, http.StatusInternalServerError)
		return
	}
	if snapshots == nil {
		snapshots = []service.PNLSnapshot{} // return empty array, not null
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"from":      fromStr,
		"to":        toStr,
		"tenant_id": tenantID,
		"snapshots": snapshots,
	})
}

// GenerateSnapshot manually triggers P&L snapshot generation for a given date.
// POST /api/v1/pnl/generate
// Body: {"date": "2025-01-15"}   (optional — defaults to yesterday)
func (h *PNLHandlers) GenerateSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := string(shared.MustTenantID(ctx))

	var req struct {
		Date string `json:"date"`
	}
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	var targetDate time.Time
	if req.Date == "" {
		targetDate = time.Now().AddDate(0, 0, -1) // default: yesterday
	} else {
		var err error
		targetDate, err = time.Parse("2006-01-02", req.Date)
		if err != nil {
			http.Error(w, `{"error":"invalid date format, use YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
	}

	snap, err := h.PNLSvc.GenerateDailySnapshot(ctx, tenantID, targetDate)
	if err != nil {
		http.Error(w, `{"error":"P&L snapshot generation failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(snap)
}
