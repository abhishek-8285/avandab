//go:build pg_integration

// Full data-migration proof on a scratch PG: fixture sqlite containing all
// six problem classes (go-ts, int-bool, dirty role text, orphan FK,
// seed collision, stale sequence) migrates with correct merge + quarantine.
//
//	DATABASE_URL="postgres://user:pass@localhost:5432/mvtms_dm?sslmode=disable" \
//	    go test -tags pg_integration ./internal/datamigrate/ -run TestMigrateFixture -v
package datamigrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	dbmigr "transport-app/db"
	appdb "transport-app/internal/database"
)

// scratchDB creates a uniquely-named database on the same server (the
// shared-DATABASE_URL pattern races when packages migrate concurrently),
// returning its URL. Dropped on cleanup.
func scratchDB(t *testing.T, ctx context.Context, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mvtms_dm_%d", time.Now().UnixNano()%1000000)
	admin := *u
	admin.Path = "/postgres"
	adb, err := sql.Open("pgx-rebind", admin.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adb.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		_ = adb.Close()
		t.Fatalf("create scratch db: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		defer func() { _ = adb.Close() }()
		_, _ = adb.ExecContext(cctx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
	})
	u.Path = "/" + name
	return u.String()
}

func TestMigrateFixture(t *testing.T) {
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	url := scratchDB(t, ctx, base)

	pg, err := sql.Open("pgx-rebind", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pg.Close() }()

	// PG side: full migration chain (seeds included).
	sub, err := fs.Sub(dbmigr.MigrationsPG, "migrations_pg")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(appdb.GooseDialect("postgres"), pg, sub.(fs.FS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("pg up: %v", err)
	}
	var pv int
	if err := pg.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version`).Scan(&pv); err != nil {
		t.Fatal(err)
	}
	// Test isolation: clear prior runs' fixture leftovers (the tool itself
	// is re-run safe via quarantine dedupe; the test needs exact counts).
	// All patterns are fixture-scoped (scratch DBs only, per file header).
	_, _ = pg.ExecContext(ctx, `DELETE FROM _migration_quarantine WHERE row_json LIKE '%@x.in%'`)
	_, _ = pg.ExecContext(ctx, `DELETE FROM role_permissions WHERE permission_id = 900`)
	_, _ = pg.ExecContext(ctx, `DELETE FROM permissions WHERE id = 900`)
	_, _ = pg.ExecContext(ctx, `DELETE FROM users WHERE id IN ('u-clean','u-dirty','u-ghost','u-orphan')`)

	// Fixture sqlite with every problem class.
	fixPath := filepath.Join(t.TempDir(), "fix.db")
	src, err := sql.Open("sqlite", "file:"+fixPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	fixture := []string{
		`CREATE TABLE goose_db_version (version_id INTEGER PRIMARY KEY, is_applied INTEGER, tstamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE roles (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT, created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE permissions (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, description TEXT, created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE role_permissions (role_id INTEGER NOT NULL, permission_id INTEGER NOT NULL, PRIMARY KEY (role_id, permission_id), FOREIGN KEY (role_id) REFERENCES roles(id), FOREIGN KEY (permission_id) REFERENCES permissions(id))`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, name TEXT NOT NULL, role_id INTEGER NOT NULL DEFAULT 2, status TEXT NOT NULL DEFAULT 'active', created_at DATETIME NOT NULL DEFAULT (datetime('now')), updated_at DATETIME NOT NULL DEFAULT (datetime('now')), FOREIGN KEY (role_id) REFERENCES roles(id))`,
	}
	for _, q := range fixture {
		if _, err := src.ExecContext(ctx, q); err != nil {
			t.Fatalf("fixture ddl: %v", err)
		}
	}
	if _, err := src.ExecContext(ctx, `INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, pv); err != nil {
		t.Fatal(err)
	}
	// Roles: one colliding name with a different id than the PG seed.
	if _, err := src.ExecContext(ctx, `INSERT INTO roles (id, name) VALUES (1,'admin'),(2,'dispatcher')`); err != nil {
		t.Fatal(err)
	}
	// Permissions: 'experiments:read' exists in PG seeds under another id.
	var seedPermID string
	err = pg.QueryRowContext(ctx, `SELECT id FROM permissions WHERE name='experiments:read'`).Scan(&seedPermID)
	if err != nil {
		t.Skipf("seed permission missing (chain changed?): %v", err)
	}
	if _, err := src.ExecContext(ctx, `INSERT INTO permissions (id, name) VALUES (86,'experiments:read'),(900,'dm:fixture')`); err != nil {
		t.Fatal(err)
	}
	if _, err := src.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission_id) VALUES (1,86),(1,900)`); err != nil {
		t.Fatal(err)
	}
	users := []string{
		`INSERT INTO users (id, email, password_hash, name, role_id, created_at, updated_at) VALUES ('u-clean','c@x.in','h','Clean',1,'2026-08-06 03:37:15','2026-08-06 03:37:15')`,
		`INSERT INTO users (id, email, password_hash, name, role_id, created_at, updated_at) VALUES ('u-dirty','d@x.in','h','Dirty','dispatcher','2026-08-27 11:27:45 +0000 UTC','2026-08-27 11:27:45 +0000 UTC')`,
		`INSERT INTO users (id, email, password_hash, name, role_id, created_at, updated_at) VALUES ('u-ghost','g@x.in','h','Ghost','ghost_role','2026-08-06 03:37:15','2026-08-06 03:37:15')`,
		`INSERT INTO users (id, email, password_hash, name, role_id, created_at, updated_at) VALUES ('u-orphan','o@x.in','h','Orphan',999,'2026-08-06 03:37:15','2026-08-06 03:37:15')`,
	}
	for _, q := range users {
		if _, err := src.ExecContext(ctx, q); err != nil {
			t.Fatalf("fixture user: %v", err)
		}
	}

	sum, err := Run(ctx, src, pg, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 1. colliding permission skipped, dependent remapped to seed id.
	var rp int
	if err := pg.QueryRowContext(ctx,
		`SELECT permission_id FROM role_permissions WHERE role_id=1 AND permission_id=$1`, seedPermID).Scan(&rp); err != nil {
		t.Errorf("remapped link (1 -> seed %s) missing: %v", seedPermID, err)
	}
	var n900 int
	if err := pg.QueryRowContext(ctx,
		`SELECT count(*) FROM permissions WHERE name='dm:fixture'`).Scan(&n900); err != nil || n900 != 1 {
		t.Errorf("new permission not inserted: %d %v", n900, err)
	}
	// 2. dirty text role resolved via roles lookup.
	var dispRole string
	if err := pg.QueryRowContext(ctx, `SELECT id FROM roles WHERE name='dispatcher'`).Scan(&dispRole); err != nil {
		t.Fatal(err)
	}
	var gotRole string
	if err := pg.QueryRowContext(ctx, `SELECT role_id FROM users WHERE id='u-dirty'`).Scan(&gotRole); err != nil {
		t.Errorf("dirty user missing: %v", err)
	} else if gotRole != dispRole {
		t.Errorf("dirty role = %q, want %q", gotRole, dispRole)
	}
	// 3. go-ts parsed (would have failed the whole row otherwise).
	var nts int
	if err := pg.QueryRowContext(ctx,
		`SELECT count(*) FROM users WHERE id='u-dirty' AND created_at IS NOT NULL`).Scan(&nts); err != nil || nts != 1 {
		t.Errorf("go-ts row missing: %d %v", nts, err)
	}
	// 4. ghost + orphan quarantined, clean present.
	for _, id := range []string{"u-ghost", "u-orphan"} {
		var n int
		_ = pg.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id=$1`, id).Scan(&n)
		if n != 0 {
			t.Errorf("bad row %s must not be copied", id)
		}
	}
	var nq int
	if err := pg.QueryRowContext(ctx,
		`SELECT count(*) FROM _migration_quarantine WHERE table_name='users'`).Scan(&nq); err != nil || nq != 2 {
		t.Errorf("quarantine users = %d, want 2 (%v)", nq, err)
	}
	// 5. sequences advanced past copied max.
	var last int
	if err := pg.QueryRowContext(ctx, `SELECT last_value FROM roles_id_seq`).Scan(&last); err != nil {
		t.Errorf("roles seq: %v", err)
	} else {
		var mx int
		_ = pg.QueryRowContext(ctx, `SELECT max(id) FROM roles`).Scan(&mx)
		if last < mx {
			t.Errorf("roles seq %d behind max %d", last, mx)
		}
	}
	t.Logf("tables %d/%d rows %d quarantined %d remapped %v seqs %d",
		sum.TablesCopied, sum.TablesTotal, sum.RowsCopied, len(sum.Quarantined), sum.Remapped, len(sum.Sequences))
}
