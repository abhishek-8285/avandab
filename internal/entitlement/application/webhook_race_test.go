package application_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	modsqlite "modernc.org/sqlite"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
)

// gateConnector/gateConn/gateStmt interpose on the subscription lookup so two
// concurrent webhook deliveries both finish their replay check before either
// proceeds to the write — the exact check-then-write window of the race.
type gateDriver struct {
	need  int32
	count int32
	gate  chan struct{}
	once  sync.Once
}

func (d *gateDriver) arrive() {
	if atomic.AddInt32(&d.count, 1) == atomic.LoadInt32(&d.need) {
		d.once.Do(func() { close(d.gate) })
	}
	<-d.gate
}

type gateConnector struct {
	base driver.Connector
	d    *gateDriver
}

func (c *gateConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &gateConn{d: c.d, base: conn}, nil
}
func (c *gateConnector) Driver() driver.Driver { return c.base.Driver() }

type gateConn struct {
	d    *gateDriver
	base driver.Conn
}

func (c *gateConn) Prepare(q string) (driver.Stmt, error) {
	s, err := c.base.Prepare(q)
	if err != nil {
		return nil, err
	}
	if strings.Contains(q, "FROM tenant_subscriptions WHERE provider_subscription_id") {
		return &gateStmt{base: s, d: c.d}, nil
	}
	return s, nil
}
func (c *gateConn) Close() error              { return c.base.Close() }
func (c *gateConn) Begin() (driver.Tx, error) { return c.base.Begin() }

type gateStmt struct {
	base driver.Stmt
	d    *gateDriver
}

func (s *gateStmt) Close() error                                 { return s.base.Close() }
func (s *gateStmt) NumInput() int                                { return s.base.NumInput() }
func (s *gateStmt) Exec(a []driver.Value) (driver.Result, error) { return s.base.Exec(a) }
func (s *gateStmt) Query(a []driver.Value) (driver.Rows, error)  { return s.base.Query(a) }
func (s *gateStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.d.arrive()
	if qc, ok := s.base.(driver.StmtQueryContext); ok {
		return qc.QueryContext(ctx, args)
	}
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	return s.base.Query(vals)
}

func setupWebhookRaceDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "webhook.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)"
	base, err := modsqlite.NewConnector(dsn)
	require.NoError(t, err)
	g := &gateDriver{need: 2, gate: make(chan struct{})}
	db := sql.OpenDB(&gateConnector{base: base, d: g})
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	migrationsDir := filepath.Join(filepath.Dir(filename), "..", "..", "..", "db", "migrations")
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug, status) VALUES ('tenant-a', 'A', 'tenant-a', 'active')`)
	require.NoError(t, err)
	return db
}

func TestProcessSubscriptionWebhook_ConcurrentSameID_ClaimsOnce(t *testing.T) {
	db := setupWebhookRaceDB(t)
	svc := application.NewService(db)
	ctx := context.Background()
	providerSubID := "sub_rzp_race"
	seedTestSubscription(t, db, "tenant-a", domain.PlanStarter, domain.SubTrial, providerSubID)

	payload := application.WebhookEventPayload{
		EventID:                "evt-race-001",
		EventType:              "subscription.activated",
		Provider:               "RAZORPAY",
		ProviderSubscriptionID: providerSubID,
		PayloadJSON:            `{}`,
		EventTimestamp:         time.Now().UTC(),
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			errs <- svc.ProcessSubscriptionWebhook(ctx, payload)
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		require.NoError(t, <-errs, "no concurrent delivery may fail with an insert-conflict error")
	}

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM subscription_webhook_events WHERE event_id = 'evt-race-001'`).Scan(&count))
	assert.Equal(t, 1, count, "exactly one delivery may claim the event")

	sub, err := svc.GetSubscription(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, domain.SubActive, sub.Status)
}
