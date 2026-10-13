package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestMigration00167_AuditLogsTenantScope_UpAndDown proves per-org attribution:
// actor rows follow users.tenant_id, tenants-table rows stay platform ('1'),
// actor-less trip rows follow trips.tenant_id, orphans fall back to '1'.
func TestMigration00167_AuditLogsTenantScope_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00167_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 166))

	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-a', 'A', 'a'), ('t-b', 'B', 'b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES ('u-a', 'a@x.com', 'h', 'A User', 6, 'active', 't-a'),
		       ('u-plat', 'plat@x.com', 'h', 'Admin User', 1, 'active', '1')`)
	require.NoError(t, err)
	// FK pragmas are off in tests so a parentless trip row is fine.
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, tenant_id)
		VALUES ('trip-b', 'TRIP-B', 'route-x', '2026-09-01T08:00:00Z', 't-b')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO audit_logs (id, user_id, action, table_name, record_id)
		VALUES ('log-actor', 'u-a', 'login', 'users', 'u-a'),
		       ('log-tenant', 'u-plat', 'tenant.suspend', 'tenants', 't-b'),
		       ('log-sys', NULL, 'pod_otp_issued', 'trips', 'trip-b'),
		       ('log-orphan', 'gone-user', 'login', 'users', 'gone-user')`)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, "migrations", 167))

	tenantOf := func(id string) string {
		var tenant sql.NullString
		require.NoError(t, db.QueryRow(`SELECT tenant_id FROM audit_logs WHERE id = ?`, id).Scan(&tenant))
		require.True(t, tenant.Valid, "audit row %s must have tenant_id after backfill", id)
		return tenant.String
	}
	require.Equal(t, "t-a", tenantOf("log-actor"), "actor row follows users.tenant_id")
	require.Equal(t, "1", tenantOf("log-tenant"), "tenants-table row stays platform-scoped")
	require.Equal(t, "t-b", tenantOf("log-sys"), "actor-less trip row follows trips.tenant_id")
	require.Equal(t, "1", tenantOf("log-orphan"), "orphan row falls back to platform scope")

	var idx int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_audit_logs_tenant'`).Scan(&idx))
	require.Equal(t, 1, idx, "tenant index must exist at v167")

	// FK guard: unknown tenant aborts, NULL tenant passes (platform rows).
	_, err = db.Exec(`INSERT INTO audit_logs (id, action, table_name, tenant_id)
		VALUES ('log-bad', 'x', 'users', 'nope')`)
	require.Error(t, err, "unknown tenant must abort")

	require.NoError(t, goose.DownTo(db, "migrations", 166))
	var colCount int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('audit_logs') WHERE name='tenant_id'`).Scan(&colCount))
	require.Equal(t, 0, colCount, "rollback must drop audit_logs.tenant_id")
	require.NoError(t, goose.UpTo(db, "migrations", 167))
}
