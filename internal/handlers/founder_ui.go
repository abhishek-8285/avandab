package handlers

import (
	"net/http"
	"time"

	"transport-app/internal/service"
)

// DashboardPage renders the founder dashboard web page (Spec 16 §8).
func (h *FounderHandlers) DashboardPage(w http.ResponseWriter, r *http.Request) {
	tenantID := h.tenantID(r)
	session, _ := h.getUserFromContext(r)

	unackCount, _ := h.App.Services.FounderSignals.CountUnacknowledged(r.Context(), tenantID)
	recentSignals, _, _ := h.App.Services.FounderSignals.ListSignals(r.Context(), tenantID, service.SignalFilters{
		UnacknowledgedOnly: true,
		Limit:              5,
	})
	activeExperiments, _ := h.App.Services.Experiments.CountByStatus(r.Context(), tenantID, service.ExperimentStatusRunning)
	openAlerts, _ := h.App.Services.OpsAlerts.CountByStatus(r.Context(), tenantID, service.OpsAlertStatusOpen)
	latestPNL, _ := h.App.Services.PNL.GetLatest(r.Context(), tenantID)

	h.renderPage(w, r, "founder_dashboard.html", PageData{
		Title: "Founder Dashboard",
		User:  session,
		Extra: map[string]interface{}{
			"UnacknowledgedSignals": unackCount,
			"RecentSignals":         recentSignals,
			"ActiveExperiments":     activeExperiments,
			"OpenOpsAlerts":         openAlerts,
			"LatestPNL":             latestPNL,
		},
	})
}

// OpsAlertsPage renders the ops alerts list web page (Spec 16 §4).
func (h *FounderHandlers) OpsAlertsPage(w http.ResponseWriter, r *http.Request) {
	tenantID := h.tenantID(r)
	session, _ := h.getUserFromContext(r)

	// ?status= drives the filter_bar status chips ("" = all).
	status := r.URL.Query().Get("status")
	alerts, _, _ := h.App.Services.OpsAlerts.ListAlerts(r.Context(), tenantID, service.OpsAlertFilters{Status: status, Limit: 50})

	h.renderPage(w, r, "ops_alerts_list.html", PageData{
		Title: "Ops Alerts",
		User:  session,
		Extra: map[string]interface{}{
			"Alerts":       alerts,
			"StatusFilter": status,
		},
	})
}

// PNLDashboardPage renders the PNL dashboard web page (Spec 16 §2).
func (h *FounderHandlers) PNLDashboardPage(w http.ResponseWriter, r *http.Request) {
	tenantID := h.tenantID(r)
	session, _ := h.getUserFromContext(r)

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	}
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}

	fromTime, _ := time.Parse("2006-01-02", from)
	toTime, _ := time.Parse("2006-01-02", to)
	snapshots, _ := h.App.Services.PNL.GetPNLRange(r.Context(), tenantID, fromTime, toTime)

	h.renderPage(w, r, "pnl_dashboard.html", PageData{
		Title: "Profit & Loss",
		User:  session,
		Extra: map[string]interface{}{
			"Snapshots": snapshots,
			"From":      from,
			"To":        to,
		},
	})
}

// ExperimentsPage renders the experiments list web page (Spec 16 §5).
func (h *FounderHandlers) ExperimentsPage(w http.ResponseWriter, r *http.Request) {
	tenantID := h.tenantID(r)
	session, _ := h.getUserFromContext(r)

	// ?status= drives the filter_bar status chips ("" = all).
	status := r.URL.Query().Get("status")
	experiments, _ := h.App.Services.Experiments.ListExperiments(r.Context(), tenantID, status)

	h.renderPage(w, r, "experiments_list.html", PageData{
		Title: "A/B Experiments",
		User:  session,
		Extra: map[string]interface{}{
			"Experiments":  experiments,
			"StatusFilter": status,
		},
	})
}
