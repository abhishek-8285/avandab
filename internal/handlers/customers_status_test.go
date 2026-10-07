package handlers

// Ratchet for the customers status filter: ?status=inactive must return only
// inactive rows (pre-fix backend ignored the param and returned everything).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func TestCustomers_StatusFilter(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	_, err := app.Services.Customers.CreateCustomer(ctx, "Active Corp", "Active Corp", "9000000011", "active@example.com", "", "Mumbai", "")
	require.NoError(t, err)
	inactive, err := app.Services.Customers.CreateCustomer(ctx, "Inactive Corp", "Inactive Corp", "9000000012", "inactive@example.com", "", "Pune", "")
	require.NoError(t, err)
	_, err = app.Services.Customers.UpdateCustomerFull(ctx, inactive.ID, service.UpdateCustomerRequest{Status: "inactive"})
	require.NoError(t, err)

	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)
	get := func(target string) string {
		req := withTenantSession(httptest.NewRequest(http.MethodGet, target, nil), string(shared.DefaultTenant), "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "GET %s", target)
		return w.Body.String()
	}

	t.Run("inactive shows only inactive", func(t *testing.T) {
		body := get("/customers/?status=inactive")
		assert.Contains(t, body, "Inactive Corp")
		assert.NotContains(t, body, "Active Corp")
	})

	t.Run("active shows only active", func(t *testing.T) {
		body := get("/customers/?status=active")
		assert.Contains(t, body, "Active Corp")
		assert.NotContains(t, body, "Inactive Corp")
	})

	t.Run("no filter shows both", func(t *testing.T) {
		body := get("/customers/")
		assert.Contains(t, body, "Active Corp")
		assert.Contains(t, body, "Inactive Corp")
	})

	t.Run("bad status falls back to all", func(t *testing.T) {
		body := get("/customers/?status=bogus")
		assert.Contains(t, body, "Active Corp")
		assert.Contains(t, body, "Inactive Corp")
	})

}
