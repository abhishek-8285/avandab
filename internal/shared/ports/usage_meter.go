package ports

import (
	"context"
	"database/sql"

	"transport-app/internal/shared"
)

// UsageMeter records metered consumption (quota) for billable operations.
// Implemented by the entitlement service; nil means metering off (tests,
// tooling). All methods are safe to retry: reserve/commit dedupe on the
// booking id, and missing subscriptions are a no-op so bootstrap/legacy
// tenants are never blocked by metering writes.
type UsageMeter interface {
	ReserveBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
	CommitBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
	ReleaseBooking(ctx context.Context, tx *sql.Tx, tenantID shared.TenantID, bookingID string) error
}
