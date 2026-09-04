package service

import (
	"context"
	"database/sql"
	"log/slog"

	"transport-app/internal/config"
	"transport-app/internal/deviation"
	"transport-app/internal/domain"
	bookingevents "transport-app/internal/domain/booking"
	tripevents "transport-app/internal/domain/trip"
	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	"transport-app/internal/founder"
	"transport-app/internal/founder/alerts"
	fuel "transport-app/internal/fuel"
	geofenceapp "transport-app/internal/geofence/application"
	invoiceapp "transport-app/internal/invoice/application"
	"transport-app/internal/repository"
)

// Store is the combined repository interface used by all services.
// The SQLite implementation (SQLRepository) satisfies this interface.
// Future PostgreSQL implementations would also satisfy it.
type Store interface {
	repository.RoleRepository
	repository.UserRepository
	repository.SessionRepository
	repository.DriverRepository
	repository.VehicleRepository
	repository.CustomerRepository
	repository.RouteRepository
	repository.BookingRepository
	repository.TripRepository
	repository.InvoiceRepository
	repository.PaymentRepository
	repository.CompanySettingsRepository
	repository.FileRepository
	repository.AuditLogRepository

	// NextInvoiceNumber atomically allocates the next GST-compliant
	// sequential invoice number ("INV/2026-27/0001") for the tenant's
	// current financial year (invoice_sequences, migration 00048).
	NextInvoiceNumber(ctx context.Context, tenantID string, prefix string) (string, error)

	// Attention counts feed the dashboard exception strip. All tenant-scoped.
	CountUnassignedBookings(ctx context.Context) (int64, error)
	CountMaintenanceDue(ctx context.Context) (int64, error)
	CountOpenWorkOrders(ctx context.Context) (int64, error)
	CountGarageVehicles(ctx context.Context) (int64, error)
	CountOpenAlerts(ctx context.Context) (int64, error)
	CountActiveDTCs(ctx context.Context) (int64, error)
	CountExpiringEwaybills(ctx context.Context) (int64, error)
	CountPendingKharcha(ctx context.Context) (int64, error)
	CountLowFastag(ctx context.Context, threshold float64) (int64, error)
}

// Services holds all service instances and shared dependencies.
type Services struct {
	Auth              *AuthService
	Users             *UserService
	Drivers           *DriverService
	Vehicles          *VehicleService
	Customers         *CustomerService
	Routes            *RouteService
	Bookings          *BookingService
	Trips             *TripService
	Invoices          *InvoiceService
	Payments          *PaymentService
	Notes             *CreditNoteService
	Settings          *CompanySettingsService
	Dashboard         *DashboardService
	Files             *FileService
	Audit             *AuditLogService
	Founder           *founder.FounderService
	Compliance        *ComplianceService
	Settlements       *DriverSettlementService
	Telemetry         *TelemetryService
	Kharcha           *KharchaService
	FuelAudit         *FuelAuditService
	Scorecard         *ScorecardService
	Documents         *DocumentService
	PNL               *PNLService
	OpsAlerts         *OpsAlertService
	Experiments       *ExperimentsService
	FounderSignals    *FounderSignalsService
	FounderAudit      *FounderAuditService
	EWayBill          *ewaybill.EWayBillService
	Deviation         *deviation.Engine
	GeofenceEvaluator *geofenceapp.RealtimeEvaluator
	Events            events.EventBus

	// TenantConfigs reads per-tenant settings overrides from company_config
	// (Spec 24 §Business logic overlay). Nil when the store exposes no raw DB
	// — every consumer nil-checks and falls through to legacy behavior.
	TenantConfigs *TenantConfigReader

	store Store
	cfg   *config.Config
	log   *slog.Logger
}

// DB returns the underlying sql.DB if available.
func (s *Services) DB() *sql.DB {
	if s == nil || s.store == nil {
		return nil
	}
	if getter, ok := s.store.(repository.DBGetter); ok && getter != nil {
		return getter.DB()
	}
	return nil
}

