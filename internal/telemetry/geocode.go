package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// geoEntry caches one reverse-geocode result. ok=false caches negative
// lookups (empty display_name / upstream miss) so a remote corner of the map
// cannot re-query Nominatim on every marker selection.
type geoEntry struct {
	name    string
	ok      bool
	expires time.Time
}

// geoCacheTTL bounds staleness; addresses move slowly relative to the ~11 m
// grid key (4 decimal places), so 10 minutes is imperceptible for ops.
const (
	geoCacheTTL       = 10 * time.Minute
	geoCacheMax       = 1024
	geoUpstreamTO     = 5 * time.Second
	geoCoordPrecision = 10000 // 1e4 ⇒ 4 decimals ≈ 11 m buckets
)

// ReverseGeocodeHandler serves GET /api/v1/telemetry/reverse_geocode?lat=&lng=
// as a thin caching proxy in front of a Nominatim-compatible service
// (NOMINATIM_URL, Spec 04 §7). The server owns the upstream User-Agent and
// absorbs repeat lookups so browser clients never talk to the provider
// directly. Mounted inside RequireAPIAuth like the rest of the live feed.
func ReverseGeocodeHandler(baseURL string) http.HandlerFunc {
	base := strings.TrimRight(baseURL, "/")
	client := &http.Client{Timeout: geoUpstreamTO}

	var mu sync.Mutex
	cache := make(map[string]geoEntry)

	lookup := func(key string) (geoEntry, bool) {
		mu.Lock()
		defer mu.Unlock()
		e, found := cache[key]
		return e, found && time.Now().Before(e.expires)
	}

	store := func(key string, e geoEntry) {
		mu.Lock()
		defer mu.Unlock()
		if len(cache) >= geoCacheMax {
			now := time.Now()
			for k, v := range cache {
				if now.After(v.expires) {
					delete(cache, k)
				}
			}
			if len(cache) >= geoCacheMax {
				// Still full: drop everything rather than grow unbounded.
				cache = make(map[string]geoEntry)
			}
		}
		cache[key] = e
	}

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if base == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "reverse geocoding not configured"})
			return
		}
		lat, errLat := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
		lng, errLng := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
		if errLat != nil || errLng != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid coordinates"})
			return
		}

		key := fmt.Sprintf("%.4f,%.4f", lat, lng)
		if e, hit := lookup(key); hit {
			if !e.ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "no address"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"display_name": e.name})
			return
		}

		q := url.Values{}
		q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
		q.Set("lon", strconv.FormatFloat(lng, 'f', -1, 64))
		q.Set("format", "jsonv2")
		q.Set("zoom", "16")

		ctx, cancel := context.WithTimeout(r.Context(), geoUpstreamTO)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/reverse?"+q.Encode(), nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request build failed"})
			return
		}
		// Nominatim usage policy requires an identifying User-Agent.
		req.Header.Set("User-Agent", "Avandab-TMS/1.0 (tracking page)")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "geocoder unreachable"})
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			store(key, geoEntry{ok: false, expires: time.Now().Add(geoCacheTTL)})
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "geocoder error"})
			return
		}

		var payload struct {
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.DisplayName) == "" {
			store(key, geoEntry{ok: false, expires: time.Now().Add(geoCacheTTL)})
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no address"})
			return
		}

		store(key, geoEntry{name: payload.DisplayName, ok: true, expires: time.Now().Add(geoCacheTTL)})
		writeJSON(w, http.StatusOK, map[string]string{"display_name": payload.DisplayName})
	}
}
