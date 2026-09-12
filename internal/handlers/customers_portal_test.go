package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/shared"
)

func portalAccessRouter(t *testing.T) (*App, chi.Router) {
	t.Helper()
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, nil)
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)
	return app, r
}

func seedPortalCustomer(t *testing.T, app *App) string {
	t.Helper()
	res, err := app.DB.Exec(`INSERT INTO customers (id, name, email, phone, address) VALUES ('cust-portal-1', 'Apex Logistics', 'apex@x.com', '9999', 'Pune')`)
	require.NoError(t, err)
	rows, _ := res.RowsAffected()
	require.Equal(t, int64(1), rows)
	return "cust-portal-1"
}

func adminCtx(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(
		shared.ContextWithTenantID(r.Context(), "1"),
		auth.ContextUser,
		&auth.SessionData{UserID: "admin-1", Role: "admin"},
	))
}

func postPortalForm(r chi.Router, customerID, email, password string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("email", email)
	form.Set("phone", "9900112233")
	form.Set("password", password)
	req := httptest.NewRequest(http.MethodPost, "/customers/"+customerID+"/portal-users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, adminCtx(req))
	return rr
}

func TestGrantPortalAccessCreatesLinkedUser(t *testing.T) {
	app, r := portalAccessRouter(t)
	custID := seedPortalCustomer(t, app)

	rr := postPortalForm(r, custID, "shipper@acme.com", "s3cret!pass")
	assert.Equal(t, http.StatusSeeOther, rr.Code, rr.Body.String())

	var userID string
	err := app.DB.QueryRow(`SELECT user_id FROM customer_users WHERE customer_id = ?`, custID).Scan(&userID)
	require.NoError(t, err, "customer_users link row must exist")

	var email, status string
	err = app.DB.QueryRow(`SELECT email, status FROM users WHERE id = ?`, userID).Scan(&email, &status)
	require.NoError(t, err)
	assert.Equal(t, "shipper@acme.com", email)
	assert.Equal(t, string(domain.UserStatusActive), status)

}

func TestGrantPortalAccessLinksExistingUserByIdempotent(t *testing.T) {
	app, r := portalAccessRouter(t)
	custID := seedPortalCustomer(t, app)

	rr := postPortalForm(r, custID, "existing@acme.com", "pw-123456")
	require.Equal(t, http.StatusSeeOther, rr.Code)

	rr2 := postPortalForm(r, custID, "existing@acme.com", "pw-123456")
	assert.Equal(t, http.StatusSeeOther, rr2.Code)

	var n int
	err := app.DB.QueryRow(`SELECT COUNT(*) FROM customer_users WHERE customer_id = ?`, custID).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "re-grant must not duplicate the link (UNIQUE constraint)")

	var users int
	err = app.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE email = 'existing@acme.com'`).Scan(&users)
	require.NoError(t, err)
	assert.Equal(t, 1, users, "existing user must be reused, not duplicated")
}

func TestGrantPortalAccessRequiresEmailAndPassword(t *testing.T) {
	app, r := portalAccessRouter(t)
	custID := seedPortalCustomer(t, app)

	form := url.Values{}
	form.Set("phone", "9900112233")
	form.Set("email", "")
	req := httptest.NewRequest(http.MethodPost, "/customers/"+custID+"/portal-users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, adminCtx(req))

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// seedPortalTrackingLane builds two customers, a portal user linked to the
// first, one booking+trip per customer, and one stop on the owned trip.
func seedPortalTrackingLane(t *testing.T, app *App) {
	t.Helper()
	exec := func(q string, args ...interface{}) {
		_, err := app.DB.Exec(q, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('cust-A', '1', 'Acme', '9111111111'), ('cust-B', '1', 'Other', '9222222222')`)
	exec(`INSERT INTO users (id, email, password_hash, name, phone, role_id, status) VALUES ('u-portal', 'portal@acme.com', 'x', 'Portal', '9333333333', 2, 'active')`)
	exec(`INSERT INTO customer_users (id, customer_id, user_id) VALUES ('cu-1', 'cust-A', 'u-portal')`)
	exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare) VALUES ('rt-1', 'Mumbai', 'Pune', 150, 4, 15000)`)
	exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id) VALUES
		('b-A', 'BK-A', 'cust-A', '2026-09-15', 'rt-1', 'truck', 0, 15000, 'confirmed', '1'),
		('b-B', 'BK-B', 'cust-B', '2026-09-15', 'rt-1', 'truck', 0, 15000, 'confirmed', '1')`)
	exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id) VALUES
		('t-own', 'TRP-OWN', 'b-A', 'rt-1', '2026-09-15', 'in_transit', '1'),
		('t-other', 'TRP-OTHER', 'b-B', 'rt-1', '2026-09-15', 'in_transit', '1')`)
	exec(`INSERT INTO trip_stops (id, tenant_id, trip_id, stop_sequence, stop_type, location_name, address, status) VALUES
		('s-1', '1', 't-own', 1, 'pickup', 'Mumbai Depot', '1 Main St', 'completed')`)
}

func portalTrackingReq(tripID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/customer/tracking/"+tripID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trip_id", tripID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req = req.WithContext(shared.ContextWithTenantID(req.Context(), "1"))
	return req.WithContext(context.WithValue(req.Context(), auth.ContextUser, &auth.SessionData{UserID: userID, Role: "customer"}))
}

func TestPortalTracking_DeniesOtherCustomerTrip(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, nil)
	seedPortalTrackingLane(t, app)
	h := NewCustomerPortalHandlers(app)

	rr := httptest.NewRecorder()
	h.Tracking(rr, portalTrackingReq("t-other", "u-portal"))
	assert.Equal(t, http.StatusNotFound, rr.Code, "other customer's trip must not render")
}

func TestPortalTracking_RendersOwnedTripWithStops(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, nil)
	seedPortalTrackingLane(t, app)
	h := NewCustomerPortalHandlers(app)

	rr := httptest.NewRecorder()
	h.Tracking(rr, portalTrackingReq("t-own", "u-portal"))
	assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String()[:min(300, rr.Body.Len())])
	assert.Contains(t, rr.Body.String(), "TRP-OWN")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
