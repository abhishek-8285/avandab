package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"transport-app/internal/repository"
)

// Geocoder resolves a free-text place to coordinates. Implementations must be
// safe for concurrent use.
type Geocoder interface {
	Geocode(ctx context.Context, query string) (lat, lng float64, displayName string, err error)
}

// NominatimGeocoder calls a Nominatim-compatible /search endpoint (Spec 04
// §7 — same NOMINATIM_URL the reverse-geocode proxy uses).
type NominatimGeocoder struct {
	base   string
	client *http.Client
}

func NewNominatimGeocoder(baseURL string) *NominatimGeocoder {
	return &NominatimGeocoder{
		base:   strings.TrimRight(baseURL, "/"),
		client: &http.Client{Timeout: 4 * time.Second},
	}
}

type nominatimHit struct {
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
}

func (g *NominatimGeocoder) Geocode(ctx context.Context, query string) (float64, float64, string, error) {
	if g.base == "" {
		return 0, 0, "", fmt.Errorf("geocoder not configured")
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "jsonv2")
	q.Set("limit", "1")

	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+"/search?"+q.Encode(), nil)
	if err != nil {
		return 0, 0, "", err
	}
	req.Header.Set("User-Agent", "Avandab-TMS/1.0 (route standardization)")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("geocoder unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, "", fmt.Errorf("geocoder status %d", resp.StatusCode)
	}
	var hits []nominatimHit
	if err := json.NewDecoder(resp.Body).Decode(&hits); err != nil {
		return 0, 0, "", fmt.Errorf("geocoder payload: %w", err)
	}
	if len(hits) == 0 {
		return 0, 0, "", fmt.Errorf("no match for %q", query)
	}
	var lat, lng float64
	if _, err := fmt.Sscanf(hits[0].Lat, "%f", &lat); err != nil {
		return 0, 0, "", fmt.Errorf("geocoder lat: %w", err)
	}
	if _, err := fmt.Sscanf(hits[0].Lon, "%f", &lng); err != nil {
		return 0, 0, "", fmt.Errorf("geocoder lng: %w", err)
	}
	return lat, lng, hits[0].DisplayName, nil
}

// routeLocation is one geocoded endpoint pair persisted beside the route.
type routeLocation struct {
	RouteID    string
	SourceLat  float64
	SourceLng  float64
	SourceName string
	DestLat    float64
	DestLng    float64
	DestName   string
}

func (s *RouteService) persistRouteLocations(ctx context.Context, loc routeLocation) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter == nil {
		return fmt.Errorf("storage does not expose a database")
	}
	var exec interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	} = getter.DB()
	if tx := repository.TxFromContext(ctx); tx != nil {
		exec = tx
	}
	_, err := exec.ExecContext(ctx,
		`INSERT OR REPLACE INTO route_locations
		    (route_id, source_lat, source_lng, source_name, dest_lat, dest_lng, dest_name, geocoded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		loc.RouteID, loc.SourceLat, loc.SourceLng, loc.SourceName,
		loc.DestLat, loc.DestLng, loc.DestName)
	return err
}

// haversineKm returns great-circle km between two points.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// geocodeEndpoints best-effort resolves both endpoints and persists the pair.
// Failures never block route creation — the route stays free-text-only.
func (s *RouteService) geocodeEndpoints(ctx context.Context, routeID, source, destination string) {
	if s.geocoder == nil {
		return
	}
	sLat, sLng, sName, serr := s.geocoder.Geocode(ctx, source)
	dLat, dLng, dName, derr := s.geocoder.Geocode(ctx, destination)
	if serr != nil || derr != nil {
		if s.log != nil {
			s.log.Warn("route geocoding skipped (best-effort)", "route_id", routeID, "source_err", serr, "dest_err", derr)
		}
		return
	}
	err := s.persistRouteLocations(ctx, routeLocation{
		RouteID:    routeID,
		SourceLat:  sLat,
		SourceLng:  sLng,
		SourceName: sName,
		DestLat:    dLat,
		DestLng:    dLng,
		DestName:   dName,
	})
	if err != nil && s.log != nil {
		s.log.Warn("route location persistence failed", "route_id", routeID, "error", err)
	}
}
