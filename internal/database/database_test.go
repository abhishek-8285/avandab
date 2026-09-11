package database_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"transport-app/internal/database"

	_ "modernc.org/sqlite"
)

type testSettings struct {
	driver   string
	url      string
	maxOpen  int
	maxIdle  int
	lifetime time.Duration
}

func (t *testSettings) GetDriver() string                 { return t.driver }
func (t *testSettings) GetURL() string                    { return t.url }
func (t *testSettings) GetMaxOpenConns() int              { return t.maxOpen }
func (t *testSettings) GetMaxIdleConns() int              { return t.maxIdle }
func (t *testSettings) GetConnMaxLifetime() time.Duration { return t.lifetime }

func TestOpen_SQLite(t *testing.T) {
	s := &testSettings{
		driver:  "sqlite",
		url:     "file:" + t.TempDir() + "/test.db?mode=rwc",
		maxIdle: -1,
	}
	db, err := database.Open(context.Background(), s, slog.Default())
	if err != nil {
		t.Fatalf("Open(sqlite) = %v, want nil", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping = %v, want nil", err)
	}
	// Verify a pragma took effect.
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpen_UnsupportedDriver(t *testing.T) {
	s := &testSettings{driver: "oracle", url: "x"}
	if _, err := database.Open(context.Background(), s, nil); err == nil {
		t.Fatal("Open(oracle) = nil error, want error")
	}
}

func TestOpen_EmptyDSN(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		s := &testSettings{driver: driver}
		if _, err := database.Open(context.Background(), s, nil); err == nil {
			t.Errorf("Open(%s) with empty DSN = nil error, want error", driver)
		}
	}
}

func TestGooseDialect(t *testing.T) {
	if got := database.GooseDialect("postgres"); got != database.GooseDialect("postgresql") {
		t.Error("postgres and postgresql should map to the same dialect")
	}
	if got := database.GooseDialect(""); got != database.GooseDialect("sqlite") {
		t.Error("empty driver should default to sqlite dialect")
	}
}

func TestResyncIdentitySequences_NonPostgresNoop(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := database.ResyncIdentitySequences(context.Background(), db); err != nil {
		t.Errorf("resync on sqlite = %v, want nil no-op", err)
	}
}
