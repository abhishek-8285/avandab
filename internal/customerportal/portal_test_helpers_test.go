package customerportal

// Shared fixtures for the customerportal package tests (extracted read/write
// paths behind internal/handlers/customer_portal.go).

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const portalTestSchema = `
CREATE TABLE tenants (id TEXT PRIMARY KEY, name TEXT);
CREATE TABLE tenant_company_profiles (tenant_id TEXT PRIMARY KEY, company_name TEXT, logo_path TEXT);
CREATE TABLE company_settings (id INTEGER PRIMARY KEY, company_name TEXT, logo_path TEXT);
CREATE TABLE telemetry_snapshots (
    id TEXT PRIMARY KEY,
    vehicle_id TEXT,
    timestamp TEXT,
    latitude REAL,
    longitude REAL,
    speed REAL,
    ts_unix INTEGER
);
CREATE TABLE customers (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT);
CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT);
CREATE TABLE customer_users (id TEXT PRIMARY KEY, customer_id TEXT, user_id TEXT);
CREATE TABLE routes (id TEXT PRIMARY KEY, tenant_id TEXT, source TEXT, destination TEXT);
CREATE TABLE bookings (id TEXT PRIMARY KEY, tenant_id TEXT, customer_id TEXT, route_id TEXT);
CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, booking_id TEXT, trip_number TEXT);
CREATE TABLE trip_feedback (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    trip_id TEXT NOT NULL REFERENCES trips(id),
    customer_id TEXT NOT NULL,
    rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func newPortalTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("customerportal_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(portalTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecPortal(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

// seedPortalFeedbackFixture seeds two tenants with owned trips:
// tenant-1: u-1 -> c-1 -> t-1, u-2 -> c-2 -> t-2 (same route/bookings tenant),
// tenant-2: u-3 -> c-3 -> t-3.
func seedPortalFeedbackFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExecPortal(t, db, `INSERT INTO tenants (id, name) VALUES ('tenant-1', 'One'), ('tenant-2', 'Two')`)
	mustExecPortal(t, db, `INSERT INTO customers (id, tenant_id, name) VALUES ('c-1', 'tenant-1', 'C1'), ('c-2', 'tenant-1', 'C2'), ('c-3', 'tenant-2', 'C3')`)
	mustExecPortal(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'u1@x.in'), ('u-2', 'u2@x.in'), ('u-3', 'u3@x.in'), ('u-9', 'u9@x.in')`)
	mustExecPortal(t, db, `INSERT INTO customer_users (id, customer_id, user_id) VALUES ('cu-1', 'c-1', 'u-1'), ('cu-2', 'c-2', 'u-2'), ('cu-3', 'c-3', 'u-3')`)
	mustExecPortal(t, db, `INSERT INTO routes (id, tenant_id, source, destination) VALUES ('r-1', 'tenant-1', 'A', 'B'), ('r-2', 'tenant-2', 'C', 'D')`)
	mustExecPortal(t, db, `INSERT INTO bookings (id, tenant_id, customer_id, route_id) VALUES ('b-1', 'tenant-1', 'c-1', 'r-1'), ('b-2', 'tenant-1', 'c-2', 'r-1'), ('b-3', 'tenant-2', 'c-3', 'r-2')`)
	mustExecPortal(t, db, `INSERT INTO trips (id, tenant_id, booking_id, trip_number) VALUES ('t-1', 'tenant-1', 'b-1', 'TRP-1'), ('t-2', 'tenant-1', 'b-2', 'TRP-2'), ('t-3', 'tenant-2', 'b-3', 'TRP-3')`)
}
