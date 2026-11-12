// Package sharing owns the public trip-share read models (token lookups,
// telemetry snapshot/polyline, timeline link, admin list) and the share-link
// write paths (create, sliding expiry/view counter, PIN lockout, revoke,
// driver-trip ownership). HTTP handlers keep the PIN cookie, ETA and rendering
// so internal/handlers/ stays an adapter layer
// (docs/tech-specs/handlers-end-state-adr.md, Phases 1-2).
package sharing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// Sentinel errors from the share write paths. The HTTP handler maps them to
// the exact status codes/messages it returned before the Phase 2 extraction
// (docs/tech-specs/handlers-end-state-adr.md).
var (
	// ErrTripNotFound means the trip does not exist or belongs to another tenant.
	ErrTripNotFound = errors.New("sharing: trip not found for tenant")
	// ErrMaxActive means the trip already holds the maximum number of live links.
	ErrMaxActive = errors.New("sharing: maximum active share links reached")
	// ErrLinkCreate means the share-link insert failed.
	ErrLinkCreate = errors.New("sharing: share link insert failed")
	// errNoDB guards writes attempted without a database handle.
	errNoDB = errors.New("sharing: no database")
)

// Create stores a new share link after checking the trip belongs to tenantID
// and the trip holds fewer than maxActive live links. It returns
// ErrTripNotFound, ErrMaxActive, ErrLinkCreate (wrapped) or a storage error.
//
// Any trip-lookup failure maps to ErrTripNotFound: the pre-extraction handler
// answered 404 for a missing row, a foreign tenant and a query error alike.
func (s *Service) Create(ctx context.Context, tenantID, tripID, linkID, tokenHash string, pinHash, pinSalt *string, createdBy string, now, expiresAt time.Time, maxActive int) (bool, error) {
	if s == nil || s.db == nil {
		return false, errNoDB
	}

	var tripTenantID string
	err := s.db.QueryRowContext(ctx,
		`SELECT tenant_id FROM trips WHERE id = $1`, tripID).Scan(&tripTenantID)
	if err != nil || tripTenantID != tenantID {
		return false, ErrTripNotFound
	}

	var activeCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM share_links
		WHERE trip_id = $1 AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP`,
		tripID).Scan(&activeCount); err != nil {
		return false, err
	}
	if activeCount >= maxActive {
		return false, ErrMaxActive
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO share_links (id, trip_id, token_hash, pin_hash, pin_salt, created_by, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		linkID, tripID, tokenHash, pinHash, pinSalt, createdBy, now, expiresAt,
	); err != nil {
		return false, fmt.Errorf("%w: %v", ErrLinkCreate, err)
	}
	return true, nil
}

// RecordView applies the sliding expiry and bumps the view counter for a
// successful public page view.
func (s *Service) RecordView(ctx context.Context, linkID string, newExpiry, now time.Time) error {
	if s == nil || s.db == nil {
		return errNoDB
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE share_links
		SET expires_at = $1, view_count = view_count + 1, last_viewed_at = $2
		WHERE id = $3`, newExpiry, now, linkID)
	return err
}

// RecordPINFailure stores the failed-PIN counter and lockout deadline.
func (s *Service) RecordPINFailure(ctx context.Context, linkID string, newAttempts int, lockedUntil *time.Time) error {
	if s == nil || s.db == nil {
		return errNoDB
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE share_links
		SET failed_pin_attempts = $1, locked_until = $2
		WHERE id = $3`, newAttempts, lockedUntil, linkID)
	return err
}

// RecordPINSuccess clears the failed-PIN counter/lock and applies the sliding
// expiry. newExpiry is part of the pre-extraction success update, so dropping
// it would silently change the link lifetime.
func (s *Service) RecordPINSuccess(ctx context.Context, linkID string, newExpiry time.Time) error {
	if s == nil || s.db == nil {
		return errNoDB
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE share_links
		SET failed_pin_attempts = 0, locked_until = NULL, expires_at = $1
		WHERE id = $2`, newExpiry, linkID)
	return err
}

// Revoke marks a tenant-scoped share link revoked and reports whether a row matched.
func (s *Service) Revoke(ctx context.Context, tenantID, shareID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, errNoDB
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE share_links
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND trip_id IN (SELECT id FROM trips WHERE tenant_id = $2)`,
		shareID, tenantID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// IsDriverOnTrip reports whether userID is the driver assigned to the trip.
// A missing/mismatched trip is (false, nil); storage failures are returned.
func (s *Service) IsDriverOnTrip(ctx context.Context, tenantID, tripID, userID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, errNoDB
	}
	var mine int
	err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM trips t
		WHERE t.id = $1 AND t.tenant_id = $2 AND t.driver_id <> ''
		  AND ( t.driver_id = $3
		        OR EXISTS (
		            SELECT 1 FROM drivers d
		            WHERE d.tenant_id = $2
		              AND (d.id = t.driver_id OR d.driver_id = t.driver_id)
		              AND ( d.id = $3
		                 OR d.email = (SELECT u.email FROM users u
		                                WHERE u.id = $3 AND u.tenant_id = $2) ) ) )`,
		tripID, tenantID, userID).Scan(&mine)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
