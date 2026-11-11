package application

// Characterization + extraction tests for the compliance dashboard
// aggregation moved out of internal/handlers/compliance.go
// (handlers-end-state ADR, Phase 2): tenant isolation vs the bootstrap global
// view, blocked/expired/expiring-soon rules and pending-document counts.
//
// Expiry columns are declared TEXT in this fixture because the legacy query
// scans them into sql.NullString and parses "2006-01-02"; that string-date
// contract is preserved verbatim.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const dashboardTestSchema = `
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    license_expiry TEXT,
    status TEXT NOT NULL DEFAULT 'available',
    blocked INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT
);
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    insurance_expiry TEXT,
    fitness_expiry TEXT,
    permit_expiry TEXT,
    puc_expiry TEXT,
    status TEXT NOT NULL DEFAULT 'available',
    blocked INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT
);
CREATE TABLE driver_documents (id TEXT PRIMARY KEY, driver_id TEXT, status TEXT);
CREATE TABLE vehicle_documents (id TEXT PRIMARY KEY, vehicle_id TEXT, status TEXT);
`

func newDashboardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("compliance_dash_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(dashboardTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecDash(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestDashboardQueries_TenantIsolationAndBootstrap(t *testing.T) {
	db := newDashboardTestDB(t)
	ctx := context.Background()
	// tenant-a: 2 drivers (1 blocked), 1 vehicle, 1+1 pending docs.
	mustExecDash(t, db, `INSERT INTO drivers (id, tenant_id, status, blocked, blocked_reason) VALUES
		('drv-a1', 'tenant-a', 'available', 0, ''),
		('drv-a2', 'tenant-a', 'available', 1, 'license expired')`)
	mustExecDash(t, db, `INSERT INTO vehicles (id, tenant_id, status, blocked) VALUES ('veh-a1', 'tenant-a', 'available', 0)`)
	mustExecDash(t, db, `INSERT INTO driver_documents (id, driver_id, status) VALUES ('ddoc-a1', 'drv-a1', 'pending_review')`)
	mustExecDash(t, db, `INSERT INTO vehicle_documents (id, vehicle_id, status) VALUES ('vdoc-a1', 'veh-a1', 'pending_review')`)
	// tenant-b must stay invisible to tenant-a.
	mustExecDash(t, db, `INSERT INTO drivers (id, tenant_id, status, blocked, blocked_reason) VALUES ('drv-b1', 'tenant-b', 'available', 1, 'permit expired')`)
	mustExecDash(t, db, `INSERT INTO driver_documents (id, driver_id, status) VALUES ('ddoc-b1', 'drv-b1', 'pending_review')`)

	svc := NewDashboardQueries(db)
	a, err := svc.Overview(ctx, "tenant-a")
	require.NoError(t, err)
	assert.Equal(t, 2, a.Drivers.Total)
	assert.Equal(t, 1, a.Drivers.Blocked)
	require.Len(t, a.BlockedDrivers, 1)
	assert.Equal(t, "drv-a2", a.BlockedDrivers[0].ID)
	assert.Equal(t, "license expired", a.BlockedDrivers[0].Reason)
	assert.Equal(t, 1, a.Vehicles.Total)
	assert.Equal(t, 2, a.DocumentsPending, "only tenant-a's pending docs")

	b, err := svc.Overview(ctx, "tenant-b")
	require.NoError(t, err)
	assert.Equal(t, 1, b.Drivers.Total)
	assert.Equal(t, 1, b.DocumentsPending)

	// Bootstrap ('1') and empty tenant keep the legacy global aggregation.
	boot, err := svc.Overview(ctx, "1")
	require.NoError(t, err)
	assert.Equal(t, 3, boot.Drivers.Total)
	assert.Equal(t, 3, boot.DocumentsPending)
	global, err := svc.Overview(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, 3, global.Drivers.Total)

	_, err = NewDashboardQueries(nil).Overview(ctx, "tenant-a")
	require.Error(t, err, "database unavailable")
}

func TestDashboardQueries_ExpiryBlockedAndExpiringRules(t *testing.T) {
	db := newDashboardTestDB(t)
	ctx := context.Background()
	now := time.Now().Truncate(24 * time.Hour)
	expired := now.AddDate(0, 0, -30).Format("2006-01-02")
	soon := now.AddDate(0, 0, 3).Format("2006-01-02")
	future := now.AddDate(0, 0, 30).Format("2006-01-02")

	mustExecDash(t, db, `INSERT INTO drivers (id, tenant_id, license_expiry, status, blocked, blocked_reason) VALUES
		('drv-expired', 'tenant-x', ?, 'available', 0, ''),
		('drv-soon', 'tenant-x', ?, 'available', 0, ''),
		('drv-flag', 'tenant-x', ?, 'available', 1, ''),
		('drv-status', 'tenant-x', ?, 'blocked', 0, '')`,
		expired, soon, future, future)
	mustExecDash(t, db, `INSERT INTO vehicles (id, tenant_id, insurance_expiry, fitness_expiry, permit_expiry, puc_expiry, status, blocked) VALUES
		('veh-ins', 'tenant-x', ?, ?, ?, ?, 'available', 0),
		('veh-puc-soon', 'tenant-x', ?, ?, ?, ?, 'available', 0),
		('veh-flag', 'tenant-x', ?, ?, ?, ?, 'available', 1)`,
		expired, future, future, future,
		future, future, future, soon,
		future, future, future, future)
	mustExecDash(t, db, `INSERT INTO driver_documents (id, driver_id, status) VALUES ('d-pending', 'drv-expired', 'pending_review'), ('d-verified', 'drv-soon', 'verified')`)
	mustExecDash(t, db, `INSERT INTO vehicle_documents (id, vehicle_id, status) VALUES ('v-pending', 'veh-ins', 'pending_review')`)

	data, err := NewDashboardQueries(db).Overview(ctx, "tenant-x")
	require.NoError(t, err)
	assert.Equal(t, 4, data.Drivers.Total)
	assert.Equal(t, 3, data.Drivers.Blocked, "expired-license + blocked-flag + blocked-status drivers")
	assert.Equal(t, 1, data.Drivers.ExpiringSoon, "license expiring within 7 days")
	require.Len(t, data.BlockedDrivers, 3)
	assert.Equal(t, "License expired "+expired, data.BlockedDrivers[0].Reason)
	assert.Equal(t, "driver blocked", data.BlockedDrivers[1].Reason, "legacy default reason for empty blocked_reason")

	assert.Equal(t, 3, data.Vehicles.Total)
	assert.Equal(t, 2, data.Vehicles.Blocked, "expired insurance + blocked flag")
	assert.Equal(t, 1, data.Vehicles.ExpiringSoon, "PUC expiring within 7 days")
	require.Len(t, data.BlockedVehicles, 2)
	assert.Equal(t, "Insurance expired "+expired, data.BlockedVehicles[0].Reason)
	assert.Equal(t, "vehicle blocked", data.BlockedVehicles[1].Reason)

	assert.Equal(t, 2, data.DocumentsPending, "only pending_review docs count")
}
