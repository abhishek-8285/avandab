package application

// Characterization + extraction tests for the per-tenant operational purge
// moved out of internal/handlers/tenant_purge.go (handlers-end-state ADR,
// Phase 2): preview vs execute, tenant isolation, FK-safe delete ordering
// (children before parents with foreign_keys ON), protected-table survival,
// bootstrap/unknown-tenant guards, dry-run semantics and idempotency.
// Platform-only authorization (tenants:manage) lives on the handler route
// gate and stays pinned by TestTenants_RoutesForbiddenWithoutManage.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared"
)

// seedPurgeTenantRows seeds the FK chain the purge must delete in order:
// customers/routes -> bookings -> trips, plus drivers/vehicles and the
// trip/driver/vehicle-scoped driver_behaviour_events row (deleted through
// the global predicate list, before its referenced parents).
func seedPurgeTenantRows(t *testing.T, db *sql.DB, tenant, suffix string) {
	t.Helper()
	seedPrivacyTenant(t, db, tenant)
	_, err := db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES (?, ?, ?, '9000000001')`,
		"c-"+suffix, tenant, "Customer "+suffix)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES (?, ?, 'A', 'B', 10, 1, 100)`,
		"r-"+suffix, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id, version)
		VALUES (?, ?, ?, datetime('now'), ?, 'truck', 1, 100, 'confirmed', ?, 1)`,
		"b-"+suffix, "BK-"+suffix, "c-"+suffix, "r-"+suffix, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id, version)
		VALUES (?, ?, ?, ?, datetime('now'), 'scheduled', ?, 1)`,
		"t-"+suffix, "TR-"+suffix, "b-"+suffix, "r-"+suffix, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES (?, ?, 'Purge', 'Driver', '9000000002', 'available', ?)`,
		"d-"+suffix, "DRV-"+suffix, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vehicles (id, vehicle_number, registration_number, vehicle_type, capacity, fuel_type, status, tenant_id)
		VALUES (?, ?, ?, 'truck', 10, 'diesel', 'available', ?)`,
		"v-"+suffix, "VEH-"+suffix, "REG-"+suffix, tenant)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_behaviour_events (id, driver_id, trip_id, vehicle_id, event_type, severity, metadata, occurred_at)
		VALUES (?, ?, ?, ?, 'speeding', 'high', '{}', datetime('now'))`,
		"ev-"+suffix, "d-"+suffix, "t-"+suffix, "v-"+suffix)
	require.NoError(t, err)
}

func countPurgeRows(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func TestTenantPurgeService_PreviewExecuteIsolationAndIdempotency(t *testing.T) {
	db := newPrivacyTestDB(t)
	ctx := context.Background()
	seedPurgeTenantRows(t, db, "tenant-px", "px")
	seedPurgeTenantRows(t, db, "tenant-other", "ot")
	// Protected table row on the purged tenant: users must survive.
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES ('u-px', 'px@x.in', 'x', 'PX', 6, 'active', 'tenant-px')`)
	require.NoError(t, err)

	purge := NewTenantPurgeService(db)

	// 1. Preview is a dry-run: counts only, nothing deleted.
	preview, err := purge.Preview(ctx, "tenant-px")
	require.NoError(t, err)
	for _, table := range []string{"customers", "routes", "bookings", "trips", "drivers", "vehicles", "driver_behaviour_events"} {
		assert.Equal(t, int64(1), preview[table], "preview count for "+table)
	}
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM bookings WHERE tenant_id = 'tenant-px'`), "preview must not delete")
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM trips WHERE tenant_id = 'tenant-px'`), "preview must not delete")
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM driver_behaviour_events WHERE id = 'ev-px'`))

	// 2. Execute deletes the tenant's chain in FK-safe order and reports per-table counts.
	deleted, err := purge.Execute(ctx, "tenant-px")
	require.NoError(t, err)
	for _, table := range []string{"customers", "routes", "bookings", "trips", "drivers", "vehicles", "driver_behaviour_events"} {
		assert.Equal(t, int64(1), deleted[table], "deleted count for "+table)
	}
	for _, table := range []string{"bookings", "trips", "customers", "routes", "drivers", "vehicles"} {
		assert.Zero(t, countPurgeRows(t, db, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant-px'`), table+" must be empty")
	}
	assert.Zero(t, countPurgeRows(t, db, `SELECT COUNT(*) FROM driver_behaviour_events WHERE id = 'ev-px'`), "global-predicate child row must be deleted")

	// Tenant isolation: the other tenant's identical chain is untouched.
	for _, table := range []string{"bookings", "trips", "customers", "routes", "drivers", "vehicles"} {
		assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM `+table+` WHERE tenant_id = 'tenant-other'`), table+" for tenant-other must survive")
	}
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM driver_behaviour_events WHERE id = 'ev-ot'`))

	// Protected rows survive: users and the tenant registry row.
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM users WHERE tenant_id = 'tenant-px'`), "users survive purge")
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM tenants WHERE id = 'tenant-px'`), "tenant row survives purge")

	// 3. Idempotent re-execute: tenant row still exists, no rows left, empty report.
	again, err := purge.Execute(ctx, "tenant-px")
	require.NoError(t, err)
	assert.Empty(t, again, "a second purge is a no-op")
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM tenants WHERE id = 'tenant-px'`))
}

func TestTenantPurgeService_GuardsAndPreviewSemantics(t *testing.T) {
	db := newPrivacyTestDB(t)
	ctx := context.Background()
	seedPurgeTenantRows(t, db, "tenant-guard", "gd")
	purge := NewTenantPurgeService(db)

	// Empty id is rejected before any statement runs.
	_, err := purge.Execute(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant id required")

	// The bootstrap tenant (platform owner org) can never be purged.
	_, err = purge.Execute(ctx, string(shared.DefaultTenant))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bootstrap tenant cannot be purged")

	// Unknown tenant is reported with the legacy quoted message.
	_, err = purge.Execute(ctx, "tenant-ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant-ghost")
	assert.Contains(t, err.Error(), "not found")

	// Legacy preview has no empty-id guard: it reports an empty count set.
	preview, err := purge.Preview(ctx, "")
	require.NoError(t, err)
	assert.Empty(t, preview)

	// Preview for the unknown tenant is empty too (nothing to delete).
	preview, err = purge.Preview(ctx, "tenant-ghost")
	require.NoError(t, err)
	assert.Empty(t, preview)

	// A refused execute never touches data.
	assert.Equal(t, int64(1), countPurgeRows(t, db, `SELECT COUNT(*) FROM bookings WHERE tenant_id = 'tenant-guard'`))
}
