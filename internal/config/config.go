package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// RoutingConfig holds route optimization provider settings (Spec 18 Wave A).
type RoutingConfig struct {
	Provider string // mock | osrm-public | osrm-selfhost | http://...
	OSRMURL  string // override for self-host
}

// DatabaseConfig selects the persistence backend. Switching engines is an
// env-only change: set DATABASE_DRIVER plus DATABASE_URL (and pool sizing)
// — no code edits required. Drivers are registered by internal/database.
type DatabaseConfig struct {
	Driver          string        // sqlite (default) | postgres | mysql
	URL             string        // engine-specific DSN
	MaxOpenConns    int           // 0 = engine default; sqlite default 64
	MaxIdleConns    int           // negative = Go default; sqlite default 32
	ConnMaxLifetime time.Duration // 0 = reuse forever
}

func (c *DatabaseConfig) GetDriver() string                 { return c.Driver }
func (c *DatabaseConfig) GetURL() string                    { return c.URL }
func (c *DatabaseConfig) GetMaxOpenConns() int              { return c.MaxOpenConns }
func (c *DatabaseConfig) GetMaxIdleConns() int              { return c.MaxIdleConns }
func (c *DatabaseConfig) GetConnMaxLifetime() time.Duration { return c.ConnMaxLifetime }

// CacheConfig selects the cache backend used for hot reads (sessions,
// lookups, rate counters). Switching drivers is env-only, same as the DB.
// none → no-op cache; memory → in-process TTL cache; redis → shared cluster.
type CacheConfig struct {
	Driver        string // none (default) | memory | redis
	RedisAddr     string // host:port when Driver=redis
	RedisPassword string
	RedisDB       int
	DefaultTTL    time.Duration // applied when Set is called without a TTL
	KeyPrefix     string        // namespace prefix for all keys
}

func (c *CacheConfig) GetDriver() string            { return c.Driver }
func (c *CacheConfig) GetRedisAddr() string         { return c.RedisAddr }
func (c *CacheConfig) GetRedisPassword() string     { return c.RedisPassword }
func (c *CacheConfig) GetRedisDB() int              { return c.RedisDB }
func (c *CacheConfig) GetDefaultTTL() time.Duration { return c.DefaultTTL }
func (c *CacheConfig) GetKeyPrefix() string         { return c.KeyPrefix }

// StorageConfig selects where uploads/documents live. local keeps files on
// disk under LocalDir; s3 is reserved for object-storage wiring.
type StorageConfig struct {
	Driver   string // local (default) | s3
	LocalDir string
}

func (c *StorageConfig) GetDriver() string   { return c.Driver }
func (c *StorageConfig) GetLocalDir() string { return c.LocalDir }

// Config holds all application configuration.
type Config struct {
	AppEnv               string
	Port                 string
	DatabaseURL          string // effective DSN; mirrors Database.URL for callers that only need the string
	Database             DatabaseConfig
	CookieSecret         string
	APITokenSecret       string
	SessionMaxAge        time.Duration
	CookieSecure         bool
	LogLevel             string
	UploadDir            string
	StaticDir            string
	MaxUploadSize        int64
	ExportMaxRows        int
	DashboardSSEEnabled  bool
	DashboardSSEInterval time.Duration
	PWAEnabled           bool
	RazorpayKeyID        string
	RazorpayKeySecret    string
	RazorpayWebhook      string
	OCRProvider          string // mock | http (Spec 22 §6)
	OCRHTTPURL           string
	OCRHTTPKey           string
	BootstrapAdmin       BootstrapAdminConfig
	MultiTenant          MultiTenantConfig
	RAG                  RAGConfig
	Agent                AgentConfig
	Experiment           ExperimentConfig
	Telemetry            TelemetryConfig
	LiveMap              LiveMapConfig
	Alerts               AlertConfig
	EWayBill             EWayBillConfig
	GSTN                 GSTNConfig
	FASTag               FASTagConfig
	Routing              RoutingConfig
	Cache                CacheConfig
	Storage              StorageConfig
	WorkerLeaderLock     bool
	Notify               NotifyConfig
	Comm                 CommConfig
	Google               GoogleConfig
	FCM                  FCMConfig
}

