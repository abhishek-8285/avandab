package handlers

// Trip ePOD section: photo upload endpoint + PODFiles mapping + section wiring.
// History: trip view showed OTP but never the attached photos; dispatchers
// also lacked files:read/create (00151), so the whole surface 403'd for them.
import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

func TestUploadTripPOD_RoundTrip(t *testing.T) {
	app, _, _ := setupFilesAPITest(t, nil)
	h := &TripHandlers{App: app}

	rtr := chi.NewRouter()
	rtr.Post("/trips/{id}/pod", h.UploadTripPOD)

	// Valid PNG -> 303 back to the trip, file row stored as trip_pod.
	req := multipartFileRequest(t, "/trips/trip-pod-1/pod", "pod.png", pngBytes(), nil)
	req = filesAPITenantContext(req, string(shared.DefaultTenant))
	w := httptest.NewRecorder()
	rtr.ServeHTTP(w, req)
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	require.Equal(t, "/trips/trip-pod-1", w.Header().Get("Location"))

	var typ string
	require.NoError(t, app.DB.QueryRow(
		`SELECT uploadable_type FROM files WHERE uploadable_id = 'trip-pod-1'`).Scan(&typ))
	require.Equal(t, "trip_pod", typ)

	// Non-multipart post (no file) -> back to trip page, never 500.
	req2 := httptest.NewRequest(http.MethodPost, "/trips/trip-pod-1/pod",
		strings.NewReader("foo=bar"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2 = filesAPITenantContext(req2, string(shared.DefaultTenant))
	w2 := httptest.NewRecorder()
	rtr.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusSeeOther, w2.Code)
}

func TestTripPODFiles_Mapping(t *testing.T) {
	app, _, _ := setupFilesAPITest(t, nil)
	h := &TripHandlers{App: app}

	ctx := filesAPITenantContext(httptest.NewRequest(http.MethodGet, "/", nil), string(shared.DefaultTenant))
	upReq := multipartFileRequest(t, "/x", "pod.png", pngBytes(), nil)
	require.NoError(t, upReq.ParseMultipartForm(10<<20))
	_, header, err := upReq.FormFile("file")
	require.NoError(t, err)
	f, err := app.Services.Files.UploadFile(ctx.Context(), header, "trip_pod", "trip-map-1")
	require.NoError(t, err)

	items := h.tripPODFiles(ctx, "trip-map-1")
	require.Len(t, items, 1)
	require.Equal(t, "/files/"+string(f.ID), items[0].URL)
	require.True(t, items[0].IsImage)
	require.NotEmpty(t, items[0].SizeLabel)

	// Unknown trip -> empty, never error.
	require.Empty(t, h.tripPODFiles(ctx, "trip-nope"))
}

func TestTripView_PODSectionWired(t *testing.T) {
	src, err := os.ReadFile("../templates/trip_view.html")
	require.NoError(t, err)
	body := string(src)
	require.Contains(t, body, "Proof of Delivery")
	require.Contains(t, body, `action="/trips/{{.Trip.ID}}/pod"`)
	require.Contains(t, body, "{{range .PODFiles}}")
}
