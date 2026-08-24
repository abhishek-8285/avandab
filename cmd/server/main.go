package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/joho/godotenv"
	"gopkg.in/telebot.v3"
	_ "modernc.org/sqlite"
	"transport-app/internal/agent"
	"transport-app/internal/agent/rl"
	alertchannels "transport-app/internal/alerts/channels"
	alertpipeline "transport-app/internal/alerts/pipeline"
	alertsqlite "transport-app/internal/alerts/repository/sqlite"
	"transport-app/internal/apiversion"
	"transport-app/internal/shared/resilience"

	dbmigr "transport-app/db"
	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/domain"
	"transport-app/internal/ewaybill"
	fastag "transport-app/internal/fastag"
	fuel "transport-app/internal/fuel"
	geofenceapp "transport-app/internal/geofence/application"
	geofencerepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	geofenceworker "transport-app/internal/geofence/infrastructure/worker"
	geofenceHandlers "transport-app/internal/geofence/presentation/api/handlers"
	"transport-app/internal/handlers"
	"transport-app/internal/integration"
	intAcc "transport-app/internal/integration/accounting"
	intEWB "transport-app/internal/integration/ewaybill"
	intFastag "transport-app/internal/integration/fastag"
	"transport-app/internal/logging"
	"transport-app/internal/maintenance"
	"transport-app/internal/middleware"
	"transport-app/internal/mqttservice"
	"transport-app/internal/openapispec"
	"transport-app/internal/operations/audit"
	"transport-app/internal/operations/dashboard"
	opserrors "transport-app/internal/operations/errors"
	"transport-app/internal/operations/health"
	"transport-app/internal/operations/notifications"
	"transport-app/internal/pnl"
	"transport-app/internal/rag"
	"transport-app/internal/realtime"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/safety"
	"transport-app/internal/service"
	"transport-app/internal/telemetry"

	// Vertical-slice use cases
	bookingApp "transport-app/internal/booking/application"
	bookingHandlers "transport-app/internal/booking/presentation/api/handlers"

	authAPIHandlers "transport-app/internal/auth/presentation/api/handlers"

	invoiceApp "transport-app/internal/invoice/application"
	invoiceHandlers "transport-app/internal/invoice/presentation/api/handlers"

	paymentApp "transport-app/internal/payment/application"
	paymentHandlers "transport-app/internal/payment/presentation/api/handlers"
	"transport-app/internal/payment/razorpay"

	tripApp "transport-app/internal/trip/application"
	tripHandlers "transport-app/internal/trip/presentation/api/handlers"

	// Shared infrastructure
	"transport-app/internal/eta"
	"transport-app/internal/events"
	"transport-app/internal/features"
	founder "transport-app/internal/founder"
	founderAlerts "transport-app/internal/founder/alerts"
	"transport-app/internal/founder/digest"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/uow"

	"github.com/pressly/goose/v3"
	cachepkg "transport-app/internal/cache"
	appdb "transport-app/internal/database"
	"transport-app/internal/leader"
	"transport-app/internal/metrics"
)

// Version is set via ldflags during build
var Version string

