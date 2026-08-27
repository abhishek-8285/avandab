package test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// ContextWithTestTenant wraps a parent context with the single-tenant bootstrap
// tenant. Tests that exercise tenant-scoped repositories (drivers, vehicles,
// trips, bookings, invoices, payments) must use this instead of a bare
// context.Background(), because TenantIDFromContext is fail-closed.
func ContextWithTestTenant(parent context.Context) context.Context {
	return shared.ContextWithTenantID(parent, shared.DefaultTenant)
}

// NewTestDB creates an in-memory SQLite database with migrations applied.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// A unique named in-memory DB (not plain ":memory:"): with ":memory:?cache=shared"
	// the database is not reliably shared across pooled connections, which
	// manifests as "no such table" when a write lands on a different
	// connection than the read.
	name := fmt.Sprintf("test_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}

	_ = goose.SetDialect("sqlite")
	if err := goose.Up(db, "../db/migrations"); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}
	// Seed common test tenants for FK strict mode (00104 deletes prod test seeds).
	// Tests that use tenant-a/b/c etc. via direct SQL or ctxAsTenant need these rows.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES
		('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'),
		('other-tenant','Other Tenant','other-tenant'), ('another-tenant','Another Tenant','another-tenant'),
		('tenant-1','Test Tenant 1','tenant-1'), ('tenant-2','Test Tenant 2','tenant-2b'),
		('tenant-7','Test Tenant 7','tenant-7b'), ('tenant-9','Test Tenant 9','tenant-9b'),
		('tenant-999','Test Tenant 999','tenant-999'), ('tenant-a','Tenant A','tenant-a'),
		('tenant-b','Tenant B','tenant-b'), ('tenant-A','Tenant A Cap','tenant-a-cap'),
		('tenant-B','Tenant B Cap','tenant-b2'), ('tenant-zz','Tenant ZZ','tenant-zz'),
		('tenant-seq','Tenant Seq','tenant-seq'), ('tenant-cap','Tenant Cap','tenant-cap'),
		('tenant-dn','Tenant DN','tenant-dn'), ('tenant-ledger','Tenant Ledger','tenant-ledger'),
		('tenant-val','Tenant Val','tenant-val'), ('tenant-fmt','Test Tenant FMT','tenant-fmt'),
		('tenant-loop','Test Tenant Loop','tenant-loop'), ('tn-b','Tenant TN-B','tn-b'),
		('tn-kpi','Tenant TN-KPI','tn-kpi'), ('tenant-c','Tenant C','tenant-c'),
		('tenant-d','Tenant D','tenant-d'), ('tenant-forged','Tenant Forged','tenant-forged'),
		('tenant-42','Tenant 42','tenant-42'), ('test-tenant','Test Tenant','test-tenant'),
		('acme','Acme','acme'), ('beta','Beta','beta')`)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

// NewTestServices creates service instances backed by a real SQLite test database.
func NewTestServices(t *testing.T, db *sql.DB) *service.Services {
	t.Helper()
	return NewTestServicesWithBus(t, db, events.NewInMemoryBus())
}

// NewTestServicesWithBus creates service instances backed by a real SQLite test
// database and injects the given event bus, so tests can prove events published
// by services reach subscribers on the SAME bus instance (Spec 09 §5.1).
func NewTestServicesWithBus(t *testing.T, db *sql.DB, bus events.EventBus) *service.Services {
	t.Helper()

	repo := sqlite.NewRepository(db)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewServices(repo, loadTestConfig(), logger, bus)
}

func loadTestConfig() *config.Config {
	return &config.Config{
		AppEnv:        "testing",
		Port:          "8080",
		DatabaseURL:   "file::memory:?cache=shared",
		CookieSecret:  "test-secret-32bytes-long-enough!",
		SessionMaxAge: 24 * 3600 * 1000000000,
		LogLevel:      "error",
		UploadDir:     "./uploads",
		MaxUploadSize: 10 << 20,
	}
}

// NewTestRepo creates a real repository backed by a test database.
func NewTestRepo(t *testing.T, db *sql.DB) *sqlite.SQLRepository {
	t.Helper()
	return sqlite.NewRepository(db)
}
