package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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
	"transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	tripapp "transport-app/internal/trip/application"
	tripapihandlers "transport-app/internal/trip/presentation/api/handlers"
)

func setupMobileAPITestEnv(t *testing.T) (*sql.DB, *App, *tripapihandlers.APITripHandler) {
	name := fmt.Sprintf("test_mobile_api_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
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

	goose.SetLogger(goose.NopLogger())
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(dbConn, migrationsDir))

	cfg := &config.Config{
		AppEnv:    "testing",
		UploadDir: t.TempDir(),
	}

	authSvc := &mockAuthSvc{
		allowed: map[string]bool{
			"u-drv-1:trips:read":   true,
			"u-drv-1:trips:update": true,
			"u-drv-2:trips:read":   true,
			"u-drv-2:trips:update": true,
			"u-drv-A:trips:read":   true,
			"u-drv-A:trips:update": true,
			"u-drv-B:trips:read":   true,
			"u-drv-B:trips:update": true,
			"u-admin-1:trips:read": true,
		},
	}
	tmpl, _ := parseTemplates(authSvc)

	repo := sqlite.NewRepository(dbConn)
	bus := events.NewInMemoryBus()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)

	app := &App{
		DB:        dbConn,
		Config:    cfg,
		Services:  services,
		Templates: tmpl,
		AuthSrv:   authSvc,
	}
	app.Drivers = &DriverHandlers{App: app}
	app.Kharcha = &KharchaHandlers{App: app}

	sqlUoW := uow.NewSQLUnitOfWork(dbConn)
	idGen := id.NewUUIDGenerator()
	realClock := clock.NewRealClock()

	createTrip := tripapp.NewCreateTripUseCase(sqlUoW, idGen, realClock)
	assignDriver := tripapp.NewAssignDriverUseCase(sqlUoW, realClock)
	assignVehicle := tripapp.NewAssignVehicleUseCase(sqlUoW, realClock)
	scheduleTrip := tripapp.NewScheduleTripUseCase(sqlUoW, realClock)
	startTrip := tripapp.NewStartTripUseCase(sqlUoW, realClock)
	reachPickup := tripapp.NewReachPickupUseCase(sqlUoW, realClock)
	startTransit := tripapp.NewStartTransitUseCase(sqlUoW, realClock)
	deliver := tripapp.NewDeliverUseCase(sqlUoW, realClock)
	completeTrip := tripapp.NewCompleteTripUseCase(sqlUoW, realClock)
	cancelTrip := tripapp.NewCancelTripUseCase(sqlUoW, realClock)
	getTrip := tripapp.NewGetTripUseCase(sqlUoW)
	listTrips := tripapp.NewListTripsUseCase(sqlUoW)

	tripAPIHandler := tripapihandlers.NewAPITripHandler(
		createTrip, assignDriver, assignVehicle, scheduleTrip, startTrip,
		reachPickup, startTransit, deliver, completeTrip, cancelTrip,
		getTrip, listTrips, authSvc,
	)

	return dbConn, app, tripAPIHandler
}

func buildTestRouterWithAuth(app *App, tripAPI *tripapihandlers.APITripHandler, currentUser *auth.SessionData) *chi.Mux {
	r := chi.NewRouter()

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := shared.ContextWithTenantID(req.Context(), shared.DefaultTenant)
			if currentUser != nil {
				ctx = context.WithValue(ctx, auth.ContextUser, currentUser)
			}
			req = req.WithContext(ctx)
			next.ServeHTTP(w, req)
		})
	})

	r.Get("/api/v1/drivers/me", app.Drivers.GetMe)
	r.Post("/api/v1/trips/{id}/deliver-pod", app.Kharcha.DeliverWithPOD)
	r.Post("/trips/{id}/deliver-pod", app.Kharcha.DeliverWithPOD)
	r.Post("/api/v1/kharcha/expense", app.Kharcha.CreateExpenseAPI)
	tripAPI.Register(r)

	return r
}