// CommConfig controls the durable outbound queue (comm_outbox, migration
// 00118). The worker delivers email through the SMTP adapter (Phase 2);
// WhatsApp consumes the same queue in Phase 3. Disabled only via
// COMM_OUTBOX_WORKER=false; an unconfigured SMTP relay makes rows fail
// honestly and dead-letter instead of vanishing (no fake successes).
type CommConfig struct {
	OutboxWorker bool
}

// GoogleOAuthConfig holds "Sign in with Google" OAuth settings. An empty
// ClientID keeps the feature disabled: the /auth/google routes redirect to
// /login with a flash message and the UI button is hidden. Zero-cost tier —
// no SDK dependency, stdlib-only OAuth2 code flow (internal/auth/oauth.go).
type GoogleOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Enabled reports whether Google OAuth is configured.
func (c *GoogleOAuthConfig) Enabled() bool {
	return c != nil && c.ClientID != ""
}

// GoogleConfig is an alias for backwards compatibility.
type GoogleConfig = GoogleOAuthConfig

// NotifyConfig holds outbound delivery channel settings. Empty values keep a
// channel unconfigured — sends then fail honestly instead of faking success.
type NotifyConfig struct {
	SMTPHost        string
	SMTPPort        string
	SMTPUser        string
	SMTPPassword    string
	SMTPFrom        string
	SMTPDirect      bool
	SMSWebhookURL   string
	SMSWebhookToken string
	// Email pool — dynamic multi-provider failover with quota tracking.
	EmailPoolEnabled   bool
	EmailPoolStrategy  string
	EmailProvidersJSON string
}

// FCMConfig holds Firebase Cloud Messaging settings for driver push notifications.
type FCMConfig struct {
	ProjectID          string
	ServerKey          string
	ServiceAccountJSON string
	Endpoint           string
}

// GSTNConfig holds configuration for GSTN / GSP / E-Invoicing (Spec 07).
type GSTNConfig struct {
	UseMock      bool
	Username     string
	Password     string
	ClientID     string
	ClientSecret string
}

// FASTagConfig holds configuration for FASTag NETC (Spec 21 §6).
type FASTagConfig struct {
	UseMock  bool
	APIKey   string
	Enabled  bool
	Endpoint string
}

// EWayBillConfig holds configuration for the E-Way Bill lifecycle worker (Spec 05 §7, Spec 07).
type EWayBillConfig struct {
	WorkerEnabled        bool
	WorkerInterval       time.Duration
	ExtensionKM          float64
	ExtensionLeadSeconds int
	MinInvoiceValue      float64
	// ExtendEnabled gates real provider extend calls from the console
	// one-tap action (Spec 22 §6). When false (default) the console
	// endpoint shifts expiry locally — no external call is made.
	ExtendEnabled bool
}

// AlertConfig holds configuration for operational alerts (Spec 05 §14).
type AlertConfig struct {
	TelegramBotToken string
	TelegramChatID   string
	WhatsAppProvider string // mock | gupshup | meta | evolution | webhook (Spec 22 §6)
	WhatsAppAPIKey   string // gupshup / evolution / webhook
	WhatsAppToken    string // meta cloud API / webhook
	WhatsAppPhoneID  string // meta cloud API phone number id
	WhatsAppURL      string // evolution / generic webhook
	WhatsAppInstance string // evolution instance name
}

// ExperimentConfig configures the server-side A/B experiment framework.
type ExperimentConfig struct {
	// Rollout is the percentage (0-100) of users assigned the treatment
	// variant of an experiment. 0 = control only, 100 = treatment only.
	Rollout int
	// ForceVariant overrides assignment for every request (QA/testing).
	// Empty means no override.
	ForceVariant string
}

// AgentConfig holds configuration for the AI operations assistant.
type AgentConfig struct {
	Enabled         bool
	APIKey          string
	BaseURL         string
	Model           string
	MaxTurns        int
	SystemPrompt    string
	RLEnabled       bool
	RLDBPath        string
	RequireApproval bool
}

// RAGConfig holds configuration for the codebase RAG system.
type RAGConfig struct {
	Enabled          bool
	EmbeddingAPIKey  string
	EmbeddingBaseURL string
	EmbeddingModel   string
	ChunkSize        int
	ChunkOverlap     int
	IndexDirs        []string
	VectorDBPath     string
}

