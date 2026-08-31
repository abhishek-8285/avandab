package application_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
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

func setupSettlementTestDB(t *testing.T) *sql.DB {
	// Use shared in-memory DB with WAL + busy timeout so 10 concurrent workers share same schema and serialize correctly
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_pragma=journal_mode(WAL)&_pragma=busy_timeout=5000", t.Name(), time.Now().UnixNano())
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	schema := `
	CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
	INSERT INTO tenants (id, name) VALUES ('tenant-1', 'Fleet Tenant 1'), ('tenant-2', 'Fleet Tenant 2');

	CREATE TABLE drivers (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		first_name TEXT NOT NULL,
		last_name TEXT,
		phone TEXT,
		email TEXT,
		bank_details TEXT
	);
	INSERT INTO drivers (id, tenant_id, first_name, last_name, phone, email, bank_details)
	VALUES 
	('drv-settle-1', 'tenant-1', 'Sunil', 'Sharma', '9876543210', 'sunil@test.com', '{"account":"1122334455"}'),
	('drv-unverified', 'tenant-1', 'Amit', 'Verma', '9876543211', 'amit@test.com', '');

	CREATE TABLE driver_payout_accounts (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		driver_id TEXT NOT NULL,
		account_type TEXT NOT NULL,
		account_number TEXT NOT NULL,
		ifsc TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'verified',
		is_primary INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT (datetime('now'))
	);
	INSERT INTO driver_payout_accounts (id, tenant_id, driver_id, account_type, account_number, ifsc, status, is_primary)
	VALUES ('acc-1', 'tenant-1', 'drv-settle-1', 'bank_account', '1122334455', 'HDFC0001234', 'verified', 1);

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

	_, err = db.Exec(schema)
	require.NoError(t, err)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPhase8_SettlementCalculationAndImmutableLedger(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret_key_123", 100.0)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-settle-101"

	// 1. Calculate & Create Settlement
	// Gross ₹2,000, Toll ₹150, Advance ₹300, Commission 10% (₹200), TDS 1% (₹18)
	// Net: 2000 - 200 + 150 - 300 - 18 = ₹1,632
	req := application.CalculateSettlementRequest{
		TripID:            tripID,
		DriverID:          driverID,
		GrossFare:         2000.0,
		TollAdjustment:    150.0,
		AdvanceDeductions: 300.0,
		CommissionRate:    0.10,
		TDSRate:           0.01,
	}

	settlement, err := svc.CalculateAndCreateSettlement(ctx, tenantID, req)
	require.NoError(t, err)
	assert.Equal(t, 2000.0, settlement.GrossFare)
	assert.Equal(t, 200.0, settlement.CommissionAmount)
	assert.Equal(t, 150.0, settlement.TollAdjustment)
	assert.Equal(t, 300.0, settlement.AdvanceDeductions)
	assert.Equal(t, 18.0, settlement.TDSAmount)
	assert.Equal(t, 1632.0, settlement.NetPayout)

	// Invariant: Concurrency / Idempotency - Repeat settlement creation produces the identical record
	repeatSettlement, err := svc.CalculateAndCreateSettlement(ctx, tenantID, req)
	require.NoError(t, err)
	assert.Equal(t, settlement.ID, repeatSettlement.ID)

	// 2. Authoritative Driver Wallet Balance
	wallet, err := svc.GetDriverWallet(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, 1632.0, wallet.AvailableBalance)
	assert.Equal(t, 0.0, wallet.HeldBalance)
	assert.True(t, len(wallet.RecentEntries) >= 4) // Gross, Commission, Toll, Advance, TDS

	// 3. Payout Disbursement: Request ₹1,000 Payout
	payoutReq := application.InitiatePayoutRequest{
		IdempotencyKey: "idemp_payout_001",
		Amount:         1000.0,
	}
	payoutResp, err := svc.InitiatePayout(ctx, tenantID, driverID, payoutReq)
	require.NoError(t, err)
	assert.NotEmpty(t, payoutResp.PayoutID)
	assert.Equal(t, domain.PayoutInitiated, payoutResp.Status)
	assert.False(t, payoutResp.IsDuplicate)

	// Invariant: Duplicate Payout Command is Idempotent
	payoutRespDup, err := svc.InitiatePayout(ctx, tenantID, driverID, payoutReq)
	require.NoError(t, err)
	assert.Equal(t, payoutResp.PayoutID, payoutRespDup.PayoutID)
	assert.True(t, payoutRespDup.IsDuplicate)

	// Available balance is immediately debited (₹1,632 - ₹1,000 = ₹632)
	walletAfterPayout, err := svc.GetDriverWallet(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, 632.0, walletAfterPayout.AvailableBalance)

	// Invariant: Unverified bank account cannot disburse
	_, err = svc.InitiatePayout(ctx, tenantID, "drv-unverified", application.InitiatePayoutRequest{
		IdempotencyKey: "idemp_unverified",
		Amount:         500.0,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unverified")

	// Invariant: Payout exceeding available balance is rejected
	_, err = svc.InitiatePayout(ctx, tenantID, driverID, application.InitiatePayoutRequest{
		IdempotencyKey: "idemp_exceed",
		Amount:         99999.0,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")

	// 4. Provider Webhook: Razorpay Payout Processed (Success)
	webhookSuccessPayload := fmt.Sprintf(`{
		"event": "payout.processed",
		"payload": {
			"payout": {
				"entity": {
					"id": "pout_rzp_12345",
					"amount": 1000,
					"currency": "INR",
					"status": "processed",
					"utr": "UTR1234567890",
					"reference_id": "%s"
				}
			}
		}
	}`, payoutResp.PayoutID)

	calcSig := func(secret, body string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(body))
		return hex.EncodeToString(mac.Sum(nil))
	}

	// Invariant: Missing signature rejected when secret configured
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_success_1", "", []byte(webhookSuccessPayload))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signature")

	// Invariant: Invalid signature rejected
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_success_1", "bad_signature", []byte(webhookSuccessPayload))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid webhook signature")

	// Valid signature passes
	validSig := calcSig("secret_key_123", webhookSuccessPayload)
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_success_1", validSig, []byte(webhookSuccessPayload))
	require.NoError(t, err)

	// Verify payout status updated to PAID
	payoutRecord, err := repo.GetPayoutByID(ctx, tenantID, payoutResp.PayoutID)
	require.NoError(t, err)
	assert.Equal(t, domain.PayoutPaid, payoutRecord.Status)
	assert.Equal(t, "UTR1234567890", *payoutRecord.UTR)

	// Invariant: Illegal status transition (e.g. Paid -> Processing) is blocked
	webhookIllegalTransition := fmt.Sprintf(`{
		"event": "payout.processing",
		"payload": {
			"payout": {
				"entity": {
					"id": "pout_rzp_12345",
					"amount": 1000,
					"currency": "INR",
					"status": "processing",
					"reference_id": "%s"
				}
			}
		}
	}`, payoutResp.PayoutID)
	illegalSig := calcSig("secret_key_123", webhookIllegalTransition)
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_illegal", illegalSig, []byte(webhookIllegalTransition))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "illegal status transition")

	// 5. Invariant: Reversed Payout creates Compensating Ledger Entry
	// Initiate another payout of ₹500
	payout2, err := svc.InitiatePayout(ctx, tenantID, driverID, application.InitiatePayoutRequest{
		IdempotencyKey: "idemp_payout_002",
		Amount:         500.0,
	})
	require.NoError(t, err)

	// Available balance is now ₹632 - ₹500 = ₹132
	walletMid, err := svc.GetDriverWallet(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, 132.0, walletMid.AvailableBalance)

	// Invariant: Amount mismatch rejected
	webhookMismatchPayload := fmt.Sprintf(`{
		"event": "payout.reversed",
		"payload": {
			"payout": {
				"entity": {
					"id": "pout_rzp_67890",
					"amount": 99999,
					"currency": "INR",
					"status": "reversed",
					"error_description": "Beneficiary account frozen",
					"reference_id": "%s"
				}
			}
		}
	}`, payout2.PayoutID)
	mismatchSig := calcSig("secret_key_123", webhookMismatchPayload)
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_mismatch", mismatchSig, []byte(webhookMismatchPayload))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "financial mismatch")

	// Bank Reversal Webhook
	webhookReversalPayload := fmt.Sprintf(`{
		"event": "payout.reversed",
		"payload": {
			"payout": {
				"entity": {
					"id": "pout_rzp_67890",
					"amount": 500,
					"currency": "INR",
					"status": "reversed",
					"error_description": "Beneficiary account frozen",
					"reference_id": "%s"
				}
			}
		}
	}`, payout2.PayoutID)
	reversalSig := calcSig("secret_key_123", webhookReversalPayload)

	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_reversal_1", reversalSig, []byte(webhookReversalPayload))
	require.NoError(t, err)

	// Compensating ledger entry restored the balance (₹132 + ₹500 = ₹632)
	walletRestored, err := svc.GetDriverWallet(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, 632.0, walletRestored.AvailableBalance)

	// Invariant: Duplicate reversal event is idempotent and does not create a duplicate compensating entry
	err = svc.ProcessProviderWebhook(ctx, tenantID, "evt_rzp_reversal_1", reversalSig, []byte(webhookReversalPayload))
	require.NoError(t, err)

	walletStillRestored, err := svc.GetDriverWallet(ctx, tenantID, driverID)
	require.NoError(t, err)
	assert.Equal(t, 632.0, walletStillRestored.AvailableBalance)
}

func TestPhase8_ConcurrentSettlementCreation(t *testing.T) {
	db := setupSettlementTestDB(t)
	repo := settleSQL.NewSQLSettlementRepository(db)
	svc := application.NewSettlementAppService(repo, "secret", 100.0)
	ctx := context.Background()

	tenantID := "tenant-1"
	driverID := "drv-settle-1"
	tripID := "trip-concurrent-999"

	var wg sync.WaitGroup
	workers := 10
	results := make([]*domain.Settlement, workers)
	errorsList := make([]error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := application.CalculateSettlementRequest{
				TripID:         tripID,
				DriverID:       driverID,
				GrossFare:      1500.0,
				CommissionRate: 0.10,
			}
			res, err := svc.CalculateAndCreateSettlement(ctx, tenantID, req)
			results[idx] = res
			errorsList[idx] = err
		}(i)
	}

	wg.Wait()

	// Invariant: Exactly one unique settlement was created across all 10 concurrent workers
	var successCount int
	var settlementID string
	for i := 0; i < workers; i++ {
		if errorsList[i] == nil && results[i] != nil {
			successCount++
			if settlementID == "" {
				settlementID = results[i].ID
			} else {
				assert.Equal(t, settlementID, results[i].ID)
			}
		}
	}
	assert.True(t, successCount > 0)
}
