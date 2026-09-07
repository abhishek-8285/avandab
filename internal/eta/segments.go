package eta

import (
	"context"
	"database/sql"
	"time"
)

// Segment is a contiguous in-transit run between stops.
type Segment struct {
	Start           string
	End             string
	DurationMinutes int
}

type pos struct {
	Lat        float64
	Lng        float64
	Speed      sql.NullFloat64
	DeviceTime time.Time
}

func parseDeviceTime(ns sql.NullString, nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time.UTC()
		return &t
	}
	if ns.Valid && ns.String != "" {
		for _, f := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05-07:00", "2006-01-02"} {
			if t, err := time.Parse(f, ns.String); err == nil {
				ut := t.UTC()
				return &ut
			}
		}
	}
	return nil
}

// extractSegments pulls telemetry_positions for a completed trip and groups
// consecutive in-transit points (speed > 5 km/h) into segments.
// Each segment is at least 2 minutes long; shorter runs are ignored.
func (s *EtaService) extractSegments(ctx context.Context, tripID string) ([]Segment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT latitude, longitude, speed, device_time, device_time
		 FROM telemetry_positions
		 WHERE trip_id = $1
		 ORDER BY device_time ASC`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []Segment
	var segStart *pos
	var segStartTime time.Time
	var lastPos *pos

	for rows.Next() {
		var p pos
		var lat, lng sql.NullFloat64
		var sp sql.NullFloat64
		var dtT sql.NullTime
		var dtS sql.NullString
		if err := rows.Scan(&lat, &lng, &sp, &dtT, &dtS); err != nil {
			return nil, err
		}
		if !lat.Valid || !lng.Valid {
			continue
		}
		p.Lat = lat.Float64
		p.Lng = lng.Float64
		p.Speed = sp
		if t := parseDeviceTime(dtS, dtT); t != nil {
			p.DeviceTime = *t
		} else {
			continue
		}
		lastPos = &pos{Lat: p.Lat, Lng: p.Lng, Speed: p.Speed, DeviceTime: p.DeviceTime}

		speed := 0.0
		if p.Speed.Valid {
			speed = p.Speed.Float64
		}
		if speed > 5.0 {
			if segStart == nil {
				cp := p
				segStart = &cp
				segStartTime = p.DeviceTime
			}
		} else {
			if segStart != nil {
				dur := int(p.DeviceTime.Sub(segStartTime).Minutes())
				if dur >= 2 {
					segments = append(segments, Segment{
						Start:           geohash6(segStart.Lat, segStart.Lng),
						End:             geohash6(p.Lat, p.Lng),
						DurationMinutes: dur,
					})
				}
				segStart = nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Close final segment if trip ended while in transit
	if segStart != nil && lastPos != nil {
		dur := int(lastPos.DeviceTime.Sub(segStartTime).Minutes())
		if dur >= 2 {
			segments = append(segments, Segment{
				Start:           geohash6(segStart.Lat, segStart.Lng),
				End:             geohash6(lastPos.Lat, lastPos.Lng),
				DurationMinutes: dur,
			})
		}
	}
	return segments, nil
}

// deriveTrafficTag classifies current conditions for ETA weighting.
func deriveTrafficTag(t time.Time) string {
	hour := t.Hour()
	month := t.Month()
	if month >= 6 && month <= 9 {
		return "monsoon"
	}
	if (hour >= 8 && hour <= 11) || (hour >= 17 && hour <= 20) {
		return "high"
	}
	if hour >= 22 || hour <= 5 {
		return "low"
	}
	return "medium"
}

// geohash6 encodes lat/lng to a 6-char geohash (base32, ~0.6km precision).
func geohash6(lat, lng float64) string {
	const base32 = "0123456789bcdefghjkmnpqrstuvwxyz"
	var hash string
	latMin, latMax := -90.0, 90.0
	lngMin, lngMax := -180.0, 180.0
	isEven := true
	bits, ch := 0, 0
	for len(hash) < 6 {
		var mid float64
		if isEven {
			mid = (lngMin + lngMax) / 2
			if lng >= mid {
				ch = ch<<1 | 1
				lngMin = mid
			} else {
				ch <<= 1
				lngMax = mid
			}
		} else {
			mid = (latMin + latMax) / 2
			if lat >= mid {
				ch = ch<<1 | 1
				latMin = mid
			} else {
				ch <<= 1
				latMax = mid
			}
		}
		isEven = !isEven
		bits++
		if bits == 5 {
			hash += string(base32[ch])
			bits, ch = 0, 0
		}
	}
	return hash
}
