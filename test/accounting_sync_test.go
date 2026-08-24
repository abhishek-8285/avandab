package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/events"
	"transport-app/internal/handlers"
	"transport-app/internal/integration/accounting"
	"transport-app/internal/shared"
)

// 1. Test Migration 00050 RoundTrip
func TestAccounting_Migration00050_RoundTrip(t *testing.T) {
	name := fmt.Sprintf("test_acc_mig_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=foreign_keys(OFF)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../db/migrations"))

	// Check tables exist
	for _, tbl := range []string{"accounting_sync_log", "accounting_mapping", "accounting_gl_rule"} {
		var count int
		err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "table %s must exist", tbl)
	}

	// Check GL rules seeded (3 rows)
	var glCount int
	err = db.QueryRow(`SELECT count(*) FROM accounting_gl_rule WHERE id IN ('gl_payout', 'gl_inv', 'gl_tds')`).Scan(&glCount)
	require.NoError(t, err)
	assert.Equal(t, 3, glCount, "3 GL rules must be seeded")

	// Check company_config keys
	var flagCount int
	err = db.QueryRow(`SELECT count(*) FROM company_config WHERE key IN ('accounting_adapter', 'accounting_enabled', 'accounting_endpoint', 'accounting_api_key')`).Scan(&flagCount)
	require.NoError(t, err)
	assert.Equal(t, 4, flagCount, "4 accounting config keys must be seeded")

	// Rollback to 49
	require.NoError(t, goose.DownTo(db, "../db/migrations", 49))

	for _, tbl := range []string{"accounting_sync_log", "accounting_mapping", "accounting_gl_rule"} {
		var count int
		err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count, "table %s must be dropped after rollback", tbl)
	}

	// Re-apply
	require.NoError(t, goose.Up(db, "../db/migrations"))
}

// 2. Test Adapter Factory & Provider Implementations
func TestAccounting_AdapterFactory_And_Providers(t *testing.T) {
	ctx := context.Background()

	// Mock Client
	mockCli := accounting.NewClient(accounting.Config{Provider: "mock", Enabled: false, UseMock: true})
	invRes, err := mockCli.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-101"})
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", invRes.Status)
	assert.Equal(t, "EXT-INV-101", invRes.ExternalID)

	contactRes, err := mockCli.SyncContacts(ctx, []accounting.Contact{{Name: "Test Driver"}})
	require.NoError(t, err)
	assert.Equal(t, 1, contactRes.Synced)

	jeRes, err := mockCli.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "REF-001"})
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", jeRes.Status)
	assert.Contains(t, jeRes.EntryID, "JE-MOCK-")

	// Disabled Real Providers -> ErrDisabled
	for _, provider := range []string{"tally", "zoho", "quickbooks"} {
		cli := accounting.NewClient(accounting.Config{Provider: provider, Enabled: false})
		_, err := cli.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-201"})
		assert.ErrorIs(t, err, accounting.ErrDisabled, "Disabled %s adapter must return ErrDisabled", provider)

		_, err = cli.SyncContacts(ctx, []accounting.Contact{{Name: "Test Contact"}})
		assert.ErrorIs(t, err, accounting.ErrDisabled, "Disabled %s adapter must return ErrDisabled", provider)

		_, err = cli.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "REF-002"})
		assert.ErrorIs(t, err, accounting.ErrDisabled, "Disabled %s adapter must return ErrDisabled", provider)
	}

	// Enabled Real Providers without UseMock -> honest ErrNotImplemented
	// (the adapters have no live HTTP integration; they must never fake success)
	for _, provider := range []string{"tally", "zoho", "quickbooks"} {
		cli := accounting.NewClient(accounting.Config{Provider: provider, Enabled: true})
		_, err := cli.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-301"})
		assert.ErrorIs(t, err, accounting.ErrNotImplemented, "%s adapter without real integration must fail honestly", provider)

		_, err = cli.SyncContacts(ctx, []accounting.Contact{{Name: "Test Contact"}})
		assert.ErrorIs(t, err, accounting.ErrNotImplemented)

		_, err = cli.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "REF-X"})
		assert.ErrorIs(t, err, accounting.ErrNotImplemented)
	}

	// Enabled Real Providers in explicit demo mode (UseMock) -> marked mock results
	tallyCli := accounting.NewClient(accounting.Config{Provider: "tally", Enabled: true, UseMock: true})
	tallyInv, err := tallyCli.ExportInvoice(ctx, accounting.ExportedInvoice{InvoiceNumber: "INV-301"})
	require.NoError(t, err)
	assert.Contains(t, tallyInv.ExternalID, "TALLY-INV-INV-301")
	assert.Contains(t, tallyInv.Message, "(mock)")

	zohoCli := accounting.NewClient(accounting.Config{Provider: "zoho", Enabled: true, UseMock: true})
	zohoJE, err := zohoCli.PushJournalEntry(ctx, accounting.JournalEntry{Reference: "REF-ZOHO"})
	require.NoError(t, err)
	assert.Contains(t, zohoJE.EntryID, "ZOHO-JE-")

	qbCli := accounting.NewClient(accounting.Config{Provider: "quickbooks", Enabled: true, UseMock: true})
	qbContacts, err := qbCli.SyncContacts(ctx, []accounting.Contact{{Name: "QB Contact"}})
	require.NoError(t, err)
	assert.Equal(t, 1, qbContacts.Synced)
}

