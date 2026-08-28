package test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	bookingApp "transport-app/internal/booking/application"
	bookingHandlers "transport-app/internal/booking/presentation/api/handlers"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// stubAuthSvc bypasses Casbin in tests; allows all permissions.
type stubAuthSvc struct{}

func (s *stubAuthSvc) Can(_ string, _, _ string) bool    { return true }
func (s *stubAuthSvc) Reload() error                     { return nil }
func (s *stubAuthSvc) AddRoleForUser(_, _ string) error  { return nil }
func (s *stubAuthSvc) DeleteRolesForUser(_ string) error { return nil }

// authInjectMiddleware injects a test session into the request context.
func authInjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), auth.ContextUser, &auth.SessionData{
			UserID: "test-user-1",
			Role:   "admin",
		})
		ctx = context.WithValue(ctx, auth.ContextIP, "192.0.2.1")
		ctx = shared.ContextWithTenantID(ctx, "1")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bookingEnv holds the router, DB, services, and UoW — all sharing one DB instance.
type bookingEnv struct {
	Router   http.Handler
	DB       *sql.DB
	Services *service.Services
}

func setupBookingHTTPTest(t *testing.T) *bookingEnv {
	t.Helper()
	db := NewTestDB(t)
	sqlUoW := uow.NewSQLUnitOfWork(db)
	realClock := clock.NewRealClock()
	idGen := id.NewUUIDGenerator()
	authSvc := &stubAuthSvc{}

	h := bookingHandlers.NewAPIBookingHandler(
		bookingApp.NewCreateBookingUseCase(sqlUoW, idGen, realClock),
		bookingApp.NewConfirmBookingUseCase(sqlUoW, realClock),
		bookingApp.NewCancelBookingUseCase(sqlUoW, realClock),
		bookingApp.NewUpdateBookingUseCase(sqlUoW),
		bookingApp.NewCompleteBookingUseCase(sqlUoW, realClock),
		bookingApp.NewDeleteBookingUseCase(sqlUoW),
		bookingApp.NewGetBookingUseCase(sqlUoW),
		bookingApp.NewListBookingsUseCase(sqlUoW),
		authSvc,
	)

	r := chi.NewRouter()
	r.Use(authInjectMiddleware)
	h.Register(r)

	return &bookingEnv{
		Router:   r,
		DB:       db,
		Services: NewTestServices(t, db),
	}
}

func doRequest(t *testing.T, h http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func mustParseJSON(t *testing.T, b []byte, v interface{}) {
	t.Helper()
	require.NoError(t, json.Unmarshal(b, v))
}

// TestBookingHTTP_CreateConfirmViewCancel exercises the full booking lifecycle
// through the HTTP transport layer — POST, GET, POST confirm, GET, POST cancel, GET.
func TestBookingHTTP_CreateConfirmViewCancel(t *testing.T) {
	env := setupBookingHTTPTest(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1") // tenant-scoped repo seam (fail-closed)

	customer, err := env.Services.Customers.CreateCustomer(ctx, "Test Co", "TCO", "555-0001", "tc@example.com", "", "", "")
	require.NoError(t, err)
	route, err := env.Services.Routes.CreateRoute(ctx, "Mumbai", "Delhi", 1400, 24, 15000, "")
	require.NoError(t, err)

	// 1. Create
	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Truck",
		"passengers":   2,
		"price":        15000.0,
		"notes":        "fragile",
	})
	require.Equal(t, http.StatusCreated, rr.Code, "body: %s", rr.Body.String())

	var createResp struct {
		ID string `json:"id"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &createResp)
	assert.NotEmpty(t, createResp.ID)
	bid := createResp.ID

	// 2. View — draft
	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+bid, nil)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())
	var detail struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &detail)
	assert.Equal(t, "pending", detail.Status)

	// 3. Confirm
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+bid+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	// 4. Verify confirmed
	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+bid, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	mustParseJSON(t, rr.Body.Bytes(), &detail)
	assert.Equal(t, "confirmed", detail.Status)

	// 5. List
	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var listResp struct {
		Total int64 `json:"total"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &listResp)
	assert.GreaterOrEqual(t, listResp.Total, int64(1))

	// 6. Cancel
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+bid+"/cancel", nil)
	require.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	// 7. Verify cancelled
	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+bid, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	mustParseJSON(t, rr.Body.Bytes(), &detail)
	assert.Equal(t, "cancelled", detail.Status)
}