func init() {
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".woff", "font/woff")
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// agentRequestTimeout lifts the global 60s request deadline for agent chat
// requests: multi-turn tool conversations need more than one LLM call. The
// parent timeout's deadline is dropped and a longer one substituted.
func agentRequestTimeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func main() {
	// Load .env (if present) before config so file-based config takes effect.
	// Real env vars take precedence; missing file is not an error.
	if err := godotenv.Load(); err != nil {
		_ = err
	}
	if Version != "" {
		_ = os.Setenv("APP_VERSION", Version)
		handlers.AppVersion = Version
	}
	cfg := config.Load()
	logging.Setup(cfg.LogLevel, cfg.AppEnv)

	logger := slog.Default()
	if cfg.IsProduction() && cfg.UsingKnownDefaultSecret() {
		logger.Error("Refusing to start in production with known default secrets. Set strong, unique COOKIE_SECRET, API_SECRET and RAZORPAY_* values in the environment.")
		os.Exit(1)
	}
	port := cfg.Port
	if port == "" {
		port = "8080"
	}
	logger.Info("Starting MVTMS server", "env", cfg.AppEnv, "port", cfg.Port)

	ctx := context.Background()

	// Open the configured DB engine (sqlite | postgres | mysql) via the
	// config-driven factory — switching engines never touches this file.
	database, err := appdb.Open(ctx, &cfg.Database, logger)
	if err != nil {
		logger.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = database.Close() }()

	// Run migrations from embedded filesystem using the engine's dialect
	migrations, err := fs.Sub(dbmigr.Migrations, "migrations")
	if err != nil {
		logger.Error("Failed to read embedded migrations", "error", err)
		os.Exit(1)
	}

	provider, err := goose.NewProvider(appdb.GooseDialect(cfg.Database.Driver), database, migrations)
	if err != nil {
		logger.Error("Failed to create migration provider", "error", err)
		os.Exit(1)
	}

	if _, err := provider.Up(ctx); err != nil {
		logger.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	logger.Info("Database migrated successfully")

	// Initialize repository
	repo := sqlite.NewRepository(database)

	// Config-selected cache backend (none | memory | redis). Attached to the
	// handler app so hot reads can be served without knowing the backend.
	appCache := cachepkg.MustNew(ctx, &cfg.Cache, logger)
	if closer, ok := appCache.(cachepkg.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	// Single shared in-memory event bus: services, automation subscribers,
	// the outbox relay, and founder handlers all publish/listen on the SAME
	// instance. A duplicated bus silently severs automation (Spec 09 §5.1).
	eventBus := events.NewInMemoryBus()

	// Initialize services
	services := service.NewServices(repo, cfg, logger, eventBus)

	// Initialize channel adapters (Spec 05 §3)
	inAppProvider := alertchannels.NewInAppProvider()
	telegramProvider := alertchannels.NewTelegramProvider(cfg, logger)
	stubProviders := alertchannels.NewStubProviders(logger)

	emailSender := notifications.NewSMTPEmailSender(notifications.SMTPConfig{
		Host:     cfg.Notify.SMTPHost,
		Port:     cfg.Notify.SMTPPort,
		User:     cfg.Notify.SMTPUser,
		Password: cfg.Notify.SMTPPassword,
		From:     cfg.Notify.SMTPFrom,
	})
	smsSender := notifications.NewWebhookSMSSender(cfg.Notify.SMSWebhookURL, cfg.Notify.SMSWebhookToken)
	notifSvc := notifications.NewServiceWithChannels(emailSender, smsSender)

	var emailChannel alertchannels.Provider = stubProviders["email"]
	if emailSender.Configured() {
		emailChannel = alertchannels.NewEmailBridge(notifSvc)
	}
	var smsChannel alertchannels.Provider = stubProviders["sms"]
	if smsSender.Configured() {
		smsChannel = alertchannels.NewSMSBridge(notifSvc)
	}

	alertProviderMap := map[string]alertchannels.Provider{
		"in_app":   inAppProvider,
		"telegram": telegramProvider,
		"email":    emailChannel,
		"sms":      smsChannel,
		"whatsapp": stubProviders["whatsapp"],
	}

	// Alerts Pipeline (Spec 05 §1, §3, §4)
	alertRepo := alertsqlite.NewAlertRepository(database)
	alertEngine := alertpipeline.NewEngine(alertRepo, alertProviderMap, logger)
	alertEscalator := alertpipeline.NewEscalator(alertRepo, alertProviderMap, logger)
	alertFlusher := alertpipeline.NewFlusher(alertRepo, alertProviderMap, logger)

	eventBus.Subscribe("AlertEvent", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})
	eventBus.Subscribe("telemetry.alert", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})
	eventBus.Subscribe("alert.dtc", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})
	eventBus.Subscribe("ComplianceBlocked", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})
	eventBus.Subscribe("SOSEvent", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})
	eventBus.Subscribe("telemetry.sos", func(ctx context.Context, e events.Event) error {
		return alertEngine.ProcessEvent(ctx, e)
	})

	// Initialize auth store
	authStore := auth.NewSessionStore(cfg.CookieSecret, cfg.CookieSecure)
	authStore.SetValidator(services.Auth)

	// Initialize Casbin authorization service
	authSvc, err := auth.NewCasbinAuthorizationService(database)
	if err != nil {
		logger.Error("Failed to initialize Casbin authorization service", "error", err)
		os.Exit(1)
	}

	// scorecard:update is referenced by the /scorecard/drivers/{id}/resolve
	// route (Spec 03 §6.1) but was not seeded by migration 00043 (only
	// scorecard:read was). Self-heal it at startup so the admin resolve
	// action works on DBs created before this fix — no migration needed.
	if err := seedScorecardUpdatePermission(ctx, database, authSvc); err != nil {
		logger.Warn("scorecard:update permission seed failed; resolve route stays admin-403", "error", err)
	}

	// dashboard:read gates the console money-strip API (Spec 22 §2.2).
	// Step 2 ships without a migration, so seed at startup (idempotent).
	if err := seedDashboardReadPermission(ctx, database, authSvc); err != nil {
		logger.Warn("dashboard:read permission seed failed; money-strip stays admin-403", "error", err)
	}

	// ewaybill:write gates the console EWB extend API (Spec 22 §2.3);
	// no migration seeds it, so self-heal at startup (idempotent).
	if err := seedEwaybillWritePermission(ctx, database, authSvc); err != nil {
		logger.Warn("ewaybill:write permission seed failed; extend stays 403", "error", err)
	}

	// Create the initial admin account from env vars (optional; skipped when
	// an admin already exists or the vars are unset).
	bootstrapAdmin(ctx, services, authSvc, cfg, logger)

	// Initialize handlers app
	resetTokens := auth.NewResetTokenStore(0)
	app := handlers.NewApp(services, cfg, authStore, database, authSvc, resetTokens)
	// Per-org feature gates (registry lives on App; shared by routes + workers).
	featureGate := func(key string) func(http.Handler) http.Handler {
		return features.Gate(app.Features, key)
	}
	// Worker-tick gate: skip a background sweep when its feature is off for
	// the default org (workers are single-tenant today). Cached → cheap.
	featureTick := func(key string) bool {
		return app.Features.Enabled(context.Background(), string(shared.DefaultTenant), key)
	}
	app.Cache = appCache
	app.Notify = notifSvc

	// Standardized route locations (gap #46): best-effort forward geocoding
	// of route endpoints whenever a Nominatim-compatible service is set.
	if cfg.LiveMap.NominatimURL != "" {
		services.Routes.WithGeocoder(service.NewNominatimGeocoder(cfg.LiveMap.NominatimURL))
	}

	// E-Way Bill and FASTag services (Spec 07)
	integCfg := integration.LoadConfig()
	ewbCfg := ewaybill.Config{
		Enabled:              cfg.EWayBill.WorkerEnabled,
		Interval:             cfg.EWayBill.WorkerInterval,
		ExtensionKM:          cfg.EWayBill.ExtensionKM,
		ExtensionLeadSeconds: cfg.EWayBill.ExtensionLeadSeconds,
		MinInvoiceValue:      cfg.EWayBill.MinInvoiceValue,
	}
	ewbClient := intEWB.NewClient(integCfg.EWayBill)
	ewbService := ewaybill.NewEWayBillService(database, eventBus, ewbClient, logger, ewbCfg)
	ewbService.SubscribeTripEvents(eventBus)
	services.EWayBill = ewbService

	fastagClient := intFastag.NewClient(integCfg.FASTag, database)
	fastagConfig := fastag.LoadConfig(database)
	fastagService := fastag.NewFASTagService(database, fastagClient, fastagConfig, logger)

	app.EWayBill = handlers.NewEWayBillHandlers(app, ewbService, authSvc)
	app.FASTag = handlers.NewFASTagHandlers(app, fastagService, authSvc)

	accountingClient := intAcc.NewClient(integCfg.Accounting)
	accountingConsumer := intAcc.NewConsumer(database, accountingClient, integCfg.Accounting)
	accountingConsumer.SubscribeEvents(eventBus)
	app.Accounting = handlers.NewAccountingHandlers(app, accountingConsumer, authSvc)
	app.Settlements = handlers.NewSettlementHandlers(app, services.Settlements, authSvc)
	app.Documents = handlers.NewDocumentHandlers(app, services.Documents, authSvc)
	app.FilesAPI = handlers.NewFilesAPIHandlers(app, services.Files, authSvc)

	// ── Ops: error reporting, login audit, dashboard ─────────────────────
	reporter := opserrors.NewReporter(notifSvc, opserrors.NewSQLiteStore(database), cfg.AppEnv, Version)
	loginAuditSvc := audit.NewLoginAuditService(notifSvc, audit.SecurityPolicy{
		NotifyOnNewDevice: true,
		NotifyOnNewIP:     true,
	})
	dashboardHandler := dashboard.NewDashboardHandler(reporter, loginAuditSvc)
	app.OpsErrors = handlers.NewOpsErrorsHandler(app, reporter)
	healthChecker := health.NewChecker(database)

	// ── Vertical-slice infrastructure ────────────────────────────────────
	sqlUoW := uow.NewSQLUnitOfWork(database)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()

	// Sprint 1 – Booking use cases
	createBookingUC := bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock)
	confirmBookingUC := bookingApp.NewConfirmBookingUseCase(sqlUoW, realClock)
	cancelBookingUC := bookingApp.NewCancelBookingUseCase(sqlUoW, realClock)
	updateBookingUC := bookingApp.NewUpdateBookingUseCase(sqlUoW)
	completeBookingUC := bookingApp.NewCompleteBookingUseCase(sqlUoW, realClock)
	deleteBookingUC := bookingApp.NewDeleteBookingUseCase(sqlUoW)
	getBookingUC := bookingApp.NewGetBookingUseCase(sqlUoW)
	listBookingsUC := bookingApp.NewListBookingsUseCase(sqlUoW)

	// Sprint 2 – Trip use cases
	createTrip := tripApp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	assignDriver := tripApp.NewAssignDriverUseCase(sqlUoW, realClock)
	assignVehicle := tripApp.NewAssignVehicleUseCase(sqlUoW, realClock)
	scheduleTrip := tripApp.NewScheduleTripUseCase(sqlUoW, realClock)
	startTrip := tripApp.NewStartTripUseCase(sqlUoW, realClock)
	reachPickup := tripApp.NewReachPickupUseCase(sqlUoW, realClock)
	startTransit := tripApp.NewStartTransitUseCase(sqlUoW, realClock)
	deliver := tripApp.NewDeliverUseCase(sqlUoW, realClock)
	completeTrip := tripApp.NewCompleteTripUseCase(sqlUoW, realClock)
	cancelTrip := tripApp.NewCancelTripUseCase(sqlUoW, realClock)
	getTrip := tripApp.NewGetTripUseCase(sqlUoW)
	listTrips := tripApp.NewListTripsUseCase(sqlUoW)

	// Sprint 3 – Invoice use cases
	generateInvoice := invoiceApp.NewGenerateInvoiceUseCase(sqlUoW, idGen, realClock)
	getInvoice := invoiceApp.NewGetInvoiceUseCase(sqlUoW)
	listInvoices := invoiceApp.NewListInvoicesUseCase(sqlUoW)
	voidInvoice := invoiceApp.NewVoidInvoiceUseCase(sqlUoW, realClock)

	// Sprint 4 – Payment use cases
	recordPayment := paymentApp.NewRecordPaymentUseCase(sqlUoW, idGen, realClock)
	getPayment := paymentApp.NewGetPaymentUseCase(sqlUoW)
	listPayments := paymentApp.NewListPaymentsUseCase(sqlUoW)
	reversePayment := paymentApp.NewReversePaymentUseCase(sqlUoW, idGen, realClock)
	listPaymentsByInvoice := paymentApp.NewListPaymentsByInvoiceUseCase(sqlUoW)

	// ── API handlers ──────────────────────────────────────────────────────
	bookingAPIHandler := bookingHandlers.NewAPIBookingHandler(
		createBookingUC, confirmBookingUC, cancelBookingUC, updateBookingUC, completeBookingUC, deleteBookingUC, getBookingUC, listBookingsUC,
		authSvc,
	)
	tripAPIHandler := tripHandlers.NewAPITripHandler(
		createTrip, assignDriver, assignVehicle, scheduleTrip, startTrip, reachPickup, startTransit, deliver, completeTrip, cancelTrip, getTrip, listTrips,
		authSvc,
	)
	invoiceAPIHandler := invoiceHandlers.NewAPIInvoiceHandler(generateInvoice, getInvoice, listInvoices, voidInvoice, authSvc)

	// Detention attach/waive endpoints (Spec 02 §6)
	detLogsRepo := geofencerepo.NewEventLogRepository(database)
	geofenceAPIHandler := geofenceHandlers.NewAPIGeofenceHandler(
		geofenceapp.NewAttachDetentionUseCase(sqlUoW, detLogsRepo, generateInvoice),
		geofenceapp.NewWaiveDetentionUseCase(sqlUoW, detLogsRepo),
		authSvc,
	)
	razorpayWebhookUC := paymentApp.NewRazorpayWebhookUseCase(recordPayment, sqlUoW, cfg.RazorpayWebhook, realClock)
	razorpayClient := razorpay.NewRazorpayClient(cfg.RazorpayKeyID, cfg.RazorpayKeySecret)
	razorpayOrderUC := paymentApp.NewCreateRazorpayOrderUseCase(sqlUoW, razorpayClient, cfg.RazorpayKeyID)
	razorpayVerifyUC := paymentApp.NewVerifyRazorpayPaymentUseCase(sqlUoW, recordPayment, razorpayClient, cfg.RazorpayKeySecret, realClock)
	paymentAPIHandler := paymentHandlers.NewAPIPaymentHandler(recordPayment, getPayment, listPayments, reversePayment, listPaymentsByInvoice, razorpayWebhookUC, razorpayOrderUC, razorpayVerifyUC, authSvc)
	integrationHandler := integration.NewHandler(integration.LoadConfig(), authSvc, database)

	// Setup router
	r := chi.NewRouter()
	r.NotFound(app.NotFoundHandler)
	r.MethodNotAllowed(app.MethodNotAllowedHandler)
	r.Use(middleware.RequestID)
	r.Use(middleware.SecurityHeaders)
	r.Use(apiversion.Middleware)
	// Golden-signal request metrics (count, latency, status per route) for
	// the Prometheus exposition mounted at GET /metrics below.
	r.Use(metrics.Middleware)
	r.Use(middleware.Logger)
	// Panic safety net: converts panics into RFC7807-style problem+json 500s
	// and reports them as CRITICAL errors to the ops reporter (Spec 16 §5.5).
	r.Use(middleware.Recoverer(reporter))
	// Exempt the SSE streams from the global 60s request timeout (Spec 04 §1.2, Spec 12 §5.1):
	// long-lived EventSource connections must outlive the deadline. REST polling
	// (/live) is unaffected and remains the source of truth in multi-instance.
	r.Use(middleware.SkipForPaths(
		chiMiddleware.Timeout(60*time.Second),
		"/dashboard/stream",
		"/map/stream",
		"/api/v1/telemetry/stream",
	))
	r.Use(middleware.SPAMiddleware)

	// Global HTTP middleware: Limit request body to 32MB in RAM (prevents disk spooling)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Body = http.MaxBytesReader(w, req.Body, 32<<20)
			next.ServeHTTP(w, req)
		})
	})

	// CSRF defense-in-depth: reject cross-site state-changing requests that
	// carry a session cookie (complements SameSite=Lax). Bearer-token API
	// requests and cookie-less requests are unaffected. Strict mode rejects
	// browser requests that omit both Origin and Referer.
	r.Use(middleware.CSRFProtectStrict(authStore))

	// Ops: liveness, health, readiness (no auth — probe endpoints)
	r.Get("/healthz", healthChecker.LivenessHandler)
	r.Get("/health", healthChecker.HealthHandler)
	r.Get("/readyz", healthChecker.ReadinessHandler)
	// Prometheus scrape endpoint (probe-style, no auth — bind the port to an
	// internal interface or firewall it in production if exposure is a concern).
	r.Get("/metrics", metrics.Handler().ServeHTTP)

	// Direct SEO Endpoints
	r.Get("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		sitemap := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://avandab.com/</loc>
    <changefreq>daily</changefreq>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://avandab.com/login</loc>
    <changefreq>monthly</changefreq>
    <priority>0.5</priority>
  </url>
  <url>
    <loc>https://avandab.com/register</loc>
    <changefreq>monthly</changefreq>
    <priority>0.5</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/dashboard</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/trips</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/routes</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/bookings</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/vehicles</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/drivers</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/customers</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/invoices</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/payments</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/reports</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/audit-logs</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/settings</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/users</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/company</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/kharcha</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/assistant</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
</urlset>`
		_, _ = w.Write([]byte(sitemap))
	})

	// API discovery and OpenAPI spec (public)
	r.Get("/api/versions", apiversion.VersionsHandler)
	openapispec.RegisterRoutes(r)

	// ── REST API v1 ───────────────────────────────────────────────────────
	// Require dedicated API token secret; fall back to cookie secret only in non-production.
	apiSecret := []byte(cfg.APITokenSecret)
	if len(apiSecret) == 0 {
		if cfg.IsProduction() {
			slog.Error("API_SECRET must be explicitly configured in production")
			os.Exit(1)
		}
		apiSecret = []byte(cfg.CookieSecret)
	}
	authAPIHandler := authAPIHandlers.NewAPIAuthHandler(services.Auth, services.Users, apiSecret)

	// ── Telemetry Ingestion Pipeline (Phase 1) ─────────────────────────
	telemetryCfg := cfg.Telemetry
	ingestCfg := telemetry.IngestConfig{
		OdometerMaxRegressionKM: telemetryCfg.OdometerMaxRegressionKM,
		FuelClampDeltaPct:       telemetryCfg.FuelClampDeltaPct,
		BatchSize:               telemetryCfg.BatchSize,
		FlushInterval:           telemetryCfg.FlushInterval,
		RawRetentionDays:        telemetryCfg.RawRetentionDays,
	}
	ingestor := telemetry.NewIngestor(database, sqlUoW, eventBus, idGen, nil, ingestCfg)
	mqttHandler := telemetry.NewMQTTIngestHandler(ingestor, logger)
	httpHandler := telemetry.NewHTTPIngestHandler(ingestor, telemetry.NewDeviceStore(database), telemetryCfg.DeviceSecretPepper)

	// ── High-Performance Architecture Protocols ──────────────────────
	// 1. MQTT Broker Client Setup
	mqttURL := os.Getenv("MQTT_URL")
	if mqttURL == "" {
		mqttURL = "tcp://localhost:1883"
	}
	// TLS hard-fail in production (Spec 01 §13.12)
	if cfg.AppEnv == "production" && strings.HasPrefix(mqttURL, "tcp://") {
		logger.Warn("MQTT_URL uses unencrypted tcp:// in production; configure TLS (ssl:// or tls://)")
	}
	// The broker spawns its own background goroutines (Paho read loop) which
	// keep the client alive for the process lifetime. The TelemetryHandler
	// callback routes canonical frames into the ingestion pipeline.
	_ = mqttservice.NewMQTTBroker(mqttURL, mqttHandler.HandleMessage)

	// ── Geofence Dwell Engine (Spec 02 §4) ───────────────────────────
	// Constructed here (needs the app DB + bus) but started with the
	// signal-scoped ctx near the other background loops.
	var dwellWorker *geofenceworker.DwellWorker
	if cfg.Telemetry.Enabled {
		dwellWorker = geofenceworker.NewDwellWorker(database, sqlUoW,
			geofenceapp.NewConfigReader(database), eventBus, logger)
		dwellWorker.WithTripTransitions(reachPickup, startTransit)
	}

	// ── Fuel Anomaly Engine (Spec 03 §1.2) ───────────────────────────
	// Single-instance, in-memory per-vehicle state replayed on startup
	// (package internal/fuel godoc). Runs only when telemetry is enabled —
	// the engine is a consumer of telemetry_snapshots.
	var fuelEngine *fuel.FuelEngine
	if cfg.Telemetry.Enabled {
		fuelEngine = fuel.NewEngine(database, sqlUoW, fuel.NewConfigReader(database), logger)
	}

	// ── Safety Event Engine (Spec 03 §4.1 producers, roadmap M2) ─────
	// Consumes telemetry_snapshots like the fuel engine; emits the five
	// missing scorecard feeds (speeding/harsh_braking/harsh_accel/idling/
	// night_driving) into driver_behaviour_events + SAFETY alerts.
	var safetyEngine *safety.Engine
	if cfg.Telemetry.Enabled {
		safetyEngine = safety.NewEngine(database, sqlUoW, fuel.NewConfigReader(database), logger)
	}

	// ── RAG (codebase search) ────────────────────────────────────────────
	// Created before the protected group below so its routes mount behind RequireAPIAuth.
	var ragHandler *rag.Handler
	if cfg.RAG.Enabled {
		ragStore, err := rag.NewVectorStore(cfg.RAG.VectorDBPath)
		if err != nil {
			logger.Warn("RAG vector store init failed, RAG disabled", "error", err)
		} else {
			var embedder rag.Embedder
			if cfg.RAG.EmbeddingAPIKey != "" {
				embedder = rag.NewOpenAIEmbedder(cfg.RAG.EmbeddingAPIKey, cfg.RAG.EmbeddingBaseURL, cfg.RAG.EmbeddingModel)
			} else {
				embedder = rag.NewHashEmbedder(384)
				logger.Warn("RAG: no embedding API key configured, using hash-based embeddings (lower quality)")
			}
			ragSvc := rag.NewService(embedder, ragStore, cfg.RAG.ChunkSize, cfg.RAG.ChunkOverlap, cfg.UploadDir)
			ragHandler = rag.NewHandler(ragSvc).WithAllowedDirs(cfg.RAG.IndexDirs).
				WithPermissionGuards(
					middleware.RequirePermission(authSvc, "rag", "read"),
					middleware.RequirePermission(authSvc, "rag", "write"),
				)

			// Auto-index on startup if dirs configured
			if len(cfg.RAG.IndexDirs) > 0 {
				go func() {
					for _, dir := range cfg.RAG.IndexDirs {
						if count, err := ragSvc.IndexDirectory(dir); err != nil {
							logger.Error("RAG auto-index failed", "dir", dir, "error", err)
						} else {
							logger.Info("RAG auto-indexed", "dir", dir, "chunks", count)
						}
					}
				}()
			}

			logger.Info("RAG enabled", "dirs", cfg.RAG.IndexDirs, "vector_db", cfg.RAG.VectorDBPath)
		}
	}

	// Public: token endpoint (no auth required) — rate-limited against brute force
	authAPIHandler.Register(r.With(middleware.RateLimitDistributed(appCache, 10)))

	// Public: mobile/API password reset (JSON, anti-enumeration generic response) — rate-limited
	r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/api/v1/auth/forgot-password", app.Auth.ForgotPasswordAPI)
	r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/api/v1/auth/reset-password", app.Auth.ResetPasswordAPI)

	// ── AI Agent (operations assistant) — built after RAG below ─────────
	var agentAPI *agent.Handler
	var approvalSvc *agent.ApprovalService

	// Public: Razorpay webhook — signature-verified, rate-limited against flood attacks
	r.With(middleware.RateLimitDistributed(appCache, 30)).Post("/api/v1/payments/razorpay-webhook", paymentAPIHandler.RazorpayWebhook)

	// Public: device GPS ingestion (own hardware + mobile app) — authenticated via
	// X-Device-Token header (HMAC-SHA256), not session/Bearer token.
	if cfg.Telemetry.Enabled {
		httpHandler.RegisterRoutes(r.With(middleware.RateLimitDistributed(appCache, 30)))
	}

	// SSE hub (single-process, in-memory fan-out, Spec 04 §1.2)
	sseHub := realtime.NewHub(cfg.LiveMap.SSEKeepaliveSec, logger)
	realtime.AttachToBus(eventBus, sseHub)

	// ETA service (pure read path, Spec 04 §5, 3D) + history recorder (Spec 18 Wave A bridge)
	etaService := eta.NewEtaService(database, cfg.LiveMap.EtaStaleMin, cfg.LiveMap.EtaWindowMin, cfg.LiveMap.EtaGuardMaxRegressMin)
	if app.Share != nil {
		app.Share.EtaService = etaService
	}
	// Bridge: TripCompleted → eta_history segments (must be before Wave B backhaul)
	etaService.SubscribeTripEvents(eventBus, logger)

	// Owner Command Center handlers (Spec 22) — shared by the protected API
	// group (money strip) and web routes (console page).
	consoleHandlers := handlers.NewConsoleHandlers(app, app.AlertsRepo, services.PNL, database, etaService, appCache).
		WithEwayBillAdapter(ewbClient, cfg.EWayBill.ExtendEnabled)

	// Protected: Telemetry, and all /api/v1/* routes require a valid session or Bearer token
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAPIAuth(authStore, apiSecret, middleware.DefaultTenantResolver))
		r.With(featureGate("telemetry")).Group(func(r chi.Router) {
			telemetry.RegisterTelemetryRoutes(r, ingestor, database, time.Duration(cfg.LiveMap.TelemetryStaleMin)*time.Minute, etaService)
			telemetry.RegisterGeocodeRoute(r, cfg.LiveMap.NominatimURL)
		})
		r.Get("/api/v1/telemetry/stream", realtime.StreamHandler(sseHub, cfg.LiveMap.SSEEnabled))
		r.With(featureGate("pnl")).Group(func(r chi.Router) {
			pnl.RegisterRoutes(r, pnl.NewService(database), authSvc)
			if app.PNL != nil {
				app.PNL.RegisterRoutes(r)
			}
		})
		if app.OpsAlerts != nil {
			app.OpsAlerts.RegisterRoutes(r)
		}
		if app.ABExperiments != nil {
			r.With(featureGate("experiments")).Group(app.ABExperiments.RegisterRoutes)
		}
		if app.Founder != nil {
			r.With(featureGate("founder")).Group(app.Founder.RegisterRoutes)
		}
		// Ops error reports API (Spec 16 §4) — errors:read / errors:update
		// permissions seeded by migration 00083.
		r.With(middleware.ResourcePermission(authSvc, "errors", "read")).Get("/api/v1/errors", app.OpsErrors.APIList)
		r.With(middleware.ResourcePermission(authSvc, "errors", "read")).Get("/api/v1/errors/{fingerprint}", app.OpsErrors.APIGetError)
		r.With(middleware.ResourcePermission(authSvc, "errors", "read")).Get("/api/v1/errors/incidents", app.OpsErrors.APIListIncidents)
		r.With(middleware.ResourcePermission(authSvc, "errors", "update")).Post("/api/v1/errors/incidents/{incidentID}/resolve", app.OpsErrors.APIResolveIncident)
		// Client-side error capture (breadcrumbs + window.onerror reports).
		r.Post("/api/v1/errors/client", app.OpsErrors.APIClientReport)
		bookingAPIHandler.Register(r)
		tripAPIHandler.Register(r)
		invoiceAPIHandler.Register(r)
		paymentAPIHandler.Register(r)
		integrationHandler.Register(r)
		geofenceAPIHandler.Register(r)
		// Spec 18 Wave A — route optimization API (tenant-scoped, permission-gated)
		r.With(middleware.ResourcePermission(authSvc, "routes", "create")).Post("/api/v1/routes/optimize", app.Routes.Optimize)
		r.With(middleware.ResourcePermission(authSvc, "routes", "read")).Get("/api/v1/routes/optimize/jobs", app.Routes.OptimizeJobs)
		r.With(middleware.ResourcePermission(authSvc, "routes", "read")).Get("/api/v1/routes/optimize/jobs/{jobID}", app.Routes.OptimizeJobStatus)
		r.Get("/api/v1/hsn-sac/search", app.Invoices.SearchHSNSAC)
		r.Get("/api/v1/drivers/me", app.Drivers.GetMe)
		r.Post("/api/v1/drivers/me/status", app.Drivers.UpdateMyStatus)
		r.Get("/api/v1/drivers/me/issues", app.Drivers.ListMyIssues)
		r.Post("/api/v1/drivers/me/issues", app.Drivers.ReportIssue)
		r.Post("/api/v1/trips/{id}/deliver-pod", app.Kharcha.DeliverWithPOD)
		// Driver expense claims from mobile (Spec 13) — same trips:update
		// gate as the web /kharcha/create form.
		r.With(middleware.ResourcePermission(authSvc, "trips", "update")).Post("/api/v1/kharcha/expense", app.Kharcha.CreateExpenseAPI)
		r.Get("/api/v1/users/me/preferences", app.Users.GetMyPreferences)
		r.Patch("/api/v1/users/me/preferences", app.Users.UpdateMyPreferences)
		r.Post("/api/v1/users/me/preferences", app.Users.UpdateMyPreferences)
		if ragHandler != nil {
			r.With(featureGate("rag")).Group(ragHandler.RegisterRoutes)
		}
		// Spec 22 S1 — ranked alert inbox API (flag: ALERT_INBOX_ENABLED).
		alertInbox := handlers.NewAlertInboxHandlers(app.AlertsRepo)
		r.With(featureGate("alert_inbox")).Group(func(r chi.Router) {
			r.With(middleware.RequirePermission(authSvc, "alerts", "read")).Get("/api/alerts/inbox", alertInbox.List)
			r.With(middleware.RequirePermission(authSvc, "alerts", "write")).Post("/api/alerts/{id}/ack", alertInbox.Ack)
			r.With(middleware.RequirePermission(authSvc, "alerts", "write")).Post("/api/alerts/{id}/snooze", alertInbox.Snooze)
			r.With(middleware.RequirePermission(authSvc, "alerts", "write")).Post("/api/alerts/snooze-all", alertInbox.SnoozeAll)
		})
		// Spec 22 S2 — money strip API (flag: COMMAND_CENTER_ENABLED).
		// Spec 22 S2 — console money strip (flag: COMMAND_CENTER_ENABLED).
		// dashboard:read is self-heal seeded at startup (no migration in S2).
		r.With(featureGate("command_center"),
			middleware.RequirePermission(authSvc, "dashboard", "read")).
			Get("/api/dashboard/money-strip", consoleHandlers.MoneyStrip)
		// Spec 22 S3 — fleet strip + per-vehicle context panel.
		r.With(featureGate("command_center")).Group(func(r chi.Router) {
			r.With(middleware.RequirePermission(authSvc, "vehicles", "read")).Get("/api/fleet", consoleHandlers.Fleet)
			r.With(middleware.RequirePermission(authSvc, "vehicles", "read")).Get("/api/fleet/{vehicleId}/context", consoleHandlers.VehicleContext)
		})
		// Spec 22 S4 — one-tap EWB extend from the console context panel.
		r.With(middleware.RequirePermission(authSvc, "ewaybill", "write")).
			Post("/api/ewaybill/{id}/extend", consoleHandlers.ExtendEwayBill)
		// Spec 22 S6 — universal search API (result-scoped by permission,
		// tenant-scoped from context).
		r.Get("/api/search", app.SearchAPI)
	})

	// Deprecated v2 alias routes (rewrite to v1) plus /api/v2/health.
	// Aliased routes require the same API auth as v1; the public health check
	// is mounted separately so probes stay unauthenticated.
	apiversion.MountV2(r, middleware.RequireAPIAuth(authStore, apiSecret, middleware.DefaultTenantResolver), http.HandlerFunc(healthChecker.HealthHandler), bookingAPIHandler, tripAPIHandler, invoiceAPIHandler, paymentAPIHandler)

	// ── AI Agent: multi-agent orchestrator + RL learning + approvals ────
	if cfg.Agent.Enabled {
		client := agent.NewClient(cfg.Agent.APIKey, cfg.Agent.BaseURL, cfg.Agent.Model)
		if cfg.Agent.APIKey == "" {
			// Keyless mode: routes fall back to keywords and chats return a
			// clear "not configured" answer — the assistant stays reachable.
			logger.Warn("AGENT_ENABLED=true but AGENT_API_KEY not set; running keyless (keyword routing only)")
		}

		var rlSvc *rl.Service
		var err error
		if cfg.Agent.RLEnabled {
			rlSvc, err = rl.New(cfg.Agent.RLDBPath)
			if err != nil {
				logger.Error("agent RL store init failed; if AGENT_REQUIRE_APPROVAL=true the approval gate will fail CLOSED (mutating tools disabled)", "error", err)
				rlSvc = nil
			} else {
				logger.Info("agent RL enabled", "db", cfg.Agent.RLDBPath)
			}
		}
		if cfg.Agent.RequireApproval && rlSvc == nil {
			logger.Error("AGENT_REQUIRE_APPROVAL=true but the approval gate could not be built (RL store unavailable); mutating tools are disabled — the agent is read-only")
		}

		toolEnv := &agent.ToolEnv{Services: services}
		toolsByName := make(map[string]*agent.RegisteredTool)
		for _, t := range agent.RegisterTools(toolEnv) {
			toolsByName[t.Name] = t
		}

		if rlSvc != nil && cfg.Agent.RequireApproval {
			approvalSvc = agent.NewApprovalService(rlSvc, toolEnv)
			for _, name := range agent.MutatingTools() {
				if t, ok := toolsByName[name]; ok {
					approvalSvc.Gate(name, t.Handler)
				}
			}
			app.AgentAdmin = handlers.NewAgentAdminHandlers(app, approvalSvc)
			logger.Info("agent approval gate enabled", "tools", len(agent.MutatingTools()))
		}

		orch := agent.NewOrchestrator(client, toolEnv, rlSvc, cfg.Agent.MaxTurns)
		for _, sub := range agent.BuildAgentSet(toolsByName, approvalSvc, agent.AgentSetOptions{
			RequireApproval: cfg.Agent.RequireApproval,
			RagService:      ragHandler.Service(),
		}) {
			orch.AddAgent(sub)
		}
		agentAPI = agent.NewHandler(orch, toolEnv)
		logger.Info("AI agent enabled", "model", cfg.Agent.Model, "sub_agents", len(orch.AgentNames()), "approval_required", cfg.Agent.RequireApproval)

		// API routes (bearer/session) — approval queue + chat
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAPIAuth(authStore, apiSecret, middleware.DefaultTenantResolver))
			r.Use(agentRequestTimeout(5 * time.Minute))
			agentAPI.RegisterAPIRoute(r)
			if approvalSvc != nil {
				agent.NewApprovalHandler(approvalSvc).RegisterRoutes(r)
			}
		})
	}

	// Static files with Cache-Control headers
	fileServer := http.FileServer(http.Dir(cfg.StaticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If request has a version query param (?v=...), cache immutably since URL changes on deploy.
		// Otherwise, use short max-age with revalidation so updates take effect immediately.
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		}
		fileServer.ServeHTTP(w, r)
	})))

	// Uploaded files (logos, documents) - require authentication
	uploadsServer := http.FileServer(http.Dir(cfg.UploadDir))
	r.With(middleware.RequireAuth(authStore, middleware.DefaultTenantResolver)).Handle("/uploads/*", http.StripPrefix("/uploads/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		uploadsServer.ServeHTTP(w, r)
	})))

	// All application routes
	r.Group(func(r chi.Router) {

		// Public routes
		r.Get("/", app.Marketing)
		app.MountPWARoutes(r)
		r.Get("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("User-agent: *\nAllow: /\nSitemap: https://avandab.com/sitemap.xml\n"))
		})
		r.Get("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/xml")
			sitemap := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://avandab.com/</loc>
    <changefreq>daily</changefreq>
    <priority>1.0</priority>
  </url>
  <url>
    <loc>https://avandab.com/login</loc>
    <changefreq>monthly</changefreq>
    <priority>0.5</priority>
  </url>
  <url>
    <loc>https://avandab.com/register</loc>
    <changefreq>monthly</changefreq>
    <priority>0.5</priority>
  </url>
  <url>
    <loc>https://avandab.com/privacy</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/terms</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/refunds</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/dashboard</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/trips</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/routes</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/bookings</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/vehicles</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/drivers</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/customers</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/invoices</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/payments</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/reports</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/audit-logs</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/settings</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/users</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/company</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/kharcha</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
  <url>
    <loc>https://avandab.com/features/assistant</loc>
    <changefreq>monthly</changefreq>
    <priority>0.3</priority>
  </url>
</urlset>`
			_, _ = w.Write([]byte(sitemap))
		})
		r.Get("/login", app.Auth.LoginPage)
		r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/login", app.Auth.Login)
		r.Get("/register", app.Auth.RegisterPage)
		r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/register", app.Auth.Register)
		r.Get("/forgot-password", app.Auth.ForgotPasswordPage)
		r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/forgot-password", app.Auth.SubmitForgotPassword)
		r.Get("/reset-password", app.Auth.ResetPasswordPage)
		r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/reset-password", app.Auth.SubmitResetPassword)
		r.Post("/logout", app.Auth.Logout)

		// Public Contact & Status Tracking
		r.Route("/contact-us", app.Contact.Routes)

		// Legal & Policy Pages
		r.Get("/privacy", app.Privacy)
		r.Get("/terms", app.Terms)
		r.Get("/refunds", app.Refunds)

		// Public feature explainer pages (login-free)
		r.Get("/features/{slug}", app.FeaturePage)
		r.Get("/features", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
		})

		// Public live trip share links (Spec 04 §4) — login-free
		r.Group(func(r chi.Router) {
			r.Use(middleware.ContentSecurityPolicy(cfg.LiveMap.CSPEnabled))
			r.Get("/share", app.Share.HandleShareIndex)
			r.With(middleware.RateLimitDistributed(appCache, 20), middleware.NoCache).Get("/share/{token}", app.Share.ViewShare)
			r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/share/{token}/verify", app.Share.VerifyPIN)
			r.With(middleware.RateLimitDistributed(appCache, 30), middleware.NoCache).Get("/share/{token}/data", app.Share.ShareData)
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(authStore, middleware.DefaultTenantResolver))

			// Trip share links (Spec 04 §4)
			r.With(middleware.ResourcePermission(authSvc, "shares", "create")).Post("/trips/{id}/share", app.Share.CreateShare)
			r.With(middleware.ResourcePermission(authSvc, "shares", "read")).Get("/shares", app.Share.ListShares)
			r.With(middleware.ResourcePermission(authSvc, "shares", "revoke")).Post("/shares/{id}/revoke", app.Share.RevokeShare)

			// User Setup & Onboarding
			r.Get("/user/onboard", app.Auth.UserOnboardingPage)

			// Global cross-entity search (topbar)
			r.Get("/search", app.SearchPage)

			// Web UI language switch (English / हिन्दी) — cookie + redirect back
			r.Get("/lang", app.SetLang)

			// Dashboard
			r.Get("/dashboard", app.Dashboard.Index)
			r.Get("/dashboard/stream", app.Dashboard.Stream)
			r.Post("/dashboard/event", app.Dashboard.Event)
			r.Get("/files/{id}", app.DownloadFile)

			// Founder visibility layer web UI (Spec 16 §8) — gated to match
			// the JSON APIs (founder/ops_alerts/pnl/experiments read perms).
			if app.Founder != nil {
				r.With(featureGate("founder"), middleware.ResourcePermission(authSvc, "founder", "read")).Get("/founder/dashboard", app.Founder.DashboardPage)
				r.With(middleware.ResourcePermission(authSvc, "ops_alerts", "read")).Get("/ops-alerts", app.Founder.OpsAlertsPage)
				r.With(featureGate("pnl"), middleware.ResourcePermission(authSvc, "pnl", "read")).Get("/pnl/dashboard", app.Founder.PNLDashboardPage)
				r.With(featureGate("experiments"), middleware.ResourcePermission(authSvc, "experiments", "read")).Get("/experiments", app.Founder.ExperimentsPage)
			}

			// Live Fleet Map (Spec 12 §2.2, §4.3)
			// /map superseded by /tracking (FlyFleet live surveillance) — redirect stragglers.
			r.Get("/map", http.RedirectHandler("/tracking", http.StatusSeeOther).ServeHTTP)
			r.Get("/map/stream", app.Map.Stream)

			// Ops dashboard (errors & incidents, login audit) - Admin only
			r.With(middleware.RoleRequired(domain.DefaultRoleID(domain.RoleAdmin))).Get("/ops/dashboard", dashboardHandler.ServeHTTP)
			// Ops error triage page (Spec 16 §4) — gated by errors:read,
			// matching the sidebar link visibility (migration 00083 seeds it).
			r.With(middleware.ResourcePermission(authSvc, "errors", "read")).Get("/ops/errors", app.OpsErrors.Page)

			// Users (Admin only)
			r.Route("/users", app.Users.Routes)

			// Drivers
			r.Route("/drivers", app.Drivers.Routes)

			// Vehicles
			r.Route("/vehicles", app.Vehicles.Routes)

			// Customers
			r.Route("/customers", app.Customers.Routes)

			// Routes
			r.Route("/routes", app.Routes.Routes)

			// Bookings
			r.Route("/bookings", app.Bookings.Routes)

			// Trips
			r.Route("/trips", app.Trips.Routes)

			// Invoices
			r.Route("/invoices", app.Invoices.Routes)

			// Payments
			r.Route("/payments", app.Payments.Routes)

			// Reports
			r.Route("/reports", app.Reports.Routes)

			// Settings & Company Onboarding
			r.Route("/settings", app.SettingsH.Routes)
			r.Route("/company", app.SettingsH.Routes)

			// Audit Logs
			r.Route("/audit-logs", app.AuditLogs.Routes)

			// Telemetry device registry / provisioning / quarantine admin
			if cfg.Telemetry.Enabled {
				r.With(featureGate("telemetry")).Route("/telemetry/devices", app.TelemetryDevices.Routes)
				r.Route("/telemetry/quarantine", app.TelemetryDevices.QuarantineRoutes)
			}

			// Geofence CRUD + drawing UI (Spec 02 §8)
			r.With(featureGate("geofences")).Route("/geofences", app.Geofences.Routes)

			// Kharcha Ledger (driver expense approvals)
			r.Route("/kharcha", app.Kharcha.Routes)

			// Fuel claim audit queue + review (Spec 03 §6.1)
			r.With(featureGate("fuel_audit")).Route("/fuel", app.FuelAudit.Routes)

			// Driver scorecard leaderboard + fraud resolve (Spec 03 §6.1)
			r.With(featureGate("scorecard")).Route("/scorecard", app.Scorecard.Routes)

			// Live fleet tracking map (Spec 04 §1.3) & Preventive Maintenance (Spec 04 §6, §12) with opt-in CSP (Spec 04 §2)
			r.Group(func(r chi.Router) {
				r.Use(middleware.ContentSecurityPolicy(cfg.LiveMap.CSPEnabled))
				r.Route("/tracking", app.Tracking.Routes)
				r.Route("/maintenance", app.Maintenance.Routes)
			})

			// Operational alerts (Spec 05 §3)
			r.With(middleware.ResourcePermission(authSvc, "alerts", "read")).Route("/alerts", app.Alerts.Routes)

			// Spec 22 S5 — bookings kanban (flag: BOOKINGS_BOARD_ENABLED).
			r.With(featureGate("bookings_board"),
				middleware.ResourcePermission(authSvc, "bookings", "read")).
				Get("/bookings/board", app.Bookings.Board)

			// Owner Command Center (Spec 22 Steps 1-2) — ranked inbox +
			// money strip; fleet/map/context panel lands in Step 3.
			r.With(featureGate("command_center"), middleware.ResourcePermission(authSvc, "alerts", "read")).Get("/console", consoleHandlers.Page)

			// Compliance management and exemptions (Spec 05 §5, §11)
			if app.Compliance != nil {
				r.With(middleware.ResourcePermission(authSvc, "compliance", "read")).Route("/compliance", app.Compliance.Routes)
			}

			// E-Way Bill & FASTag routes (Spec 07)
			if app.EWayBill != nil {
				r.With(featureGate("ewaybill")).Group(app.EWayBill.Mount)
			}
			if app.FASTag != nil {
				r.With(featureGate("fastag")).Group(app.FASTag.Mount)
			}
			if app.Accounting != nil {
				r.With(featureGate("accounting_sync")).Group(app.Accounting.Mount)
			}
			if app.Settlements != nil {
				app.Settlements.Mount(r)
			}
			if app.Documents != nil {
				app.Documents.Mount(r)
			}
			if app.FilesAPI != nil {
				app.FilesAPI.Mount(r)
			}
			if app.Compliance != nil {
				app.Compliance.Mount(r)
			}

			// AI Operations Assistant
			r.With(featureGate("agent")).Route("/assistant", app.Assistant.Routes)
			if agentAPI != nil {
				r.Group(func(r chi.Router) {
					r.Use(agentRequestTimeout(5 * time.Minute))
					agentAPI.RegisterRoutes(r)
				})
			}

			// Agent approval queue (admin page)
			if approvalSvc != nil {
				r.Route("/agent-actions", app.AgentAdmin.Routes)
			}

			// e-POD delivery from driver mobile
			r.Post("/trips/{id}/deliver-pod", app.Kharcha.DeliverWithPOD)

			// Shipper portal (Spec 21 §2.3) — customer-scoped bookings/invoices/tracking + feedback
			customerPortal := handlers.NewCustomerPortalHandlers(app)
			r.With(middleware.ResourcePermission(authSvc, "customer_portal", "read")).Get("/customer/bookings", customerPortal.ListMyBookings)
			r.With(middleware.ResourcePermission(authSvc, "customer_portal", "read")).Get("/customer/invoices", customerPortal.ListMyInvoices)
			r.With(middleware.ResourcePermission(authSvc, "customer_portal", "read")).Get("/customer/tracking/{trip_id}", customerPortal.Tracking)
			r.With(middleware.ResourcePermission(authSvc, "customer_portal", "write")).Post("/customer/feedback", customerPortal.Feedback)

			// Profile (auth)
			r.Get("/profile", app.Auth.ProfilePage)
			r.Post("/profile", app.Auth.UpdateProfile)
			r.Get("/change-password", app.Auth.ChangePasswordPage)
			// Rate-limited: prevents unlimited old-password guessing inside
			// an active session window.
			r.With(middleware.RateLimitDistributed(appCache, 10)).Post("/change-password", app.Auth.ChangePassword)
		})
	})

	// Start server with graceful shutdown
	addr := fmt.Sprintf(":%s", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ── Background worker leadership ─────────────────────────────────────
	// Every cron/sweeper below runs on exactly one replica when
	// WORKER_LEADER_LOCK=true (default): a DB lease table elects a leader.
	// Single-instance deployments are unaffected — the claim is trivial.
	// sseHub.Run stays per-replica: SSE fan-out must live where the
	// connections are.
	var runLeadered func(name string, fn func(context.Context))
	if cfg.WorkerLeaderLock {
		leaderMgr := leader.NewManager(database, "", 0, logger)
		runLeadered = func(name string, fn func(context.Context)) { go leaderMgr.RunAsLeader(ctx, name, fn) }
	} else {
		logger.Warn("WORKER_LEADER_LOCK=false: background workers will duplicate across replicas")
		runLeadered = func(name string, fn func(context.Context)) { go fn(ctx) }
	}

	// SQLite WAL checkpoint hygiene: the WAL grew unbounded (>500MB observed)
	// without periodic truncation. Hourly TRUNCATE checkpoints cap it. Only
	// meaningful for the sqlite driver; skipped for network engines.
	// Uses resilience wrapper: TRUNCATE needs exclusive lock — low-load
	// collisions ("table is locked (6)") are retried, then PASSIVE fallback.
	if cfg.Database.Driver == "" || cfg.Database.Driver == "sqlite" {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Pattern for future services: wrap any fallible op with resilience.DoVoid
					// — it handles retry + panic recovery + context cancellation.
					err := resilience.DoVoid(ctx, resilience.Config{
						MaxAttempts:  3,
						InitialDelay: 100 * time.Millisecond,
						MaxDelay:     300 * time.Millisecond,
						Logger:       logger,
					}, func(ctx context.Context) error {
						_, err := database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);")
						return err
					})
					if err != nil {
						// TRUNCATE failed after retries — PASSIVE never blocks writers.
						if pErr := resilience.DoVoid(ctx, resilience.Config{
							MaxAttempts:  1,
							InitialDelay: 50 * time.Millisecond,
							Logger:       logger,
						}, func(ctx context.Context) error {
							_, e := database.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE);")
							return e
						}); pErr != nil {
							logger.Debug("WAL checkpoint PASSIVE skipped", "error", pErr, "truncate_err", err)
						} else if resilience.IsRetryable(err) {
							logger.Debug("WAL checkpoint TRUNCATE busy, PASSIVE ok", "error", err)
						} else {
							logger.Warn("WAL checkpoint failed", "error", err)
						}
					}
				}
			}
		}()
	}

	// ── Outbox relay & founder notifications ──────────────────────────
	// NOTE: eventBus is the SAME instance injected into services above.
	founderSvc := founder.NewFounderService(newFounderNotifier(logger))
	founderSvc.RegisterEventHandlers(eventBus)
	if founderConfigured() {
		runLeadered("founder_digest", func(ctx context.Context) {
			if !featureTick("founder") {
				return
			}
			runDailyDigest(ctx, founderSvc, logger)
		})
	}
	outboxRelay := outbox.NewRelay(database, eventBus, logger)
	runLeadered("outbox_relay", outboxRelay.Run)
	go sseHub.Run(ctx) // per-replica: SSE fan-out must live where connections are

	if dwellWorker != nil {
		runLeadered("geofence_dwell", func(ctx context.Context) {
			if !featureTick("geofences") {
				return
			}
			dwellWorker.Run(ctx)
		})
	}
	if fuelEngine != nil {
		// Incremental scorecard trigger (Spec 03 §4.3): after each engine
		// pass that wrote behaviour events, recompute the affected driver.
		if services.Scorecard != nil {
			fuelEngine.WithBehaviourHook(func(ctx context.Context, driverID string) {
				if _, err := services.Scorecard.RecomputeDriverScore(ctx, driverID); err != nil {
					logger.Error("scorecard incremental recompute failed", "driver_id", driverID, "error", err)
				}
			})
		}
		if services.OpsAlerts != nil {
			fuelEngine.WithSiphonHook(func(ctx context.Context, vehicleID, tripID, driverID string, drop float64, stopMinutes int) {
				_, err := services.OpsAlerts.CreateAlert(ctx, service.OpsAlert{
					TenantID:    string(shared.DefaultTenant),
					AlertType:   service.OpsAlertFuelTheftConfirmed,
					Severity:    service.OpsAlertSeverityCritical,
					Title:       "Fuel siphoning confirmed",
					Description: fmt.Sprintf("Vehicle %s lost %.1fL during %d min stop", vehicleID, drop, stopMinutes),
					EntityType:  service.StrPtr("vehicle"),
					EntityID:    &vehicleID,
				})
				if err != nil {
					logger.Error("failed to create ops alert for fuel siphon", "vehicle_id", vehicleID, "error", err)
				}
			})
		}
		runLeadered("fuel_engine", func(ctx context.Context) {
			if !featureTick("fuel_audit") {
				return
			}
			fuelEngine.Run(ctx)
		})
	}

	if safetyEngine != nil {
		if services.Scorecard != nil {
			safetyEngine.WithBehaviourHook(func(ctx context.Context, driverID string) {
				if _, err := services.Scorecard.RecomputeDriverScore(ctx, driverID); err != nil {
					logger.Error("scorecard incremental recompute (safety) failed", "driver_id", driverID, "error", err)
				}
			})
		}
		runLeadered("safety_engine", func(ctx context.Context) {
			if !featureTick("scorecard") {
				return
			}
			safetyEngine.Run(ctx)
		})
	}

	// Nightly scorecard sweep (Spec 03 §4.3): recompute every driver with
	// behaviour events in the window so decayed scores stay fresh even when
	// no new engine events arrive.
	if services.Scorecard != nil {
		runLeadered("scorecard_sweep", func(ctx context.Context) {
			if !featureTick("scorecard") {
				return
			}
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			if err := services.Scorecard.RecomputeAllDrivers(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("scorecard nightly sweep failed", "error", err)
			}
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := services.Scorecard.RecomputeAllDrivers(ctx); err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("scorecard nightly sweep failed", "error", err)
					}
				}
			}
		})
	}

	// Nightly PNL daily snapshot (Spec 16 §2): runs every 24 h, first fires
	// ~1 minute after startup then on a 24 h ticker. Snapshots yesterday for
	// all active tenants. Idempotent — safe to re-run.
	if services.PNL != nil {
		runLeadered("pnl_snapshot", func(ctx context.Context) {
			if !featureTick("pnl") {
				return
			}
			// Fire once shortly after boot (catches a missed cron on restarts).
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Minute):
			}
			runPNLSnapshot := func() {
				yesterday := time.Now().AddDate(0, 0, -1)
				tenantIDs, err := service.GetActiveTenantIDs(ctx, database)
				if err != nil {
					logger.Error("PNL: failed to list tenants", "error", err)
					return
				}
				for _, tid := range tenantIDs {
					if _, err := services.PNL.GenerateDailySnapshot(ctx, tid, yesterday); err != nil && !errors.Is(err, context.Canceled) {
						logger.Error("PNL daily snapshot failed", "tenant", tid, "date", yesterday.Format("2006-01-02"), "error", err)
					} else {
						logger.Info("PNL daily snapshot ok", "tenant", tid, "date", yesterday.Format("2006-01-02"))
					}
				}
			}
			runPNLSnapshot()
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runPNLSnapshot()
				}
			}
		})
	}

	// Fuel claim audit pass (Spec 03 §3.2 step 2): runs on its own 5-minute
	// ticker rather than inside the engine tick so the audit service stays
	// independent of the anomaly engine (and avoids an internal/fuel →
	// internal/service import cycle).
	runLeadered("fuel_audit_pass", func(ctx context.Context) {
		if !featureTick("fuel_audit") {
			return
		}
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		if _, err := services.FuelAudit.AuditPendingClaims(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("fuel audit pass failed", "error", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := services.FuelAudit.AuditPendingClaims(ctx); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("fuel audit pass failed", "error", err)
				}
			}
		}
	})

	// Preventive maintenance worker (Spec 04 §6, §9)
	if cfg.LiveMap.PMEnabled {
		maintWorker := maintenance.NewWorker(database, eventBus, logger, cfg.LiveMap.PMCheckIntervalMin, cfg.LiveMap.PMCriticalDTCs)
		if app.Maintenance != nil {
			app.Maintenance.SetWorker(maintWorker)
		}
		runLeadered("preventive_maintenance", maintWorker.Run)
	}

	// Operational alerts escalation and storm batch flusher (Spec 05 §4)
	runLeadered("alerts_escalator", alertEscalator.Run)
	runLeadered("alerts_flusher", alertFlusher.Run)

	// Spec 22 S1 — reopen snoozed inbox alerts whose snooze expired.
	if app.AlertsRepo != nil {
		inboxRepo := app.AlertsRepo
		runLeadered("alerts_snooze_sweep", func(c context.Context) {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-c.Done():
					return
				case <-ticker.C:
					if !featureTick("alert_inbox") {
						continue
					}
					n, err := inboxRepo.ReopenExpiredSnoozes(c, time.Now().UTC())
					if err != nil {
						logger.Error("snooze sweep failed", "error", err)
					} else if n > 0 {
						logger.Info("snooze sweep reopened alerts", "count", n)
					}
				}
			}
		})
	}

	// E-Way Bill expiry monitor (Spec 07 §2.8)
	ewbMonitor := ewaybill.NewMonitor(ewbService, ewbCfg)
	runLeadered("ewaybill_monitor", ewbMonitor.Run)

	// ETA history cleanup + monthly aggregation crons (Spec 18 Wave A — 90-day retention)
	runLeadered("eta_cleanup", func(c context.Context) { etaService.RunCleanupCron(c, 24*time.Hour, logger) })
	runLeadered("eta_aggregation", func(c context.Context) { etaService.RunAggregationCron(c, 24*time.Hour, logger) })

	go func() {
		logger.Info("Server listening", "address", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Graceful shutdown failed", "error", err)
	}
}

