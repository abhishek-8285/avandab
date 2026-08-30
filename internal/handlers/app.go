package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"transport-app/internal/alerts/repository"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/auth"
	"transport-app/internal/cache"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/experiments"
	"transport-app/internal/features"
	"transport-app/internal/i18n"
	"transport-app/internal/operations/notifications"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/telemetry"

	"github.com/go-chi/chi/v5"
)

const datastarRequestHeader = "Datastar-Request"

// App holds shared handler dependencies.
type App struct {
	Services    *service.Services
	Config      *config.Config
	AuthStore   *auth.SessionStore
	DB          *sql.DB
	Templates   *template.Template
	TemplatesHI *template.Template // Hindi set; nil falls back to Templates
	// Features is the per-org feature-flag registry; nil disables gating.
	Features *features.Registry
	AuthSrv  auth.AuthorizationService

	// ResetTokens issues and verifies single-use password-reset tokens.
	ResetTokens *auth.ResetTokenStore

	// Notify delivers outbound email/SMS (nil-safe: handlers check
	// EmailConfigured/SMSConfigured before offering delivery UX).
	Notify *notifications.Service

	// Cache is the config-selected cache backend (none | memory | redis).
	// Hot reads should go through it instead of hitting the DB directly.
	Cache cache.Cache

	// Experiments records A/B experiment events (best-effort).
	Experiments *experiments.Recorder

	// Handler groups
	Auth       *AuthHandlers
	Dashboard  *DashboardHandlers
	Users      *UserHandlers
	Drivers    *DriverHandlers
	Vehicles   *VehicleHandlers
	Customers  *CustomerHandlers
	Routes     *RouteHandlers
	Bookings   *BookingHandlers
	Trips      *TripHandlers
	Invoices   *InvoiceHandlers
	Payments   *PaymentHandlers
	Reports    *ReportHandlers
	SettingsH  *SettingsHandlers
	AuditLogs  *AuditLogHandlers
	Contact    *ContactHandlers
	Kharcha    *KharchaHandlers
	Assistant  *AssistantHandlers
	AgentAdmin *AgentAdminHandlers
	// OpsErrors powers the /ops/errors triage page + /api/v1/errors API (Spec 16 §4, §5.5).
	OpsErrors *OpsErrorsHandler
	// TelemetryDevices powers the device registry / provisioning / quarantine UI.
	TelemetryDevices *TelemetryDeviceHandlers
	// Geofences powers the geofence CRUD + drawing UI (Spec 02 §8).
	Geofences *GeofenceHandlers
	// FuelAudit powers the fuel claim audit queue + review (Spec 03 §6.1).
	FuelAudit *FuelAuditHandlers
	// Scorecard powers the driver scorecard leaderboard + settlement bonus
	// (Spec 03 §6.1, §7).
	Scorecard *ScorecardHandlers
	// Tracking powers the live fleet map page (Spec 04 §1.3).
	Tracking *TrackingHandlers
	// Map powers the live fleet map page and SSE stream (Spec 12 §2.2, §4.3).
	Map *MapHandlers
	// Share powers trip share link generation, public viewing & admin management (Spec 04 §4).
	Share *ShareHandlers
	// Maintenance powers preventive maintenance schedules, DTCs, and records (Spec 04 §6).
	Maintenance *MaintenanceHandlers
	// Alerts repository and operational alerts handler (Spec 05 §3).
	AlertsRepo repository.AlertRepository
	Alerts     *AlertHandlers
	// PNLService backs the console money strip (Spec 22 §2.2); nil when the
	// service layer is unavailable.
	PNLService *service.PNLService
	// Compliance handler (Spec 05 §5).
	Compliance *ComplianceHandlers
	// E-Way Bill handler (Spec 07 §2).
	EWayBill *EWayBillHandlers
	// FASTag handler (Spec 07 §5).
	FASTag *FASTagHandlers
	// Accounting sync & reconcile handler (Spec 08 §2.2).
	Accounting *AccountingHandlers
	// Settlements handler (Spec 08 §2.1).
	Settlements *SettlementHandlers
	// Document vault handler (Spec 08 §2.3).
	Documents *DocumentHandlers
	// FilesAPI powers the Fleetbase-style generic file/image upload API.
	FilesAPI *FilesAPIHandlers
	// PNL daily snapshot handler (Spec 16 §2).
	PNL *PNLHandlers
	// Operational alerts handler (Spec 16 §4).
	OpsAlerts *OpsAlertHandlers
	// ABExperiments powers the A/B experiment framework + feature flag API (Spec 16 §5).
	ABExperiments *ExperimentHandlers
	// Founder powers the founder signals + audit visibility layer (Spec 16 §6, §7).
	Founder *FounderHandlers
	// SOS powers the driver panic/emergency SOS endpoint (Phase 8 §P3A).
	SOS *SOSHandlers
}

