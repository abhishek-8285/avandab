package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/fuel"
	fuelapp "transport-app/internal/fuel/application"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupFuelCardTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"admin-user:fuel:read":   true,
			"admin-user:fuel:write":  true,
			"viewer-user:fuel:read":  true,
			"viewer-user:fuel:write": false,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		DB:        db,
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
	}

	fuelRepo := fuel.NewSQLFuelCardRepository(db)
	fuelUseCase := fuelapp.NewFuelCardUseCase(fuelRepo)
	fuelHandlers := NewFuelCardHandlers(app, fuelUseCase)
	app.FuelCards = fuelHandlers

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			userID := req.Header.Get("X-Test-User")
			tenant := req.Header.Get("X-Test-Tenant")
			if tenant == "" {
				tenant = string(shared.DefaultTenant)
			}
			ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID(tenant))
			if userID != "" {
				ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
					UserID: userID,
					Role:   "manager",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	fuelHandlers.RegisterAPIRoutes(r)
	return app, r
}

func TestFuelCardsAPI_EndpointsAndRBAC(t *testing.T) {
	app, r := setupFuelCardTestApp(t)

	tenantID := "tenant-fc-api"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ($1, 'Fuel Corp', 'fuel-corp')`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO vehicles (id, tenant_id, vehicle_number, registration_number, vehicle_type, capacity, tank_capacity_litres)
		VALUES ('veh-api-1', $1, 'DL01FC1234', 'DL01FC1234', 'truck', 12000, 250.0)`, tenantID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone)
		VALUES ('drv-api-1', $1, 'DRV-API-1', 'Rajesh', 'Verma', '9812345678')`, tenantID)
	require.NoError(t, err)

	// 1. POST /api/v1/fuel-cards — Forbidden for viewer
	cardPayload := `{
		"card_number": "7102987654321098",
		"provider": "IOCL",
		"assigned_vehicle_id": "veh-api-1",
		"assigned_driver_id": "drv-api-1",
		"daily_spend_limit": 45000
	}`
	reqViewer := httptest.NewRequest(http.MethodPost, "/api/v1/fuel-cards", bytes.NewBufferString(cardPayload))
	reqViewer.Header.Set("Content-Type", "application/json")
	reqViewer.Header.Set("X-Test-User", "viewer-user")
	reqViewer.Header.Set("X-Test-Tenant", tenantID)
	recViewer := httptest.NewRecorder()
	r.ServeHTTP(recViewer, reqViewer)
	assert.Equal(t, http.StatusForbidden, recViewer.Code)

	// 2. POST /api/v1/fuel-cards — Authorized Admin
	reqAdmin := httptest.NewRequest(http.MethodPost, "/api/v1/fuel-cards", bytes.NewBufferString(cardPayload))
	reqAdmin.Header.Set("Content-Type", "application/json")
	reqAdmin.Header.Set("X-Test-User", "admin-user")
	reqAdmin.Header.Set("X-Test-Tenant", tenantID)
	recAdmin := httptest.NewRecorder()
	r.ServeHTTP(recAdmin, reqAdmin)
	assert.Equal(t, http.StatusCreated, recAdmin.Code)

	var createdCard fuel.FuelCard
	err = json.NewDecoder(recAdmin.Body).Decode(&createdCard)
	require.NoError(t, err)
	assert.NotEmpty(t, createdCard.ID)
	assert.Equal(t, "**** **** **** 1098", createdCard.CardNumberMasked)
	assert.Equal(t, fuel.ProviderIOCL, createdCard.Provider)

	// 3. GET /api/v1/fuel-cards — Allowed for viewer and admin
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/fuel-cards", nil)
	reqList.Header.Set("X-Test-User", "viewer-user")
	reqList.Header.Set("X-Test-Tenant", tenantID)
	recList := httptest.NewRecorder()
	r.ServeHTTP(recList, reqList)
	assert.Equal(t, http.StatusOK, recList.Code)

	var listCards []fuel.FuelCard
	err = json.NewDecoder(recList.Body).Decode(&listCards)
	require.NoError(t, err)
	require.Len(t, listCards, 1)
	assert.Equal(t, createdCard.ID, listCards[0].ID)

	// 4. POST /api/v1/fuel-cards/transactions/sync
	now := time.Now().UTC()
	syncPayload := `{
		"transactions": [
			{
				"card_number": "7102987654321098",
				"external_txn_id": "API-TXN-001",
				"txn_time": "` + now.Format(time.RFC3339) + `",
				"fuel_station_name": "IOCL Express Way",
				"fuel_type": "DIESEL",
				"volume_litres": 80.0,
				"rate_per_litre": 92.5,
				"total_amount": 7400.0
			}
		]
	}`
	reqSync := httptest.NewRequest(http.MethodPost, "/api/v1/fuel-cards/transactions/sync", bytes.NewBufferString(syncPayload))
	reqSync.Header.Set("Content-Type", "application/json")
	reqSync.Header.Set("X-Test-User", "admin-user")
	reqSync.Header.Set("X-Test-Tenant", tenantID)
	recSync := httptest.NewRecorder()
	r.ServeHTTP(recSync, reqSync)
	assert.Equal(t, http.StatusOK, recSync.Code)

	var syncRes fuelapp.SyncResult
	err = json.NewDecoder(recSync.Body).Decode(&syncRes)
	require.NoError(t, err)
	assert.Equal(t, 1, syncRes.IngestedCount)
	assert.Equal(t, 1, syncRes.GeneratedCount)
	require.Len(t, syncRes.Transactions, 1)
	txnID := syncRes.Transactions[0].ID

	// 5. POST /api/v1/fuel-cards/transactions/{id}/reconcile
	_, err = app.DB.Exec(`INSERT INTO driver_expenses (id, tenant_id, driver_id, category, expense_type, amount, status, created_at)
		VALUES ('exp-api-manual', $1, 'drv-api-1', 'fuel', 'fuel', 7400.0, 'pending', $2)`, tenantID, now.Format(time.RFC3339))
	require.NoError(t, err)

	reconPayload := `{
		"expense_id": "exp-api-manual",
		"notes": "Reconciled via API integration test"
	}`
	reqRecon := httptest.NewRequest(http.MethodPost, "/api/v1/fuel-cards/transactions/"+txnID+"/reconcile", bytes.NewBufferString(reconPayload))
	reqRecon.Header.Set("Content-Type", "application/json")
	reqRecon.Header.Set("X-Test-User", "admin-user")
	reqRecon.Header.Set("X-Test-Tenant", tenantID)
	recRecon := httptest.NewRecorder()
	r.ServeHTTP(recRecon, reqRecon)
	assert.Equal(t, http.StatusOK, recRecon.Code)

	var reconciledTxn fuel.FuelCardTransaction
	err = json.NewDecoder(recRecon.Body).Decode(&reconciledTxn)
	require.NoError(t, err)
	assert.Equal(t, fuel.ReconStatusMatchedExpense, reconciledTxn.ReconciliationStatus)
	assert.Equal(t, "exp-api-manual", *reconciledTxn.MatchedExpenseID)
}
