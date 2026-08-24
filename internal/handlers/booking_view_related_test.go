package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestSelectedBookings_ViewRelatedRecords verifies the booking detail page
// surfaces the trip and invoice linked to the booking, and hides the
// Related Records card when neither exists.
func TestSelectedBookings_ViewRelatedRecords(t *testing.T) {
	db := newBookingsSelectedDB(t)
	app := newBookingsSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/bookings", app.Bookings.Routes)

	cust, route := seedBookingPrereqs(t, app)
	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	createBooking := func(t *testing.T) string {
		t.Helper()
		form := strings.NewReader(fmt.Sprintf(
			"customer_id=%s&route_id=%s&pickup_date=%s&vehicle_type=truck&passengers=2&price=1500",
			cust.ID, route.ID, time.Now().AddDate(0, 0, 3).Format("2006-01-02"),
		))
		req := withBookingTenantSession(httptest.NewRequest(http.MethodPost, "/bookings/new", form), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusSeeOther, w.Code)
		var bid string
		err := db.QueryRow(`SELECT id FROM bookings WHERE tenant_id = ? ORDER BY created_at DESC LIMIT 1`, shared.DefaultTenant).Scan(&bid)
		require.NoError(t, err)
		return bid
	}

	getView := func(t *testing.T, id string) *httptest.ResponseRecorder {
		t.Helper()
		req := withBookingTenantSession(httptest.NewRequest(http.MethodGet, "/bookings/"+id, nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("no related records hides section", func(t *testing.T) {
		bid := createBooking(t)
		w := getView(t, bid)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.NotContains(t, w.Body.String(), "Related Records")
	})

	t.Run("trip and invoice shown when linked", func(t *testing.T) {
		bid := createBooking(t)

		trip, err := app.Services.Trips.CreateTrip(ctx, service.CreateTripRequest{
			BookingID:     (*domain.BookingID)(&bid),
			RouteID:       route.ID,
			DepartureTime: time.Now().AddDate(0, 0, 3).Format("2006-01-02 15:04"),
		})
		require.NoError(t, err)

		invoice, err := app.Services.Invoices.GenerateInvoiceFromTrip(ctx, trip.ID)
		require.NoError(t, err)

		w := getView(t, bid)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		body := w.Body.String()

		assert.Contains(t, body, "Related Records")
		assert.Contains(t, body, trip.TripNumber)
		assert.Contains(t, body, "/trips/"+string(trip.ID))
		assert.Contains(t, body, invoice.InvoiceNumber)
		assert.Contains(t, body, "/invoices/"+string(invoice.ID))
	})

	t.Run("history shows audit trail after confirm", func(t *testing.T) {
		bid := createBooking(t)

		// Confirm through the web route so the DDD use case writes the audit entry.
		req := withBookingTenantSession(httptest.NewRequest(http.MethodPost, "/bookings/"+bid+"/confirm", nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusSeeOther, w.Code)

		req = withBookingTenantSession(httptest.NewRequest(http.MethodGet, "/bookings/"+bid, nil), "1", "user-1", "admin")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		body := w.Body.String()

		assert.Contains(t, body, "History")
		assert.Contains(t, body, ">create<")
		assert.Contains(t, body, ">confirm<")
	})
}
