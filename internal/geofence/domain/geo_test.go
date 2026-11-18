package domain

import (
	"math"
	"testing"
)

func TestHaversine_KnownDistances(t *testing.T) {
	tests := []struct {
		name       string
		lat1, lng1 float64
		lat2, lng2 float64
		want       float64 // metres
		tolerance  float64 // metres
	}{
		{"identical points", 12.97, 77.59, 12.97, 77.59, 0, 0.5},
		{"one degree latitude", 0, 0, 1, 0, 111195, 5},
		{"one degree longitude at equator", 0, 0, 0, 1, 111195, 5},
		{"symmetry", 28.6139, 77.2090, 19.0760, 72.8777, 1150000, 25000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Haversine(tt.lat1, tt.lng1, tt.lat2, tt.lng2)
			if math.Abs(got-tt.want) > tt.tolerance {
				t.Fatalf("Haversine(%v,%v,%v,%v) = %.2fm, want ≈ %.2fm", tt.lat1, tt.lng1, tt.lat2, tt.lng2, got, tt.want)
			}
		})
	}
}

func TestCircleContains(t *testing.T) {
	centerLat, centerLng, radius := 12.97, 77.59, 100.0
	// 50m north of centre — inside.
	latInside := centerLat + (50.0 / 111320.0)
	if !CircleContains(centerLat, centerLng, radius, latInside, centerLng) {
		t.Fatal("point 50m north should be inside")
	}
	// Exactly on the boundary — inclusive.
	latBoundary := centerLat + (radius / 111320.0)
	if !CircleContains(centerLat, centerLng, radius, latBoundary, centerLng) {
		t.Fatal("point on boundary should be inside (inclusive)")
	}
	// 2km away — outside.
	latFar := centerLat + 0.01 // ~1.1km
	if CircleContains(centerLat, centerLng, radius, latFar, centerLng) {
		t.Fatal("point 1.1km away should be outside")
	}
}

func TestPointInPolygon_RejectsFewerThanThreePoints(t *testing.T) {
	if PointInPolygon(0, 0, []Point{{Lat: 0, Lng: 0}, {Lat: 1, Lng: 1}}) {
		t.Fatal("2-point ring must be rejected")
	}
	if PointInPolygon(0, 0, nil) {
		t.Fatal("nil ring must be rejected")
	}
}

func TestPointInPolygon_ClosedAndOpenRings(t *testing.T) {
	open := []Point{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 2}, {Lat: 2, Lng: 2}, {Lat: 2, Lng: 0}}
	closed := append(append([]Point{}, open...), open[0])

	if !PointInPolygon(1, 1, open) {
		t.Fatal("open square ring should contain centre")
	}
	if !PointInPolygon(1, 1, closed) {
		t.Fatal("closed square ring should contain centre")
	}
	if PointInPolygon(5, 5, closed) {
		t.Fatal("point outside the square must be rejected")
	}
}

func TestPointInPolygon_VertexAndEdgeBoundary(t *testing.T) {
	ring := []Point{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 2}, {Lat: 2, Lng: 2}, {Lat: 2, Lng: 0}}
	if !PointInPolygon(0, 0, ring) {
		t.Fatal("vertex-on-edge point must count as inside")
	}
	// Midpoint of the bottom edge (0,0)-(2,0).
	if !PointInPolygon(0, 1, ring) {
		t.Fatal("point on edge must count as inside")
	}
}

func TestPointInPolygon_ConcaveAndDegenerate(t *testing.T) {
	// Concave L-shaped polygon.
	lShape := []Point{
		{Lat: 0, Lng: 0}, {Lat: 0, Lng: 3}, {Lat: 1, Lng: 3},
		{Lat: 1, Lng: 1}, {Lat: 3, Lng: 1}, {Lat: 3, Lng: 0},
	}
	if !PointInPolygon(0.5, 0.5, lShape) {
		t.Fatal("point in the thick arm should be inside")
	}
	if PointInPolygon(2, 2, lShape) {
		t.Fatal("point in the notch should be outside")
	}
	// Duplicate consecutive points must not break the ray-cast.
	degenerate := []Point{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 0}, {Lat: 0, Lng: 2}, {Lat: 2, Lng: 2}, {Lat: 2, Lng: 0}}
	if !PointInPolygon(1, 1, degenerate) {
		t.Fatal("degenerate ring with duplicate vertex should still contain centre")
	}
}

func TestPointToPolygonDistance(t *testing.T) {
	ring := []Point{{Lat: 0, Lng: 0}, {Lat: 0, Lng: 0.001}, {Lat: 0.001, Lng: 0.001}, {Lat: 0.001, Lng: 0}}
	inside := Point{Lat: 0.0005, Lng: 0.0005}
	// Distance is to the boundary even inside: hysteresis needs it.
	if d := PointToPolygonDistance(inside, ring); d < 50 || d > 60 {
		t.Fatalf("inside point boundary distance = %f, want ≈55m", d)
	}
	// 111m west of the left edge (0,0)-(0.001,0).
	outside := Point{Lat: 0.0005, Lng: -0.001}
	d := PointToPolygonDistance(outside, ring)
	if d < 100 || d > 125 {
		t.Fatalf("edge distance = %f, want ≈ 111m", d)
	}
}
