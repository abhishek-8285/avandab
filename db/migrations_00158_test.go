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

func TestMigration00158_EngineStateTenantCleanup_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00158_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	// Seed the orphan under the pre-strict regime: migration 00105 makes the
	// engine_state tenant trigger fail-closed (empty string rejected), so a
	// legacy '' row can only be created at version 104, where the trigger
	// still carries the `!= ''` bypass. Triggers fire on write, never
	// retroactively, so the orphan survives the 105..157 upgrades and 00158
	// is what removes it.
	require.NoError(t, goose.UpTo(db, "migrations", 104))
	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-state', 'State Co', 'state-co')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO engine_state (vehicle_id, tenant_id, state, updated_at)
		VALUES ('v1', 't-state', 'outside', CURRENT_TIMESTAMP),
		       ('v2', '', 'outside', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	require.NoError(t, goose.UpTo(db, "migrations", 157))
	// Strict triggers (00105) must not purge pre-existing rows on upgrade.
	var preCount int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM engine_state WHERE vehicle_id IN ('v1','v2')").Scan(&preCount))
	require.Equal(t, 2, preCount)

	require.NoError(t, goose.UpTo(db, "migrations", 158))
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM engine_state WHERE vehicle_id = 'v1'").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM engine_state WHERE vehicle_id = 'v2'").Scan(&count))
	require.Equal(t, 0, count)

	// Down is intentionally a no-op for this data-cleanup migration.
	require.NoError(t, goose.DownTo(db, "migrations", 157))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM engine_state WHERE vehicle_id = 'v1'").Scan(&count))
	require.Equal(t, 1, count)
}
