package deviation

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	geodomain "transport-app/internal/geofence/domain"
)

// RouteCorridor represents the planned spatial geometry of a trip's assigned route.
type RouteCorridor struct {
	RouteID    string
	Source     string
	Dest       string
	DistanceKM float64
	Waypoints  []geodomain.Point
}

// DistanceToPoint calculates the shortest distance (in metres) from point (lat, lng) to the route corridor.
func (c *RouteCorridor) DistanceToPoint(lat, lng float64) float64 {
	if len(c.Waypoints) < 2 {
		return 0 // Insufficient route geometry to evaluate deviation
	}

	p := geodomain.Point{Lat: lat, Lng: lng}
	minDist := math.MaxFloat64

	for i := 0; i < len(c.Waypoints)-1; i++ {
		a := c.Waypoints[i]
		b := c.Waypoints[i+1]
		dist := geodomain.PointToSegmentDistance(p, a, b)
		if dist < minDist {
			minDist = dist
		}
	}

	return minDist
}

// LoadRouteCorridor loads the route geometry for a given route ID from `routes` and `route_locations`.
func LoadRouteCorridor(ctx context.Context, db *sql.DB, routeID string) (*RouteCorridor, error) {
	if routeID == "" {
		return nil, fmt.Errorf("empty route_id")
	}

	var source, dest string
	var distanceKM float64
	err := db.QueryRowContext(ctx,
		`SELECT source, destination, distance FROM routes WHERE id = ?`, routeID).
		Scan(&source, &dest, &distanceKM)
	if err != nil {
		return nil, fmt.Errorf("load route %s: %w", routeID, err)
	}

	corridor := &RouteCorridor{
		RouteID:    routeID,
		Source:     source,
		Dest:       dest,
		DistanceKM: distanceKM,
		Waypoints:  make([]geodomain.Point, 0),
	}

	// 1. Check if geocoded coordinates exist in route_locations (Migration 00091)
	var sLat, sLng, dLat, dLng float64
	err = db.QueryRowContext(ctx,
		`SELECT source_lat, source_lng, dest_lat, dest_lng FROM route_locations WHERE route_id = ?`,
		routeID).Scan(&sLat, &sLng, &dLat, &dLng)

	if err == nil && (sLat != 0 || sLng != 0 || dLat != 0 || dLng != 0) {
		corridor.Waypoints = append(corridor.Waypoints,
			geodomain.Point{Lat: sLat, Lng: sLng},
			geodomain.Point{Lat: dLat, Lng: dLng},
		)
		return corridor, nil
	}

	return corridor, nil
}