func TestGetDriverMe_Success(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// Seed user and driver
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id)
		VALUES ('u-drv-1', 'Rajesh Kumar', 'rajesh@example.com', 'hash', 5);

		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-1', 'DRV-101', 'Rajesh', 'Kumar', '+919820011223', 'rajesh@example.com', 'DL12345', '2030-01-01', 'on_trip', '1');

		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id)
		VALUES ('v-1', 'VEH-01', 'MH-12-PQ-4521', 'truck', 25, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1');

		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-1', 'Mumbai Central', 'Pune Hub', 150.0, 3.5, 5000.0, '1');

		INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id)
		VALUES ('t-1', 'TRP-8492', 'd-1', 'v-1', 'r-1', '2026-08-19 10:30:00', 'in_transit', '1');

		INSERT INTO vehicle_latest_position (vehicle_id, imei, device_time, latitude, longitude, speed, heading, ignition, tenant_id)
		VALUES ('v-1', 'IMEI-001', '2026-08-19 11:00:00', 18.5204, 73.8567, 45.0, 90.0, 1, '1');
	`)
	require.NoError(t, err)

	user := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r := buildTestRouterWithAuth(app, tripAPI, user)

	req := httptest.NewRequest("GET", "/api/v1/drivers/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, "DRV-101", resp["driver_id"])
	assert.Equal(t, "u-drv-1", resp["user_id"])
	assert.Equal(t, "Rajesh Kumar", resp["name"])
	assert.Equal(t, "+919820011223", resp["phone"])
	assert.Equal(t, "on_trip", resp["status"])
	assert.Equal(t, "MH-12-PQ-4521", resp["vehicle_plate"])

	loc, ok := resp["current_location"].(map[string]interface{})
	require.True(t, ok)
	assert.InDelta(t, 18.5204, loc["latitude"].(float64), 0.0001)
	assert.InDelta(t, 73.8567, loc["longitude"].(float64), 0.0001)
}

func TestGetDriverMe_NotFound(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// Seed user with no linked driver
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id)
		VALUES ('u-admin-1', 'Admin User', 'admin@example.com', 'hash', 1);
	`)
	require.NoError(t, err)

	user := &auth.SessionData{UserID: "u-admin-1", Role: "admin"}
	r := buildTestRouterWithAuth(app, tripAPI, user)

	req := httptest.NewRequest("GET", "/api/v1/drivers/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "driver not found", resp["error"])
}

func TestGetDriverMe_Unauthorized(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// No session context
	r := buildTestRouterWithAuth(app, tripAPI, nil)

	req := httptest.NewRequest("GET", "/api/v1/drivers/me", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "unauthorized", resp["error"])
}