// NewServices creates all services with the given dependencies.
// eventBus is the single shared event bus: services publish to it and the
// outbox relay, founder handlers, and automation subscribers all listen on
// the SAME instance. Passing a nil bus creates a local one (test convenience).
func NewServices(store Store, cfg *config.Config, log *slog.Logger, eventBus events.EventBus) *Services {
	s := &Services{store: store, cfg: cfg, log: log}

	if eventBus == nil {
		eventBus = events.NewInMemoryBus()
	}
	s.Events = eventBus

	var tm repository.TxManager
	if dbGetter, ok := store.(repository.DBGetter); ok {
		tm = repository.NewTxManager(dbGetter)
	} else {
		log.Warn("store does not implement DB() — TxManager unavailable")
	}

	// Per-tenant settings overlay (Spec 24 §Business logic overlay). Built
	// over the raw DB when available; left nil otherwise so every call site
	// falls through to the legacy company_settings-only behavior.
	var tenantCfg *TenantConfigReader
	if dbGetter, ok := store.(repository.DBGetter); ok && dbGetter != nil {
		tenantCfg = NewTenantConfigReader(dbGetter.DB())
	}
	s.TenantConfigs = tenantCfg

	// Publish the overlay as the invoice application layer's process-wide
	// default. SetDefaultTenantOverlay (not constructor injection) is used
	// deliberately: NewGenerateInvoiceUseCase has 45+ call sites across
	// handlers, facades and tests; additive registration keeps them all
	// untouched. Nil is a legal value — the use case then skips overlaying.
	invoiceapp.SetDefaultTenantOverlay(tenantCfg)

	bs := baseService{store: store, cfg: cfg, log: log, txManager: tm, events: eventBus, tenantCfg: tenantCfg}
	s.Auth = &AuthService{baseService: bs}
	s.Users = &UserService{baseService: bs}
	s.Drivers = &DriverService{baseService: bs}
	s.Vehicles = &VehicleService{baseService: bs}
	s.Customers = &CustomerService{baseService: bs}
	s.Routes = &RouteService{baseService: bs}
	s.Bookings = &BookingService{baseService: bs}
	s.Trips = &TripService{baseService: bs}
	s.Invoices = &InvoiceService{baseService: bs}
	s.Payments = &PaymentService{baseService: bs}
	s.Notes = &CreditNoteService{baseService: bs}
	s.Settings = &CompanySettingsService{baseService: bs}
	s.Dashboard = &DashboardService{baseService: bs}
	s.Files = &FileService{baseService: bs}
	s.Documents = NewDocumentService(bs, s.Files)
	s.Audit = &AuditLogService{baseService: bs}
	s.Compliance = &ComplianceService{baseService: bs}
	s.Trips.compliance = s.Compliance
	s.Settlements = &DriverSettlementService{
		baseService:       bs,
		defaultFare:       defaultSettlementFare,
		defaultAdvances:   defaultSettlementAdvances,
		defaultDeductions: defaultSettlementDeductions,
	}
	s.Telemetry = &TelemetryService{baseService: bs}
	s.Kharcha = &KharchaService{baseService: bs}

	// Fuel claim audit service (Spec 03 §3). The ConfigReader reads
	// company_config with a short-lived cache; nil-safe when the store has
	// no raw DB access (tests with fakes fall back to compiled defaults).
	if dbGetter, ok := store.(repository.DBGetter); ok {
		s.FuelAudit = &FuelAuditService{baseService: bs, config: fuel.NewConfigReader(dbGetter.DB())}

		// Driver scorecard (Spec 03 §4). Built over the same raw DB. The
		// settlement service receives it as a dependency for the performance
		// bonus hook (Spec 03 §7, gotcha 6 — nil-safe for backward compat).
		s.Scorecard = NewScorecardService(bs, dbGetter.DB())
		s.Settlements.scorecard = s.Scorecard

		// PNL daily snapshot service (Spec 16 §2).
		s.PNL = NewPNLService(dbGetter.DB())

		// Operational alerts service (Spec 16 §4).
		s.OpsAlerts = NewOpsAlertService(bs, dbGetter.DB())
		s.Settlements.opsAlerts = s.OpsAlerts
		s.Compliance.opsAlerts = s.OpsAlerts

		// A/B experiments service (Spec 16 §5).
		s.Experiments = NewExperimentsService(bs, dbGetter.DB())

		// Founder signals + audit trail (Spec 16 §6, §7).
		s.FounderAudit = NewFounderAuditService(bs, dbGetter.DB())
		s.FounderSignals = NewFounderSignalsService(bs, dbGetter.DB())
		s.FounderSignals.SetAudit(s.FounderAudit)
		s.Experiments.SetAudit(s.FounderAudit)

		// Founder-signal integration points (Spec 16 §6 hooks).
		s.OpsAlerts.SetFounderSignals(s.FounderSignals)
		s.PNL.SetFounderSignals(s.FounderSignals)

		// GPS Route Deviation Engine (Spec 03 §P3C).
		s.Deviation = deviation.NewEngine(dbGetter.DB(), s.Events, fuel.NewConfigReader(dbGetter.DB()), log)

		// Realtime Geofence Evaluator (Spec 02 §P3D).
		s.GeofenceEvaluator = geofenceapp.NewRealtimeEvaluator(dbGetter.DB(), s.Events, geofenceapp.NewConfigReader(dbGetter.DB()), log)
	}

	// Instantiate Telegram Bot Notifier if token configured, otherwise graceful fallback
	var founderNotifier founder.Notifier = alerts.NewTelegramBotNotifier(nil, 0)
	s.Founder = founder.NewFounderService(founderNotifier)
	s.Founder.RegisterEventHandlers(bs.events)

	s.initEventHandlers()

	return s
}

