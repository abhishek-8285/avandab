package application_test

// Red-green ownership for concurrent payout overspend.
//
// Two concurrent same-driver InitiatePayout calls must not both be authorized
// against the same funds. The balance check and debit run in one transaction
// but with no row-level serialization (SQLite has no FOR UPDATE), so both
// readers see the full balance and both proceed to debit — a lost update.
// On Postgres both debits commit and the wallet overspends; on WAL-mode
// SQLite the loser happens to abort later at commit (snapshot conflict),
// which masks the bug at the commit layer. This test therefore asserts at
// the validation layer, where the missing serialization lives on every
// engine: at most one racer may observe sufficient funds.
//
// RED before the driver-row lock lands (both racers observe 1782 and proceed
// to debit a combined 2400), GREEN after (the loser blocks on the lock, then
// reads the winner's debit and fails with insufficient balance).
//
// Two independent *sql.DB handles (independent connections) share one
// file-backed DB; deliberately no SetMaxOpenConns(1) pool crutch. A hook on
// the in-transaction balance read parks each racer after it observes, so
// scheduler luck can neither hide the bug nor flake the fix.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/settlement/application"
	"transport-app/internal/settlement/domain"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
)

const payoutRaceSchema = `
CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Fleet Tenant 1');

CREATE TABLE drivers (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	first_name TEXT NOT NULL,
	last_name TEXT,
	phone TEXT,
	email TEXT,
	bank_details TEXT
);

CREATE TABLE driver_payout_accounts (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	driver_id TEXT NOT NULL,
	account_holder_name TEXT NOT NULL DEFAULT '',
	account_number_encrypted TEXT NOT NULL DEFAULT '',
	account_number_masked TEXT NOT NULL DEFAULT '',
	ifsc_code TEXT NOT NULL DEFAULT '',
	bank_name TEXT NOT NULL DEFAULT '',
	is_primary INTEGER NOT NULL DEFAULT 1,
	verification_status TEXT NOT NULL DEFAULT 'unverified',
	created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE driver_settlements (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	trip_id TEXT NOT NULL,
	driver_id TEXT NOT NULL,
	gross_fare REAL NOT NULL,
	deductions REAL NOT NULL,
	net_payout REAL NOT NULL,
	status TEXT NOT NULL DEFAULT 'calculated',
	commission_rate REAL,
	commission_amount REAL,
	toll_adjustment REAL,
	advance_deductions REAL,
	tds_rate REAL,
	tds_amount REAL,
	created_at DATETIME DEFAULT (datetime('now')),
	updated_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE driver_ledger_entries (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	driver_id TEXT NOT NULL,
	trip_id TEXT,
	entry_type TEXT NOT NULL,
	amount REAL NOT NULL,
	currency TEXT NOT NULL DEFAULT 'INR',
	reference_type TEXT NOT NULL,
	reference_id TEXT NOT NULL,
	balance_after REAL NOT NULL,
	description TEXT,
	created_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE payout_instructions (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	driver_id TEXT NOT NULL,
	payout_account_id TEXT,
	amount REAL NOT NULL,
	currency TEXT NOT NULL DEFAULT 'INR',
	idempotency_key TEXT NOT NULL,
	provider_payout_id TEXT,
	status TEXT NOT NULL DEFAULT 'initiated',
	failure_reason TEXT,
	utr TEXT,
	initiated_at DATETIME NOT NULL DEFAULT (datetime('now')),
	completed_at DATETIME,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE provider_events (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	provider TEXT NOT NULL,
	provider_event_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	payload TEXT NOT NULL,
	processed_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`

// observeRepo decorates the real repository: after the in-transaction balance
// read returns it records the observed balance, reports arrival, and parks
// until the test releases both racers together. Timing-only hook; every
// write passes straight through with the ambient transaction intact.
type observeRepo struct {
	domain.SettlementRepository
	seen    chan<- float64
	arrived chan<- struct{}
	release <-chan struct{}
}

func (r *observeRepo) GetDriverWallet(ctx context.Context, tenantID, driverID string) (*domain.DriverWallet, error) {
	w, err := r.SettlementRepository.GetDriverWallet(ctx, tenantID, driverID)
	if err == nil {
		select {
		case r.seen <- w.AvailableBalance:
		default:
		}
		select {
		case r.arrived <- struct{}{}:
		default:
		}
		<-r.release
	}
	return w, err
}