func TestBookingHTTP_CompleteWorkflow(t *testing.T) {
	env := setupBookingHTTPTest(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1") // tenant-scoped repo seam (fail-closed)

	customer, _ := env.Services.Customers.CreateCustomer(ctx, "Complete Co", "CC", "555-0005", "cc@example.com", "", "", "")
	route, _ := env.Services.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")

	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Van",
		"passengers":   3,
		"price":        5000.0,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var resp struct {
		ID string `json:"id"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &resp)

	// Complete without confirm → fails
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+resp.ID+"/complete", nil)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// Confirm
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+resp.ID+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	// Complete
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+resp.ID+"/complete", nil)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify completed
	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+resp.ID, nil)
	var detail struct {
		Status string `json:"status"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &detail)
	assert.Equal(t, "completed", detail.Status)
}

func TestBookingHTTP_CreateValidation(t *testing.T) {
	env := setupBookingHTTPTest(t)

	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"route_id": "route-1",
	})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestBookingHTTP_GetNotFound(t *testing.T) {
	env := setupBookingHTTPTest(t)

	rr := doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/nonexistent", nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestBookingHTTP_Delete(t *testing.T) {
	env := setupBookingHTTPTest(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1") // tenant-scoped repo seam (fail-closed)

	customer, _ := env.Services.Customers.CreateCustomer(ctx, "Del Co", "DC", "555-0006", "dc@example.com", "", "", "")
	route, _ := env.Services.Routes.CreateRoute(ctx, "C", "D", 120, 3, 4000, "")

	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Bus",
		"passengers":   10,
		"price":        4000.0,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var resp struct {
		ID string `json:"id"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &resp)

	rr = doRequest(t, env.Router, http.MethodDelete, "/api/v1/bookings/"+resp.ID, nil)
	assert.Equal(t, http.StatusNoContent, rr.Code)

	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+resp.ID, nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestBookingHTTP_AuditLogCreated(t *testing.T) {
	env := setupBookingHTTPTest(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1") // tenant-scoped repo seam (fail-closed)

	customer, _ := env.Services.Customers.CreateCustomer(ctx, "Audit Co", "AC", "555-0007", "ac@example.com", "", "", "")
	route, _ := env.Services.Routes.CreateRoute(ctx, "E", "F", 80, 1, 2000, "")

	// Create
	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Truck",
		"passengers":   2,
		"price":        2000.0,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var resp struct {
		ID string `json:"id"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &resp)

	// Verify create audit log
	var count int
	err := env.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE table_name = 'bookings' AND action = 'create'`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1, "expected at least 1 audit log entry for create")

	// Confirm
	rr = doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings/"+resp.ID+"/confirm", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	// Verify confirm audit log
	err = env.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE table_name = 'bookings' AND action = 'confirm'`).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1, "expected at least 1 audit log entry for confirm")
}

func TestBookingHTTP_Update(t *testing.T) {
	env := setupBookingHTTPTest(t)
	ctx := shared.ContextWithTenantID(context.Background(), "1") // tenant-scoped repo seam (fail-closed)

	customer, _ := env.Services.Customers.CreateCustomer(ctx, "Update Co", "UC", "555-0004", "uc@example.com", "", "", "")
	route, _ := env.Services.Routes.CreateRoute(ctx, "A", "B", 100, 2, 5000, "")

	rr := doRequest(t, env.Router, http.MethodPost, "/api/v1/bookings", map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Truck",
		"passengers":   2,
		"price":        5000.0,
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	var resp struct {
		ID string `json:"id"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &resp)

	rr = doRequest(t, env.Router, http.MethodPut, "/api/v1/bookings/"+resp.ID, map[string]interface{}{
		"customer_id":  string(customer.ID),
		"route_id":     string(route.ID),
		"pickup_date":  time.Now().Add(72 * time.Hour).Format(time.RFC3339),
		"vehicle_type": "Van",
		"passengers":   4,
		"price":        6000.0,
	})
	assert.Equal(t, http.StatusOK, rr.Code, "body: %s", rr.Body.String())

	rr = doRequest(t, env.Router, http.MethodGet, "/api/v1/bookings/"+resp.ID, nil)
	var detail struct {
		VehicleType string `json:"vehicle_type"`
		Passengers  int64  `json:"passengers"`
	}
	mustParseJSON(t, rr.Body.Bytes(), &detail)
	assert.Equal(t, "Van", detail.VehicleType)
	assert.Equal(t, int64(4), detail.Passengers)

	// Verify update audit log was created with the client IP
	var auditCount int
	auditErr := env.DB.QueryRow(
		`SELECT COUNT(*) FROM audit_logs WHERE table_name = 'bookings' AND action = 'update' AND ip_address = '192.0.2.1'`,
	).Scan(&auditCount)
	require.NoError(t, auditErr)
	assert.GreaterOrEqual(t, auditCount, 1, "expected at least 1 audit log entry for update")
}