// NewApp creates a new handler app with all handler groups initialized.
func NewApp(svc *service.Services, cfg *config.Config, authStore *auth.SessionStore, db *sql.DB, authSrv auth.AuthorizationService, resetTokens *auth.ResetTokenStore) *App {
	templates, err := parseTemplates(authSrv)
	if err != nil {
		slog.Error("failed to parse templates; serving with minimal template set", "error", err)
		templates = template.New("")
	}
	templatesHI, errHI := parseTemplatesLang(authSrv, "hi")
	if errHI != nil {
		slog.Warn("Hindi template set unavailable; web UI stays English", "error", errHI)
		templatesHI = nil
	}

	app := &App{
		Services:    svc,
		Config:      cfg,
		AuthStore:   authStore,
		DB:          db,
		Templates:   templates,
		TemplatesHI: templatesHI,
		AuthSrv:     authSrv,
		ResetTokens: resetTokens,
		Features:    features.NewRegistry(db, nil),
	}

	app.Experiments = experiments.NewRecorder(db)

	app.Auth = &AuthHandlers{App: app}
	app.Dashboard = &DashboardHandlers{App: app}
	app.Users = &UserHandlers{App: app}
	app.Drivers = &DriverHandlers{App: app}
	app.Vehicles = &VehicleHandlers{App: app}
	app.Customers = &CustomerHandlers{App: app}
	app.Routes = &RouteHandlers{App: app}
	app.Bookings = &BookingHandlers{App: app}
	app.Trips = &TripHandlers{App: app}
	app.Invoices = &InvoiceHandlers{App: app}
	app.Payments = &PaymentHandlers{App: app}
	app.Reports = &ReportHandlers{App: app}
	app.SettingsH = &SettingsHandlers{App: app}
	app.AuditLogs = &AuditLogHandlers{App: app}
	app.Contact = &ContactHandlers{App: app}
	app.Kharcha = &KharchaHandlers{App: app}
	app.Assistant = &AssistantHandlers{App: app}
	// Device registry / provisioning / quarantine admin UI.
	app.TelemetryDevices = NewTelemetryDeviceHandlers(app, db, cfg.Telemetry.DeviceSecretPepper)
	// Geofence CRUD + drawing UI.
	app.Geofences = NewGeofenceHandlers(app, db)
	// Fuel claim audit queue + review (Spec 03 §6.1).
	app.FuelAudit = &FuelAuditHandlers{App: app}
	// Driver scorecard leaderboard + fraud resolve (Spec 03 §6.1).
	app.Scorecard = &ScorecardHandlers{App: app}
	// Live fleet tracking map (Spec 04 §1.3).
	app.Tracking = &TrackingHandlers{App: app}
	// Live map & stream handler (Spec 12 §2.2, §4.3).
	app.Map = NewMapHandlers(app, telemetry.NewLiveStore(db, 15*time.Minute))
	// Trip share links (Spec 04 §4).
	app.Share = NewShareHandlers(app, db)
	// Preventive maintenance (Spec 04 §6).
	app.Maintenance = NewMaintenanceHandlers(app, db)
	// Operational alerts (Spec 05 §3).
	app.AlertsRepo = alertsqlite.NewAlertRepository(db)
	app.Alerts = NewAlertHandlers(app, app.AlertsRepo)
	// Compliance (Spec 05 §5).
	if svc != nil {
		app.Compliance = NewComplianceHandlers(app, svc.Compliance)
	}
	// PNL daily snapshot (Spec 16 §2).
	if svc != nil && svc.PNL != nil {
		app.PNL = NewPNLHandlers(app, svc.PNL, authSrv)
		app.PNLService = svc.PNL
	}
	// Operational alerts (Spec 16 §4).
	if svc != nil && svc.OpsAlerts != nil {
		app.OpsAlerts = NewOpsAlertHandlers(app, svc.OpsAlerts, authSrv)
	}
	// A/B experiments (Spec 16 §5).
	if svc != nil && svc.Experiments != nil {
		app.ABExperiments = NewExperimentHandlers(app, svc.Experiments, authSrv)
	}
	// Founder signals + audit (Spec 16 §6, §7).
	if svc != nil && svc.FounderSignals != nil && svc.FounderAudit != nil {
		app.Founder = NewFounderHandlers(app, svc.FounderSignals, svc.FounderAudit, authSrv)
		// Spec 22 §10-S12 — pilot KPI scorecard source.
		app.Founder.KPIs = service.NewKPIService(app.DB)
	}

	app.SOS = NewSOSHandlers(app, db)

	return app
}

// parseTemplates loads and parses all HTML templates with custom functions.
// The English set; parseTemplatesLang builds a set whose `t` func translates
// into the given language (App keeps one set per language, so the shared
// *template.Template instances stay read-only and race-free).
func parseTemplates(authSrv auth.AuthorizationService) (*template.Template, error) {
	return parseTemplatesLang(authSrv, "en")
}

