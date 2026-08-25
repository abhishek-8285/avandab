// Package domain holds pure geofence math and aggregates (Spec 02).
package domain

import (
	"math"
)

// EarthRadiusM is the mean Earth radius in metres (WGS-84 mean).
const EarthRadiusM = 6371000.0

// Haversine returns the great-circle distance between two points in metres.
// Handles antipodal points (returns ~πR) and identical points (0).
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	lat1r := lat1 * math.Pi / 180
	lat2r := lat2 * math.Pi / 180
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1r)*math.Cos(lat2r)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return EarthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// onSegment reports whether point c lies on the segment a→b within an
// absolute epsilon in coordinate space (≈1.1e-9 deg ≈ 0.1 mm at equator).
func onSegment(a, b, c Point, eps float64) bool {
	cross := (c.Lat-a.Lat)*(b.Lng-a.Lng) - (c.Lng-a.Lng)*(b.Lat-a.Lat)
	if math.Abs(cross) > eps {
		return false
	}
	// c must be within the bounding box of a→b.
	if c.Lat < math.Min(a.Lat, b.Lat)-eps || c.Lat > math.Max(a.Lat, b.Lat)+eps {
		return false
	}
	if c.Lng < math.Min(a.Lng, b.Lng)-eps || c.Lng > math.Max(a.Lng, b.Lng)+eps {
		return false
	}
	return true
}

// PointInPolygon reports whether the point (lat, lng) is inside the closed
// ring using the even-odd ray-cast rule.
//
//   - Rings with fewer than 3 points are rejected (false).
//   - The ring may be closed explicitly (first == last); the duplicate
//     closing point is ignored.
//   - Points on an edge or vertex are treated as inside (true).
//   - The horizontal ray follows the half-open convention: an edge counts
//     as a crossing only when the ray passes through its interior.
func PointInPolygon(lat, lng float64, ring []Point) bool {
	if len(ring) < 3 {
		return false
	}
	p := Point{Lat: lat, Lng: lng}
	eps := 1e-9

	n := len(ring)
	// Normalise a closed ring by dropping the duplicate closing point.
	if ring[0] == ring[n-1] {
		n--
	}
	if n < 3 {
		return false
	}

	inside := false
	for i := 0; i < n; i++ {
		a := ring[i]
		b := ring[(i+1)%n]
		if onSegment(a, b, p, eps) {
			return true
		}
		// Half-open ray-cast: crossing if the segment straddles the point's
		// latitude and the intersection is strictly to the east.
		if (a.Lat > p.Lat) != (b.Lat > p.Lat) {
			x := a.Lng + (p.Lat-a.Lat)*(b.Lng-a.Lng)/(b.Lat-a.Lat)
			if x > p.Lng {
				inside = !inside
			}
		}
	}
	return inside
}

// equirectM approximates a lat/lng pair to a local metre-space vector.
// Accurate to <0.1% within ~100 km of the anchor latitude.
func equirectM(lat, lng, anchorLat float64) (x, y float64) {
	latScale := 111320.0 // metres per degree latitude (mean)
	lngScale := 111320.0 * math.Cos(anchorLat*math.Pi/180)
	return (lng * lngScale), (lat * latScale)
}

// PointToSegmentDistance returns the shortest distance (metres) from point p
// to segment a→b. Zero-length segments return the distance to their endpoint.
func PointToSegmentDistance(p, a, b Point) float64 {
	ax, ay := equirectM(a.Lat, a.Lng, p.Lat)
	bx, by := equirectM(b.Lat, b.Lng, p.Lat)
	px, py := equirectM(p.Lat, p.Lng, p.Lat)

	dx := bx - ax
	dy := by - ay
	if dx == 0 && dy == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	if t < 0 {
		return math.Hypot(px-ax, py-ay)
	}
	if t > 1 {
		return math.Hypot(px-bx, py-by)
	}
	cx := ax + t*dx
	cy := ay + t*dy
	return math.Hypot(px-cx, py-cy)
}

// PointToPolygonDistance returns the distance (metres) from point p to the
// polygon boundary: 0 if inside, otherwise the minimum distance to any edge.
func PointToPolygonDistance(p Point, ring []Point) float64 {
	if PointInPolygon(p.Lat, p.Lng, ring) {
		return 0
	}
	n := len(ring)
	if n < 2 {
		return math.MaxFloat64
	}
	if ring[0] == ring[n-1] {
		n--
	}
	minDist := math.MaxFloat64
	for i := 0; i < n; i++ {
		d := PointToSegmentDistance(p, ring[i], ring[(i+1)%n])
		if d < minDist {
			minDist = d
		}
	}
	return minDist
}

// CircleContains reports whether the point lies within a circle of the given
// radius (metres) around the centre.
func CircleContains(centerLat, centerLng, radiusM, lat, lng float64) bool {
	return Haversine(centerLat, centerLng, lat, lng) <= radiusM
}

// PointInPolygonWinding is the winding-number alternative to PointInPolygon.
// More robust for self-intersecting / degenerate rings; slower but handles
// edge cases where ray-cast half-open rule miscounts. Returns same inside
// definition (on-edge => true). Use for validation when ring.IsValid fails.
//
// NOTE on S2/PostGIS: true S2 cell covering (github.com/google/s2) or
// PostGIS ST_Contains would give O(log n) spherical indexing and anti-meridian
// handling, but requires cgo (s2) or Postgres extension (PostGIS). Current
// deploy is pure-Go modernc.org/sqlite on distroless, so we keep indexed
// ray-cast + this winding fallback. Switch to S2 when count >500 zones or
// Postgres cutover (Spec 23 scale tiering) lands.
func PointInPolygonWinding(lat, lng float64, ring []Point) bool {
	if len(ring) < 3 {
		return false
	}
	p := Point{Lat: lat, Lng: lng}
	eps := 1e-9
	n := len(ring)
	if ring[0] == ring[n-1] {
		n--
	}
	if n < 3 {
		return false
	}
	for i := 0; i < n; i++ {
		if onSegment(ring[i], ring[(i+1)%n], p, eps) {
			return true
		}
	}
	winding := 0
	for i := 0; i < n; i++ {
		a := ring[i]
		b := ring[(i+1)%n]
		if a.Lat <= p.Lat {
			if b.Lat > p.Lat && isLeft(a, b, p) > 0 {
				winding++
			}
		} else {
			if b.Lat <= p.Lat && isLeft(a, b, p) < 0 {
				winding--
			}
		}
	}
	return winding != 0
}

func isLeft(a, b, p Point) float64 {
	return (b.Lng-a.Lng)*(p.Lat-a.Lat) - (p.Lng-a.Lng)*(b.Lat-a.Lat)
}
