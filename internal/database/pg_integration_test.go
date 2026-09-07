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
	if version != 128 {
		t.Errorf("postgres version = %d, want 128 (full chain)", version)
	}
}

func fsSub() (fs.FS, error) {
	return fs.Sub(dbmigr.MigrationsPG, appdb.MigrationDir("postgres"))
}
