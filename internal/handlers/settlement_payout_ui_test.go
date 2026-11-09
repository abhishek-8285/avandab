package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/secure"
	"transport-app/internal/service"
	settlementApp "transport-app/internal/settlement/application"
	settlementSQL "transport-app/internal/settlement/infrastructure/persistence/sql"
	"transport-app/internal/settlement/payout"
	"transport-app/internal/shared"
)

type uiFakePayoutProvider struct{}

func (uiFakePayoutProvider) CreatePayout(context.Context, payout.CreateRequest) (*payout.CreateResult, error) {
	return &payout.CreateResult{Status: payout.StatusProcessing}, nil
}

func (uiFakePayoutProvider) VerifyBankAccount(context.Context, payout.VerifyRequest) (*payout.VerifyResult, error) {
	return &payout.VerifyResult{FundAccountID: "fa_ui_1", Status: "created"}, nil
}

func TestSettlementUI_PayoutRailSectionAndVerification(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	db := newSettlementUITestDB(t)
	repo := sqlite.NewRepository(db)
	cfg := &config.Config{AppEnv: "testing", Port: "8080"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, events.NewInMemoryBus())
	authSvc := &mockAuthSvc{allowed: map[string]bool{
		"u-1:settlements:read":  true,
		"u-1:settlements:write": true,
	}}
	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)
	app := &App{Config: cfg, Templates: tmpl, AuthSrv: authSvc, Services: services, DB: db}
	settleHandler := NewSettlementHandlers(app, services.Settlements, authSvc)
	app.Settlements = settleHandler

	// Payout rail service with a fake provider.
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	settleAppSvc := settlementApp.NewSettlementAppService(settlementSQL.NewSQLSettlementRepository(db), "secret", 100.0)
	settleAppSvc.SetPayoutProvider(uiFakePayoutProvider{})
	settleAppSvc.SetPayoutRailEnabled(true)
	settleHandler.SetPayoutService(settleAppSvc)

	// Seed a completed trip and generate its settlement.
	_, err = db.ExecContext(ctx, `
		INSERT INTO customers (id, name, phone) VALUES ('cust-ui', 'Test Customer', '9876543210');
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('drv-ui', 'DRVUI', 'Rajesh', 'Kumar', '9876543211', 'DL12345', '2030-01-01', 'available', '1');
		INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id) VALUES ('veh-ui', 'MH12UI1234', 'VUI', 'truck', 10, '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1');
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-ui', 'Mumbai', 'Pune', 150, 4, 15000);
		INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk-ui', 'BK-UI', 'cust-ui', '2026-08-19 10:00:00', 'rt-ui', 'truck', 25000, 'confirmed', '1');
		INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id, departure_time, status, tenant_id) VALUES ('trip-ui', 'TRP-UI', 'bk-ui', 'drv-ui', 'veh-ui', 'rt-ui', '2026-08-19 10:00:00', 'completed', '1');
	`)
	require.NoError(t, err)
	rec, err := services.Settlements.GenerateSettlement(ctx, "trip-ui", false)
	require.NoError(t, err)

	// Seed the driver's unverified bank account + one payout instruction.
	t.Setenv("PAYOUT_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	enc, err := secure.EncryptAccountNumber("1122334455")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO driver_payout_accounts
		(id, tenant_id, driver_id, account_holder_name, account_number_encrypted, account_number_masked, ifsc_code, bank_name, is_primary, verification_status)
		VALUES ('acc-ui', '1', 'drv-ui', 'Rajesh Kumar', ?, 'XXXX4455', 'SBIN0001234', 'Test Bank', 1, 'unverified')`, enc)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO payout_instructions
		(id, tenant_id, driver_id, payout_account_id, amount, currency, idempotency_key, status, initiated_at, created_at, updated_at)
		VALUES ('pout-ui', '1', 'drv-ui', 'acc-ui', 500.00, 'INR', 'idem-ui', 'processing', datetime('now'), datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			c = context.WithValue(c, auth.ContextUser, &auth.SessionData{UserID: "u-1", Role: "admin"})
			next.ServeHTTP(w, req.WithContext(c))
		})
	})
	settleHandler.Mount(r)

	// 1. View page renders the payout rail card.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/settlements/"+rec.ID, nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Payout Instructions")
	assert.Contains(t, body, "Not verified")
	assert.Contains(t, body, "Start penny-drop verification")
	assert.Contains(t, body, "₹500.00")

	// 2. Admin starts penny-drop; the account moves to pending with a reference.
	form := url.Values{}
	form.Set("driver_id", "drv-ui")
	form.Set("settlement_id", rec.ID)
	req := httptest.NewRequest("POST", "/settlements/payouts/verify-account", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	assert.Equal(t, http.StatusSeeOther, w2.Code)

	var status, ref string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT verification_status, COALESCE(verification_reference,'') FROM driver_payout_accounts WHERE id = 'acc-ui'`).Scan(&status, &ref))
	assert.Equal(t, "penny_drop_pending", status)
	assert.Equal(t, "fa_ui_1", ref)
}