// bootstrapAdmin creates the initial admin account from environment config.
// It runs only when BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD are
// set, no admin account exists yet, and the password meets the policy.
// Admin provisioning otherwise happens through the authenticated user
// management interface.
func bootstrapAdmin(ctx context.Context, services *service.Services, authSvc auth.AuthorizationService, cfg *config.Config, logger *slog.Logger) {
	ba := cfg.BootstrapAdmin
	if ba.Email == "" || ba.Password == "" {
		logger.Info("bootstrap admin skipped: BOOTSTRAP_ADMIN_EMAIL / BOOTSTRAP_ADMIN_PASSWORD not set")
		return
	}

	users, _, err := services.Users.ListUsers(ctx, "", "", 100, 0)
	if err != nil {
		logger.Error("bootstrap admin failed: cannot list users", "error", err)
		return
	}
	for _, u := range users {
		if u.RoleID == 1 {
			logger.Info("bootstrap admin skipped: an admin account already exists")
			return
		}
	}

	user, err := services.Users.CreateUserWithPassword(ctx, ba.Email, ba.Name, "", ba.Password, 1, domain.UserStatusActive)
	if err != nil {
		logger.Error("bootstrap admin failed", "error", err)
		return
	}
	if err := authSvc.AddRoleForUser(user.ID.String(), "admin"); err != nil {
		logger.Warn("bootstrap admin created but RBAC role assignment failed", "error", err)
	}
	logger.Info("bootstrap admin created", "email", ba.Email)
}

