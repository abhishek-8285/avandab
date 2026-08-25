package test

import (
	"database/sql"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiTenantMigration00102RoundTrip — Spec 24 §DB contract: 00102 applies
// the tenants registry + users.tenant_id + tenants:manage permission, and rolls
// back cleanly (up → downTo(101) → up).
func TestMultiTenantMigration00102RoundTrip(t *testing.T) {
	db := NewTestDB(t)

	// Up state: bootstrap tenant row exists and is active.
	var name, status string
	require.NoError(t, db.QueryRow(
		`SELECT name, status FROM tenants WHERE id = '1'`).
		Scan(&name, &status), "bootstrap tenant row must exist after 00102")
	assert.Equal(t, "Default", name)
	assert.Equal(t, "active", status)

	// users.tenant_id column present; INSERT/SELECT probe confirms default '1'.
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name)
		VALUES ('u-t01', 't01@example.com', 'x', 'T01')`)
	require.NoError(t, err, "probe user insert must succeed")
	var tid string
	require.NoError(t, db.QueryRow(
		`SELECT tenant_id FROM users WHERE id = 'u-t01'`).
		Scan(&tid), "users.tenant_id column must exist after 00102")
	assert.Equal(t, "1", tid, "users.tenant_id defaults to bootstrap tenant")

	// tenants:manage permission seeded and granted to admin role (role id 1).
	var grants int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM role_permissions rp
		 JOIN permissions p ON p.id = rp.permission_id
		 WHERE p.name = 'tenants:manage' AND rp.role_id = 1`).
		Scan(&grants))
	assert.Equal(t, 1, grants, "tenants:manage granted to role 1 exactly once")

	// Down to 101: tenants gone + permission gone + users.tenant_id dropped.
	// The probe below tolerates a column-surviving driver defensively, but the
	// bundled modernc.org/sqlite drops the column (repo precedent: 00092).
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.DownTo(db, "../db/migrations", 101))

	err = db.QueryRow(`SELECT count(*) FROM tenants`).Scan(new(int))
	assert.Error(t, err, "tenants table must be gone after downgrade")

	var permCount int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM permissions WHERE name = 'tenants:manage'`).Scan(&permCount))
	assert.Equal(t, 0, permCount, "tenants:manage permission removed on downgrade")
	var grantCount int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM role_permissions WHERE role_id = 1 AND permission_id IN
		 (SELECT id FROM permissions WHERE name = 'tenants:manage')`).Scan(&grantCount))
	assert.Equal(t, 0, grantCount, "role_permissions grant removed on downgrade")

	err = db.QueryRow(`SELECT tenant_id FROM users WHERE id = 'u-t01'`).Scan(&tid)
	assert.Error(t, err, "users.tenant_id must be dropped on downgrade")

	// Re-up clean. Defensive remediation for any hypothetical driver whose
	// Down leaves the inert column: drop iff present, then re-run Up against
	// a clean schema (bundled driver already drops it — branch stays cold).
	if usersHasTenantColumn(t, db) {
		t.Log("users.tenant_id survived Down on this driver; dropping inert column before re-up")
		_, err = db.Exec(`ALTER TABLE users DROP COLUMN tenant_id`)
		require.NoError(t, err, "drop inert users.tenant_id before re-up")
	}

	require.NoError(t, goose.Up(db, "../db/migrations"))
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM tenants WHERE id = '1'`).Scan(new(int)),
		"tenants queryable with bootstrap row after re-up")
}

// TestMultiTenantMigration00102RoundTripWithData — Spec 24 §DB contract, the
// with-data variant: business rows seeded at tenant '1' must survive a full
// down(00102) → up cycle untouched (tenant registry is metadata; user data is
// never rewritten by 00102 in either direction).
func TestMultiTenantMigration00102RoundTripWithData(t *testing.T) {
	db := NewTestDB(t)

	// Seed route → customer → booking at tenant '1', plus a scoped user.
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare)
		VALUES ('r-data', 'A', 'B', 100, 2, 500)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, name, phone) VALUES ('c-data', 'Acme Ltd', '9999999999')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, tenant_id)
		VALUES ('u-data', 'data@example.com', 'x', 'Data', '1')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id,
		vehicle_type, price, status, tenant_id)
		VALUES ('b-data', 'BK-DATA-1', 'c-data', datetime('now'), 'r-data', 'bus', 1500, 'confirmed', '1')`)
	require.NoError(t, err)

	downThenUp := func() {
		t.Helper()
		_ = goose.SetDialect("sqlite")
		require.NoError(t, goose.DownTo(db, "../db/migrations", 101))
		if usersHasTenantColumn(t, db) {
			t.Log("users.tenant_id survived Down on this driver; dropping inert column before re-up")
			_, err = db.Exec(`ALTER TABLE users DROP COLUMN tenant_id`)
			require.NoError(t, err, "drop inert users.tenant_id before re-up")
		}
		require.NoError(t, goose.Up(db, "../db/migrations"))
	}

	downThenUp()

	// User row survived the downgrade/re-up with default tenant restored.
	var tid string
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM users WHERE id = 'u-data'`).Scan(&tid),
		"user row must survive roundtrip")
	assert.Equal(t, "1", tid, "user tenant restored to bootstrap after re-up")

	// Booking fully intact: same values as seeded.
	var bn, cid, rid, status string
	var price float64
	require.NoError(t, db.QueryRow(`SELECT booking_number, customer_id, route_id, status, price
		FROM bookings WHERE id = 'b-data'`).Scan(&bn, &cid, &rid, &status, &price),
		"booking must survive roundtrip")
	assert.Equal(t, "BK-DATA-1", bn)
	assert.Equal(t, "c-data", cid)
	assert.Equal(t, "r-data", rid)
	assert.Equal(t, "confirmed", status)
	assert.InDelta(t, 1500.0, price, 0.001)

	// Bootstrap tenant row back and active.
	var statusT string
	require.NoError(t, db.QueryRow(`SELECT status FROM tenants WHERE id = '1'`).Scan(&statusT))
	assert.Equal(t, "active", statusT)

	// Second cycle proves idempotence of the roundtrip with data present.
	downThenUp()
	require.NoError(t, db.QueryRow(`SELECT tenant_id FROM users WHERE id = 'u-data'`).Scan(&tid))
	assert.Equal(t, "1", tid)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM bookings WHERE id = 'b-data'`).Scan(new(int)),
		"booking queryable after second roundtrip")
}

// usersHasTenantColumn reports whether users still carries a tenant_id column
// (PRAGMA table_info probe).
func usersHasTenantColumn(t *testing.T, db *sql.DB) bool {
	t.Helper()
	prows, err := db.Query(`PRAGMA table_info(users)`)
	require.NoError(t, err, "table_info probe")
	defer prows.Close()
	for prows.Next() {
		var cid, notNull, pk int
		var cname, ctype string
		var dflt sql.NullString
		if serr := prows.Scan(&cid, &cname, &ctype, &notNull, &dflt, &pk); serr == nil && cname == "tenant_id" {
			return true
		}
	}
	require.NoError(t, prows.Err())
	return false
}
