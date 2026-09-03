package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"transport-app/internal/experiments"
	"transport-app/internal/shared"
)

// DashboardHandlers handles dashboard-related requests.
type DashboardHandlers struct {
	*App
}

// Index renders the dashboard page.
func (h *DashboardHandlers) Index(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)

	company, _ := h.Services.Settings.GetSettings(r.Context())
	if session != nil && (session.Role == "admin" || session.Role == "org_admin") && isCompanyIncomplete(company) {
		http.SetCookie(w, flashCookie("flash_error", "Please complete mandatory company compliance details to unlock fleet operations."))
		http.Redirect(w, r, "/company/onboard", http.StatusSeeOther)
		return
	}

	userID := ""
	if session != nil {
		userID = session.UserID
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	variant := experiments.Assign(h.Config.Experiment.Rollout, h.Config.Experiment.ForceVariant, tenantID, userID)
	if qv := strings.ToUpper(r.URL.Query().Get("variant")); qv == experiments.VariantA || qv == experiments.VariantB {
		variant = qv
	}

	data, err := h.Services.Dashboard.GetDashboardData(r.Context())
	if err != nil {
		http.Error(w, "Failed to load dashboard: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.Experiments.RecordAsync(r.Context(), tenantID, userID, experiments.DashboardExperiment, variant, "dashboard_view", map[string]any{
		"today_trips": data.TodaysTripsCount,
	})

	h.renderPage(w, r, "dashboard.html", PageData{
		Title:      "Dashboard",
		User:       session,
		Settings:   company,
		FlashError: "",
		Extra: map[string]interface{}{
			"Stats":            data,
			"DashboardVariant": variant,
			"ChartData": map[string]interface{}{
				"variant":       variant,
				"statusCounts":  data.StatusCounts,
				"revenueByDay":  data.RevenueByDay,
				"bookingsByDay": data.BookingsByDay,
			},
		},
	})
}

// Event records an experiment event (e.g. dashboard_click) from the browser.
func (h *DashboardHandlers) Event(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	session, _ := h.getUserFromContext(r)
	userID := ""
	if session != nil {
		userID = session.UserID
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var payload struct {
		Experiment string         `json:"experiment"`
		Variant    string         `json:"variant"`
		Event      string         `json:"event"`
		Meta       map[string]any `json:"meta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if payload.Experiment == "" || payload.Event == "" {
		http.Error(w, "experiment and event are required", http.StatusBadRequest)
		return
	}
	if payload.Variant != experiments.VariantA && payload.Variant != experiments.VariantB {
		http.Error(w, "invalid variant", http.StatusBadRequest)
		return
	}
	h.Experiments.RecordAsync(r.Context(), tenantID, userID, payload.Experiment, payload.Variant, payload.Event, payload.Meta)
	w.WriteHeader(http.StatusNoContent)
}

// Stream emits real-time SSE updates for the dashboard (Spec 12 §2.1).
func (h *DashboardHandlers) Stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if !h.Config.DashboardSSEEnabled {
		// Graceful no-op: emit one snapshot then close
		h.writeDashFrame(w, flusher, r.Context())
		return
	}

	interval := h.Config.DashboardSSEInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Prime immediately
	h.writeDashFrame(w, flusher, r.Context())

	for {
		select {
		case <-r.Context().Done():
			return // Client disconnected
		case <-ticker.C:
			h.writeDashFrame(w, flusher, r.Context())
		}
	}
}

func (h *DashboardHandlers) writeDashFrame(w http.ResponseWriter, f http.Flusher, ctx context.Context) {
	data, err := h.Services.Dashboard.GetDashboardData(ctx)
	if err != nil {
		return // Silently skip failed ticks
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return
	}

	// Datastar v1.0.2 SSE integration format
	fmt.Fprintf(w, "event: datastar-merge-signals\ndata: {\"dashboard\":%s}\n\n", jsonBytes)
	f.Flush()
}