// noopNotifier drops alerts; used when Telegram is not configured.
type noopNotifier struct{}

func (noopNotifier) SendAlert(founderAlerts.AlertEvent) error { return nil }

// founderConfigured reports whether Telegram founder alerting is configured.
func founderConfigured() bool {
	return os.Getenv("FOUNDER_TELEGRAM_BOT_TOKEN") != "" && os.Getenv("FOUNDER_TELEGRAM_CHAT_ID") != ""
}

// newFounderNotifier builds the Telegram notifier from env config,
// falling back to a noop notifier when Telegram is not configured.
func newFounderNotifier(logger *slog.Logger) founder.Notifier {
	token := os.Getenv("FOUNDER_TELEGRAM_BOT_TOKEN")
	chatID, err := strconv.ParseInt(os.Getenv("FOUNDER_TELEGRAM_CHAT_ID"), 10, 64)
	if token == "" || err != nil || chatID == 0 {
		if token != "" {
			logger.Warn("founder telegram notifier disabled: invalid FOUNDER_TELEGRAM_CHAT_ID")
		}
		return noopNotifier{}
	}
	bot, err := telebot.NewBot(telebot.Settings{Token: token})
	if err != nil {
		logger.Warn("founder telegram notifier unavailable", "error", err)
		return noopNotifier{}
	}
	logger.Info("founder telegram notifier enabled")
	return founderAlerts.NewTelegramBotNotifier(bot, chatID)
}

