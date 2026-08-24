package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/domain"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// TestSelectedCustomers_ViewRecentActivity verifies the customer detail page
// surfaces recent bookings (main column) and invoices (sidebar).
func TestSelectedCustomers_ViewRecentActivity(t *testing.T) {
	db := newCustomersSelectedDB(t)
	app := newCustomersSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/customers", app.Customers.Routes)

	ctx := shared.ContextWithTenantID(context.Background(), shared.DefaultTenant)

	cust, err := app.Services.Customers.CreateCustomer(ctx, "Recent Activity Co", "RAC", "9005550001", "rac@example.com", "", "Mumbai", "")
	require.NoError(t, err)
	custID := cust.ID

	route, err := app.Services.Routes.CreateRoute(ctx, "Pune", "Nagpur", 700, 12, 9000, "")
	require.NoError(t, err)

	getView := func(t *testing.T) string {
		t.Helper()
		req := withTenantSession(httptest.NewRequest(http.MethodGet, "/customers/"+string(custID), nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}

	t.Run("empty state", func(t *testing.T) {
		body := getView(t)
		assert.Contains(t, body, "No bookings for this customer yet.")
		assert.Contains(t, body, "No invoices yet.")
	})

	t.Run("booking and invoice listed", func(t *testing.T) {
		booking, err := app.Services.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
			CustomerID:  custID,
			RouteID:     route.ID,
			PickupDate:  time.Now().AddDate(0, 0, 2).Format("2006-01-02"),
			VehicleType: domain.VehicleTypeTruck,
			Passengers:  2,
			CargoWeight: &[]float64{500}[0],
			Price:       12000,
		})
		require.NoError(t, err)

		trip, err := app.Services.Trips.CreateTrip(ctx, service.CreateTripRequest{
			BookingID:     &booking.ID,
			RouteID:       route.ID,
			DepartureTime: time.Now().AddDate(0, 0, 2).Format("2006-01-02 15:04"),
		})
		require.NoError(t, err)

		invoice, err := app.Services.Invoices.GenerateInvoiceFromTrip(ctx, trip.ID)
		require.NoError(t, err)

		body := getView(t)
		assert.Contains(t, body, booking.BookingNumber)
		assert.Contains(t, body, "/bookings/"+string(booking.ID))
		assert.Contains(t, body, invoice.InvoiceNumber)
		assert.Contains(t, body, "/invoices/"+string(invoice.ID))
	})
}
