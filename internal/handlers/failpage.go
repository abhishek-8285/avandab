package handlers

import (
	"log/slog"
	"net/http"

	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"transport-app/internal/apm"
	"transport-app/internal/apperr"
	"transport-app/internal/logging"
)

func (a *App) failPage(w http.ResponseWriter, r *http.Request, err error, fallbackStatus int, title string) {
	status := fallbackStatus
	msg := "Something went wrong. Please try again, or contact support if it keeps happening."
	if ae, ok := apperr.From(err); ok {
		status = ae.HTTPStatus
		msg = ae.UserMsg
	}
	slog.ErrorContext(r.Context(), "handler error",
		slog.String("title", title),
		slog.String("path", r.URL.Path),
		slog.String("error", logging.Redact(err.Error())),
	)
	session, _ := a.getUserFromContext(r)
	a.renderError(w, status, title, msg, session)
}

// ---------------------------------------------------------------------------
// Merged from status.go
// ---------------------------------------------------------------------------

// SubsystemStatus describes the real-time operational state of a single subsystem.
type SubsystemStatus struct {
	Name         string `json:"name"`
	Category     string `json:"category"`
	Status       string `json:"status"` // "operational", "degraded", "outage"
	Latency      string `json:"latency,omitempty"`
	Availability string `json:"availability,omitempty"`
	Description  string `json:"description"`
}

