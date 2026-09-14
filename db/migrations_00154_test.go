package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigration00154_BreachIncidents_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00154_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "migrations", 154))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM breach_incidents`).Scan(&count))
	assert.Equal(t, 0, count)

	// Tenant trigger rejects unknown tenant.
	_, err = db.Exec(`INSERT INTO breach_incidents (id, tenant_id, title)
		VALUES ('b-bad', 'non-existent-tenant-888', 'x')`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger")

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-b1', 'Breach Co', 'breach-co')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO breach_incidents (id, tenant_id, title, affected_count)
		VALUES ('b-1', 't-b1', 'lost laptop', 3)`)
	require.NoError(t, err)

	// privacy:manage backfilled to roles 1 (admin) and 6 (org_admin).
	for _, role := range []int{1, 6} {
		var n int
		require.NoError(t, db.QueryRow(`
			SELECT COUNT(*) FROM role_permissions rp
			JOIN permissions p ON p.id = rp.permission_id
			WHERE rp.role_id = $1 AND p.name = 'privacy:manage'`, role).Scan(&n))
		assert.Equal(t, 1, n, "role %d must hold privacy:manage", role)
	}

	// CHECK backstop on status.
	_, err = db.Exec(`INSERT INTO breach_incidents (id, tenant_id, title, status)
		VALUES ('b-2', 't-b1', 'x', 'bogus')`)
	require.Error(t, err, "expected CHECK rejection on bad status")

	require.NoError(t, goose.Down(db, "migrations"))

	_, err = db.Exec(`SELECT COUNT(*) FROM breach_incidents`)
	require.Error(t, err, "expected breach_incidents gone after rollback")
}
