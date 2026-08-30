package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
)

// FuelAuditHandlers serves the fuel claim audit UI (Spec 03 §6.1).
type FuelAuditHandlers struct {
	*App
}

// Routes mounts the /fuel routes. Static paths (queue, run, reports) registered
// before the {id} parameter route so chi matches them first.
func (h *FuelAuditHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "read")).Get("/audit", h.Dashboard)
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "read")).Get("/audit/queue", h.Queue)
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "update")).Post("/audit/run", h.RunAudit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "read")).Get("/audit/{id}", h.Detail)
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "update")).Post("/audit/{id}/review", h.Review)
	r.With(middleware.ResourcePermission(h.AuthSrv, "fuel", "read")).Get("/reports/kmpl", h.KmplReport)
}

// GET /fuel/audit — audit queue dashboard (page).
func (h *FuelAuditHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	ctx := r.Context()

	claims, _ := h.Services.FuelAudit.ListAuditClaims(ctx)
	stats, _ := h.Services.FuelAudit.GetAuditStats(ctx)

	h.renderPage(w, r, "fuel_audit_dashboard.html", PageData{
		Title: "Fuel Audit",
		User:  session,
		Extra: map[string]interface{}{
			"AuditClaims": claims,
			"AuditStats":  stats,
		},
	})
}

// GET /fuel/audit/queue — HTMX partial: live queue refresh every 30s.
func (h *FuelAuditHandlers) Queue(w http.ResponseWriter, r *http.Request) {
	claims, _ := h.Services.FuelAudit.ListAuditClaims(r.Context())
	h.renderFragment(w, "fuel_audit_queue.html", map[string]interface{}{
		"AuditClaims": claims,
	})
}

// GET /fuel/audit/{id} — claim detail with checks A/B/C breakdown.
func (h *FuelAuditHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	expenseID := chi.URLParam(r, "id")

	claim, err := h.Services.FuelAudit.GetAuditDetail(r.Context(), expenseID)
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Claim Not Found", err.Error(), session)
		return
	}

	h.renderPage(w, r, "fuel_audit_detail.html", PageData{
		Title: "Fuel Claim Audit",
		User:  session,
		Extra: map[string]interface{}{
			"Claim": claim,
		},
	})
}

// POST /fuel/audit/{id}/review — admin verdict (passed|failed) + note.
func (h *FuelAuditHandlers) Review(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	verdict := r.FormValue("verdict")
	note := r.FormValue("note")

	if err := h.Services.FuelAudit.ReviewClaim(ctx, expenseID, verdict, note, session.UserID); err != nil {
		h.renderError(w, http.StatusBadRequest, "Review Failed", err.Error(), session)
		return
	}

	http.Redirect(w, r, "/fuel/audit/"+expenseID, http.StatusSeeOther)
}

// POST /fuel/audit/run — manual audit backfill pass.
func (h *FuelAuditHandlers) RunAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, _ := h.getUserFromContext(r)

	audited, err := h.Services.FuelAudit.RunAudit(ctx)
	if err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, `<div class="px-4 py-2 text-xs font-semibold text-status-alert bg-status-alert/10 rounded">Audit failed: %s</div>`, template.HTMLEscapeString(err.Error()))
			return
		}
		h.renderError(w, http.StatusInternalServerError, "Audit Run Failed", err.Error(), session)
		return
	}

	if isDatastarRequest(r) {
		stats, _ := h.Services.FuelAudit.GetAuditStats(ctx)
		w.Header().Set("HX-Trigger", `{"showToast":{"tone":"success","msg":"Audit pass complete"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<div class="px-4 py-2 text-xs font-semibold text-status-success bg-status-success/10 rounded">Audit pass complete — %d claim(s) evaluated.</div>`, audited)
		// htmx 4 partials: KPIs morph in same response (queue refreshes via every 30s poll)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#fuel-kpi-pending" hx-swap="innerMorph">%d</template>`, stats.PendingCount)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#fuel-kpi-needs-review" hx-swap="innerMorph">%d</template>`, stats.NeedsReviewCount)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#fuel-kpi-passed" hx-swap="innerMorph">%d</template>`, stats.PassedCount)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#fuel-kpi-failed" hx-swap="innerMorph">%d</template>`, stats.FailedCount)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#fuel-kpi-variance" hx-swap="innerMorph">%.1f%%</template>`, stats.AvgVariancePct)
		return
	}
	http.Redirect(w, r, "/fuel/audit", http.StatusSeeOther)
}

// auditResultBadge returns badge classes for the audit result pill.
func auditResultBadge(result string) template.CSS {
	switch result {
	case "passed":
		return "bg-status-success/10 text-status-success"
	case "failed":
		return "bg-status-alert/10 text-status-alert"
	case "needs_review":
		return "bg-status-warning/10 text-status-warning"
	default:
		return "bg-surface-container-low text-secondary"
	}
}

// GET /fuel/reports/kmpl — per-vehicle efficiency report (Spec 03 §6.1).
func (h *FuelAuditHandlers) KmplReport(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	ctx := r.Context()

	// Default window: last 30 days.
	to := time.Now()
	from := to.AddDate(0, 0, -30)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			to = t.Add(24*time.Hour - time.Second)
		}
	}

	rows, _ := h.Services.FuelAudit.KmplReport(ctx, from, to)
	h.renderPage(w, r, "fuel_kmpl_report.html", PageData{
		Title: "Fuel Efficiency (Kmpl) Report",
		User:  session,
		Extra: map[string]interface{}{
			"KmplRows": rows,
			"From":     from.Format("2006-01-02"),
			"To":       to.Format("2006-01-02"),
		},
	})
}