func parseTemplatesLang(authSrv auth.AuthorizationService, lang string) (*template.Template, error) {
	tmpl := template.New("").Funcs(template.FuncMap{
		"t": func(key string) string { return i18n.T(lang, key) },
		"can": func(user interface{}, resource string, action string) bool {
			if user == nil {
				return false
			}
			var uid string
			switch u := user.(type) {
			case *auth.SessionData:
				if u == nil {
					return false
				}
				uid = u.UserID
			case auth.SessionData:
				uid = u.UserID
			case string:
				uid = u
			default:
				return false
			}
			return authSrv.Can(uid, resource, action)
		},
		"formatDateTime": formatDateTime,
		"formatDate":     formatDate,
		"datetime": func(t time.Time) string {
			return t.Format("02-01-2006 15:04")
		},
		"date_only": func(t time.Time) string {
			return t.Format("02-01-2006")
		},
		"lower":         strings.ToLower,
		"upper":         strings.ToUpper,
		"replace":       func(s, old, new string, n int) string { return strings.Replace(s, old, new, n) },
		"join":          strings.Join,
		"safeHTML":      func(s string) template.HTML { return template.HTML(s) },
		"icon":          icon,
		"iconWithClass": iconWithClass,
		"json": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			return template.JS(strings.ReplaceAll(string(b), "</", "<\\/"))
		},
		"abbr": func(s string, max int) string {
			if len(s) <= max {
				return s
			}
			if max <= 3 {
				return s[:max] + "..."
			}
			return s[:max-3] + "..."
		},
		"fileExt":     filepath.Ext,
		"add":         func(a, b int) int { return a + b },
		"sub":         func(a, b int) int { return a - b },
		"mul":         func(a, b int) int { return a * b },
		"div":         func(a, b int) int { return a / b },
		"statusBadge": statusBadgeClass,
		"inDate":      inDate,
		"auditBadge":  auditResultBadge,
		"tierBadge":   tierBadgeClass,
		"priceFormat": func(f float64) string { return fmt.Sprintf("%.2f", f) },
		"yesNo": func(b bool) string {
			if b {
				return "Yes"
			}
			return "No"
		},
		"nullString": func(s *string) string {
			if s == nil {
				return ""
			}
			return *s
		},
		"slice": func(s string, i, j int) string {
			r := []rune(s)
			if i < 0 {
				i = 0
			}
			if j > len(r) {
				j = len(r)
			}
			if i >= j {
				return ""
			}
			return string(r[i:j])
		},
		"derefTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
		"chips": func(pairs ...interface{}) []map[string]interface{} {
			out := make([]map[string]interface{}, 0, len(pairs)/2)
			for i := 0; i+1 < len(pairs); i += 2 {
				out = append(out, map[string]interface{}{"Label": pairs[i], "Value": pairs[i+1]})
			}
			return out
		},
		"default": func(def interface{}, v interface{}) interface{} {
			switch x := v.(type) {
			case nil:
				return def
			case string:
				if x == "" {
					return def
				}
				return x
			default:
				return v
			}
		},
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict needs even args")
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				k, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict key must be string")
				}
				m[k] = values[i+1]
			}
			return m, nil
		},
	})

	// Support TEMPLATES_DIR env var for deployments where CWD != repo root
	templatesDir := os.Getenv("TEMPLATES_DIR")
	if templatesDir == "" {
		templatesDir = "internal/templates"
	}
	partialsDir := filepath.Join(templatesDir, "partials")

	_, err := tmpl.ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates from %q: %w", templatesDir, err)
	}
	// Parse partial templates from subdirectories
	if _, err := tmpl.ParseGlob(filepath.Join(partialsDir, "*.html")); err != nil {
		return nil, fmt.Errorf("failed to parse partial templates from %q: %w", partialsDir, err)
	}
	return tmpl, nil
}

func statusBadgeClass(status interface{}) string {
	s := fmt.Sprintf("%v", status)
	classes := map[string]string{
		"pending":        "bg-yellow-100 text-yellow-800",
		"confirmed":      "bg-blue-100 text-blue-800",
		"completed":      "bg-green-100 text-green-800",
		"cancelled":      "bg-red-100 text-red-800",
		"draft":          "bg-gray-100 text-gray-800",
		"scheduled":      "bg-purple-100 text-purple-800",
		"assigned":       "bg-indigo-100 text-indigo-800",
		"started":        "bg-orange-100 text-orange-800",
		"reached_pickup": "bg-blue-100 text-blue-800",
		"in_transit":     "bg-teal-100 text-teal-800",
		"delivered":      "bg-emerald-100 text-emerald-800",
		"available":      "bg-green-100 text-green-800",
		"on_trip":        "bg-orange-100 text-orange-800",
		"maintenance":    "bg-yellow-100 text-yellow-800",
		"running":        "bg-blue-100 text-blue-800",
		"inactive":       "bg-gray-100 text-gray-800",
		"paid":           "bg-green-100 text-green-800",
		"partially_paid": "bg-yellow-100 text-yellow-800",
	}
	if cls, ok := classes[s]; ok {
		return cls
	}
	return "bg-gray-100 text-gray-800"
}

