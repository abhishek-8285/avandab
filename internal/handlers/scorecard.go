package handlers

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// ScorecardHandlers serves the driver scorecard UI (Spec 03 §6.1).
type ScorecardHandlers struct {
	*App
}

// Routes mounts the /scorecard routes. Static paths (table) are registered
// before the {id} parameter route so chi matches them first.
func (h *ScorecardHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "scorecard", "read")).Get("/", h.Leaderboard)
	r.With(middleware.ResourcePermission(h.AuthSrv, "scorecard", "read")).Get("/table", h.Table)
	r.With(middleware.ResourcePermission(h.AuthSrv, "scorecard", "read")).Get("/drivers/{id}", h.DriverDetail)
	r.With(middleware.ResourcePermission(h.AuthSrv, "scorecard", "update")).Post("/drivers/{id}/resolve", h.Resolve)
}

// tenantID resolves the acting org. Fail closed: scorecard routes sit behind
// auth middleware which always sets tenant (panics surface as 500 via Recoverer).
func (h *ScorecardHandlers) tenantID(r *http.Request) string {
	return string(shared.MustTenantID(r.Context()))
}

// GET /scorecard — leaderboard page (Spec 03 §6.3).
func (h *ScorecardHandlers) Leaderboard(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	ctx := r.Context()

	rows, stats, err := h.Services.Scorecard.Leaderboard(ctx, h.tenantID(r), 100)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Leaderboard Error", err.Error(), session)
		return
	}

	h.renderPage(w, r, "scorecard_leaderboard.html", PageData{
		Title: "Driver Scorecard",
		User:  session,
		Extra: map[string]interface{}{
			"Leaderboard": rows,
			"Stats":       stats,
		},
	})
}

// GET /scorecard/table — HTMX partial: ranked rows for the 60s auto-refresh.
func (h *ScorecardHandlers) Table(w http.ResponseWriter, r *http.Request) {
	rows, stats, err := h.Services.Scorecard.Leaderboard(r.Context(), h.tenantID(r), 100)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderFragment(w, r, "scorecard_table.html", map[string]interface{}{
		"Leaderboard": rows,
		"Stats":       stats,
	})
}

// GET /scorecard/drivers/{id} — driver detail with score history + event
// breakdown + fraud-cap resolve actions (Spec 03 §6.3).
func (h *ScorecardHandlers) DriverDetail(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	driverID := chi.URLParam(r, "id")

	detail, err := h.Services.Scorecard.DriverDetail(r.Context(), driverID)
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Driver Not Found", err.Error(), session)
		return
	}

	h.renderPage(w, r, "scorecard_driver.html", PageData{
		Title: "Driver Scorecard",
		User:  session,
		Extra: map[string]interface{}{
			"Detail": detail,
		},
	})
}

// POST /scorecard/drivers/{id}/resolve — resolve a fraud-cap event (admin,
// scorecard:update). The {id} is the driver; the event id comes from the form.
func (h *ScorecardHandlers) Resolve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	driverID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	eventID := r.FormValue("event_id")
	if eventID == "" {
		http.Error(w, "event_id is required", http.StatusBadRequest)
		return
	}

	if err := h.Services.Scorecard.ResolveFraudEvent(ctx, eventID, session.UserID); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}

	http.Redirect(w, r, "/scorecard/drivers/"+driverID, http.StatusSeeOther)
}

// tierBadgeClass returns the badge classes for a driver tier (A emerald,
// B blue, C rose — Spec 03 §6.3).
//
// These are the same semantic `badge-*` utilities statusBadgeClass emits, so a
// tier chip and a status chip are styled by one mechanism and both follow dark
// mode. Being Go-emitted, they are safelisted via `@source inline` in
// src/input.css — without that the classes would be absent from the bundle.
//
// Note the text step moves from -700 to the container's -800 on-token. That is
// a deliberate normalisation to the shared badge tokens, not an accident.
func tierBadgeClass(tier string) template.CSS {
	switch tier {
	case "A":
		return "badge-success"
	case "B":
		return "badge-info"
	default:
		return "badge-alert"
	}
}