// StatusIncident represents a public operational notice.
type StatusIncident struct {
	Date        string `json:"date"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

// StatusPageData is the view model for the public /status page and JSON status endpoint.
type StatusPageData struct {
	Version           string `json:"version"`
	Title             string `json:"title"`
	SEODescription    string `json:"seo_description"`
	CanonicalPath     string `json:"canonical_path"`
	NoIndex           bool   `json:"no_index"`
	OGType            string `json:"og_type"`
	OGImage           string `json:"og_image"`
	SEOJSONLD         string `json:"seo_jsonld,omitempty"`
	PWAEnabled        bool   `json:"pwa_enabled"`
	Lang              string `json:"lang"`
	OverallStatus     string `json:"overall_status"`
	OverallBadgeClass string `json:"overall_badge_class"`
	// UptimePercentage is only meaningful when UptimeMeasured is true. No
	// continuous availability measurement exists yet, so it ships as 0 and
	// the UI shows "not measured" instead of an invented 99.98% (P0 stop-fake-data).
	UptimePercentage float64           `json:"uptime_percentage"`
	UptimeMeasured   bool              `json:"uptime_measured"`
	SLATarget        string            `json:"sla_target"`
	RPO              string            `json:"rpo"`
	RTO              string            `json:"rto"`
	DBLatency        string            `json:"db_latency"`
	P95LatencyMs     int64             `json:"p95_latency_ms"`
	OperationalCount int               `json:"operational_subsystems"`
	SubsystemCount   int               `json:"subsystem_count"`
	LastUpdated      string            `json:"last_updated"`
	Subsystems       []SubsystemStatus `json:"subsystems"`
	// HistoricalDays is empty until availability history is actually recorded.
	HistoricalDays   []int            `json:"historical_days"`
	HistoryAvailable bool             `json:"history_available"`
	Incidents        []StatusIncident `json:"incidents"`
}

// buildStatusData aggregates live system diagnostics and APM percentiles.
func (a *App) buildStatusData(ctx context.Context, lang string) StatusPageData {
	overallStatus := "Operational"
	overallBadge := "bg-emerald-500"

	// 1. Probe database health and latency
	var dbLatencyStr string
	dbStatus := "operational"
	if a.DB != nil {
		pingStart := time.Now()
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := a.DB.PingContext(pingCtx); err != nil {
			dbStatus = "degraded"
			overallStatus = "Degraded"
			overallBadge = "bg-amber-500"
			dbLatencyStr = "timeout / error"
		} else {
			dbLatencyStr = fmt.Sprintf("%d ms", time.Since(pingStart).Milliseconds())
		}
	} else {
		dbStatus = "degraded"
		overallStatus = "Degraded"
		overallBadge = "bg-amber-500"
		dbLatencyStr = "uninitialized"
	}

	// 2. Query APM telemetry and HTTP percentiles (measured, not synthesized).
	httpStats := apm.Default.GetStats("http.request")
	ingestStats := apm.Default.GetStats("telemetry.batch_ingest")

	var p95Ms int64
	if httpStats.P95 > 0 {
		p95Ms = httpStats.P95.Milliseconds()
	}

	ingestLatencyStr := "not measured"
	ingestStatus := "not_probed"
	if ingestStats.P95 > 0 {
		ingestLatencyStr = fmt.Sprintf("%d ms", ingestStats.P95.Milliseconds())
		ingestStatus = "operational"
	}

	// 3. Subsystem checks. Only states this handler can actually probe are
	// reported. Availability percentages, RPO/RTO, synthetic checks and a
	// 30-day history do not exist yet and are not invented here (P0
	// "stop fake data"; AGENTS.md prime directive).
	subsystems := []SubsystemStatus{
		{
			Name:        "API & Web Gateway",
			Category:    "Routing & Edge",
			Status:      "operational",
			Latency:     fmt.Sprintf("%d ms", p95Ms),
			Description: "Public and authenticated REST endpoints and request router.",
		},
		{
			Name:        "Database Persistence Engine",
			Category:    "Storage & Cache",
			Status:      dbStatus,
			Latency:     dbLatencyStr,
			Description: "Relational ledger and ACID write transactions.",
		},
		{
			Name:        "Telemetry & GPS Ingestion",
			Category:    "IoT Pipeline",
			Status:      ingestStatus,
			Latency:     ingestLatencyStr,
			Description: "Batch position ingest; latency appears once spans are recorded.",
		},
		{
			Name:        "Async Outbox Dispatcher",
			Category:    "Event Bus",
			Status:      "not_probed",
			Description: "At-least-once outbox dispatcher; no public health probe yet.",
		},
		{
			Name:        "Compliance & E-Way Bill Rails",
			Category:    "Regulatory Integrations",
			Status:      "not_configured",
			Description: "Provider adapters are enabled per contract; no live probe on this page.",
		},
	}

	// SMS status is derived from configuration, never assumed.
	smsStatus := "not_configured"
	smsDesc := "No SMS provider configured; OTP and driver notifications fail loudly."
	if a.Config != nil && a.Config.Notify.SMSProvider != "" &&
		!strings.EqualFold(a.Config.Notify.SMSProvider, "mock") {
		smsStatus = "configured"
		smsDesc = "SMS provider configured: " + a.Config.Notify.SMSProvider + "."
	}
	subsystems = append(subsystems, SubsystemStatus{
		Name:        "SMS & Communication Gateway",
		Category:    "Alerts & Carrier",
		Status:      smsStatus,
		Description: smsDesc,
	})

	operational := 0
	for _, s := range subsystems {
		if s.Status == "operational" || s.Status == "configured" {
			operational++
		}
	}

	return StatusPageData{
		Version:           AppVersion,
		Title:             "System Status",
		SEODescription:    "Avandab system status: live subsystem checks and measured API latency.",
		CanonicalPath:     "/status",
		NoIndex:           false,
		OGType:            "website",
		OGImage:           "https://avandab.com/static/icons/icon-512.png",
		PWAEnabled:        a.Config != nil && a.Config.PWAEnabled,
		Lang:              lang,
		OverallStatus:     overallStatus,
		OverallBadgeClass: overallBadge,
		// No continuous availability measurement exists yet: report 0 + flag
		// so consumers never mistake an invented number for telemetry.
		UptimePercentage: 0,
		UptimeMeasured:   false,
		SLATarget:        "99.9%",
		RPO:              "not measured",
		RTO:              "not measured",
		DBLatency:        dbLatencyStr,
		P95LatencyMs:     p95Ms,
		OperationalCount: operational,
		SubsystemCount:   len(subsystems),
		LastUpdated:      time.Now().UTC().Format("15:04:05 UTC"),
		Subsystems:       subsystems,
		HistoricalDays:   nil,
		HistoryAvailable: false,
		Incidents:        nil,
	}
}

// Status serves the public HTML status dashboard at GET /status.
func (a *App) Status(w http.ResponseWriter, r *http.Request) {
	// If caller explicitly requested JSON (synthetic monitor or API client)
	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("format") == "json" {
		a.StatusJSON(w, r)
		return
	}

	data := a.buildStatusData(r.Context(), langOf(r))
	tmpl := a.templatesFor(r).Lookup("status.html")
	if tmpl == nil {
		http.Error(w, "status.html template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=15, must-revalidate")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

// StatusJSON serves machine-readable status at GET /api/v1/health/status and GET /api/v1/status.
func (a *App) StatusJSON(w http.ResponseWriter, r *http.Request) {
	data := a.buildStatusData(r.Context(), langOf(r))

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	statusCode := http.StatusOK
	if data.OverallStatus == "Degraded" {
		statusCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}
