package customerportal

// Shipper-portal telemetry "last seen" fix + SQLite timestamp parsing,
// extracted from internal/handlers/customer_portal.go (handlers-end-state
// ADR, Phase 2). The handler keeps thin parseTimeFlex/lastSeenFix wrappers so
// its existing characterization test and ETA fallback keep their signatures.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ParseTimeFlex parses DATETIME strings from SQLite (multiple formats).
func ParseTimeFlex(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	// Fallback: try truncating to 19 chars (sqlite datetime)
	if len(s) >= 19 {
		if t, err := time.Parse("2006-01-02 15:04:05", s[:19]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// LastSeenFix returns the newest telemetry fix recorded for a vehicle.
//
// Ordering uses ts_unix (migration 00134), not the `timestamp` text: that
// column mixes RFC3339 ("2026-06-01T10:00:00Z") and Go's time.String()
// layout ("2026-06-01 15:00:00 +0000 UTC") - 'T' > ' ' - so ORDER BY
// timestamp DESC sorts bytes and hands back a different row than the newest
// fix.
//
// Returns (nil, nil, "", nil) when the vehicle has never reported. A real
// query failure is returned to the caller so it can be logged: a dead table
// and an unreported vehicle must not look identical to the shipper.
func LastSeenFix(ctx context.Context, db *sql.DB, vehicleID string) (lat, lng *float64, ts string, err error) {
	if db == nil {
		return nil, nil, "", nil
	}
	var sLat, sLng sql.NullFloat64
	var sTs sql.NullString
	err = db.QueryRowContext(ctx, `
		SELECT latitude, longitude, timestamp
		FROM telemetry_snapshots
		WHERE vehicle_id = $1 AND latitude IS NOT NULL AND longitude IS NOT NULL
		ORDER BY ts_unix DESC LIMIT 1`, vehicleID).Scan(&sLat, &sLng, &sTs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, "", nil
	}
	if err != nil {
		return nil, nil, "", err
	}
	if sLat.Valid && sLng.Valid {
		vLat, vLng := sLat.Float64, sLng.Float64
		lat, lng = &vLat, &vLng
	}
	if sTs.Valid && sTs.String != "" {
		if t := ParseTimeFlex(sTs.String); !t.IsZero() {
			ts = t.UTC().Format(time.RFC3339)
		} else {
			ts = sTs.String
		}
	}
	return lat, lng, ts, nil
}