func TestGetTrips_DriverMe_Filter(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// Seed 2 drivers, 2 vehicles, 1 route, and trips for each driver
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id) VALUES 
			('u-drv-A', 'Driver Alpha', 'alpha@example.com', 'hash', 5),
			('u-drv-B', 'Driver Bravo', 'bravo@example.com', 'hash', 5);

		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id) VALUES 
			('d-A', 'DRV-A', 'Driver', 'Alpha', '+919999911111', 'alpha@example.com', 'DL-A', '2030-01-01', 'available', '1'),
			('d-B', 'DRV-B', 'Driver', 'Bravo', '+919999922222', 'bravo@example.com', 'DL-B', '2030-01-01', 'available', '1');

		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id) VALUES 
			('v-A', 'VEH-A', 'MH-01-AA-1111', 'truck', 20, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1'),
			('v-B', 'VEH-B', 'MH-02-BB-2222', 'truck', 20, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1');

		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES 
			('r-1', 'Mumbai Hub', 'Pune Depot', 150.0, 3.5, 5000.0, '1');

		INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id) VALUES 
			('t-A1', 'TRP-A1', 'd-A', 'v-A', 'r-1', '2026-08-19 08:00:00', 'in_transit', '1'),
			('t-A2', 'TRP-A2', 'd-A', 'v-A', 'r-1', '2026-08-18 08:00:00', 'completed', '1'),
			('t-B1', 'TRP-B1', 'd-B', 'v-B', 'r-1', '2026-08-19 09:00:00', 'in_transit', '1');
	`)
	require.NoError(t, err)

	// 1. Query as Driver Alpha with driver_id=me
	userA := &auth.SessionData{UserID: "u-drv-A", Role: "driver"}
	rA := buildTestRouterWithAuth(app, tripAPI, userA)

	reqA := httptest.NewRequest("GET", "/api/v1/trips?driver_id=me", nil)
	recA := httptest.NewRecorder()
	rA.ServeHTTP(recA, reqA)

	assert.Equal(t, http.StatusOK, recA.Code)
	assert.Contains(t, recA.Header().Get("Content-Type"), "application/json")

	var respA struct {
		Trips []struct {
			ID            string `json:"id"`
			TripNumber    string `json:"trip_number"`
			DriverName    string `json:"driver_name"`
			VehiclePlate  string `json:"vehicle_plate"`
			Origin        string `json:"origin"`
			Destination   string `json:"destination"`
			Status        string `json:"status"`
			DepartureTime string `json:"departure_time"`
		} `json:"trips"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(recA.Body.Bytes(), &respA))

	assert.Equal(t, int64(2), respA.Total)
	require.Len(t, respA.Trips, 2)
	assert.Equal(t, "TRP-A1", respA.Trips[0].TripNumber)
	assert.Equal(t, "TRP-A2", respA.Trips[1].TripNumber)
	assert.Equal(t, "Driver Alpha", respA.Trips[0].DriverName)
	assert.Equal(t, "MH-01-AA-1111", respA.Trips[0].VehiclePlate)
	assert.Equal(t, "Mumbai Hub", respA.Trips[0].Origin)
	assert.Equal(t, "Pune Depot", respA.Trips[0].Destination)
	assert.Equal(t, "in_transit", respA.Trips[0].Status)

	// 2. Query as Driver Bravo with driver_id=me
	userB := &auth.SessionData{UserID: "u-drv-B", Role: "driver"}
	rB := buildTestRouterWithAuth(app, tripAPI, userB)

	reqB := httptest.NewRequest("GET", "/api/v1/trips?driver_id=me", nil)
	recB := httptest.NewRecorder()
	rB.ServeHTTP(recB, reqB)

	assert.Equal(t, http.StatusOK, recB.Code)

	var respB struct {
		Trips []struct {
			TripNumber string `json:"trip_number"`
		} `json:"trips"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(recB.Body.Bytes(), &respB))
	assert.Equal(t, int64(1), respB.Total)
	require.Len(t, respB.Trips, 1)
	assert.Equal(t, "TRP-B1", respB.Trips[0].TripNumber)

	// 3. Test pagination: page=1&limit=1 for Driver Alpha
	reqPaginated := httptest.NewRequest("GET", "/api/v1/trips?driver_id=me&page=1&limit=1", nil)
	recPaginated := httptest.NewRecorder()
	rA.ServeHTTP(recPaginated, reqPaginated)

	assert.Equal(t, http.StatusOK, recPaginated.Code)
	var respPaginated struct {
		Trips []struct {
			TripNumber string `json:"trip_number"`
		} `json:"trips"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(recPaginated.Body.Bytes(), &respPaginated))
	assert.Equal(t, int64(2), respPaginated.Total)
	require.Len(t, respPaginated.Trips, 1)

	// 4. Test driver_id=me with user having no linked driver -> 404
	userNoDriver := &auth.SessionData{UserID: "u-admin-1", Role: "admin"}
	rNoDriver := buildTestRouterWithAuth(app, tripAPI, userNoDriver)

	reqNoDriver := httptest.NewRequest("GET", "/api/v1/trips?driver_id=me", nil)
	recNoDriver := httptest.NewRecorder()
	rNoDriver.ServeHTTP(recNoDriver, reqNoDriver)

	assert.Equal(t, http.StatusNotFound, recNoDriver.Code)
	assert.Contains(t, recNoDriver.Header().Get("Content-Type"), "application/json")

	// 5. Test unauthenticated request -> 401
	rUnauth := buildTestRouterWithAuth(app, tripAPI, nil)
	reqUnauth := httptest.NewRequest("GET", "/api/v1/trips?driver_id=me", nil)
	recUnauth := httptest.NewRecorder()
	rUnauth.ServeHTTP(recUnauth, reqUnauth)

	assert.Equal(t, http.StatusUnauthorized, recUnauth.Code)
}