// 3. Test Outbox Consumer, Idempotency, and GL Rule Mapping
func TestAccounting_OutboxConsumer_Idempotency_And_GLRules(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	bus := events.NewInMemoryBus()
	cfg := accounting.Config{Provider: "mock", Enabled: true, UseMock: true}
	client := accounting.NewClient(cfg)
	consumer := accounting.NewConsumer(db, client, cfg)
	consumer.SubscribeEvents(bus)

	// Publish DriverPayoutSettled
	bus.Publish(ctx, events.Event{
		Type: "DriverPayoutSettled",
		Payload: map[string]interface{}{
			"SettlementID": "stl-test-001",
			"DriverID":     "drv-001",
			"NetPayout":    4500.0,
		},
	})

	// Check accounting_sync_log entry
	var status, extID, payloadJSON string
	var attempts int
	err := db.QueryRow(`
		SELECT status, external_id, payload_json, attempts
		FROM accounting_sync_log
		WHERE idempotency_key = 'DriverPayoutSettled:stl-test-001'
	`).Scan(&status, &extID, &payloadJSON, &attempts)
	require.NoError(t, err)
	assert.Equal(t, "acked", status)
	assert.Contains(t, extID, "JE-MOCK-")
	assert.Equal(t, 0, attempts)

	// Check accounting_mapping
	var mappedExtID string
	err = db.QueryRow(`
		SELECT external_id FROM accounting_mapping
		WHERE entity_type = 'payout' AND entity_id = 'stl-test-001'
	`).Scan(&mappedExtID)
	require.NoError(t, err)
	assert.Equal(t, extID, mappedExtID)

	// Re-publish same event -> Idempotency check: row count in sync log remains 1
	bus.Publish(ctx, events.Event{
		Type: "DriverPayoutSettled",
		Payload: map[string]interface{}{
			"SettlementID": "stl-test-001",
			"NetPayout":    4500.0,
		},
	})

	var logCount int
	_ = db.QueryRow(`SELECT count(*) FROM accounting_sync_log WHERE idempotency_key = 'DriverPayoutSettled:stl-test-001'`).Scan(&logCount)
	assert.Equal(t, 1, logCount, "Replayed event must be skipped by idempotency constraint")

	// Publish InvoiceExported
	bus.Publish(ctx, events.Event{
		Type: "InvoiceExported",
		Payload: map[string]interface{}{
			"InvoiceID":     "inv-test-002",
			"InvoiceNumber": "INV-2026-999",
			"CustomerName":  "Acme Corp",
			"TotalAmount":   15000.0,
		},
	})

	var invStatus, invExtID string
	err = db.QueryRow(`
		SELECT status, external_id
		FROM accounting_sync_log
		WHERE idempotency_key = 'InvoiceExported:inv-test-002'
	`).Scan(&invStatus, &invExtID)
	require.NoError(t, err)
	assert.Equal(t, "acked", invStatus)
	assert.Equal(t, "EXT-INV-2026-999", invExtID)
}