// initEventHandlers wires up event subscribers that coordinate across services.
func (s *Services) initEventHandlers() {
	bus := s.Bookings.events

	// BookingConfirmed → TripCreated: automatically create a trip when a booking is confirmed.
	bus.Subscribe(events.BookingConfirmed, func(ctx context.Context, e events.Event) error {
		evt, ok := e.Payload.(bookingevents.BookingConfirmedEvent)
		if !ok {
			return nil
		}
		b, err := s.store.GetBookingByID(ctx, evt.BookingID)
		if err != nil {
			return err
		}
		_, _ = s.Trips.CreateTrip(ctx, CreateTripRequest{
			BookingID:     &evt.BookingID,
			RouteID:       b.RouteID,
			DriverID:      nil,
			VehicleID:     nil,
			DepartureTime: b.PickupDate.Format("2006-01-02T15:04:05"),
			ArrivalTime:   "",
			Remarks:       "Auto-created from confirmed booking",
		})
		return nil
	})

	// TripCompleted → InvoiceGenerated: automatically generate an invoice when a trip completes.
	bus.Subscribe(events.TripCompleted, func(ctx context.Context, e events.Event) error {
		evt, ok := e.Payload.(tripevents.TripCompletedEvent)
		if !ok {
			return nil
		}
		if _, err := s.Invoices.GenerateInvoiceFromTrip(ctx, evt.TripID); err != nil {
			s.log.Error("auto-invoice generation failed for completed trip", "trip_id", evt.TripID, "error", err)
		}
		return nil
	})

	// Rule 2: TripDelivered → Auto-generate GST Invoice + Driver Settlement Statement
	bus.Subscribe(events.TripDelivered, func(ctx context.Context, e events.Event) error {
		payload, ok := e.Payload.(map[string]interface{})
		if !ok {
			return nil
		}
		tripIDVal, exists := payload["trip_id"]
		if !exists {
			return nil
		}
		tripID, ok := tripIDVal.(domain.TripID)
		if !ok {
			return nil
		}
		if _, err := s.Invoices.GenerateInvoiceFromTrip(ctx, tripID); err != nil {
			s.log.Error("auto-invoice generation failed for delivered trip", "trip_id", tripID, "error", err)
		}
		if _, err := s.Settlements.GenerateSettlement(ctx, string(tripID), false); err != nil {
			s.log.Error("auto-settlement generation failed for delivered trip", "trip_id", tripID, "error", err)
		}
		return nil
	})
}

type baseService struct {
	store     Store
	cfg       *config.Config
	log       *slog.Logger
	txManager repository.TxManager
	events    events.EventBus

	// tenantCfg is the shared per-tenant settings overlay reader; nil in
	// tests/fakes without a raw DB (overlay helpers become pass-through).
	tenantCfg *TenantConfigReader
}
