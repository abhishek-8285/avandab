//go:build pg_integration

// Postgres engine integration test — verifies the full migration set applies
// cleanly on a real PostgreSQL server via the appdb.Open factory + goose
// provider, mirroring cmd/server startup. Not part of default `go test ./...`:
//
//	DATABASE_URL="postgres://user:pass@localhost:5432/mvtms_it?sslmode=disable" \
//	    go test -tags pg_integration ./internal/database/ -run TestPostgresMigrations -v
//
// The database must exist; migrations create all tables. The test drops and
// recreates nothing — use a disposable scratch DB.
package database_test

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	dbmigr "transport-app/db"
	appdb "transport-app/internal/database"
)

// TestMain migrates the scratch PG once before any gate runs. File order
// does not guarantee TestPostgresMigrations runs first, so every gate
// test would otherwise race an empty DB on fresh databases.
func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s := &testSettings{driver: "postgres", url: url, maxOpen: 4, maxIdle: 2}
		db, err := appdb.Open(ctx, s, slog.Default())
		if err == nil {
			defer func() { _ = db.Close() }()
			if migrations, ferr := fsSub(); ferr == nil {
				if provider, perr := goose.NewProvider(appdb.GooseDialect("postgres"), db, migrations); perr == nil {
					// Mirror cmd/server startup (A7): two-phase migrate so the
					// identity-resync repair runs before 00137 on fresh DBs.
					_, _ = provider.UpTo(ctx, appdb.PreGooseCutVersion)
					_ = appdb.ResyncIdentitySequences(ctx, db)
					_, _ = provider.Up(ctx)
				}
			}
		}
	}
	os.Exit(m.Run())
}

func TestPostgresMigrations(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}

	s := &testSettings{
		driver:  "postgres",
		url:     url,
		maxOpen: 4,
		maxIdle: 2,
	}
	db, err := appdb.Open(context.Background(), s, slog.Default())
	if err != nil {
		t.Fatalf("Open(postgres) = %v, want nil", err)
	}
	defer func() { _ = db.Close() }()

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		t.Fatalf("Ping = %v, want nil (is the server reachable?)", err)
	}

	migrations, err := fsSub()
	if err != nil {
		t.Fatalf("embed fs: %v", err)
	}
	provider, err := goose.NewProvider(appdb.GooseDialect("postgres"), db, migrations)
	if err != nil {
		t.Fatalf("goose.NewProvider = %v", err)
	}
	// Two-phase migrate mirroring cmd/server startup (A7): UpTo the cut,
	// resync identity sequences behind explicit-id seeds, then full Up.
	// A single provider.Up fails on fresh chains at 00137 (roles_pkey).
	if _, err := provider.UpTo(pingCtx, int64(appdb.PreGooseCutVersion)); err != nil {
		t.Fatalf("migrations up-to %d on postgres = %v", appdb.PreGooseCutVersion, err)
	}
	if err := appdb.ResyncIdentitySequences(pingCtx, db); err != nil {
		t.Fatalf("resync identity sequences = %v", err)
	}
	if _, err := provider.Up(pingCtx); err != nil {
		t.Fatalf("migrations up on postgres = %v", err)
	}

	// Spot-check core tables exist.
	for _, tbl := range []string{"users", "drivers", "vehicles", "bookings", "trips", "files", "worker_leases"} {
		var n int
		if err := db.QueryRowContext(pingCtx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name = $1`, tbl).Scan(&n); err != nil {
			t.Fatalf("table check %s: %v", tbl, err)
		}
		if n != 1 {
			t.Errorf("table %s missing after migrations", tbl)
		}
	}

	var version int
	if err := db.QueryRowContext(pingCtx,
		`SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil {
		t.Fatalf("goose version check: %v", err)
	}
	t.Logf("postgres migrated to version %d", version)
	// Expected version derives from the migration files themselves, never a
	// hardcoded literal — hardcoding rotted this gate on every migration
	// (want-128 vs actual-134 after 00129–00134 landed).
	want := maxMigrationVersion(t, migrations)
	if version != want {
		t.Errorf("postgres version = %d, want %d (full chain)", version, want)
	}
}

func fsSub() (fs.FS, error) {
	return fs.Sub(dbmigr.MigrationsPG, appdb.MigrationDir("postgres"))
}

// maxMigrationVersion returns the highest numeric prefix among the embedded
// .sql migration files (e.g. 00134_... → 134). Numbering has gaps from
// historical renumbers, so file count is not a substitute.
func maxMigrationVersion(t *testing.T, migFS fs.FS) int {
	t.Helper()
	entries, err := fs.ReadDir(migFS, ".")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	max := 0
	for _, e := range entries {
		name := e.Name()
		if len(name) < 5 || name[len(name)-4:] != ".sql" {
			continue
		}
		var v int
		if v, err = strconv.Atoi(name[:5]); err == nil && v > max {
			max = v
		}
	}
	if max == 0 {
		t.Fatal("no versioned migrations found")
	}
	return max
}

// TestResyncIdentitySequences proves the A7 repair: an explicit-id insert
// desyncs roles_id_seq (the 00027/00064 failure mode); after Resync, the
// next generated id is back above MAX(id) instead of colliding with it.
func TestResyncIdentitySequences(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping postgres integration test")
	}
	ctx := context.Background()
	db, err := appdb.Open(ctx, &testSettings{driver: "postgres", url: url, maxOpen: 4, maxIdle: 2}, slog.Default())
	if err != nil {
		t.Fatalf("Open(postgres) = %v, want nil", err)
	}
	defer func() { _ = db.Close() }()

	var baseMax int64
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM roles`).Scan(&baseMax); err != nil {
		t.Fatalf("roles max: %v", err)
	}
	probe := baseMax + 500
	if _, err := db.ExecContext(ctx, `INSERT INTO roles (id, name, description) VALUES ($1, 'resync-probe', 'a7 test') ON CONFLICT (id) DO NOTHING`, probe); err != nil {
		t.Fatalf("probe insert: %v", err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM roles WHERE name = 'resync-probe'`) }()
	if err := appdb.ResyncIdentitySequences(ctx, db); err != nil {
		t.Fatalf("resync = %v, want nil", err)
	}
	var last int64
	if err := db.QueryRowContext(ctx, `SELECT last_value FROM roles_id_seq`).Scan(&last); err != nil {
		t.Fatalf("seq read: %v", err)
	}
	if last != probe {
		t.Errorf("roles_id_seq = %d, want %d (MAX after probe insert)", last, probe)
	}
}