func TestPayout_ConcurrentSameDriverCannotOverspend(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "payout_race.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout=5000", path)

	setup, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { _ = setup.Close() }()
	_, err = setup.Exec(payoutRaceSchema)
	require.NoError(t, err)

	// Independent handles => independent connections sharing one database.
	dbA, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { _ = dbA.Close() }()
	dbB, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { _ = dbB.Close() }()

	fundSvc := application.NewSettlementAppService(settleSQL.NewSQLSettlementRepository(setup), "secret", 100.0)

	// 2000 gross - 200 commission - 18 TDS = 1782 funded; each payout 1200
	// fits alone but the pair combined (2400) exceeds the wallet.
	const funded = 1782.0
	const payout = 1200.0

	for attempt := 0; attempt < 3; attempt++ {
		driverID := fmt.Sprintf("drv-race-%d", attempt)
		_, err = setup.Exec(
			`INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, email, bank_details)
			 VALUES (?, 'tenant-1', 'Race', 'Driver', '9000000000', '', '{}')`, driverID)
		require.NoError(t, err)
		_, err = setup.Exec(
			`INSERT INTO driver_payout_accounts (id, tenant_id, driver_id, verification_status, is_primary)
			 VALUES (?, 'tenant-1', ?, 'verified', 1)`,
			fmt.Sprintf("acc-race-%d", attempt), driverID)
		require.NoError(t, err)
		_, err = fundSvc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
			TripID: fmt.Sprintf("trip-race-%d", attempt), DriverID: driverID,
			GrossFare: 2000.0, CommissionRate: 0.10, TDSRate: 0.01,
		})
		require.NoError(t, err)

		seen := make(chan float64, 2)
		arrived := make(chan struct{}, 2)
		release := make(chan struct{})
		newRacer := func(db *sql.DB) *application.SettlementAppService {
			return application.NewSettlementAppService(
				&observeRepo{settleSQL.NewSQLSettlementRepository(db), seen, arrived, release},
				"secret", 100.0)
		}
		svcs := []*application.SettlementAppService{newRacer(dbA), newRacer(dbB)}

		start := make(chan struct{})
		var wg sync.WaitGroup
		type outcome struct {
			amount float64
			err    error
		}
		outs := [2]outcome{}
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				resp, err := svcs[i].InitiatePayout(ctx, "tenant-1", driverID, application.InitiatePayoutRequest{
					IdempotencyKey: fmt.Sprintf("race-%d-%d", attempt, i),
					Amount:         payout,
				})
				if err == nil && resp != nil {
					outs[i].amount = resp.Amount
				} else {
					outs[i].err = err
				}
			}(i)
		}
		close(start)

		// Release once both racers finish the balance read; on GREEN the
		// loser blocks on the driver lock first and never arrives, so the
		// wait times out and the winner proceeds alone.
		for n := 0; n < 2; {
			select {
			case <-arrived:
				n++
			case <-time.After(500 * time.Millisecond):
				n = 2
			}
		}
		close(release)
		wg.Wait()

		// The invariant: both racers must never observe sufficient funds
		// against the same balance. Two full reads authorize a combined
		// debit that exceeds the wallet.
		fullReads := 0
		for len(seen) > 0 {
			if b := <-seen; b >= payout {
				fullReads++
			}
		}
		if fullReads == 2 {
			t.Fatalf("concurrent payouts both observed full funds %.2f for %.2f each — combined %.2f exceeds wallet (attempt %d)",
				funded, payout, 2*payout, attempt)
		}

		var okCount int
		var okSum float64
		for _, o := range outs {
			if o.err == nil {
				okCount++
				okSum += o.amount
			}
		}
		require.Equal(t, 1, okCount, "exactly one of the pair must win (attempt %d): %v", attempt, outs)
		require.LessOrEqual(t, okSum, funded, "combined debits must not exceed wallet (attempt %d)", attempt)

		wallet, err := fundSvc.GetDriverWallet(ctx, "tenant-1", driverID)
		require.NoError(t, err)
		assert.Equal(t, funded-payout, wallet.AvailableBalance, "loser must see the winner's debit (attempt %d)", attempt)
	}
}
