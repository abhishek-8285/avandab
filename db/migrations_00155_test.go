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

func TestMigration00155_AccessReviews_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00155_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "migrations", 155))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM access_reviews`).Scan(&count))
	assert.Equal(t, 0, count)

	// Tenant trigger rejects unknown tenant.
	_, err = db.Exec(`INSERT INTO access_reviews (id, tenant_id, user_id, period, due_at)
		VALUES ('r-bad', 'non-existent-tenant-888', 'u1', '2026-H2', '2026-06-30 00:00:00')`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger")

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-r1', 'Review Co', 'review-co')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO access_reviews (id, tenant_id, user_id, role_name, period, due_at)
		VALUES ('r-1', 't-r1', 'u1', 'dispatcher', '2026-H2', '2026-06-30 00:00:00')`)
	require.NoError(t, err)

	// One row per (tenant, user, period).
	_, err = db.Exec(`INSERT INTO access_reviews (id, tenant_id, user_id, period, due_at)
		VALUES ('r-2', 't-r1', 'u1', '2026-H2', '2026-06-30 00:00:00')`)
	require.Error(t, err, "expected UNIQUE rejection on second row for same period")

	// Same user + different period is a new review.
	_, err = db.Exec(`INSERT INTO access_reviews (id, tenant_id, user_id, period, due_at)
		VALUES ('r-3', 't-r1', 'u1', '2027-H1', '2026-12-31 00:00:00')`)
	require.NoError(t, err)

	// CHECK backstop on status.
	_, err = db.Exec(`INSERT INTO access_reviews (id, tenant_id, user_id, period, status, due_at)
		VALUES ('r-4', 't-r1', 'u2', '2026-H2', 'bogus', '2026-06-30 00:00:00')`)
	require.Error(t, err, "expected CHECK rejection on bad status")

	// No new permission: routes reuse privacy:manage (00154 backfill).
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM permissions WHERE name = 'access:review'`).Scan(&count))
	assert.Equal(t, 0, count, "00155 must not seed a new permission")

	require.NoError(t, goose.Down(db, "migrations"))

	_, err = db.Exec(`SELECT COUNT(*) FROM access_reviews`)
	require.Error(t, err, "expected access_reviews gone after rollback")
}
