package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// The public ePOD endpoint must only resolve UUID trip ids. A guessable
// sequential trip_number used to render any tenant's certificate.
func TestPublicEPODRejectsTripNumber(t *testing.T) {
	h := &TripHandlers{App: &App{}}
	r := chi.NewRouter()
	r.Get("/epod/{tripId}", h.PublicEPODCertificate)

	req := httptest.NewRequest(http.MethodGet, "/epod/TRIP-000123", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
