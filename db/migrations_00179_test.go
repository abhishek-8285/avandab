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

func TestMigration00179_PrivacyDSARRequests_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00179_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 178))

	// Verify table does not exist prior to 179
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='privacy_dsar_requests'`).Scan(&count))
	assert.Equal(t, 0, count, "privacy_dsar_requests table must not exist before v179")

	// Apply migration 00179
	require.NoError(t, goose.UpTo(db, "migrations", 179))

	// Verify table created
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='privacy_dsar_requests'`).Scan(&count))
	assert.Equal(t, 1, count, "table privacy_dsar_requests must exist after v179")

	// Verify index created
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_privacy_dsar_requests_tenant_user'`).Scan(&count))
	assert.Equal(t, 1, count, "idx_privacy_dsar_requests_tenant_user must exist after v179")

	// Insert test row
	_, err = db.Exec(`
		INSERT INTO privacy_dsar_requests (id, tenant_id, user_id, request_type, status, requested_by, details, created_at)
		VALUES ('dsar_test_1', '1', 'usr_test', 'export', 'completed', 'usr_test', 'Test export', datetime('now'))
	`)
	require.NoError(t, err, "insert into privacy_dsar_requests must succeed")

	// Down migration
	require.NoError(t, goose.DownTo(db, "migrations", 178))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='privacy_dsar_requests'`).Scan(&count))
	assert.Equal(t, 0, count, "privacy_dsar_requests table must be dropped on down migration")
}
