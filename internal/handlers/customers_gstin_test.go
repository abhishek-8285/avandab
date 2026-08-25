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
)

// TestCustomerGstinValidation covers server-side GSTIN enforcement on the
// customer create/update paths — the entry point that let the malformed
// "07KUKPS5477RDAF" into the system.
func TestCustomerGstinValidation(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	t.Run("service rejects invalid gstin on create", func(t *testing.T) {
		_, err := app.Services.Customers.CreateCustomer(context.Background(),
			"Bad GST", "Bad Co", "9100000001", "bad@example.com", "07KUKPS5477RDAF", "Delhi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid GSTIN")
	})

	t.Run("service accepts valid gstin and normalizes case", func(t *testing.T) {
		c, err := app.Services.Customers.CreateCustomer(context.Background(),
			"Good GST", "Good Co", "9100000002", "good@example.com", " 27aabcu9603r1zx ", "Delhi", "")
		require.NoError(t, err)
		require.NotNil(t, c.GST)
		assert.Equal(t, "27AABCU9603R1ZX", *c.GST)
	})

	t.Run("service allows empty gstin (b2c)", func(t *testing.T) {
		_, err := app.Services.Customers.CreateCustomer(context.Background(),
			"No GST", "Retail Co", "9100000003", "", "", "Delhi", "")
		require.NoError(t, err)
	})

	t.Run("service rejects invalid gstin on update", func(t *testing.T) {
		c, err := app.Services.Customers.CreateCustomer(context.Background(),
			"Upd Me", "Upd Co", "9100000004", "", "", "Delhi", "")
		require.NoError(t, err)
		_, err = app.Services.Customers.UpdateCustomer(context.Background(), c.ID,
			"Upd Me", "Upd Co", "9100000004", "", "27PQRSX5678K1Z2", "Delhi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid GSTIN")
	})

	postForm := func(t *testing.T, path string, vals url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := withTenantSession(httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("create handler re-renders form with flash on invalid gstin", func(t *testing.T) {
		w := postForm(t, "/customers/new", url.Values{
			"name": {"Form Bad"}, "phone": {"9100000005"}, "gst": {"07KUKPS5477RDAF"},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "invalid GSTIN")
		assert.Contains(t, body, "New Customer")
	})

	t.Run("update handler re-renders form with flash on invalid gstin", func(t *testing.T) {
		c, err := app.Services.Customers.CreateCustomer(context.Background(),
			"Form Upd", "F Co", "9100000006", "", "", "Delhi", "")
		require.NoError(t, err)
		w := postForm(t, "/customers/"+c.ID.String()+"/edit", url.Values{
			"name": {"Form Upd"}, "phone": {"9100000006"}, "gst": {"07KUKPS5477RDAF"},
		})
		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "invalid GSTIN")
		assert.NotEqual(t, http.StatusSeeOther, w.Code)
	})

	t.Run("update handler succeeds with valid gstin", func(t *testing.T) {
		c, err := app.Services.Customers.CreateCustomer(context.Background(),
			"Form Ok", "F Co", "9100000007", "", "", "Delhi", "")
		require.NoError(t, err)
		w := postForm(t, "/customers/"+c.ID.String()+"/edit", url.Values{
			"name": {"Form Ok"}, "phone": {"9100000007"}, "gst": {"29AABCB1234C1Z7"},
		})
		assert.Equal(t, http.StatusSeeOther, w.Code)
	})
}