func TestDeliverPOD_SuccessAndErrors(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// Seed driver, vehicle, route, and trip in transit
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id) VALUES 
			('u-drv-1', 'Rajesh Kumar', 'rajesh@example.com', 'hash', 5),
			('u-drv-2', 'Suresh Patel', 'suresh@example.com', 'hash', 5);

		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id) VALUES 
			('d-1', 'DRV-1', 'Rajesh', 'Kumar', '+919820011223', 'rajesh@example.com', 'DL-1', '2030-01-01', 'available', '1'),
			('d-2', 'DRV-2', 'Suresh', 'Patel', '+919820099887', 'suresh@example.com', 'DL-2', '2030-01-01', 'available', '1');

		INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, insurance_expiry, fitness_expiry, permit_expiry, status, tenant_id) VALUES 
			('v-1', 'VEH-1', 'MH-12-PQ-4521', 'truck', 25, 'diesel', '2030-01-01', '2030-01-01', '2030-01-01', 'available', '1');

		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id) VALUES 
			('r-1', 'Mumbai', 'Pune', 150.0, 3.5, 5000.0, '1');

		INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id) VALUES 
			('t-pod-1', 'TRP-POD-101', 'd-1', 'v-1', 'r-1', '2026-08-19 10:00:00', 'in_transit', '1');
	`)
	require.NoError(t, err)

	// Helper to create multipart form request
	createMultipartRequest := func(url string, fields map[string]string, fileField, fileName string, fileContent []byte) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		for k, v := range fields {
			_ = writer.WriteField(k, v)
		}
		if fileField != "" {
			part, err := writer.CreateFormFile(fileField, fileName)
			if err != nil {
				return nil, err
			}
			_, _ = io.Copy(part, bytes.NewReader(fileContent))
		}
		_ = writer.Close()

		req := httptest.NewRequest("POST", url, body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}

	// 1. Success: Assigned driver submits e-POD via /api/v1/trips/{id}/deliver-pod
	user1 := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r1 := buildTestRouterWithAuth(app, tripAPI, user1)

	req1, err := createMultipartRequest(
		"/api/v1/trips/t-pod-1/deliver-pod",
		map[string]string{
			"consignee_name":  "Amit Sharma",
			"consignee_phone": "+919876543210",
			"notes":           "Delivered in good condition",
			"latitude":        "18.5204",
			"longitude":       "73.8567",
		},
		"pod_photo", "pod_receipt.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82"),
	)
	require.NoError(t, err)

	rec1 := httptest.NewRecorder()
	r1.ServeHTTP(rec1, req1)

	assert.Equal(t, http.StatusOK, rec1.Code)
	assert.Contains(t, rec1.Header().Get("Content-Type"), "application/json")

	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	assert.Equal(t, "TRP-POD-101", resp1["trip_number"])
	assert.Equal(t, "delivered", resp1["status"])
	assert.NotEmpty(t, resp1["pod_url"])

	// Verify database trip state updated to delivered
	var tripStatus string
	require.NoError(t, dbConn.QueryRow("SELECT status FROM trips WHERE id = 't-pod-1'").Scan(&tripStatus))
	assert.Equal(t, "delivered", tripStatus)

	// 2. Forbidden: Driver 2 tries to submit e-POD for trip assigned to Driver 1
	// Reset trip status for testing
	_, _ = dbConn.Exec("UPDATE trips SET status = 'in_transit' WHERE id = 't-pod-1'")

	user2 := &auth.SessionData{UserID: "u-drv-2", Role: "driver"}
	r2 := buildTestRouterWithAuth(app, tripAPI, user2)

	req2, err := createMultipartRequest(
		"/api/v1/trips/t-pod-1/deliver-pod",
		map[string]string{"consignee_name": "Unauthorized"},
		"", "", nil,
	)
	require.NoError(t, err)

	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusForbidden, rec2.Code)
	assert.Contains(t, rec2.Header().Get("Content-Type"), "application/json")

	var resp2 map[string]string
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Contains(t, resp2["error"], "forbidden: trip is not assigned to this driver")

	// 3. Not Found: Trip ID does not exist
	req3, _ := createMultipartRequest("/api/v1/trips/non-existent-trip/deliver-pod", map[string]string{"consignee_name": "Test"}, "", "", nil)
	rec3 := httptest.NewRecorder()
	r1.ServeHTTP(rec3, req3)

	assert.Equal(t, http.StatusNotFound, rec3.Code)
	assert.Contains(t, rec3.Header().Get("Content-Type"), "application/json")
	var resp3 map[string]string
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	assert.Equal(t, "trip not found", resp3["error"])

	// 4. Unauthorized: No session context
	rAnon := buildTestRouterWithAuth(app, tripAPI, nil)
	req4, _ := createMultipartRequest("/api/v1/trips/t-pod-1/deliver-pod", map[string]string{"consignee_name": "Test"}, "", "", nil)
	rec4 := httptest.NewRecorder()
	rAnon.ServeHTTP(rec4, req4)

	assert.Equal(t, http.StatusUnauthorized, rec4.Code)
	assert.Contains(t, rec4.Header().Get("Content-Type"), "application/json")
	var resp4 map[string]string
	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &resp4))
	assert.Equal(t, "unauthorized", resp4["error"])
}

func TestDeliverPOD_OTPVerification(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id)
		VALUES ('u-drv-otp', 'OTP Driver', 'otpd@example.com', 'hash', 5);

		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-otp', 'DRV-OTP', 'Ravi', 'Patil', '+919820011224', 'otpd@example.com', 'DL-9', '2030-01-01', 'available', '1');

		INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r-otp', 'Mumbai', 'Nashik', 170.0, 4.0, 6000.0, '1');

		INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id, pod_otp, pod_otp_expires_at)
		VALUES ('t-otp-1', 'TRP-OTP-101', 'd-otp', 'r-otp', '2026-08-19 10:00:00', 'in_transit', '1',
			'419876', '2099-01-01T00:00:00Z');
	`)
	require.NoError(t, err)

	user := &auth.SessionData{UserID: "u-drv-otp", Role: "driver"}
	router := buildTestRouterWithAuth(app, tripAPI, user)

	post := func(fields map[string]string) *httptest.ResponseRecorder {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		for k, v := range fields {
			_ = writer.WriteField(k, v)
		}
		_ = writer.Close()
		req := httptest.NewRequest("POST", "/api/v1/trips/t-otp-1/deliver-pod", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	// Missing OTP → rejected
	w := post(map[string]string{"consignee_name": "Amit"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "OTP required")

	// Wrong OTP → rejected
	w = post(map[string]string{"consignee_name": "Amit", "otp": "000000"})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Correct OTP → delivered + pod_otp_verified finally written
	w = post(map[string]string{"consignee_name": "Amit", "otp": "419876", "pod_signature_data": "data:image/png;base64,iVBORw0KGgo="})
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var verified int
	require.NoError(t, dbConn.QueryRow(`SELECT pod_otp_verified FROM trips WHERE id = 't-otp-1'`).Scan(&verified))
	assert.Equal(t, 1, verified, "pod_otp_verified must be written on verified delivery")
}

func TestCreateExpenseAPI_SuccessAndErrors(t *testing.T) {
	dbConn, app, tripAPI := setupMobileAPITestEnv(t)
	defer dbConn.Close()

	// Seed driver user + linked driver record
	_, err := dbConn.Exec(`
		INSERT INTO users (id, name, email, password_hash, role_id)
		VALUES ('u-drv-1', 'Rajesh Kumar', 'rajesh@example.com', 'hash', 5);

		INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Rajesh', 'Kumar', '+919820011223', 'rajesh@example.com', 'DL-1', '2030-01-01', 'available', '1');
	`)
	require.NoError(t, err)

	createExpenseRequest := func(fields map[string]string, withFile bool) (*http.Request, error) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		for k, v := range fields {
			_ = writer.WriteField(k, v)
		}
		if withFile {
			part, err := writer.CreateFormFile("receipt_photo", "receipt.png")
			if err != nil {
				return nil, err
			}
			_, _ = io.Copy(part, bytes.NewReader([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")))
		}
		_ = writer.Close()
		req := httptest.NewRequest("POST", "/api/v1/kharcha/expense", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}

	user := &auth.SessionData{UserID: "u-drv-1", Role: "driver"}
	r := buildTestRouterWithAuth(app, tripAPI, user)

	// 1. Success: fuel expense with receipt photo
	req1, err := createExpenseRequest(map[string]string{
		"trip_id":      "t-1",
		"type":         "fuel",
		"expense_type": "fuel",
		"amount":       "1500.50",
		"notes":        "Pump bharwai Delhi road",
		"latitude":     "28.6139",
		"longitude":    "77.2090",
	}, true)
	require.NoError(t, err)

	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)

	require.Equal(t, http.StatusCreated, rec1.Code, "body: %s", rec1.Body.String())
	var resp1 map[string]interface{}
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	assert.Equal(t, "created", resp1["status"])
	expenseID1, _ := resp1["id"].(string)
	assert.NotEmpty(t, expenseID1)

	// Verify DB row: driver resolved from session (d-1), receipt uploaded
	var dID, category, status string
	var amount float64
	require.NoError(t, dbConn.QueryRow(
		`SELECT driver_id, category, amount, status FROM driver_expenses WHERE id = ?`,
		expenseID1).Scan(&dID, &category, &amount, &status))
	assert.Equal(t, "d-1", dID)
	assert.Equal(t, "fuel", category)
	assert.InDelta(t, 1500.50, amount, 0.001)
	assert.Equal(t, "pending", status)

	// GPS captured with the claim (migration 00082 columns)
	var lat, lng float64
	require.NoError(t, dbConn.QueryRow(
		`SELECT latitude, longitude FROM driver_expenses WHERE id = ?`,
		expenseID1).Scan(&lat, &lng))
	assert.InDelta(t, 28.6139, lat, 0.0001)
	assert.InDelta(t, 77.2090, lng, 0.0001)

	var receiptURL string
	require.NoError(t, dbConn.QueryRow(
		`SELECT receipt_url FROM driver_expenses WHERE id = ?`, expenseID1).Scan(&receiptURL))
	assert.NotEmpty(t, receiptURL)

	// 2. Invalid category -> 400 with JSON error
	req2, _ := createExpenseRequest(map[string]string{
		"trip_id": "t-1", "type": "booze", "amount": "100",
	}, false)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusBadRequest, rec2.Code)
	var resp2 map[string]string
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Contains(t, resp2["error"], "invalid category")

	// 2b. India categories the mobile app sends (rto/tyre/bhatta) must be
	// accepted — migration 00088 widened the CHECK for exactly this parity.
	for _, cat := range []string{"rto", "tyre", "bhatta"} {
		reqCat, _ := createExpenseRequest(map[string]string{
			"trip_id": "t-1", "type": cat, "expense_type": cat, "amount": "200",
		}, false)
		recCat := httptest.NewRecorder()
		r.ServeHTTP(recCat, reqCat)
		require.Equal(t, http.StatusCreated, recCat.Code, "category %s rejected: %s", cat, recCat.Body.String())
	}

	// 3. Amount <= 0 -> 400
	req3, _ := createExpenseRequest(map[string]string{
		"trip_id": "t-1", "type": "food", "amount": "-5",
	}, false)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusBadRequest, rec3.Code)

	// 4. Unauthorized: no session
	rAnon := buildTestRouterWithAuth(app, tripAPI, nil)
	req4, _ := createExpenseRequest(map[string]string{
		"trip_id": "t-1", "type": "food", "amount": "10",
	}, false)
	rec4 := httptest.NewRecorder()
	rAnon.ServeHTTP(rec4, req4)
	assert.Equal(t, http.StatusUnauthorized, rec4.Code)

	// 5. Idempotent retry returns the same expense id
	idemKey := "mobile-exp-" + fmt.Sprint(time.Now().UnixNano())
	req5a, _ := createExpenseRequest(map[string]string{
		"trip_id": "t-1", "type": "toll", "amount": "200", "idempotency_key": idemKey,
	}, false)
	rec5a := httptest.NewRecorder()
	r.ServeHTTP(rec5a, req5a)
	require.Equal(t, http.StatusCreated, rec5a.Code)
	var resp5a map[string]interface{}
	require.NoError(t, json.Unmarshal(rec5a.Body.Bytes(), &resp5a))

	req5b, _ := createExpenseRequest(map[string]string{
		"trip_id": "t-1", "type": "toll", "amount": "200", "idempotency_key": idemKey,
	}, false)
	rec5b := httptest.NewRecorder()
	r.ServeHTTP(rec5b, req5b)
	require.Equal(t, http.StatusCreated, rec5b.Code)
	var resp5b map[string]interface{}
	require.NoError(t, json.Unmarshal(rec5b.Body.Bytes(), &resp5b))

	assert.Equal(t, resp5a["id"], resp5b["id"])
}
