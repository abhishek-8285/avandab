package service

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

func nilBus() events.EventBus { return events.NewInMemoryBus() }

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func sharedTenantCtx() context.Context {
	return shared.ContextWithTenantID(context.Background(), "1")
}

func nominatimStub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRouteGeocoder_PersistsLocationsOnCreate(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("q") {
		case "Bengaluru":
			_, _ = w.Write([]byte(`[{"lat":"12.9716","lon":"77.5946","display_name":"Bengaluru, Karnataka"}]`))
		default:
			_, _ = w.Write([]byte(`[{"lat":"18.5204","lon":"73.8567","display_name":"Pune, Maharashtra"}]`))
		}
	}))
	t.Cleanup(srv.Close)
	svc := NewServices(repo, nil, discardLog(), nilBus()).Routes.WithGeocoder(NewNominatimGeocoder(srv.URL))

	route, err := svc.CreateRoute(sharedTenantCtx(), "Bengaluru", "Pune", 950, 14, 25000, "")
	require.NoError(t, err)

	var sLat, dLat float64
	var sName string
	require.NoError(t, db.QueryRow(
		`SELECT source_lat, dest_lat, COALESCE(source_name,'') FROM route_locations WHERE route_id = ?`,
		string(route.ID)).Scan(&sLat, &dLat, &sName))
	assert.InDelta(t, 12.9716, sLat, 0.0001)
	assert.InDelta(t, 18.5204, dLat, 0.0001)
	assert.Equal(t, "Bengaluru, Karnataka", sName)
}

func TestRouteGeocoder_UpstreamFailureKeepsFreeTextOnly(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	nominatim := nominatimStub(t, http.StatusBadGateway, `upstream down`)
	svc := NewServices(repo, nil, discardLog(), nilBus()).Routes.WithGeocoder(NewNominatimGeocoder(nominatim.URL))

	route, err := svc.CreateRoute(sharedTenantCtx(), "Nowhere", "Elsewhere", 100, 2, 3000, "")
	require.NoError(t, err, "route creation must not fail when the geocoder is down")

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM route_locations WHERE route_id = ?`, string(route.ID)).Scan(&n))
	assert.Zero(t, n)
}

func TestRouteGeocoder_UpdateRegeocodes(t *testing.T) {
	db := newTelemetryTestDB(t)
	repo := sqliterepo.NewRepository(db)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`[{"lat":"28.6139","lon":"77.2090","display_name":"Delhi"}]`))
	}))
	t.Cleanup(srv.Close)
	svc := NewServices(repo, nil, discardLog(), nilBus()).Routes.WithGeocoder(NewNominatimGeocoder(srv.URL))
	ctx := sharedTenantCtx()

	route, err := svc.CreateRoute(ctx, "A", "B", 100, 2, 3000, "")
	require.NoError(t, err)

	_, err = svc.UpdateRoute(ctx, route.ID, "Delhi", "Jaipur", 280, 5, 8000, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, calls, 4, "create+update must geocode both endpoints each time")

	var sLat float64
	require.NoError(t, db.QueryRow(`SELECT source_lat FROM route_locations WHERE route_id = ?`, string(route.ID)).Scan(&sLat))
	assert.InDelta(t, 28.6139, sLat, 0.0001)
}

func TestHaversineKmKnownPair(t *testing.T) {
	blrToPune := haversineKm(12.9716, 77.5946, 18.5204, 73.8567)
	assert.InDelta(t, 737, blrToPune, 10)
}