// TelemetryConfig holds configuration for the telemetry ingestion pipeline
// (Phase 1: Specs 01 §8, 17 §3).
type TelemetryConfig struct {
	Enabled                 bool
	WebhookSecretLocoNav    string
	WebhookSecretWheelsEye  string
	WheelsEyeAccessToken    string
	WheelsEyePollInterval   time.Duration
	DeviceSecretPepper      string
	WebhookRateLimit        int
	RawRetentionDays        int
	BatchSize               int
	FlushInterval           time.Duration
	OdometerMaxRegressionKM float64
	FuelClampDeltaPct       float64
}

// LiveMapConfig holds configuration for the live map + share links + ETA +
// preventive-maintenance stack (Spec 04 §9).
type LiveMapConfig struct {
	MapTileProvider       string // google | osm | auto (google → OSM on tileerror)
	MapGoogleStyle        string // m=roadmap, s=satellite, y=hybrid, p=terrain
	MapGL                 string // Google tile country bias
	MapOSMURL             string // OSM fallback tile template
	NominatimURL          string // Geocoding base
	MapPollSec            int    // Tracking page REST poll interval
	CSPEnabled            bool
	ShareLinkTTLHours     int
	ShareLinkMaxTTLHours  int
	ShareLinkMaxActive    int
	EtaStaleMin           int
	EtaWindowMin          int
	EtaGuardMaxRegressMin int
	TelemetryStaleMin     int
	SSEEnabled            bool
	SSEKeepaliveSec       int
	PMEnabled             bool
	PMCheckIntervalMin    int
	PMCriticalDTCs        string
}

// BootstrapAdminConfig configures the initial admin account created at
// startup when no admin exists yet. Intentionally not set by default.
type BootstrapAdminConfig struct {
	Email    string
	Name     string
	Password string
}

// MultiTenantConfig gates per-user tenant resolution (Spec 24). Disabled
// (default) keeps the single-org bootstrap tenant for every request; enabled
// resolves each user's tenant from users.tenant_id and rejects suspended orgs.
type MultiTenantConfig struct {
	Enabled bool
}

