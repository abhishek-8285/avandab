package application

// Driver identity resolution for the expense, e-POD and driver-money
// endpoints, extracted from internal/handlers/kharcha.go and
// internal/handlers/driver_money.go (handlers-end-state ADR, Phase 2).
//
// Legacy rule preserved verbatim: drivers.id = acting user id OR
// drivers.email = (SELECT email FROM users WHERE id = acting user), with NO
// tenant filter - mobile/driver endpoints resolve the driver from the
// authenticated user and the driver row itself carries the tenant. A missing
// driver row is a valid state (callers fall back / return not-linked); real
// lookup failures are returned so callers can log them instead of hiding them.

import (
	"context"
	"database/sql"
	"errors"
)

// DriverRef identifies the driver row behind an authenticated user.
type DriverRef struct {
	ID         string
	DriverCode string
}

// LookupUserDriver returns the driver row for the acting user via the legacy
// id-or-email identity rule. Returns (nil, nil) when no driver row exists and
// the raw error for real lookup failures.
func LookupUserDriver(ctx context.Context, db *sql.DB, userID string) (*DriverRef, error) {
	if db == nil {
		return nil, nil
	}
	var ref DriverRef
	var code sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, driver_id FROM drivers
		WHERE id = $1 OR email = (SELECT email FROM users WHERE id = $2)
		LIMIT 1`, userID, userID).Scan(&ref.ID, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ref.DriverCode = code.String
	return &ref, nil
}

// DriverOwnsTrip reports whether tripID is assigned to driverID inside tenantID.
// The tenant filter is intentional: a driver must not file expenses against a
// trip that belongs to another org even if ids collide across tenants.
func DriverOwnsTrip(ctx context.Context, db *sql.DB, tenantID, driverID, tripID string) (bool, error) {
	if tripID == "" {
		return true, nil
	}
	if db == nil {
		return false, nil
	}
	var assigned string
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(driver_id, '') FROM trips
		WHERE id = $1 AND tenant_id = $2`, tripID, tenantID).Scan(&assigned)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return assigned == driverID, nil
}
