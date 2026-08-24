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
