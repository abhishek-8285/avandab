package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/settlement/application"
	"transport-app/internal/settlement/domain"
	settleSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
	"transport-app/internal/shared"
)

func setupSettlementHandlerDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_settle_hdl_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Fleet Tenant 1');

	CREATE TABLE drivers (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, first_name TEXT NOT NULL,
		last_name TEXT, phone TEXT, email TEXT, bank_details TEXT
	);
	INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, email, bank_details)
	VALUES ('drv-1', 'tenant-1', 'Sunil', 'Sharma', '9876543210', 'sunil@test.com', '{"account":"1122334455"}');

	CREATE TABLE driver_payout_accounts (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, driver_id TEXT NOT NULL,
		account_holder_name TEXT NOT NULL DEFAULT '', account_number_encrypted TEXT NOT NULL DEFAULT '',
		account_number_masked TEXT NOT NULL DEFAULT '', ifsc_code TEXT NOT NULL DEFAULT '',
		bank_name TEXT NOT NULL DEFAULT '', is_primary INTEGER NOT NULL DEFAULT 1,
		verification_status TEXT NOT NULL DEFAULT 'unverified', created_at DATETIME DEFAULT (datetime('now'))
	);
	INSERT INTO driver_payout_accounts (id, tenant_id, driver_id, verification_status, is_primary)
	VALUES ('acc-1', 'tenant-1', 'drv-1', 'verified', 1);

	CREATE TABLE driver_settlements (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, trip_id TEXT NOT NULL, driver_id TEXT NOT NULL,
		gross_fare REAL NOT NULL, deductions REAL NOT NULL, net_payout REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'calculated', commission_rate REAL, commission_amount REAL,
		toll_adjustment REAL, advance_deductions REAL, tds_rate REAL, tds_amount REAL,
		created_at DATETIME DEFAULT (datetime('now')), updated_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE driver_ledger_entries (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, driver_id TEXT NOT NULL, trip_id TEXT,
		entry_type TEXT NOT NULL, amount REAL NOT NULL, currency TEXT NOT NULL DEFAULT 'INR',
		reference_type TEXT NOT NULL, reference_id TEXT NOT NULL, balance_after REAL NOT NULL,
		description TEXT, created_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE payout_instructions (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, driver_id TEXT NOT NULL,
		payout_account_id TEXT NOT NULL, amount REAL NOT NULL, currency TEXT NOT NULL DEFAULT 'INR',
		status TEXT NOT NULL DEFAULT 'pending', payout_rail TEXT,
		idempotency_key TEXT UNIQUE, provider_payout_id TEXT, failure_reason TEXT,
		utr TEXT, initiated_at DATETIME, completed_at DATETIME,
		created_at DATETIME DEFAULT (datetime('now')), updated_at DATETIME DEFAULT (datetime('now'))
	);

	CREATE TABLE provider_events (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, provider TEXT NOT NULL,
		provider_event_id TEXT NOT NULL UNIQUE, event_type TEXT NOT NULL, payload TEXT,
		status TEXT DEFAULT 'processed', processed_at DATETIME DEFAULT (datetime('now'))
	);
	`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSettlementHandler_RegisterRoutes(t *testing.T) {
	r := chi.NewRouter()
	h := NewSettlementHandler(nil)
	h.RegisterRoutes(r)

	// Verify route registration
	routes := r.Routes()
	require.NotEmpty(t, routes)
}

func TestSettlementHandler_CalculateSettlement(t *testing.T) {
	db := setupSettlementHandlerDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	h := NewSettlementHandler(svc)

	// 1. Unauthorized (no tenant)
	req := httptest.NewRequest("POST", "/api/v1/settlements/calculate", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()
	h.CalculateSettlement(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 2. Bad JSON
	req = httptest.NewRequest("POST", "/api/v1/settlements/calculate", bytes.NewBufferString("invalid json"))
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "drv-1"})
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.CalculateSettlement(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Success
	calcReq := application.CalculateSettlementRequest{
		TripID:            "trip-101",
		DriverID:          "drv-1",
		GrossFare:         10000.0,
		AdvanceDeductions: 500.0,
		TollAdjustment:    200.0,
	}
	body, _ := json.Marshal(calcReq)
	req = httptest.NewRequest("POST", "/api/v1/settlements/calculate", bytes.NewBuffer(body))
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.CalculateSettlement(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp domain.Settlement
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "trip-101", resp.TripID)
	assert.Equal(t, "drv-1", resp.DriverID)
	assert.Equal(t, 10000.0, resp.GrossFare)
}

func TestSettlementHandler_GetDriverWallet(t *testing.T) {
	db := setupSettlementHandlerDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	h := NewSettlementHandler(svc)

	// 1. Unauthorized
	req := httptest.NewRequest("GET", "/api/v1/drivers/me/wallet", nil)
	w := httptest.NewRecorder()
	h.GetDriverWallet(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 2. Success (initial zero wallet balance)
	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "drv-1"})
	req = httptest.NewRequest("GET", "/api/v1/drivers/me/wallet", nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.GetDriverWallet(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var wallet domain.DriverWallet
	require.NoError(t, json.NewDecoder(w.Body).Decode(&wallet))
	assert.Equal(t, "drv-1", wallet.DriverID)
	assert.Equal(t, 0.0, wallet.AvailableBalance)
}

func TestSettlementHandler_InitiatePayout(t *testing.T) {
	db := setupSettlementHandlerDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	svc.SetPayoutRailEnabled(true)
	h := NewSettlementHandler(svc)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "drv-1"})

	// First credit wallet so driver has balance
	_, err := svc.CalculateAndCreateSettlement(ctx, "tenant-1", application.CalculateSettlementRequest{
		TripID:    "trip-wallet-credit",
		DriverID:  "drv-1",
		GrossFare: 5000.0,
	})
	require.NoError(t, err)

	// 1. Unauthorized
	req := httptest.NewRequest("POST", "/api/v1/drivers/me/payouts", nil)
	w := httptest.NewRecorder()
	h.InitiatePayout(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 2. Bad JSON
	req = httptest.NewRequest("POST", "/api/v1/drivers/me/payouts", bytes.NewBufferString("not json"))
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.InitiatePayout(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 3. Valid Payout Creation (201 Created)
	payoutReq := application.InitiatePayoutRequest{
		Amount:         500.0,
		IdempotencyKey: "idemp-key-101",
	}
	body, _ := json.Marshal(payoutReq)
	req = httptest.NewRequest("POST", "/api/v1/drivers/me/payouts", bytes.NewBuffer(body))
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.InitiatePayout(w, req)
	if w.Code != http.StatusCreated {
		t.Logf("InitiatePayout body: %s", w.Body.String())
	}
	assert.Equal(t, http.StatusCreated, w.Code)

	// 4. Duplicate Payout with same idempotency key (200 OK)
	reqDup := httptest.NewRequest("POST", "/api/v1/drivers/me/payouts", bytes.NewBuffer(body))
	reqDup = reqDup.WithContext(ctx)
	wDup := httptest.NewRecorder()
	h.InitiatePayout(wDup, reqDup)
	assert.Equal(t, http.StatusOK, wDup.Code)

	// 5. Unprocessable (below min limit)
	smallReq := application.InitiatePayoutRequest{
		Amount: 10.0, // below 100.0 limit
	}
	bodySmall, _ := json.Marshal(smallReq)
	reqSmall := httptest.NewRequest("POST", "/api/v1/drivers/me/payouts", bytes.NewBuffer(bodySmall))
	reqSmall = reqSmall.WithContext(ctx)
	wSmall := httptest.NewRecorder()
	h.InitiatePayout(wSmall, reqSmall)
	assert.Equal(t, http.StatusUnprocessableEntity, wSmall.Code)
}

func TestSettlementHandler_RazorpayPayoutWebhook(t *testing.T) {
	db := setupSettlementHandlerDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	webhookSecret := "webhook_test_secret"
	svc := application.NewSettlementAppService(repo, webhookSecret, 100.0)
	h := NewSettlementHandler(svc)

	ctx := shared.ContextWithTenantID(context.Background(), "tenant-1")

	// 1. Missing header / no event id
	req := httptest.NewRequest("POST", "/api/v1/webhooks/payouts/razorpay", bytes.NewBufferString("{}"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.RazorpayPayoutWebhook(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 2. Webhook secret unconfigured
	svcNoSecret := application.NewSettlementAppService(repo, "", 100.0)
	hNoSecret := NewSettlementHandler(svcNoSecret)
	reqNoSecret := httptest.NewRequest("POST", "/api/v1/webhooks/payouts/razorpay", bytes.NewBufferString(`{"event":"payout.processed"}`))
	reqNoSecret.Header.Set("X-Razorpay-Event-Id", "ev-123")
	reqNoSecret = reqNoSecret.WithContext(ctx)
	wNoSecret := httptest.NewRecorder()
	hNoSecret.RazorpayPayoutWebhook(wNoSecret, reqNoSecret)
	assert.Equal(t, http.StatusServiceUnavailable, wNoSecret.Code)

	// 3. Invalid signature
	payload := `{"event":"payout.processed","payload":{"payout":{"entity":{"id":"pout_123","status":"processed"}}}}`
	reqBadSig := httptest.NewRequest("POST", "/api/v1/webhooks/payouts/razorpay", bytes.NewBufferString(payload))
	reqBadSig.Header.Set("X-Razorpay-Event-Id", "ev-123")
	reqBadSig.Header.Set("X-Razorpay-Signature", "invalid_sig")
	reqBadSig = reqBadSig.WithContext(ctx)
	wBadSig := httptest.NewRecorder()
	h.RazorpayPayoutWebhook(wBadSig, reqBadSig)
	assert.Equal(t, http.StatusBadRequest, wBadSig.Code)

	// 4. Valid signature & event
	_, err := db.Exec(`INSERT INTO payout_instructions (id, tenant_id, driver_id, payout_account_id, amount, status, payout_rail, idempotency_key, initiated_at)
		VALUES ('pout-inst-123', 'tenant-1', 'drv-1', 'acc-1', 500.0, 'initiated', 'razorpay', 'idemp-wh-123', datetime('now'))`)
	require.NoError(t, err)

	validPayload := `{"event":"payout.processed","payload":{"payout":{"entity":{"id":"pout_rzp_999","reference_id":"pout-inst-123","status":"processed","amount":500}}}}`

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(validPayload))
	validSig := hex.EncodeToString(mac.Sum(nil))

	reqValid := httptest.NewRequest("POST", "/api/v1/webhooks/payouts/razorpay", bytes.NewBufferString(validPayload))
	reqValid.Header.Set("X-Razorpay-Event-Id", "ev-123")
	reqValid.Header.Set("X-Razorpay-Signature", validSig)
	reqValid = reqValid.WithContext(ctx)
	wValid := httptest.NewRecorder()
	h.RazorpayPayoutWebhook(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Logf("RazorpayPayoutWebhook body: %s", wValid.Body.String())
	}
	assert.Equal(t, http.StatusOK, wValid.Code)
}
