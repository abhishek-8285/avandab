package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/config"
	"transport-app/internal/events"
	"transport-app/internal/ewaybill"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newSettlementUITestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_settle_ui_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSettlementUI_PagesAndEWayBillCard(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newSettlementUITestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv: "testing",
		Port:   "8080",
		EWayBill: config.EWayBillConfig{
			WorkerEnabled: false,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"u-1:settlements:read":    true,
			"u-1:settlements:write":   true,
			"u-1:settlements:approve": true,
			"u-1:ewaybill:read":       true,
			"u-1:ewaybill:create":     true,
			"u-1:ewaybill:update":     true,
		},
	}

	tmpl, err := parseTemplates(authSvc)
	require.NoError(t, err)

	app := &App{
		Config:    cfg,
		Templates: tmpl,
		AuthSrv:   authSvc,
		Services:  services,
		DB:        db,
	}

	settleHandler := NewSettlementHandlers(app, services.Settlements, authSvc)
	app.Settlements = settleHandler

	ewbClient := intEWB.NewClient(intEWB.Config{Enabled: true, UseMock: true})
	ewbSvc := ewaybill.NewEWayBillService(db, bus, ewbClient, logger, ewaybill.Config{Enabled: true})
	ewbHandler := NewEWayBillHandlers(app, ewbSvc, authSvc)
	app.EWayBill = ewbHandler

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{
				UserID: "u-1",
				Role:   "admin",
			})
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	settleHandler.Mount(r)
	ewbHandler.Mount(r)

	// 1. GET /settlements list page
	reqList := httptest.NewRequest("GET", "/settlements", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)

	assert.Equal(t, http.StatusOK, wList.Code)
	assert.Contains(t, wList.Body.String(), "Driver Settlements")
	assert.Contains(t, wList.Body.String(), "Total Pending Payouts")
	assert.Contains(t, wList.Body.String(), "Average Net Payout")

	// 2. Seed a trip & generate a settlement
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)
	_, err = db.ExecContext(ctx, `
		INSERT INTO customers (id, name, phone) VALUES ('cust-1', 'Test Customer', '9876543210');
		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id) VALUES ('drv-1', 'DRV001', 'Rajesh', 'Kumar', '9876543211', 'DL12345', '2030-01-01', 'available', '1');
		INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id) VALUES ('veh-1', 'MH12AB1234', 'V1', 'truck', 10, '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1');
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-1', 'Mumbai', 'Pune', 150, 4, 15000);
		INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id) VALUES ('bk-1', 'BK-001', 'cust-1', '2026-08-19 10:00:00', 'rt-1', 'truck', 25000, 'confirmed', '1');
		INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id, departure_time, status, tenant_id) VALUES ('trip-1', 'TRP-001', 'bk-1', 'drv-1', 'veh-1', 'rt-1', '2026-08-19 10:00:00', 'completed', '1');
	`)
	require.NoError(t, err)

	rec, err := services.Settlements.GenerateSettlement(ctx, "trip-1", false)
	require.NoError(t, err)
	require.NotEmpty(t, rec.ID)

	// 3. GET /settlements/{id} view page
	reqView := httptest.NewRequest("GET", "/settlements/"+rec.ID, nil)
	wView := httptest.NewRecorder()
	r.ServeHTTP(wView, reqView)

	assert.Equal(t, http.StatusOK, wView.Code)
	assert.Contains(t, wView.Body.String(), "Settlement Information")
	assert.Contains(t, wView.Body.String(), "Financial Ledger Items")
	assert.Contains(t, wView.Body.String(), "Net Payout")
	assert.Contains(t, wView.Body.String(), "Mark as Paid")

	// 4. GET /trips/{id}/ewaybill partial
	reqEWB := httptest.NewRequest("GET", "/trips/trip-1/ewaybill", nil)
	wEWB := httptest.NewRecorder()
	r.ServeHTTP(wEWB, reqEWB)

	assert.Equal(t, http.StatusOK, wEWB.Code)
	assert.Contains(t, wEWB.Body.String(), "E-Way Bill")

	// 5. POST /trips/{id}/ewaybill/generate with mock adapter
	reqGenEWB := httptest.NewRequest("POST", "/trips/trip-1/ewaybill/generate?force=true", nil)
	wGenEWB := httptest.NewRecorder()
	r.ServeHTTP(wGenEWB, reqGenEWB)

	t.Logf("Response body: %s", wGenEWB.Body.String())
	assert.Equal(t, http.StatusSeeOther, wGenEWB.Code)
	assert.Equal(t, "/trips/trip-1", wGenEWB.Header().Get("Location"))
}