// runDailyDigest sends a daily founder report at FOUNDER_DIGEST_HOUR
// (default 9, UTC) until ctx is cancelled. Report metrics are zero-valued
// until a data source exists; the notification service is wired for
// future population.
func runDailyDigest(ctx context.Context, svc *founder.FounderService, logger *slog.Logger) {
	hour := 9
	if v := os.Getenv("FOUNDER_DIGEST_HOUR"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 && h < 24 {
			hour = h
		}
	}
	logger.Info("daily founder digest scheduled", "hour", hour)
	for {
		next := time.Now().Truncate(24 * time.Hour).Add(time.Duration(hour) * time.Hour)
		if !next.After(time.Now()) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := svc.SendDailyDigest(digest.DailyDigestReport{Date: time.Now()}); err != nil {
				logger.Error("daily founder digest failed", "error", err)
			}
		}
	}
}

// seedScorecardUpdatePermission creates the scorecard:update permission and
// grants it to the admin role when missing, then reloads the Casbin enforcer.
// Migration 00043 seeded only scorecard:read; the resolve route (Spec 03
// §6.1) needs the update permission, and editing that migration is forbidden
// by the Migration Ownership Index — so this runs at startup instead.
func seedScorecardUpdatePermission(ctx context.Context, db *sql.DB, authSvc auth.AuthorizationService) error {
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO permissions (name, description)
		 VALUES ('scorecard:update', 'Resolve scorecard fraud-cap events')`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
		 SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
		 WHERE r.name = 'admin' AND p.name = 'scorecard:update'`); err != nil {
		return err
	}
	return authSvc.Reload()
}

// seedDashboardReadPermission ensures the dashboard:read permission exists
// (Spec 22 §2.2 money-strip gate). Step 2 adds no migration, so this runs
// idempotently at startup; admins are granted by default.
func seedDashboardReadPermission(ctx context.Context, db *sql.DB, authSvc auth.AuthorizationService) error {
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO permissions (name, description)
		 VALUES ('dashboard:read', 'View console money strip and dashboard metrics')`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
		 SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
		 WHERE r.name = 'admin' AND p.name = 'dashboard:read'`); err != nil {
		return err
	}
	return authSvc.Reload()
}

// seedEwaybillWritePermission ensures the ewaybill:write permission exists
// (Spec 22 §2.3 console extend gate). Idempotent startup self-heal; admins
// are granted by default.
func seedEwaybillWritePermission(ctx context.Context, db *sql.DB, authSvc auth.AuthorizationService) error {
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO permissions (name, description)
		 VALUES ('ewaybill:write', 'Extend and manage e-way bills from the console')`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
		 SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
		 WHERE r.name = 'admin' AND p.name = 'ewaybill:write'`); err != nil {
		return err
	}
	return authSvc.Reload()
}
