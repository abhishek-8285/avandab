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