// 4. Test TriggerSync, SyncContacts, and Reconciliation
func TestAccounting_TriggerSync_And_Reconciliation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	cfg := accounting.Config{Provider: "mock", Enabled: true, UseMock: true}
	client := accounting.NewClient(cfg)
	consumer := accounting.NewConsumer(db, client, cfg)

	// Seed pending/failed logs
	_, err := db.Exec(`
		INSERT INTO accounting_sync_log (id, idempotency_key, direction, entity_type, entity_id, adapter, payload_json, status, attempts, created_at, updated_at)
		VALUES
		('acc-f1', 'DriverPayoutSettled:stl-retry-1', 'out', 'payout', 'stl-retry-1', 'mock', '{"NetPayout": 2000.0}', 'failed', 1, datetime('now'), datetime('now')),
		('acc-p1', 'InvoiceExported:inv-retry-1', 'out', 'invoice', 'inv-retry-1', 'mock', '{"InvoiceNumber": "INV-RET-1", "TotalAmount": 8000.0}', 'pending', 0, datetime('now'), datetime('now'))
	`)
	require.NoError(t, err)

	// Trigger sync flush
	trigRes, err := consumer.TriggerSync(ctx, 60)
	require.NoError(t, err)
	assert.Equal(t, 2, trigRes.Dispatched)
	assert.Equal(t, 0, trigRes.Failed)

	// Verify both transitioned to acked
	var unackedCount int
	_ = db.QueryRow(`SELECT count(*) FROM accounting_sync_log WHERE status != 'acked'`).Scan(&unackedCount)
	assert.Equal(t, 0, unackedCount)

	// Seed Customer & Driver and Sync Contacts
	_, _ = db.Exec(`INSERT OR REPLACE INTO customers (id, name, email, phone, gst) VALUES ('cust-acc-1', 'Apex Logistics', 'apex@test.com', '9988112233', '27AABCA1234A1Z1')`)
	_, _ = db.Exec(`INSERT OR REPLACE INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status) VALUES ('drv-acc-1', 'DRV-001', 'Suresh', 'Patil', '9876543210', 'DL-MH-1122', '2029-12-31', 'available')`)

	syncRes, err := consumer.SyncContacts(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, syncRes.Synced)

	// Check mapping created
	var mapCount int
	_ = db.QueryRow(`SELECT count(*) FROM accounting_mapping WHERE adapter = 'mock'`).Scan(&mapCount)
	assert.GreaterOrEqual(t, mapCount, 2)

	// Reconcile status check
	recon, err := consumer.Reconcile(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, recon.Total)
	assert.Equal(t, 2, recon.Acked)
	assert.Equal(t, 0, recon.Unacked)
}

// 5. Test Accounting HTTP Endpoints & RBAC
func TestAccounting_API_Endpoints_And_RBAC(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := accounting.Config{Provider: "mock", Enabled: true, UseMock: true}
	client := accounting.NewClient(cfg)
	consumer := accounting.NewConsumer(db, client, cfg)

	// Setup authorized app
	appAuth := &handlers.App{DB: db, AuthSrv: &mockPhase6Auth{allowed: true}}
	accHandler := handlers.NewAccountingHandlers(appAuth, consumer, &mockPhase6Auth{allowed: true})

	r := chi.NewRouter()
	accHandler.Mount(r)

	ctxAdmin := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "admin-1", Role: "admin"})

	// 1. GET /api/accounting/sync/status
	reqStatus := httptest.NewRequest("GET", "/api/accounting/sync/status", nil).WithContext(ctxAdmin)
	recStatus := httptest.NewRecorder()
	r.ServeHTTP(recStatus, reqStatus)
	assert.Equal(t, http.StatusOK, recStatus.Code)

	var statusResp accounting.SyncStatusResponse
	err := json.Unmarshal(recStatus.Body.Bytes(), &statusResp)
	require.NoError(t, err)
	assert.Equal(t, "mock", statusResp.Adapter)
	assert.True(t, statusResp.Enabled)

	// 2. POST /api/accounting/sync/trigger
	reqTrig := httptest.NewRequest("POST", "/api/accounting/sync/trigger", bytes.NewReader([]byte(`{"since_minutes": 30}`))).WithContext(ctxAdmin)
	reqTrig.Header.Set("Content-Type", "application/json")
	recTrig := httptest.NewRecorder()
	r.ServeHTTP(recTrig, reqTrig)
	assert.Equal(t, http.StatusOK, recTrig.Code)

	// 3. POST /api/accounting/contacts/sync
	reqContacts := httptest.NewRequest("POST", "/api/accounting/contacts/sync", nil).WithContext(ctxAdmin)
	recContacts := httptest.NewRecorder()
	r.ServeHTTP(recContacts, reqContacts)
	assert.Equal(t, http.StatusOK, recContacts.Code)

	// 4. GET /api/accounting/reconcile
	reqRecon := httptest.NewRequest("GET", "/api/accounting/reconcile", nil).WithContext(ctxAdmin)
	recRecon := httptest.NewRecorder()
	r.ServeHTTP(recRecon, reqRecon)
	assert.Equal(t, http.StatusOK, recRecon.Code)

	// 5. RBAC Denied -> 403 Forbidden
	appDeny := &handlers.App{DB: db, AuthSrv: &mockPhase6Auth{allowed: false}}
	accHandlerDeny := handlers.NewAccountingHandlers(appDeny, consumer, &mockPhase6Auth{allowed: false})
	rDeny := chi.NewRouter()
	accHandlerDeny.Mount(rDeny)

	ctxGuest := context.WithValue(shared.ContextWithTenantID(context.Background(), "1"), auth.ContextUser, &auth.SessionData{UserID: "guest-1", Role: "guest"})
	reqDeny := httptest.NewRequest("GET", "/api/accounting/sync/status", nil).WithContext(ctxGuest)
	recDeny := httptest.NewRecorder()
	rDeny.ServeHTTP(recDeny, reqDeny)
	assert.Equal(t, http.StatusForbidden, recDeny.Code, "Disallowed user must receive 403 Forbidden")
}
