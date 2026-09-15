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

// TestMigration00157_DispatcherWorkflow_UpAndDown proves 00157 (dispatcher
// workflow: planner_runs -> planned_routes -> planned_stops + dispatch_exceptions)
// applies and rolls back cleanly, and enforces the tenant-scope FK trigger rule
// (required for all migrations >= 00103). Prove-It protocol #4.
func TestMigration00157_DispatcherWorkflow_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00157_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.UpTo(db, "migrations", 157))

	// All four tables exist and are empty.
	for _, tbl := range []string{"planner_runs", "planned_routes", "planned_stops", "dispatch_exceptions"} {
		var count int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+tbl).Scan(&count))
		assert.Equal(t, 0, count, "%s should be empty after up", tbl)
	}

	// Tenant trigger on planner_runs rejects unknown tenant.
	_, err = db.Exec(`INSERT INTO planner_runs (id, tenant_id, created_by)
		VALUES ('pr-bad', 'non-existent-tenant-999', 'u1')`)
	require.Error(t, err, "expected foreign key rejection from tenant trigger on planner_runs")

	_, err = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-d1', 'Dispatch Co', 'dispatch-co')`)
	require.NoError(t, err)

	// Valid planner run accepts (kind/status defaults + valid tenant).
	_, err = db.Exec(`INSERT INTO planner_runs (id, tenant_id, created_by)
		VALUES ('pr-1', 't-d1', 'u1')`)
	require.NoError(t, err)

	// tenant_id is enforce-guarded downstream tables too (supply required NOT NULL
	// columns so the ONLY failure mode is the tenant trigger, not NOT NULL).
	cases := map[string]string{
		"planned_routes":      "(id, tenant_id, seq) VALUES ('x-bad', 'no-such-tenant', 1)",
		"planned_stops":       "(id, tenant_id, seq, source_type, address, lat, lng) VALUES ('x-bad', 'no-such-tenant', 1, 'pickup', 'A', 0, 0)",
		"dispatch_exceptions": "(id, tenant_id, kind) VALUES ('x-bad', 'no-such-tenant', 'late')",
	}
	for tbl, insert := range cases {
		_, err = db.Exec("INSERT INTO " + tbl + " " + insert)
		require.Error(t, err, "expected tenant trigger rejection on %s", tbl)
	}

	// A full valid cascade: run -> route -> stop.
	_, err = db.Exec(`INSERT INTO planned_routes (id, tenant_id, run_id, seq)
		VALUES ('rte-1', 't-d1', 'pr-1', 1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO planned_stops (id, tenant_id, route_id, seq, source_type, address, lat, lng)
		VALUES ('st-1', 't-d1', 'rte-1', 1, 'pickup', 'Addr', 12.9, 77.5)`)
	require.NoError(t, err)

	// CHECK backstop on status.
	_, err = db.Exec(`INSERT INTO planned_routes (id, tenant_id, run_id, seq, status)
		VALUES ('rte-bad', 't-d1', 'pr-1', 2, 'bogus')`)
	require.Error(t, err, "expected CHECK rejection on bad planned_routes.status")

	// Rollback drops all tables and triggers.
	require.NoError(t, goose.Down(db, "migrations"))
	for _, tbl := range []string{"planner_runs", "planned_routes", "planned_stops", "dispatch_exceptions"} {
		_, err = db.Exec("SELECT COUNT(*) FROM " + tbl)
		require.Error(t, err, "expected %s gone after rollback", tbl)
	}
}
