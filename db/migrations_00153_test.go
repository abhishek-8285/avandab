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

func TestMigration00153_UserConsents_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00153_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "migrations", 153))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM user_consents`).Scan(&count))
	assert.Equal(t, 0, count)

	// Tenant trigger rejects unknown tenant.
	_, err = db.Exec(`INSERT INTO user_consents (id, tenant_id, user_id, purpose)
		VALUES ('c-bad', 'non-existent-tenant-888', 'u1', 'platform_use')`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger")

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-c1', 'Consent Co', 'consent-co')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO user_consents (id, tenant_id, user_id, purpose, notice_version)
		VALUES ('c-1', 't-c1', 'u1', 'platform_use', 'v1')`)
	require.NoError(t, err)

	// One row per (tenant, user, purpose).
	_, err = db.Exec(`INSERT INTO user_consents (id, tenant_id, user_id, purpose)
		VALUES ('c-2', 't-c1', 'u1', 'platform_use')`)
	require.Error(t, err, "expected UNIQUE rejection on second grant row")

	require.NoError(t, goose.Down(db, "migrations"))

	_, err = db.Exec(`SELECT COUNT(*) FROM user_consents`)
	require.Error(t, err, "expected user_consents gone after rollback")
}
