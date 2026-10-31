package fastag

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	intFastag "transport-app/internal/integration/fastag"
)

// LoadConfig with nil DB must return safe defaults without touching storage.
func TestLoadConfig_NilDBDefaults(t *testing.T) {
	cfg := LoadConfig(context.Background(), nil)
	assert.False(t, cfg.AutoKharcha, "AutoKharcha should default to false")
	assert.Equal(t, "MOCK", cfg.Provider, "Provider should default to MOCK")
}

func TestLoadConfig_WithDB(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE company_config (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO company_config (key, value) VALUES 
		('fastag_auto_kharcha', 'true'),
		('fastag_merchant_id', 'MERCH_99'),
		('fastag_provider', 'ICICI');`)
	require.NoError(t, err)

	cfg := LoadConfig(context.Background(), db)
	assert.True(t, cfg.AutoKharcha)
	assert.Equal(t, "MERCH_99", cfg.MerchantID)
	assert.Equal(t, "ICICI", cfg.Provider)
}

// Constructor must wire config without requiring DB/client connections.
func TestNewFASTagService_Constructor(t *testing.T) {
	cfg := Config{AutoKharcha: false, MerchantID: "M123", Provider: "MOCK"}
	s := NewFASTagService(nil, nil, cfg)
	require.NotNil(t, s, "NewFASTagService returned nil")
	assert.Equal(t, cfg, s.config)
}

// parseTimeFlexible must handle provider timestamp formats without infra.
func TestParseTimeFlexible(t *testing.T) {
	cases := map[string]string{
		time.RFC3339:          "2026-09-01T10:30:00Z",
		"2006-01-02 15:04:05": "2026-09-01 10:30:00",
		"2006-01-02":          "2026-09-01",
	}
	for format, input := range cases {
		got := parseTimeFlexible(input)
		want, err := time.Parse(format, input)
		require.NoError(t, err, "bad test case %q", input)
		assert.True(t, got.Equal(want), "parseTimeFlexible(%q) = %v, want %v", input, got, want)
	}

	before := time.Now()
	got := parseTimeFlexible("not-a-timestamp")
	assert.False(t, got.Before(before) || time.Since(got) > time.Minute, "unparseable input should fall back to now")
}

type mockClientImpl struct {
	bal    intFastag.Balance
	balErr error
	txn    intFastag.TollTransaction
	txnErr error
}

func (m *mockClientImpl) GetBalance(ctx context.Context, vehicleNumber, tagID string) (intFastag.Balance, error) {
	return m.bal, m.balErr
}

func (m *mockClientImpl) DeductToll(ctx context.Context, req intFastag.DeductTollRequest) (intFastag.TollTransaction, error) {
	return m.txn, m.txnErr
}

func (m *mockClientImpl) ListTransactions(ctx context.Context, vehicleNumber string, limit int) ([]intFastag.TollTransaction, error) {
	return nil, nil
}

func (m *mockClientImpl) Reconcile(ctx context.Context, vehicleNumber, from, to string) (intFastag.ReconcileResult, error) {
	return intFastag.ReconcileResult{}, nil
}

func TestFASTagService_GetBalance(t *testing.T) {
	ctx := context.Background()

	// Success
	mock := &mockClientImpl{
		bal: intFastag.Balance{VehicleNumber: "MH12AB1234", Balance: 1500.50},
	}
	s := NewFASTagService(nil, mock, Config{})
	bal, err := s.GetBalance(ctx, "MH12AB1234")
	require.NoError(t, err)
	assert.Equal(t, 1500.50, bal.Balance)

	// Error
	mockErr := &mockClientImpl{
		balErr: errors.New("provider timeout"),
	}
	sErr := NewFASTagService(nil, mockErr, Config{})
	_, err = sErr.GetBalance(ctx, "MH12AB1234")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider timeout")
}

func TestFASTagService_DeductToll(t *testing.T) {
	ctx := context.Background()

	// Success
	mock := &mockClientImpl{
		txn: intFastag.TollTransaction{
			TransactionID: "TX123",
			Amount:        240.0,
			PlazaName:     "Khed Shivapur",
		},
	}
	s := NewFASTagService(nil, mock, Config{})
	txn, err := s.DeductToll(ctx, intFastag.DeductTollRequest{
		VehicleNumber: "MH12AB1234",
		Amount:        240.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "TX123", txn.TransactionID)
	assert.Equal(t, 240.0, txn.Amount)

	// Error
	mockErr := &mockClientImpl{
		txnErr: errors.New("insufficient balance"),
	}
	sErr := NewFASTagService(nil, mockErr, Config{})
	_, err = sErr.DeductToll(ctx, intFastag.DeductTollRequest{Amount: 5000})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient balance")
}

func TestFASTagService_ListTransactionsAndTags(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	schema := `
	CREATE TABLE fastag_transactions (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, tag_id TEXT NOT NULL,
		vehicle_id TEXT, vehicle_number TEXT, trip_id TEXT, plaza_id TEXT, plaza_name TEXT,
		amount REAL NOT NULL DEFAULT 0, txn_timestamp DATETIME NOT NULL,
		status TEXT NOT NULL DEFAULT 'SUCCESS',
		source TEXT NOT NULL DEFAULT 'PROVIDER',
		reconciled INTEGER NOT NULL DEFAULT 0, kharcha_id TEXT
	);
	CREATE TABLE fastag_tags (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, vehicle_id TEXT, tag_id TEXT NOT NULL,
		vehicle_number TEXT, issuer TEXT, tag_class TEXT, balance REAL, status TEXT, last_sync DATETIME
	);`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO fastag_transactions 
		(id, tenant_id, tag_id, vehicle_id, vehicle_number, trip_id, plaza_id, plaza_name, amount, txn_timestamp, status, source, reconciled, kharcha_id)
		VALUES 
		('tx-1', 't-1', 'tag-1', 'veh-1', 'DL01A1111', 'trip-1', 'p-1', 'Plaza A', 150.0, ?, 'SUCCESS', 'PROVIDER', 1, 'kh-1'),
		('tx-2', 't-1', 'tag-2', NULL, 'DL02B2222', NULL, 'p-2', 'Plaza B', 250.0, ?, 'SUCCESS', 'PROVIDER', 0, NULL);`,
		now, now.Add(-time.Hour))
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO fastag_tags 
		(id, tenant_id, vehicle_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync)
		VALUES 
		('tag-row-1', 't-1', 'veh-1', 'tag-1', 'DL01A1111', 'ICICI', 'VC4', 1200.0, 'ACTIVE', ?),
		('tag-row-2', 't-1', NULL, 'tag-2', 'DL02B2222', 'PAYTM', 'VC4', 450.0, 'ACTIVE', NULL);`,
		now)
	require.NoError(t, err)

	s := NewFASTagService(db, nil, Config{})
	ctx := context.Background()

	// ListTransactions with filter
	txs, err := s.ListTransactions(ctx, "DL01A1111", 10)
	require.NoError(t, err)
	assert.Len(t, txs, 1)
	assert.Equal(t, "tx-1", txs[0].ID)
	assert.True(t, txs[0].Reconciled)
	assert.NotNil(t, txs[0].VehicleID)
	assert.NotNil(t, txs[0].TripID)
	assert.NotNil(t, txs[0].KharchaID)

	// ListTransactions without filter (all)
	allTxs, err := s.ListTransactions(ctx, "", 0)
	require.NoError(t, err)
	assert.Len(t, allTxs, 2)

	// ListTags
	tags, err := s.ListTags(ctx)
	require.NoError(t, err)
	assert.Len(t, tags, 2)
	assert.Equal(t, "DL01A1111", tags[0].VehicleNumber)
	assert.Equal(t, "ICICI", tags[0].Issuer)
	assert.NotEqual(t, "-", tags[0].LastSyncStr)
	assert.Equal(t, "DL02B2222", tags[1].VehicleNumber)
	assert.Equal(t, "-", tags[1].LastSyncStr)
}