// Load reads configuration from environment variables.
func Load() *Config {
	maxUpload := int64(10 << 20) // 10 MB default
	if v := os.Getenv("MAX_UPLOAD_SIZE"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.Error("invalid MAX_UPLOAD_SIZE", "value", v, "error", err)
		} else {
			maxUpload = parsed << 20
		}
	}

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	sessionMaxAge := 24 * time.Hour
	if v := os.Getenv("SESSION_MAX_AGE"); v != "" {
		if hours, err := strconv.Atoi(v); err == nil {
			sessionMaxAge = time.Duration(hours) * time.Hour
		}
	}

	cookieSecure := env == "production"
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		cookieSecure = v == "true" || v == "1"
	}

	// Database: driver defaults to sqlite with the legacy file DSN. Setting
	// DATABASE_DRIVER=postgres|mysql plus a matching DATABASE_URL switches
	// engines without touching code (pool knobs are optional).
	dbURL := getEnv("DATABASE_URL", "file:transport.db?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL")
	dbDriver := strings.ToLower(getEnv("DATABASE_DRIVER", "sqlite"))
	maxOpenConns := 0
	if v := os.Getenv("DB_MAX_OPEN_CONNS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			maxOpenConns = parsed
		} else {
			slog.Error("invalid DB_MAX_OPEN_CONNS", "value", v)
		}
	}
	maxIdleConns := -1
	if v := os.Getenv("DB_MAX_IDLE_CONNS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			maxIdleConns = parsed
		} else {
			slog.Error("invalid DB_MAX_IDLE_CONNS", "value", v)
		}
	}

	cfg := &Config{
		AppEnv:      env,
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: dbURL,
		Database: DatabaseConfig{
			Driver:          dbDriver,
			URL:             dbURL,
			MaxOpenConns:    maxOpenConns,
			MaxIdleConns:    maxIdleConns,
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 15*time.Minute),
		},
		CookieSecret:         getEnv("COOKIE_SECRET", "dev-secret-key-change-in-production-32b!"),
		APITokenSecret:       getEnv("API_SECRET", ""),
		SessionMaxAge:        sessionMaxAge,
		CookieSecure:         cookieSecure,
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		UploadDir:            getEnv("UPLOAD_DIR", "./uploads"),
		StaticDir:            getEnv("STATIC_DIR", "internal/static"),
		MaxUploadSize:        maxUpload,
		ExportMaxRows:        getEnvInt("EXPORT_MAX_ROWS", 50000),
		DashboardSSEEnabled:  getEnvBool("DASHBOARD_SSE_ENABLED", true),
		DashboardSSEInterval: time.Duration(getEnvInt("DASHBOARD_SSE_INTERVAL_SEC", 5)) * time.Second,
		PWAEnabled:           getEnvBool("PWA_ENABLED", false),
		RazorpayKeyID:        getEnv("RAZORPAY_KEY_ID", ""),
		RazorpayKeySecret:    getEnv("RAZORPAY_KEY_SECRET", ""),
		RazorpayWebhook:      os.Getenv("RAZORPAY_WEBHOOK_SECRET"),
		OCRProvider:          getEnv("OCR_PROVIDER", "mock"),
		OCRHTTPURL:           getEnv("OCR_HTTP_URL", ""),
		OCRHTTPKey:           getEnv("OCR_HTTP_KEY", ""),
		BootstrapAdmin: BootstrapAdminConfig{
			Email:    os.Getenv("BOOTSTRAP_ADMIN_EMAIL"),
			Name:     getEnv("BOOTSTRAP_ADMIN_NAME", "Admin"),
			Password: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
		},
		MultiTenant: MultiTenantConfig{
			Enabled: getEnv("MULTI_TENANT_ENABLED", "false") == "true",
		},
		RAG: RAGConfig{
			Enabled:          getEnv("RAG_ENABLED", "false") == "true",
			EmbeddingAPIKey:  os.Getenv("RAG_EMBEDDING_API_KEY"),
			EmbeddingBaseURL: getEnv("RAG_EMBEDDING_BASE_URL", "https://api.openai.com/v1"),
			EmbeddingModel:   getEnv("RAG_EMBEDDING_MODEL", "text-embedding-3-small"),
			ChunkSize:        getEnvInt("RAG_CHUNK_SIZE", 512),
			ChunkOverlap:     getEnvInt("RAG_CHUNK_OVERLAP", 50),
			VectorDBPath:     getEnv("RAG_VECTOR_DB_PATH", "./rag_vectors.db"),
		},
	}

	// Parse RAG index directories from comma-separated env var
	if dirs := os.Getenv("RAG_INDEX_DIRS"); dirs != "" {
		cfg.RAG.IndexDirs = strings.Split(dirs, ",")
		for i := range cfg.RAG.IndexDirs {
			cfg.RAG.IndexDirs[i] = strings.TrimSpace(cfg.RAG.IndexDirs[i])
		}
	}

	cfg.Agent = AgentConfig{
		Enabled:         getEnv("AGENT_ENABLED", "false") == "true",
		APIKey:          os.Getenv("AGENT_API_KEY"),
		BaseURL:         getEnv("AGENT_BASE_URL", "https://api.openai.com/v1"),
		Model:           getEnv("AGENT_MODEL", "gpt-4o-mini"),
		MaxTurns:        getEnvInt("AGENT_MAX_TURNS", 10),
		RLEnabled:       getEnv("AGENT_RL_ENABLED", "true") == "true",
		RLDBPath:        getEnv("AGENT_RL_DB_PATH", "agent_rl.db"),
		RequireApproval: getEnv("AGENT_REQUIRE_APPROVAL", "true") == "true",
	}

	cfg.Experiment = ExperimentConfig{
		Rollout:      getEnvInt("EXPERIMENT_ROLLOUT", 100),
		ForceVariant: getEnv("EXPERIMENT_FORCE_VARIANT", ""),
	}

	cfg.Telemetry = TelemetryConfig{
		Enabled:                 getEnv("TELEMETRY_ENABLED", "true") == "true",
		WebhookSecretLocoNav:    os.Getenv("TELEMETRY_WEBHOOK_SECRET_LOCONAV"),
		WebhookSecretWheelsEye:  os.Getenv("TELEMETRY_WEBHOOK_SECRET_WHEELSEYE"),
		WheelsEyeAccessToken:    os.Getenv("TELEMETRY_WHEELSEYE_ACCESS_TOKEN"),
		WheelsEyePollInterval:   getEnvDuration("TELEMETRY_WHEELSEYE_POLL_INTERVAL", 5*time.Minute),
		DeviceSecretPepper:      os.Getenv("TELEMETRY_DEVICE_SECRET_PEPPER"),
		WebhookRateLimit:        getEnvInt("TELEMETRY_WEBHOOK_RATE_LIMIT", 30),
		RawRetentionDays:        getEnvInt("TELEMETRY_RAW_RETENTION_DAYS", 30),
		BatchSize:               getEnvInt("TELEMETRY_BATCH_SIZE", 500),
		FlushInterval:           getEnvDuration("TELEMETRY_FLUSH_INTERVAL", 2*time.Second),
		OdometerMaxRegressionKM: getEnvFloat("TELEMETRY_ODOMETER_MAX_REGRESSION_KM", 1.0),
		FuelClampDeltaPct:       getEnvFloat("TELEMETRY_FUEL_CLAMP_DELTA_PCT", 5.0),
	}

	// Spec 04 §9 — live map + share links + ETA + preventive maintenance.
	cfg.LiveMap = LiveMapConfig{
		MapTileProvider:       getEnv("MAP_TILE_PROVIDER", "auto"),
		MapGoogleStyle:        getEnv("MAP_GOOGLE_STYLE", "m"),
		MapGL:                 getEnv("MAP_GL", "IN"),
		MapOSMURL:             getEnv("MAP_OSM_URL", "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"),
		NominatimURL:          getEnv("NOMINATIM_URL", "https://nominatim.openstreetmap.org"),
		MapPollSec:            getEnvInt("MAP_POLL_SEC", 10),
		CSPEnabled:            getEnv("CSP_ENABLED", "false") == "true",
		ShareLinkTTLHours:     getEnvInt("SHARE_LINK_TTL_HOURS", 24),
		ShareLinkMaxTTLHours:  getEnvInt("SHARE_LINK_MAX_TTL_HOURS", 168),
		ShareLinkMaxActive:    getEnvInt("SHARE_LINK_MAX_ACTIVE", 20),
		EtaStaleMin:           getEnvInt("ETA_STALE_MIN", 15),
		EtaWindowMin:          getEnvInt("ETA_WINDOW_MIN", 30),
		EtaGuardMaxRegressMin: getEnvInt("ETA_GUARD_MAX_REGRESS_MIN", 5),
		TelemetryStaleMin:     getEnvInt("TELEMETRY_STALE_MIN", 15),
		SSEEnabled:            getEnv("SSE_ENABLED", "true") == "true",
		SSEKeepaliveSec:       getEnvInt("SSE_KEEPALIVE_SEC", 15),
		PMEnabled:             getEnv("PM_ENABLED", "true") == "true",
		PMCheckIntervalMin:    getEnvInt("PM_CHECK_INTERVAL_MIN", 15),
		PMCriticalDTCs:        getEnv("PM_CRITICAL_DTCS", "P0A0F,P1602"),
	}

	// Spec 05 §14 — Operational alerts Telegram configuration.
	cfg.Alerts = AlertConfig{
		TelegramBotToken: getEnv("ALERT_TELEGRAM_BOT_TOKEN", os.Getenv("FOUNDER_TELEGRAM_BOT_TOKEN")),
		TelegramChatID:   getEnv("ALERT_TELEGRAM_CHAT_ID", os.Getenv("FOUNDER_TELEGRAM_CHAT_ID")),
		WhatsAppProvider: getEnv("WHATSAPP_PROVIDER", "mock"),
		WhatsAppAPIKey:   getEnv("WHATSAPP_GUPSHUP_API_KEY", getEnv("WHATSAPP_API_KEY", "")),
		WhatsAppToken:    getEnv("WHATSAPP_META_TOKEN", getEnv("WHATSAPP_TOKEN", "")),
		WhatsAppPhoneID:  getEnv("WHATSAPP_META_PHONE_ID", getEnv("WHATSAPP_PHONE_ID", "")),
		WhatsAppURL:      getEnv("WHATSAPP_URL", getEnv("WHATSAPP_EVOLUTION_URL", "")),
		WhatsAppInstance: getEnv("WHATSAPP_INSTANCE", getEnv("WHATSAPP_EVOLUTION_INSTANCE", "default")),
	}

	// Spec 05 §7, Spec 07 — E-Way Bill lifecycle worker configuration.
	cfg.EWayBill = EWayBillConfig{
		WorkerEnabled:        getEnvBool("EWAYBILL_WORKER_ENABLED", true),
		WorkerInterval:       getEnvDuration("EWAYBILL_WORKER_INTERVAL", 60*time.Second),
		ExtensionKM:          getEnvFloat("EWAYBILL_EXTENSION_KM", 5.0),
		ExtensionLeadSeconds: getEnvInt("EWAYBILL_EXTENSION_LEAD_SECONDS", 14400),
		MinInvoiceValue:      getEnvFloat("EWAYBILL_MIN_INVOICE_VALUE", 50000.0),
		ExtendEnabled:        getEnvBool("EWB_EXTEND_ENABLED", false),
	}

	// Spec 07 — GST E-Invoicing / GSTN configuration.
	cfg.GSTN = GSTNConfig{
		UseMock:      getEnvBool("INTEGRATION_GSTN_USE_MOCK", true),
		Username:     os.Getenv("INTEGRATION_GSTN_USERNAME"),
		Password:     os.Getenv("INTEGRATION_GSTN_PASSWORD"),
		ClientID:     os.Getenv("INTEGRATION_GSTN_CLIENT_ID"),
		ClientSecret: os.Getenv("INTEGRATION_GSTN_CLIENT_SECRET"),
	}

	// Spec 21 §6 — FASTag NETC configuration (fix: load INTEGRATION_FASTAG_USE_MOCK).
	cfg.FASTag = FASTagConfig{
		UseMock:  getEnvBool("INTEGRATION_FASTAG_USE_MOCK", true),
		APIKey:   getEnv("INTEGRATION_FASTAG_API_KEY", os.Getenv("FASTAG_API_KEY")),
		Enabled:  getEnvBool("INTEGRATION_FASTAG_ENABLED", false),
		Endpoint: getEnv("INTEGRATION_FASTAG_ENDPOINT", "https://api.fastag.org"),
	}

	// Spec 18 — Route optimization (Wave A)
	cfg.Routing = RoutingConfig{
		Provider: getEnv("ROUTING_PROVIDER", "mock"),
		OSRMURL:  getEnv("OSRM_URL", getEnv("ROUTING_OSRM_URL", "http://osrm.internal:5000")),
	}

	// Cache backend (none by default; memory for single-instance dev,
	// redis for shared/multi-instance deployments).
	cfg.Cache = CacheConfig{
		Driver:        strings.ToLower(getEnv("CACHE_DRIVER", "none")),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		DefaultTTL:    getEnvDuration("CACHE_DEFAULT_TTL", 5*time.Minute),
		KeyPrefix:     getEnv("CACHE_KEY_PREFIX", "mvtms:"),
	}

	// File storage backend + worker leader election. Leader lock defaults ON:
	// on a single instance it is a no-op claim; at scale-out it stops
	// duplicate cron/worker execution automatically.
	cfg.Storage = StorageConfig{
		Driver:   strings.ToLower(getEnv("STORAGE_DRIVER", "local")),
		LocalDir: getEnv("LOCAL_STORAGE_DIR", cfg.UploadDir),
	}
	cfg.WorkerLeaderLock = getEnvBool("WORKER_LEADER_LOCK", true)

	smtpPass := os.Getenv("SMTP_PASSWORD")
	if smtpPass == "" {
		smtpPass = os.Getenv("SMTP_PASS")
	}
	smtpPass = strings.ReplaceAll(smtpPass, " ", "")

	cfg.Notify = NotifyConfig{
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           os.Getenv("SMTP_PORT"),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       smtpPass,
		SMTPFrom:           os.Getenv("SMTP_FROM"),
		SMTPDirect:         getEnvBool("SMTP_DIRECT", false) || strings.EqualFold(os.Getenv("SMTP_HOST"), "direct"),
		SMSWebhookURL:      os.Getenv("SMS_WEBHOOK_URL"),
		SMSWebhookToken:    os.Getenv("SMS_WEBHOOK_TOKEN"),
		EmailPoolEnabled:   getEnvBool("EMAIL_POOL_ENABLED", true),
		EmailPoolStrategy:  getEnv("EMAIL_POOL_STRATEGY", getEnv("EMAIL_PROVIDER_STRATEGY", "priority")),
		EmailProvidersJSON: os.Getenv("EMAIL_PROVIDERS_JSON"),
	}
	cfg.Comm = CommConfig{
		OutboxWorker: getEnvBool("COMM_OUTBOX_WORKER", true),
	}

	// Google OAuth ("Sign in with Google") — zero-cost web identity.
	cfg.Google = GoogleConfig{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  getEnv("GOOGLE_REDIRECT_URL", ""),
	}

	// FCM Push Notification configuration for driver mobile apps.
	cfg.FCM = FCMConfig{
		ProjectID:          os.Getenv("FCM_PROJECT_ID"),
		ServerKey:          os.Getenv("FCM_SERVER_KEY"),
		ServiceAccountJSON: getEnv("FCM_SERVICE_ACCOUNT", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")),
		Endpoint:           os.Getenv("FCM_ENDPOINT"),
	}

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
	}

	return cfg
}

func validateDatabaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: invalid value %q: %w", raw, err)
	}
	if u.Scheme == "" {
		if !strings.HasSuffix(strings.ToLower(raw), ".db") &&
			!strings.HasSuffix(strings.ToLower(raw), ".sqlite") &&
			!strings.HasSuffix(strings.ToLower(raw), ".sqlite3") {
			return fmt.Errorf("DATABASE_URL: plain file path %q must end in .db, .sqlite or .sqlite3", raw)
		}
		return nil
	}
	if u.Scheme != "file" {
		return fmt.Errorf("DATABASE_URL: unsupported scheme %q in %q; use a plain file path or a file: URI", u.Scheme, raw)
	}
	return nil
}

// Validate checks the configuration for invalid values.
func (c *Config) Validate() error {
	// Effective DSN: Database.URL wins, falling back to the legacy flat
	// field so hand-built configs (tests) keep validating.
	dsn := c.Database.URL
	if dsn == "" {
		dsn = c.DatabaseURL
	}
	switch c.Database.Driver {
	case "", "sqlite":
		if err := validateDatabaseURL(dsn); err != nil {
			return err
		}
	case "postgres", "postgresql":
		if strings.TrimSpace(dsn) == "" {
			return fmt.Errorf("DATABASE_URL: required when DATABASE_DRIVER=postgres")
		}
	case "mysql":
		if strings.TrimSpace(dsn) == "" {
			return fmt.Errorf("DATABASE_URL: required when DATABASE_DRIVER=mysql")
		}
	default:
		return fmt.Errorf("DATABASE_DRIVER: unsupported driver %q; use sqlite, postgres or mysql", c.Database.Driver)
	}

	switch c.Cache.Driver {
	case "", "none", "memory", "redis": // "" = unset = none
	default:
		return fmt.Errorf("CACHE_DRIVER: unsupported driver %q; use none, memory or redis", c.Cache.Driver)
	}
	if c.Cache.Driver == "redis" && strings.TrimSpace(c.Cache.RedisAddr) == "" {
		return fmt.Errorf("REDIS_ADDR: required when CACHE_DRIVER=redis")
	}

	switch c.Storage.Driver {
	case "", "local", "s3":
	default:
		return fmt.Errorf("STORAGE_DRIVER: unsupported driver %q; use local or s3", c.Storage.Driver)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil {
			return parsed
		}
	}
	return fallback
}

// IsProduction returns true if the app is running in production.
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

// IsDevelopment returns true if the app is running in development.
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

// UsingKnownDefaultSecret returns true when production would rely on known,
// committed default values for secrets instead of environment-provided ones.
func (c *Config) UsingKnownDefaultSecret() bool {
	if c.CookieSecret == "dev-secret-key-change-in-production-32b!" {
		return true
	}
	if c.CookieSecret == "dev-secret-32bytes-for-cookie-signing!" {
		return true
	}
	if c.APITokenSecret == "" {
		return true
	}
	return false
}
