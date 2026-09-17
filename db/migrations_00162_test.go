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

func TestMigration00162_DropDeadTables_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00162_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 161))

	// All eight tables exist pre-drop (proves the test targets real objects).
	for _, tbl := range []string{"i18n_keys", "notifications_preferences",
		"revoked_refresh_tokens", "provider_poll_state", "route_constraints",
		"offline_sync_log", "audit_events", "telemetry_events"} {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		require.Equal(t, 1, n, "table %s must exist at v161", tbl)
	}

	require.NoError(t, goose.UpTo(db, "migrations", 162))

	for _, tbl := range []string{"i18n_keys", "notifications_preferences",
		"revoked_refresh_tokens", "provider_poll_state", "route_constraints",
		"offline_sync_log", "audit_events", "telemetry_events"} {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		require.Equal(t, 0, n, "table %s must be gone at v162", tbl)
	}

	// alert_sources is deliberately KEPT (live alert_rules FK parent).
	var kept int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='alert_sources'`).Scan(&kept))
	require.Equal(t, 1, kept, "alert_sources must survive 00162")

	// Live neighbors untouched.
	var rules int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM alert_rules`).Scan(&rules))
	require.GreaterOrEqual(t, rules, 1, "seeded alert_rules must survive")

	// Down recreates empty shells (rollback path works structurally).
	require.NoError(t, goose.DownTo(db, "migrations", 161))
	for _, tbl := range []string{"audit_events", "telemetry_events", "offline_sync_log"} {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n))
		require.Equal(t, 1, n, "table %s shell must come back on rollback", tbl)
		var rows int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+tbl).Scan(&rows))
		require.Equal(t, 0, rows, "table %s shell must be empty", tbl)
	}
	require.NoError(t, goose.UpTo(db, "migrations", 162))
	var gone int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='audit_events'`).Scan(&gone))
	require.Equal(t, 0, gone, "audit_events must be gone again after re-up")
}