func (a *App) getUserFromContext(r *http.Request) (*auth.SessionData, bool) {
	data, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	return data, ok
}

func isDatastarRequest(r *http.Request) bool {
	return r.Header.Get(datastarRequestHeader) == "true" ||
		r.Header.Get("HX-Request") == "true" ||
		r.URL.Query().Get("_fragment") == "true"
}

// Indian display convention: DD-MM-YYYY. Input controls stay ISO (YYYY-MM-DD)
// so native date pickers and parsing keep working; these helpers are display-only.
func formatDateTime(t time.Time) string {
	return t.Format("02-01-2006 15:04")
}

func formatDate(t time.Time) string {
	return t.Format("02-01-2006")
}

// PageData is the base data passed to all page templates.
type PageData struct {
	Title         string
	Version       string
	User          *auth.SessionData
	UserDetail    interface{}
	Roles         interface{}
	Settings      interface{}
	FlashError    string
	FlashSuccess  string
	RazorpayKeyID string
	PWAEnabled    bool
	Extra         map[string]interface{}
}

// PaginationData contains pagination data for templates.
type PaginationData struct {
	Page       int
	PerPage    int
	Total      int64
	TotalPages int
	HasPrev    bool
	HasNext    bool
	BasePath   string
	From       string
	To         string
}

// PaginationParams holds pagination and search parameters.
type PaginationParams struct {
	Query    string
	Status   string
	Limit    int
	Page     int
	Offset   int
	DateFrom string
	DateTo   string
}

// dateLayout is the accepted format for from/to list filters (YYYY-MM-DD).
const dateLayout = "2006-01-02"

// dateLayoutIN is the Indian display format accepted alongside ISO (DD-MM-YYYY).
const dateLayoutIN = "02-01-2006"

func parseDateParam(raw string) string {
	if raw == "" {
		return ""
	}
	if _, err := time.Parse(dateLayout, raw); err == nil {
		return raw
	}
	if t, err := time.Parse(dateLayoutIN, raw); err == nil {
		return t.Format(dateLayout)
	}
	return ""
}

// inDate renders an ISO date (YYYY-MM-DD) in Indian format (DD-MM-YYYY)
// for display in filter inputs. Unparseable input passes through unchanged.
func inDate(v interface{}) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t.Format(dateLayoutIN)
	}
	return s
}

func parsePaginationParams(r *http.Request) PaginationParams {
	query := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	if status == "all" {
		status = ""
	}
	limit := 20
	_, _ = fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	page := 1
	_, _ = fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit
	from := parseDateParam(r.URL.Query().Get("from"))
	to := parseDateParam(r.URL.Query().Get("to"))
	if from != "" && to != "" && from > to {
		from, to = to, from
	}
	return PaginationParams{Query: query, Status: status, Limit: limit, Page: page, Offset: offset, DateFrom: from, DateTo: to}
}

func newPaginationData(pp PaginationParams, total int64, basePath string) PaginationData {
	totalPages := int(total / int64(pp.Limit))
	if total%int64(pp.Limit) > 0 {
		totalPages++
	}
	if totalPages < 1 {
		totalPages = 1
	}
	return PaginationData{
		Page:       pp.Page,
		PerPage:    pp.Limit,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    pp.Page > 1,
		HasNext:    pp.Page < totalPages,
		BasePath:   basePath,
	}
}

// AppVersion holds the build version string populated from environment or fallback
var AppVersion = func() string {
	if v := os.Getenv("APP_VERSION"); v != "" {
		return v
	}
	return fmt.Sprintf("%d", time.Now().Unix())
}()

// buildTemplateData creates a flat map for templates from PageData.
func buildTemplateData(data PageData) map[string]interface{} {
	v := data.Version
	if v == "" {
		v = AppVersion
	}
	m := map[string]interface{}{
		"Title":        data.Title,
		"Version":      v,
		"User":         data.User,
		"UserDetail":   data.UserDetail,
		"Roles":        data.Roles,
		"Settings":     data.Settings,
		"FlashError":   data.FlashError,
		"FlashSuccess": data.FlashSuccess,
		"PWAEnabled":   data.PWAEnabled,
	}
	for k, v := range data.Extra {
		m[k] = v
	}
	return m
}

