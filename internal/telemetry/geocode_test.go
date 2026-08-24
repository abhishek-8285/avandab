package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func geoSrv(t *testing.T, status int, body string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits++
		}
		assert.Equal(t, "/reverse", r.URL.Path)
		assert.NotEmpty(t, r.URL.Query().Get("lat"))
		assert.Equal(t, "jsonv2", r.URL.Query().Get("format"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func doGet(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestReverseGeocode_HappyPath(t *testing.T) {
	var hits int
	srv := geoSrv(t, http.StatusOK, `{"display_name":"Bandra West, Mumbai, Maharashtra"}`, &hits)
	defer srv.Close()

	h := ReverseGeocodeHandler(srv.URL)
	rec := doGet(h, "/api/v1/telemetry/reverse_geocode?lat=19.0759&lng=72.8777")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Bandra West, Mumbai, Maharashtra")
	assert.Equal(t, 1, hits)
}

func TestReverseGeocode_CachesNearbyLookups(t *testing.T) {
	var hits int
	srv := geoSrv(t, http.StatusOK, `{"display_name":"Pune, Maharashtra"}`, &hits)
	defer srv.Close()

	h := ReverseGeocodeHandler(srv.URL)
	for i := 0; i < 3; i++ {
		rec := doGet(h, "/api/v1/telemetry/reverse_geocode?lat=18.52040&lng=73.85670")
		require.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, 1, hits, "identical coordinate buckets must hit the cache")

	// Sub-bucket jitter (4th decimal) still lands in the same bucket.
	rec := doGet(h, "/api/v1/telemetry/reverse_geocode?lat=18.520412&lng=73.856733")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, hits)

	// A far-away point must miss the cache.
	rec = doGet(h, "/api/v1/telemetry/reverse_geocode?lat=28.6139&lng=77.2090")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 2, hits)
}

func TestReverseGeocode_NegativeResultCached(t *testing.T) {
	var hits int
	srv := geoSrv(t, http.StatusOK, `{"error":"unable to geocode"}`, &hits)
	defer srv.Close()

	h := ReverseGeocodeHandler(srv.URL)
	url := "/api/v1/telemetry/reverse_geocode?lat=0.5&lng=0.5"
	for i := 0; i < 2; i++ {
		rec := doGet(h, url)
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
	assert.Equal(t, 1, hits, "empty lookups are cached too")
}

func TestReverseGeocode_UpstreamError(t *testing.T) {
	var hits int
	srv := geoSrv(t, http.StatusInternalServerError, `{}`, &hits)
	defer srv.Close()

	h := ReverseGeocodeHandler(srv.URL)
	rec := doGet(h, "/api/v1/telemetry/reverse_geocode?lat=19.07&lng=72.87")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestReverseGeocode_InvalidInput(t *testing.T) {
	h := ReverseGeocodeHandler("http://unused.invalid")
	cases := []string{
		"/api/v1/telemetry/reverse_geocode?lat=abc&lng=72.87",
		"/api/v1/telemetry/reverse_geocode?lat=19.07",
		"/api/v1/telemetry/reverse_geocode?lat=999&lng=72.87",
		"/api/v1/telemetry/reverse_geocode?lat=19.07&lng=-500",
	}
	for _, tc := range cases {
		rec := doGet(h, tc)
		assert.Equal(t, http.StatusBadRequest, rec.Code, tc)
	}
}

func TestReverseGeocode_Disabled(t *testing.T) {
	h := ReverseGeocodeHandler("")
	rec := doGet(h, "/api/v1/telemetry/reverse_geocode?lat=19.07&lng=72.87")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
