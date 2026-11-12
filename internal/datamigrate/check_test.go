package datamigrate

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newOrphanTestDB(t *testing.T, withOrphans bool) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("orphan_%s_%d", t.Name(), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	stmts := []string{
		`CREATE TABLE tenants (id TEXT PRIMARY KEY)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)`,
		`CREATE TABLE user_roles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)`,
		`CREATE TABLE workspaces (id TEXT PRIMARY KEY)`,
		`INSERT INTO tenants (id) VALUES ('1')`,
		`INSERT INTO users (id, tenant_id) VALUES ('u-ok', '1')`,
		`INSERT INTO sessions (id, user_id) VALUES ('s-ok', 'u-ok')`,
		`INSERT INTO user_roles (id, user_id) VALUES ('ur-ok', 'u-ok')`,
	}
	if withOrphans {
		stmts = append(stmts,
			`INSERT INTO users (id, tenant_id) VALUES ('u-orphan', 'ws_deadbeef')`,
			`INSERT INTO sessions (id, user_id) VALUES ('s-orphan', 'u-orphan')`,
			`INSERT INTO user_roles (id, user_id) VALUES ('ur-orphan', 'u-orphan')`,
			`INSERT INTO workspaces (id) VALUES ('w1'), ('w2')`,
		)
	}
	for _, s := range stmts {
		_, err := db.Exec(s)
		require.NoError(t, err, s)
	}
	return db
}

func TestDetectTenantOrphans(t *testing.T) {
	ctx := context.Background()

	clean := detectTenantOrphans(ctx, newOrphanTestDB(t, false))
	assert.False(t, clean.Blocking(), "clean DB must not block")
	assert.Zero(t, clean.Users)
	assert.Zero(t, clean.Sessions)
	assert.Zero(t, clean.UserRoles)

	dirty := detectTenantOrphans(ctx, newOrphanTestDB(t, true))
	assert.True(t, dirty.Blocking(), "orphan users must block the cutover gate")
	assert.Equal(t, 1, dirty.Users, "users.tenant_id not in tenants")
	assert.Equal(t, 1, dirty.Sessions, "sessions cascade from orphan users")
	assert.Equal(t, 1, dirty.UserRoles, "user_roles cascade from orphan users")
}

func TestLegacyRows(t *testing.T) {
	db := newOrphanTestDB(t, true)
	rows := legacyRows(context.Background(), db, []string{"workspaces", "does_not_exist"})
	require.Len(t, rows, 1, "only existing tables with rows are reported")
	assert.Equal(t, "workspaces", rows[0].Table)
	assert.Equal(t, 2, rows[0].Rows)
}

func TestPrecheckReport_TenantOrphanSection(t *testing.T) {
	p := &Precheck{
		SQLiteVersion: 182,
		PGVersion:     182,
		VersionsMatch: true,
		TenantOrphans: TenantOrphans{Users: 8, Sessions: 8, UserRoles: 8},
		LegacyRows:    []LegacyTable{{Table: "workspaces", Rows: 8}},
	}
	report := p.Report()
	assert.Contains(t, report, "tenant orphans (trigger-enforced, invisible to FK check): users=8 sessions=8 user_roles=8")
	assert.Contains(t, report, "live copy WILL quarantine")
	assert.Contains(t, report, "sqlite-only tables with rows (will be quarantined): 1")
	assert.Contains(t, report, "workspaces: 8 rows")
}