// renderPage renders a full page with the layout.
func (a *App) renderPage(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	layout := a.templatesFor(r).Lookup("layout.html")
	if layout == nil {
		http.Error(w, "layout template not found", http.StatusInternalServerError)
		return
	}

	contentTmpl := a.Templates.Lookup(name)
	if contentTmpl == nil {
		a.renderError(w, http.StatusNotFound, "Page Not Found", fmt.Sprintf("Template %q could not be located.", name), data.User)
		return
	}

	pwaEnabled := data.PWAEnabled
	if a.Config != nil && !pwaEnabled {
		pwaEnabled = a.Config.PWAEnabled
	}
	if data.Extra != nil {
		if extraPWA, ok := data.Extra["PWAEnabled"].(bool); ok {
			pwaEnabled = extraPWA
		}
	}
	data.PWAEnabled = pwaEnabled

	templateData := buildTemplateData(data)

	// Per-org feature snapshot for nav visibility + upsell locks.
	if a.Features != nil {
		tid := string(shared.TenantIDFromContext(r.Context()))
		if tid == "" {
			tid = string(shared.DefaultTenant)
		}
		on := map[string]bool{}
		for _, e := range a.Features.Snapshot(r.Context(), tid) {
			on[e.Key] = e.Enabled
		}
		templateData["Features"] = on
	}

	var buf strings.Builder
	if err := contentTmpl.Execute(&buf, templateData); err != nil {
		a.renderError(w, http.StatusInternalServerError, "Template Execution Error", err.Error(), data.User)
		return
	}

	// NOTE: This branch is reachable when a client sends X-SPA-Request: true
	// (set by SPAMiddleware). The current SPA router does NOT send this header;
	// it is a dormant fast-path for future native-SPA clients. Do not delete
	// unless the team decides to remove SPA support entirely.
	if s, ok := w.(interface{ IsSPARequest() bool }); ok && s.IsSPARequest() {
		w.Header().Set("X-Page-Title", data.Title)
		_, _ = w.Write([]byte(buf.String()))
		return
	}

	var notifications interface{}
	var unreadCount int
	if a.AlertsRepo != nil && r != nil {
		userID := ""
		if user, ok := a.getUserFromContext(r); ok && user != nil {
			userID = user.UserID
		}
		if count, err := a.AlertsRepo.UnreadCount(r.Context(), userID); err == nil {
			unreadCount = count
		}
		if recent, err := a.AlertsRepo.Recent(r.Context(), userID, 5); err == nil {
			notifications = recent
		}
	}
	if unreadCount > 99 {
		unreadCount = 99
	}

	pd := struct {
		Title         string
		Content       template.HTML
		User          *auth.SessionData
		Query         string
		Notifications interface{}
		UnreadCount   int
		HasUnread     bool
		FlashError    string
		FlashSuccess  string
		Version       string
		PWAEnabled    bool
		Features      map[string]bool
		Extra         map[string]interface{}
	}{
		Title:   data.Title,
		Content: template.HTML(buf.String()),
		User:    data.User,
		Query: func() string {
			if q, ok := templateData["Query"].(string); ok {
				return q
			}
			return ""
		}(),
		Notifications: notifications,
		UnreadCount:   unreadCount,
		HasUnread:     unreadCount > 0,
		Features: func() map[string]bool {
			if m, ok := templateData["Features"].(map[string]bool); ok {
				return m
			}
			return nil
		}(),
		FlashError:   data.FlashError,
		FlashSuccess: data.FlashSuccess,
		Version:      AppVersion,
		PWAEnabled:   pwaEnabled,
		Extra:        data.Extra,
	}

	if err := layout.Execute(w, pd); err != nil {
		http.Error(w, fmt.Sprintf("layout template error: %v", err), http.StatusInternalServerError)
	}
}

// renderAuthPage renders a full page with the auth layout (no sidebar).
func (a *App) renderAuthPage(w http.ResponseWriter, name string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	authLayout := a.Templates.Lookup("auth_layout.html")
	if authLayout == nil {
		http.Error(w, "auth layout template not found", http.StatusInternalServerError)
		return
	}

	contentTmpl := a.Templates.Lookup(name)
	if contentTmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}

	templateData := buildTemplateData(data)

	var buf strings.Builder
	if err := contentTmpl.Execute(&buf, templateData); err != nil {
		http.Error(w, fmt.Sprintf("content template error: %v", err), http.StatusInternalServerError)
		return
	}

	pd := struct {
		Title        string
		Content      template.HTML
		User         *auth.SessionData
		FlashError   string
		FlashSuccess string
		Version      string
	}{
		Title:        data.Title,
		Content:      template.HTML(buf.String()),
		User:         data.User,
		FlashError:   data.FlashError,
		FlashSuccess: data.FlashSuccess,
		Version:      AppVersion,
	}

	if err := authLayout.Execute(w, pd); err != nil {
		http.Error(w, fmt.Sprintf("auth layout template error: %v", err), http.StatusInternalServerError)
	}
}

