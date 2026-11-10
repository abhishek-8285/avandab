// Package sharing owns the public trip-share read models (token lookups,
// telemetry snapshot/polyline, timeline link, admin list). HTTP handlers keep
// the PIN cookie, ETA, rendering and writes; queries live here so
// internal/handlers/ stays an adapter layer
// (docs/tech-specs/handlers-end-state-adr.md, Phase 1).
package sharing

import (
	"context"
	"database/sql"
	"time"
)

// Service runs share-link read queries.
type Service struct {
	db *sql.DB
}

// New builds a share read-model service over the app database.
func New(database *sql.DB) *Service {
	return &Service{db: database}
}

// Link is the share-link row needed by the public view page.
type Link struct {
	ID                string
	TripID            string
	PinHash           sql.NullString
	PinSalt           sql.NullString
	CreatedAt         time.Time
	ExpiresAt         time.Time
	LastViewedAt      sql.NullTime
	ViewCount         int
	FailedPinAttempts int
	LockedUntil       sql.NullTime
	RevokedAt         sql.NullTime
	TripNumber        string
	Status            string
	VehicleID         sql.NullString
	ArrivalTime       sql.NullString
}

// ByToken loads the share-link row (joined to its trip) for a token hash.
func (s *Service) ByToken(ctx context.Context, tokenHash string) (*Link, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	var l Link
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.trip_id, s.pin_hash, s.pin_salt, s.created_at, s.expires_at,
		       s.last_viewed_at, s.view_count, s.failed_pin_attempts, s.locked_until, s.revoked_at,
		       t.trip_number, t.status, t.vehicle_id, t.arrival_time
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&l.ID, &l.TripID, &l.PinHash, &l.PinSalt, &l.CreatedAt, &l.ExpiresAt,
		&l.LastViewedAt, &l.ViewCount, &l.FailedPinAttempts, &l.LockedUntil, &l.RevokedAt,
		&l.TripNumber, &l.Status, &l.VehicleID, &l.ArrivalTime,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// PINLink is the share-link row needed by PIN verification.
type PINLink struct {
	ID                string
	PinHash           sql.NullString
	PinSalt           sql.NullString
	CreatedAt         time.Time
	ExpiresAt         time.Time
	FailedPinAttempts int
	LockedUntil       sql.NullTime
	RevokedAt         sql.NullTime
}

// PINByToken loads the PIN-relevant columns for a token hash.
func (s *Service) PINByToken(ctx context.Context, tokenHash string) (*PINLink, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	var l PINLink
	err := s.db.QueryRowContext(ctx, `
		SELECT id, pin_hash, pin_salt, created_at, expires_at,
		       failed_pin_attempts, locked_until, revoked_at
		FROM share_links
		WHERE token_hash = $1`, tokenHash).Scan(
		&l.ID, &l.PinHash, &l.PinSalt, &l.CreatedAt, &l.ExpiresAt,
		&l.FailedPinAttempts, &l.LockedUntil, &l.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// DataLink is the share-link + trip/vehicle row needed by the public data API.
type DataLink struct {
	ID            string
	TripID        string
	PinHash       sql.NullString
	ExpiresAt     time.Time
	RevokedAt     sql.NullTime
	TripNumber    string
	Status        string
	VehicleID     sql.NullString
	ArrivalTime   sql.NullString
	RegNumber     sql.NullString
	VehicleNumber sql.NullString
}

// DataByToken loads the share-link row for the telemetry data endpoint.
func (s *Service) DataByToken(ctx context.Context, tokenHash string) (*DataLink, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	var l DataLink
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.trip_id, s.pin_hash, s.expires_at, s.revoked_at,
		       t.trip_number, t.status, t.vehicle_id, t.arrival_time,
		       v.registration_number, v.vehicle_number
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&l.ID, &l.TripID, &l.PinHash, &l.ExpiresAt, &l.RevokedAt,
		&l.TripNumber, &l.Status, &l.VehicleID, &l.ArrivalTime,
		&l.RegNumber, &l.VehicleNumber,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Snapshot is the latest telemetry fix for the shared vehicle.
type Snapshot struct {
	Found    bool
	Lat      *float64
	Lng      *float64
	Speed    *float64
	Fuel     *float64
	Odometer *float64
	LastSeen *time.Time
}

// Snapshot returns the latest usable telemetry fix for a vehicle.
func (s *Service) Snapshot(ctx context.Context, vehicleID string) Snapshot {
	out := Snapshot{}
	if s == nil || s.db == nil || vehicleID == "" {
		return out
	}
	var sLat, sLng, sSpeed, sFuel, sOdo sql.NullFloat64
	var sTs sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT latitude, longitude, speed, fuel_level, odometer, timestamp
		FROM telemetry_snapshots
		WHERE vehicle_id = $1 AND latitude IS NOT NULL AND longitude IS NOT NULL
		ORDER BY timestamp DESC LIMIT 1`, vehicleID).Scan(
		&sLat, &sLng, &sSpeed, &sFuel, &sOdo, &sTs,
	)
	if err != nil || !sLat.Valid || !sLng.Valid {
		return out
	}
	out.Found = true
	out.Lat = &sLat.Float64
	out.Lng = &sLng.Float64
	if sSpeed.Valid {
		out.Speed = &sSpeed.Float64
	}
	if sFuel.Valid {
		out.Fuel = &sFuel.Float64
	}
	if sOdo.Valid {
		out.Odometer = &sOdo.Float64
	}
	if sTs.Valid {
		tUTC := sTs.Time.UTC()
		out.LastSeen = &tUTC
	}
	return out
}

// MaintenanceDue reports the vehicle maintenance flag (false when unknown).
func (s *Service) MaintenanceDue(ctx context.Context, vehicleID string) bool {
	if s == nil || s.db == nil || vehicleID == "" {
		return false
	}
	var md sql.NullBool
	if err := s.db.QueryRowContext(ctx, `SELECT maintenance_due FROM vehicles WHERE id = $1`, vehicleID).Scan(&md); err != nil {
		return false
	}
	return md.Valid && md.Bool
}

// Polyline returns the last 500 GPS points for a trip (empty on error).
func (s *Service) Polyline(ctx context.Context, tripID string) [][]float64 {
	if s == nil || s.db == nil || tripID == "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT latitude, longitude
		FROM telemetry_positions
		WHERE trip_id = $1
		ORDER BY device_time ASC
		LIMIT 500`, tripID)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var polyline [][]float64
	for rows.Next() {
		var pLat, pLng float64
		if err := rows.Scan(&pLat, &pLng); err == nil {
			polyline = append(polyline, []float64{pLat, pLng})
		}
	}
	return polyline
}

// TimelineLink is the share-link + trip tenant row needed by the CX timeline.
type TimelineLink struct {
	TripID    string
	TenantID  string
	PinHash   sql.NullString
	ExpiresAt time.Time
	RevokedAt sql.NullTime
}

// TimelineByToken loads the share-link row for the public timeline endpoint.
func (s *Service) TimelineByToken(ctx context.Context, tokenHash string) (*TimelineLink, error) {
	if s == nil || s.db == nil {
		return nil, sql.ErrNoRows
	}
	var l TimelineLink
	err := s.db.QueryRowContext(ctx, `
		SELECT s.trip_id, t.tenant_id, s.pin_hash, s.expires_at, s.revoked_at
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		WHERE s.token_hash = $1`, tokenHash).Scan(
		&l.TripID, &l.TenantID, &l.PinHash, &l.ExpiresAt, &l.RevokedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LinkItem is one row of the administrative share-link list.
type LinkItem struct {
	ID                string
	TripID            string
	TripNumber        string
	TokenHash         string
	HasPIN            bool
	CreatedBy         string
	CreatorName       string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	LastViewedAt      *time.Time
	ViewCount         int
	FailedPinAttempts int
	LockedUntil       *time.Time
	RevokedAt         *time.Time
	Status            string
}

// List returns the tenant's share links, newest first, with derived status.
func (s *Service) List(ctx context.Context, tenantID string) ([]LinkItem, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.trip_id, t.trip_number, s.created_by, COALESCE(u.name, s.created_by),
		       s.created_at, s.expires_at, s.last_viewed_at, s.view_count,
		       s.pin_hash IS NOT NULL, s.revoked_at
		FROM share_links s
		JOIN trips t ON t.id = s.trip_id
		LEFT JOIN users u ON u.id = s.created_by
		WHERE t.tenant_id = $1
		ORDER BY s.created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	now := time.Now().UTC()
	var out []LinkItem
	for rows.Next() {
		var item LinkItem
		var lastViewed, revoked sql.NullTime
		var hasPIN int
		if err := rows.Scan(
			&item.ID, &item.TripID, &item.TripNumber, &item.CreatedBy, &item.CreatorName,
			&item.CreatedAt, &item.ExpiresAt, &lastViewed, &item.ViewCount,
			&hasPIN, &revoked,
		); err != nil {
			continue
		}
		item.HasPIN = hasPIN != 0
		if lastViewed.Valid {
			item.LastViewedAt = &lastViewed.Time
		}
		if revoked.Valid {
			item.RevokedAt = &revoked.Time
			item.Status = "revoked"
		} else if item.ExpiresAt.Before(now) {
			item.Status = "expired"
		} else {
			item.Status = "active"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
