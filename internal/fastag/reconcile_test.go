package fastag

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	intFastag "transport-app/internal/integration/fastag"
	"transport-app/internal/shared"
)

type stubClient struct {
	txs []intFastag.TollTransaction
}

func (s stubClient) GetBalance(context.Context, string, string) (intFastag.Balance, error) {
	return intFastag.Balance{}, nil
}

func (s stubClient) DeductToll(context.Context, intFastag.DeductTollRequest) (intFastag.TollTransaction, error) {
	return intFastag.TollTransaction{}, nil
}

func (s stubClient) ListTransactions(context.Context, string, int) ([]intFastag.TollTransaction, error) {
	return s.txs, nil
}

func (s stubClient) Reconcile(context.Context, string, string, string) (intFastag.ReconcileResult, error) {
	return intFastag.ReconcileResult{}, nil
}

func newReconcileTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	schema := `
	CREATE TABLE vehicles (id TEXT PRIMARY KEY, registration_number TEXT);
	CREATE TABLE trips (
		id TEXT PRIMARY KEY, tenant_id TEXT, vehicle_id TEXT, driver_id TEXT,
		departure_time DATETIME, arrival_time DATETIME, status TEXT
	);
	CREATE TABLE fastag_transactions (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, tag_id TEXT NOT NULL,
		vehicle_id TEXT, vehicle_number TEXT, trip_id TEXT, plaza_id TEXT, plaza_name TEXT,
		amount REAL NOT NULL DEFAULT 0, txn_timestamp DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'SUCCESS' CHECK (status IN ('SUCCESS','FAILURE','PENDING')),
		source TEXT NOT NULL DEFAULT 'PROVIDER' CHECK (source IN ('PROVIDER','MANUAL','GPS')),
		reconciled INTEGER NOT NULL DEFAULT 0, kharcha_id TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE driver_expenses (
		id TEXT PRIMARY KEY, trip_id TEXT, driver_id TEXT, expense_type TEXT, category TEXT,
		amount REAL, description TEXT, approved INTEGER, status TEXT, approved_at DATETIME, tenant_id TEXT
	);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func seedReconcileTripAndTxn(t *testing.T, db *sql.DB) {
	t.Helper()
	dep := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	arr := dep.Add(3 * time.Hour)
	txnAt := dep.Add(1 * time.Hour)

	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO vehicles (id, registration_number) VALUES ('veh-1', 'MH12AB1234')`, nil},
		{`INSERT INTO trips (id, tenant_id, vehicle_id, driver_id, departure_time, arrival_time, status)
		  VALUES ('trip-1', '1', 'veh-1', 'drv-1', ?, ?, 'completed')`, []any{dep, arr}},
		{`INSERT INTO fastag_transactions (id, tenant_id, tag_id, vehicle_number, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled)
		  VALUES ('tx-seeded', '1', 'TAG-1', 'MH12AB1234', 'PLZ-1', 'Khalapur Plaza', 120.0, ?, 'SUCCESS', 'PROVIDER', 0)`, []any{txnAt}},
	}
	for _, s := range seed {
		if _, err := db.Exec(s.q, s.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func countExpenses(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM driver_expenses`).Scan(&n); err != nil {
		t.Fatalf("count driver_expenses: %v", err)
	}
	return n
}

// Mock provider data must never become real approved kharcha, even when the
// company config still has fastag_auto_kharcha=true.
func TestReconcile_MockProviderCreatesNoKharcha(t *testing.T) {
	db := newReconcileTestDB(t)
	seedReconcileTripAndTxn(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	svc := NewFASTagService(db, stubClient{}, Config{AutoKharcha: true, Provider: "MOCK"})
	res, err := svc.Reconcile(ctx, "MH12AB1234", "", "")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if res.Matched != 1 {
		t.Errorf("Matched = %d, want 1", res.Matched)
	}
	if res.KharchaCreated != 0 {
		t.Errorf("KharchaCreated = %d, want 0 for mock provider", res.KharchaCreated)
	}
	if n := countExpenses(t, db); n != 0 {
		t.Errorf("driver_expenses rows = %d, want 0 for mock provider", n)
	}
}

// A configured real provider keeps the auto-kharcha behavior.
func TestReconcile_RealProviderCreatesKharcha(t *testing.T) {
	db := newReconcileTestDB(t)
	seedReconcileTripAndTxn(t, db)
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	svc := NewFASTagService(db, stubClient{}, Config{AutoKharcha: true, Provider: "NETC"})
	res, err := svc.Reconcile(ctx, "MH12AB1234", "", "")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if res.KharchaCreated != 1 {
		t.Errorf("KharchaCreated = %d, want 1 for NETC provider", res.KharchaCreated)
	}
	if n := countExpenses(t, db); n != 1 {
		t.Errorf("driver_expenses rows = %d, want 1 for NETC provider", n)
	}
}

func TestAutoKharchaAllowed(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"mock provider", Config{AutoKharcha: true, Provider: "MOCK"}, false},
		{"mock provider lowercase", Config{AutoKharcha: true, Provider: " mock "}, false},
		{"empty provider", Config{AutoKharcha: true}, false},
		{"auto disabled", Config{AutoKharcha: false, Provider: "NETC"}, false},
		{"real provider", Config{AutoKharcha: true, Provider: "NETC"}, true},
		{"real provider mixed case", Config{AutoKharcha: true, Provider: "icici"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoKharchaAllowed(tc.cfg); got != tc.want {
				t.Errorf("autoKharchaAllowed(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}