// renderFragment renders a fragment or template safely.
// templatesFor picks the template set for the request's language cookie.
// A nil request (error-render paths) falls back to the default set.
func (a *App) templatesFor(r *http.Request) *template.Template {
	if r != nil {
		if c, err := r.Cookie("lang"); err == nil && i18n.Normalize(c.Value) == "hi" && a.TemplatesHI != nil {
			return a.TemplatesHI
		}
	}
	return a.Templates
}

// SetLang switches the web UI language: GET /lang?to=hi&next=/bookings sets a
// year-long cookie and redirects back. Open redirect is blocked by requiring a
// same-origin path.
func (a *App) SetLang(w http.ResponseWriter, r *http.Request) {
	to := i18n.Normalize(r.URL.Query().Get("to"))
	next := r.URL.Query().Get("next")
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name: "lang", Value: to, Path: "/", MaxAge: 365 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) renderFragment(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl := a.Templates.Lookup(name)
	if tmpl == nil {
		// Fallback: strip _table suffix if present and attempt main template lookup
		fallbackName := strings.Replace(name, "_table.html", ".html", 1)
		tmpl = a.Templates.Lookup(fallbackName)
	}
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}

	// If data is PageData, flatten it
	if pd, ok := data.(PageData); ok {
		data = buildTemplateData(pd)
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

// renderForm renders a form template (full page or fragment).
func (a *App) renderForm(w http.ResponseWriter, r *http.Request, name string, data PageData) {
	if isDatastarRequest(r) {
		a.renderFragment(w, name, data)
		return
	}
	a.renderPage(w, r, name, data)
}

const homeCacheTTL = 15 * time.Minute

var (
	homeCacheMu    sync.Mutex
	cachedHomeHTML []byte
	cachedHomeAt   time.Time
)

// Marketing renders the landing homepage using an in-memory cache with TTL.
func (a *App) Marketing(w http.ResponseWriter, r *http.Request) {
	homeCacheMu.Lock()
	if len(cachedHomeHTML) == 0 || time.Since(cachedHomeAt) > homeCacheTTL {
		tmpl := a.Templates.Lookup("home.html")
		if tmpl != nil {
			var buf bytes.Buffer
			data := map[string]interface{}{"Version": AppVersion}
			if err := tmpl.Execute(&buf, data); err == nil {
				cachedHomeHTML = buf.Bytes()
				cachedHomeAt = time.Now()
			}
		}
	}
	html := cachedHomeHTML
	homeCacheMu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800")
	if len(html) > 0 {
		_, _ = w.Write(html)
		return
	}

	tmpl := a.Templates.Lookup("home.html")
	if tmpl == nil {
		http.Error(w, "home template not found", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{"Version": AppVersion}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

// PolicyPage renders a static legal/policy page template by file name.
func (a *App) PolicyPage(w http.ResponseWriter, r *http.Request, name string) {
	tmpl := a.Templates.Lookup(name)
	if tmpl == nil {
		http.Error(w, name+" template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800")
	data := map[string]interface{}{"Version": AppVersion}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

// Privacy serves the DPDPA privacy policy page.
func (a *App) Privacy(w http.ResponseWriter, r *http.Request) {
	a.PolicyPage(w, r, "privacy.html")
}

// Terms serves the B2B terms of service page.
func (a *App) Terms(w http.ResponseWriter, r *http.Request) {
	a.PolicyPage(w, r, "terms.html")
}

// Refunds serves the refund policy page.
func (a *App) Refunds(w http.ResponseWriter, r *http.Request) {
	a.PolicyPage(w, r, "refunds.html")
}

// FeaturePage serves a public, login-free explainer for a single product
// feature. Unknown slugs return a native 404 (never a redirect to /login).
func (a *App) FeaturePage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	fc, ok := GetFeature(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800")
	related := make([]FeatureContent, 0, len(fc.Related))
	for _, slug := range fc.Related {
		if rf, ok := GetFeature(slug); ok {
			related = append(related, rf)
		}
	}
	data := map[string]interface{}{"Version": AppVersion, "Feature": fc, "RelatedFeatures": related}
	tmpl := a.Templates.Lookup("feature.html")
	if tmpl == nil {
		http.Error(w, "feature.html template not found", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

// DownloadFile serves an uploaded file by ID with authentication and ownership authorization.
func (a *App) DownloadFile(w http.ResponseWriter, r *http.Request) {
	session, ok := a.getUserFromContext(r)
	if !ok || session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	id := filepath.Base(r.URL.Path)
	if !isValidFileID(id) {
		a.renderError(w, http.StatusBadRequest, "Invalid File ID", "The requested file identifier is invalid.", nil)
		return
	}
	file, err := a.Services.Files.GetFile(r.Context(), domain.FileID(id))
	if err != nil {
		a.renderError(w, http.StatusNotFound, "File Not Found", "The requested document or file does not exist.", nil)
		return
	}

	// Authorization check:
	// - Users with "files:read" permission have broad access.
	// - Public tenant assets ("company_logo", "logo") are accessible to all authenticated users.
	// - Specific uploads owned by the user (matching UploadableID).
	isAllowed := false
	if a.AuthSrv != nil {
		isAllowed = a.AuthSrv.Can(session.UserID, "files", "read")
	} else {
		isAllowed = session.Role == "admin" || session.Role == "dispatcher" || session.Role == "accountant"
	}
	isPublicAsset := file.UploadableType == "company_logo" || file.UploadableType == "logo"
	isOwner := file.UploadableID != nil && *file.UploadableID == session.UserID

	if !isAllowed && !isPublicAsset && !isOwner {
		a.renderError(w, http.StatusForbidden, "Access Denied", "You do not have permission to access this file.", nil)
		return
	}

	uploadDir := filepath.Clean(a.Config.UploadDir)
	if uploadDir == "." {
		uploadDir = ""
	}
	filePath := filepath.Clean(filepath.Join(uploadDir, file.Path))
	if uploadDir != "" && !strings.HasPrefix(filePath, uploadDir+string(os.PathSeparator)) && filePath != uploadDir {
		a.renderError(w, http.StatusBadRequest, "Invalid File Path", "The requested file path is invalid.", nil)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, filePath)
}

func isValidFileID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	return !strings.ContainsAny(id, `/\`) && !strings.Contains(id, "..")
}

// ErrorInfo encapsulates full diagnostic context for error rendering and API responses.
type ErrorInfo struct {
	StatusCode int
	Title      string
	Message    string
	ErrorCode  string
	Model      string
	Path       string
	RequestID  string
	User       *auth.SessionData
}

func defaultErrorCode(statusCode int, model string) string {
	prefix := "ERR_"
	if model != "" {
		sanitized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(model), " ", "_"))
		prefix = "ERR_" + sanitized + "_"
	}
	switch statusCode {
	case http.StatusBadRequest:
		if model != "" {
			return prefix + "INVALID"
		}
		return "ERR_BAD_REQUEST"
	case http.StatusUnauthorized:
		return "ERR_UNAUTHORIZED"
	case http.StatusForbidden:
		return "ERR_FORBIDDEN"
	case http.StatusNotFound:
		if model != "" {
			return prefix + "NOT_FOUND"
		}
		return "ERR_NOT_FOUND"
	case http.StatusMethodNotAllowed:
		return "ERR_METHOD_NOT_ALLOWED"
	case http.StatusConflict:
		if model != "" {
			return prefix + "CONFLICT"
		}
		return "ERR_CONFLICT"
	case http.StatusGone:
		if model != "" {
			return prefix + "EXPIRED"
		}
		return "ERR_GONE"
	case http.StatusTooManyRequests:
		return "ERR_RATE_LIMITED"
	default:
		if model != "" {
			return prefix + "ERROR"
		}
		return "ERR_INTERNAL_SERVER"
	}
}

func isAPIRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html") {
		return true
	}
	return false
}

// renderErrorInfo renders an error with full error identification (code, model, request ID).
func (a *App) renderErrorInfo(w http.ResponseWriter, r *http.Request, info ErrorInfo) {
	if info.StatusCode == 0 {
		info.StatusCode = http.StatusInternalServerError
	}
	if info.Title == "" {
		info.Title = http.StatusText(info.StatusCode)
	}
	if info.ErrorCode == "" {
		info.ErrorCode = defaultErrorCode(info.StatusCode, info.Model)
	}
	if info.RequestID == "" && r != nil {
		if reqID, ok := r.Context().Value(auth.ContextReqID).(string); ok {
			info.RequestID = reqID
		}
	}
	if info.Path == "" && r != nil {
		info.Path = r.URL.Path
	}
	if info.User == nil && r != nil {
		info.User, _ = a.getUserFromContext(r)
	}

	// API / JSON clients
	if r != nil && isAPIRequest(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(info.StatusCode)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":        info.ErrorCode,
				"model":       info.Model,
				"message":     info.Message,
				"status_code": info.StatusCode,
				"path":        info.Path,
				"request_id":  info.RequestID,
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			},
		})
		return
	}

	// HTML clients
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(info.StatusCode)

	escapedTitle := html.EscapeString(info.Title)
	escapedMessage := html.EscapeString(info.Message)
	fallback := fmt.Sprintf("<!DOCTYPE html><html><head><title>%d - %s</title></head><body><h1>%d - %s</h1><p>%s</p><p><small>Error ID: %s | Request ID: %s</small></p></body></html>",
		info.StatusCode, escapedTitle, info.StatusCode, escapedTitle, escapedMessage, info.ErrorCode, info.RequestID)

	errTmpl := a.Templates.Lookup("error.html")
	if errTmpl == nil {
		_, _ = w.Write([]byte(fallback))
		return
	}

	var buf strings.Builder
	if err := errTmpl.Execute(&buf, map[string]interface{}{
		"StatusCode": info.StatusCode,
		"Title":      info.Title,
		"Message":    info.Message,
		"ErrorCode":  info.ErrorCode,
		"Model":      info.Model,
		"Path":       info.Path,
		"RequestID":  info.RequestID,
		"User":       info.User,
	}); err != nil {
		slog.Error("error template execution failed", "statusCode", info.StatusCode, "title", info.Title, "error", err)
		_, _ = w.Write([]byte(fallback))
		return
	}

	layout := a.templatesFor(r).Lookup("layout.html")
	if layout == nil {
		_, _ = w.Write([]byte(buf.String()))
		return
	}

	pwaEnabled := false
	if a.Config != nil {
		pwaEnabled = a.Config.PWAEnabled
	}

	if err := layout.Execute(w, struct {
		Title         string
		Content       template.HTML
		User          *auth.SessionData
		Query         string
		Notifications interface{}
		UnreadCount   int
		HasUnread     bool
		FlashError    string
		FlashSuccess  string
		Version       string
		PWAEnabled    bool
		Features      map[string]bool
		Extra         map[string]interface{}
	}{
		Title:      info.Title,
		Content:    template.HTML(buf.String()),
		User:       info.User,
		Version:    AppVersion,
		PWAEnabled: pwaEnabled,
		Features:   nil,
		Extra:      map[string]interface{}{},
	}); err != nil {
		slog.Error("error layout execution failed", "statusCode", info.StatusCode, "title", info.Title, "error", err)
		_, _ = w.Write([]byte(fallback))
	}
}

// renderError renders a friendly user-facing error screen using error.html and layout.html.
func (a *App) renderError(w http.ResponseWriter, statusCode int, title string, message string, user *auth.SessionData) {
	a.renderErrorInfo(w, nil, ErrorInfo{
		StatusCode: statusCode,
		Title:      title,
		Message:    message,
		User:       user,
	})
}

// RenderErrorWithContext renders an error with full request context (path, reqID, model, error code).
func (a *App) RenderErrorWithContext(w http.ResponseWriter, r *http.Request, statusCode int, title, message, model, errCode string) {
	a.renderErrorInfo(w, r, ErrorInfo{
		StatusCode: statusCode,
		Title:      title,
		Message:    message,
		Model:      model,
		ErrorCode:  errCode,
	})
}

// NotFoundHandler is the global 404 handler mounted on the router.
func (a *App) NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	reqID, _ := r.Context().Value(auth.ContextReqID).(string)
	user, _ := a.getUserFromContext(r)

	msg := fmt.Sprintf("The requested URL %q could not be found.", r.URL.Path)
	if r.URL.Path == "/share" {
		msg = "Live trip tracking requires a valid share token (e.g., /share/{token}). If you are looking for managed share links, please navigate to Shares."
	}

	a.renderErrorInfo(w, r, ErrorInfo{
		StatusCode: http.StatusNotFound,
		Title:      "Page Not Found",
		Message:    msg,
		ErrorCode:  "ERR_PAGE_NOT_FOUND",
		Path:       r.URL.Path,
		RequestID:  reqID,
		User:       user,
	})
}

// MethodNotAllowedHandler is the global 405 handler mounted on the router.
func (a *App) MethodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	reqID, _ := r.Context().Value(auth.ContextReqID).(string)
	user, _ := a.getUserFromContext(r)

	a.renderErrorInfo(w, r, ErrorInfo{
		StatusCode: http.StatusMethodNotAllowed,
		Title:      "Method Not Allowed",
		Message:    fmt.Sprintf("HTTP method %s is not supported for %q.", r.Method, r.URL.Path),
		ErrorCode:  "ERR_METHOD_NOT_ALLOWED",
		Path:       r.URL.Path,
		RequestID:  reqID,
		User:       user,
	})
}

// MountPWARoutes mounts the web manifest and service worker if PWA is enabled.
func (a *App) MountPWARoutes(r chi.Router) {
	if a.Config == nil || !a.Config.PWAEnabled {
		return
	}
	staticDir := "internal/static"
	if a.Config != nil && a.Config.StaticDir != "" {
		staticDir = a.Config.StaticDir
	}

	r.Get("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, filepath.Join(staticDir, "manifest.webmanifest"))
	})

	r.Get("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
		http.ServeFile(w, r, filepath.Join(staticDir, "js", "sw.js"))
	})
}
