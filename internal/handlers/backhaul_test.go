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
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func setupBackhaulTestApp(t *testing.T) (*App, *chi.Mux) {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}

	db := newReportsTestDB(t)
	repo := sqlite.NewRepository(db)
	bus := events.NewInMemoryBus()
	cfg := &config.Config{
		AppEnv:        "testing",
		Port:          "8080",
		ExportMaxRows: 50000,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"dispatcher-user:trips:read":   true,
			"dispatcher-user:trips:update": true,
			"viewer-user:trips:read":       true,
			"viewer-user:trips:update":     false,
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
	backhaulHandlers := &BackhaulHandlers{App: app, BackhaulSvc: service.NewBackhaulService(db)}
	app.Backhaul = backhaulHandlers

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
					Role:   "dispatcher",
				})
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	backhaulHandlers.RegisterAPIRoutes(r)
	return app, r
}

func TestBackhaulAPI_EndToEnd(t *testing.T) {
	app, r := setupBackhaulTestApp(t)

	tenantID := "tenant-gamma"
	_, err := app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-gamma', 'Gamma Freight', 'gamma')`)
	require.NoError(t, err)

	// Base Facility
	facID := "fac-mumbai-01"
	_, err = app.DB.Exec(`
		INSERT INTO facilities (id, tenant_id, facility_code, name, facility_type, latitude, longitude, is_active)
		VALUES ($1, $2, 'BOM-HUB', 'Mumbai Hub', 'hub', 19.0760, 72.8777, 1)`,
		facID, tenantID)
	require.NoError(t, err)

	// Vehicle & Driver
	vehID := "veh-gamma-01"
	_, err = app.DB.Exec(`
		INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, facility_id)
		VALUES ($1, $2, 'MH01AB9999', 'G-9999', 'truck', 8000, 'diesel', '2028-01-01', '2028-01-01', '2028-01-01', 'available', $3)`,
		vehID, tenantID, facID)
	require.NoError(t, err)

	drvID := "drv-gamma-01"
	_, err = app.DB.Exec(`
		INSERT INTO drivers (id, tenant_id, driver_id, first_name, last_name, phone, status)
		VALUES ($1, $2, 'DRVGAMMA', 'Anil', 'Sharma', '+919123456780', 'available')`,
		drvID, tenantID)
	require.NoError(t, err)

	// Outbound Route Mumbai to Pune
	routeID := "route-bom-pnq"
	_, err = app.DB.Exec(`
		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ($1, 'Mumbai', 'Pune', 150, 3.5, 9000)`,
		routeID)
	require.NoError(t, err)

	_, err = app.DB.Exec(`
		INSERT INTO route_locations (route_id, source_lat, source_lng, source_name, dest_lat, dest_lng, dest_name)
		VALUES ($1, 19.0760, 72.8777, 'Mumbai', 18.5204, 73.8567, 'Pune')`,
		routeID)
	require.NoError(t, err)

	// Active Trip completing in Pune
	tripID := "trip-pune-complete"
	now := time.Now()
	_, err = app.DB.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, driver_id, vehicle_id, route_id, departure_time, arrival_time, status)
		VALUES ($1, $2, 'TRIP-PUNE-99', $3, $4, $5, $6, $7, 'delivered')`,
		tripID, tenantID, drvID, vehID, routeID, now.Add(-5*time.Hour), now.Add(-10*time.Minute))
	require.NoError(t, err)

	_, err = app.DB.Exec(`
		INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, latitude, longitude, status)
		VALUES 
		('stop-g1', $1, $2, 1, 'pickup', 'Mumbai Hub', 'Mumbai', 19.0760, 72.8777, 'completed'),
		('stop-g2', $1, $2, 2, 'drop', 'Pune Hub', 'Pune', 18.5204, 73.8567, 'completed')`,
		tenantID, tripID)
	require.NoError(t, err)

	// Customer
	custID := "cust-gamma"
	_, err = app.DB.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ($1, $2, 'Tata Motors', '+919876543210')`,
		custID, tenantID)
	require.NoError(t, err)

	// Return Booking: Pimpri (near Pune, ~15km) back to Mumbai (homeward)
	bID := "bk-pimpri-mumbai"
	_, err = app.DB.Exec(`
		INSERT INTO bookings (id, tenant_id, booking_number, customer_id, route_id, vehicle_type, cargo_weight, price, pickup_date, status)
		VALUES ($1, $2, 'BK-PIMPRI-01', $3, $4, 'truck', 5000, 18500, $5, 'pending')`,
		bID, tenantID, custID, routeID, now.Add(2*time.Hour))
	require.NoError(t, err)

	_, err = app.DB.Exec(`
		INSERT INTO customer_booking_details (booking_id, tenant_id, pickup_address, pickup_lat, pickup_lng, delivery_address, delivery_lat, delivery_lng)
		VALUES ($1, $2, 'Pimpri Pune', 18.6279, 73.8009, 'Mumbai JNPT', 19.0760, 72.8777)`,
		bID, tenantID)
	require.NoError(t, err)

	// 1. GET /api/v1/backhaul/matches as dispatcher-user
	req := httptest.NewRequest(http.MethodGet, "/api/v1/backhaul/matches?trip_id="+tripID+"&radius_km=50", nil)
	req.Header.Set("X-Test-User", "dispatcher-user")
	req.Header.Set("X-Test-Tenant", tenantID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var matches []service.BackhaulMatchDTO
	err = json.NewDecoder(rec.Body).Decode(&matches)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, bID, matches[0].BookingID)
	assert.True(t, matches[0].DeadheadKm < 25.0)
	assert.Equal(t, 18500.0, matches[0].Price)

	// 2. Missing trip_id query param -> 400 Bad Request
	reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/backhaul/matches", nil)
	reqBad.Header.Set("X-Test-User", "dispatcher-user")
	reqBad.Header.Set("X-Test-Tenant", tenantID)
	recBad := httptest.NewRecorder()
	r.ServeHTTP(recBad, reqBad)
	assert.Equal(t, http.StatusBadRequest, recBad.Code)

	// 3. POST /api/v1/backhaul/offers as dispatcher-user
	offerBody := `{"trip_id": "` + tripID + `", "booking_id": "` + bID + `", "offered_rate": 18500}`
	reqPost := httptest.NewRequest(http.MethodPost, "/api/v1/backhaul/offers", bytes.NewBufferString(offerBody))
	reqPost.Header.Set("Content-Type", "application/json")
	reqPost.Header.Set("X-Test-User", "dispatcher-user")
	reqPost.Header.Set("X-Test-Tenant", tenantID)
	recPost := httptest.NewRecorder()
	r.ServeHTTP(recPost, reqPost)

	assert.Equal(t, http.StatusCreated, recPost.Code)
	var offerResp service.BackhaulOfferResponse
	err = json.NewDecoder(recPost.Body).Decode(&offerResp)
	require.NoError(t, err)
	assert.NotEmpty(t, offerResp.OfferID)
	assert.Equal(t, "offered", offerResp.Status)
	assert.Equal(t, 18500.0, offerResp.OfferedRate)

	// 4. RBAC: viewer-user cannot POST offers -> 403 Forbidden
	reqViewer := httptest.NewRequest(http.MethodPost, "/api/v1/backhaul/offers", bytes.NewBufferString(offerBody))
	reqViewer.Header.Set("Content-Type", "application/json")
	reqViewer.Header.Set("X-Test-User", "viewer-user")
	reqViewer.Header.Set("X-Test-Tenant", tenantID)
	recViewer := httptest.NewRecorder()
	r.ServeHTTP(recViewer, reqViewer)
	assert.Equal(t, http.StatusForbidden, recViewer.Code)
}
