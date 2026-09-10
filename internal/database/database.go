// Package database centralizes DB engine selection behind config so moving
// from SQLite to Postgres/MySQL is an env-only change (DATABASE_DRIVER +
// DATABASE_URL), never a code change. All SQL drivers are registered here at
// import time; callers only ever see *sql.DB.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers "mysql" driver
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers "sqlite" driver
)

func init() {
	// "pgx-rebind" speaks postgres but rewrites ?/?N to $N first, so the
	// entire sqlite-dialect query corpus (sqlc output + hand SQL) runs
	// unchanged. See rebind_driver.go.
	sql.Register("pgx-rebind", WrapRebind(stdlib.GetDefaultDriver()))
}

// Driver names accepted by DATABASE_DRIVER.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
)

// sqlDrivers maps config driver names to database/sql registration names.
var sqlDrivers = map[string]string{
	DriverSQLite:   "sqlite",
	DriverPostgres: "pgx-rebind",
	DriverMySQL:    "mysql",
}

// drivers remembers which engine each opened handle speaks, so query code
// can branch on dialect for the handful of expressions with no portable
// spelling (datetime arithmetic, bucket truncation). Unknown handles
// (tests, sidecars) report sqlite semantics.
var drivers sync.Map // map[*sql.DB]string

// IsPostgres reports whether db was opened for the postgres driver.
func IsPostgres(db *sql.DB) bool {
	if d, ok := drivers.Load(db); ok {
		return d == DriverPostgres
	}
	return false
}

// sqlitePragmas mirror the tuned startup PRAGMAs previously inline in
// cmd/server/main.go. Only executed when the driver is sqlite — harmless
// on other engines because they are never sent there.
var sqlitePragmas = []string{
	"PRAGMA journal_mode=WAL;",
	"PRAGMA synchronous=NORMAL;",
	"PRAGMA busy_timeout=10000;",
	"PRAGMA cache_size=-32768;",   // 32MB page cache (tuned for 1GB VPS)
	"PRAGMA mmap_size=134217728;", // 128MB memory-mapped file I/O (tuned for 1GB VPS)
	"PRAGMA locking_mode=NORMAL;",
	"PRAGMA foreign_keys=ON;",
	"PRAGMA temp_store=MEMORY;",
}

// Settings is the minimal view of config.DatabaseConfig this package needs.
// Accepting an interface keeps internal/database free of a config import and
// makes it trivial to unit test with plain structs.
type Settings interface {
	GetDriver() string
	GetURL() string
	GetMaxOpenConns() int              // <=0 means engine default
	GetMaxIdleConns() int              // <0 means Go default
	GetConnMaxLifetime() time.Duration // 0 means reuse forever
}

// Open connects using cfg, applies pool sizing, runs SQLite PRAGMAs when the
// engine is sqlite, then verifies connectivity with a ping. The returned
// *sql.DB is engine-agnostic; nothing downstream knows which driver is live.
func Open(ctx context.Context, cfg Settings, logger *slog.Logger) (*sql.DB, error) {
	driver := normalizeDriver(cfg.GetDriver())
	regName, ok := sqlDrivers[driver]
	if !ok {
		return nil, fmt.Errorf("database: unsupported driver %q (use sqlite, postgres or mysql)", cfg.GetDriver())
	}
	if cfg.GetURL() == "" {
		return nil, fmt.Errorf("database: empty DSN for driver %q", driver)
	}

	db, err := sql.Open(regName, cfg.GetURL())
	if err != nil {
		return nil, fmt.Errorf("database: open %s: %w", driver, err)
	}
	drivers.Store(db, driver)

	// Pool sizing: sqlite defaults tuned for single-writer WAL;
	// network engines fall back to database/sql defaults unless set.
	if driver == DriverSQLite {
		maxOpen := cfg.GetMaxOpenConns()
		if maxOpen <= 0 {
			maxOpen = 16
		}
		maxIdle := cfg.GetMaxIdleConns()
		if maxIdle < 0 {
			maxIdle = 8
		}
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxIdle)
	} else {
		if maxOpen := cfg.GetMaxOpenConns(); maxOpen > 0 {
			db.SetMaxOpenConns(maxOpen)
		}
		if maxIdle := cfg.GetMaxIdleConns(); maxIdle >= 0 {
			db.SetMaxIdleConns(maxIdle)
		}
	}
	if lt := cfg.GetConnMaxLifetime(); lt > 0 {
		db.SetConnMaxLifetime(lt)
	}

	if driver == DriverSQLite {
		if err := ApplySQLitePragmas(ctx, db, logger); err != nil {
			return nil, err
		}
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("database: ping %s: %w", driver, err)
	}

	if logger != nil {
		logger.Info("Database connected", "driver", driver)
	}
	return db, nil
}

// ApplySQLitePragmas (re-)establishes the tuned SQLite posture on one pooled
// handle. Called at connect AND after migrations: NO TRANSACTION migrations
// (e.g. 00126's parent-table rebuild, which must run with foreign_keys OFF)
// mutate the pragma state of the handle they run on, so the posture is
// re-asserted post-migration rather than trusted across it.
func ApplySQLitePragmas(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	for _, p := range sqlitePragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			if logger != nil {
				logger.Warn("Failed to execute pragma", "pragma", p, "error", err)
			}
		}
	}
	return nil
}

// GooseDialect maps a config driver name to the goose migration dialect.
func GooseDialect(driver string) goose.Dialect {
	switch normalizeDriver(driver) {
	case DriverPostgres:
		return goose.DialectPostgres
	case DriverMySQL:
		return goose.DialectMySQL
	default:
		return goose.DialectSQLite3
	}
}

// MigrationDir returns the embedded migrations subdirectory for a driver.
// sqlite runs the original chain ("migrations", frozen); postgres runs the
// version-locked PG port ("migrations_pg"). Same version numbers both sides.
func MigrationDir(driver string) string {
	if normalizeDriver(driver) == DriverPostgres {
		return "migrations_pg"
	}
	return "migrations"
}

func normalizeDriver(d string) string {
	switch d {
	case "", "sqlite3":
		return DriverSQLite
	case "postgresql":
		return DriverPostgres
	default:
		return d
	}
}
